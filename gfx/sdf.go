package gfx

import (
	"image"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/matjam/bunyip/lin"
)

// SDF fonts store each glyph as a signed distance field rasterised once
// at sdfEmPixels, so text draws sharp at any size or rotation.
const (
	sdfEmPixels   = 48 // atlas size of one em
	sdfSpread     = 8  // atlas pixels of distance encoded either side of the edge
	sdfOversample = 4  // the mask is rasterised this many times larger for sub-pixel edges
)

// NewSDFFont prepares a scalable font. Size is a nominal em size in view
// units used by DrawText; DrawTextSized draws at any size.
func (g *Graphics) NewSDFFont(ttf []byte, size float32, opts FontOptions) (*Font, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: sdfEmPixels * sdfOversample, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, err
	}
	side := opts.AtlasSize
	if side <= 0 {
		side = 1024
	}
	metrics := face.Metrics()
	// scale is face pixels per view unit at the nominal size; the atlas is
	// sdfOversample times coarser than the face.
	scale := float32(sdfEmPixels*sdfOversample) / size
	f := &Font{
		Size:       size,
		LineHeight: fixedToFloat(metrics.Height) / scale,
		Ascent:     fixedToFloat(metrics.Ascent) / scale,
		glyphs:     map[rune]glyph{},
		face:       face,
		scale:      scale,
		sdf:        true,
		packer:     shelfPacker{width: side, height: side, pad: 1},
		pix:        image.NewRGBA(image.Rect(0, 0, side, side)),
		g:          g,
	}
	for r := rune(32); r < 127; r++ {
		f.add(r)
	}
	for _, r := range opts.Preload {
		f.add(r)
	}
	for _, rg := range opts.Ranges {
		for r := rg[0]; r <= rg[1]; r++ {
			f.add(r)
		}
	}
	if err := f.flush(); err != nil {
		return nil, err
	}
	return f, nil
}

// addSDF rasterises a glyph mask at sdfOversample times the atlas size,
// converts it to distances with a linear-time transform and downsamples,
// so the stored edge position is accurate to a fraction of an atlas pixel.
func (f *Font) addSDF(r rune) {
	bounds, advance, ok := f.face.GlyphBounds(r)
	if !ok {
		bounds, advance, _ = f.face.GlyphBounds(0xFFFD)
	}
	const os = sdfOversample
	gl := glyph{advance: fixedToFloat(advance) / f.scale}
	gw := (bounds.Max.X - bounds.Min.X).Ceil()
	gh := (bounds.Max.Y - bounds.Min.Y).Ceil()
	if gw <= 0 || gh <= 0 {
		gl.empty = true
		f.glyphs[r] = gl
		return
	}
	// Atlas cell, then the oversampled mask that fills it exactly.
	w := (gw+os-1)/os + 2*sdfSpread
	h := (gh+os-1)/os + 2*sdfSpread
	x, y, ok := f.packer.place(w, h)
	if !ok {
		gl.empty = true
		f.glyphs[r] = gl
		return
	}
	mw, mh := w*os, h*os
	mask := image.NewAlpha(image.Rect(0, 0, mw, mh))
	dot := fixed.Point26_6{X: -bounds.Min.X + fixed.I(sdfSpread*os), Y: -bounds.Min.Y + fixed.I(sdfSpread*os)}
	dr, maskImg, maskPt, _, _ := f.face.Glyph(dot, r)
	if maskImg != nil {
		draw.Draw(mask, dr, maskImg, maskPt, draw.Src)
	}
	field := distanceField(mask, sdfSpread*os)
	for yy := range h {
		for xx := range w {
			var sum float64
			for sy := range os {
				for sx := range os {
					sum += field[(yy*os+sy)*mw+xx*os+sx]
				}
			}
			f.pix.SetRGBA(x+xx, y+yy, rgbaPremul(uint8(math.Round(sum/(os*os)*255))))
		}
	}
	side := float32(f.packer.width)
	gl.uv0 = lin.V2(float32(x)/side, float32(y)/side)
	gl.uv1 = lin.V2(float32(x+w)/side, float32(y+h)/side)
	gl.size = lin.V2(float32(w*os)/f.scale, float32(h*os)/f.scale)
	gl.bearing = lin.V2((fixedToFloat(bounds.Min.X)-sdfSpread*os)/f.scale, (fixedToFloat(bounds.Min.Y)-sdfSpread*os)/f.scale)
	f.glyphs[r] = gl
	f.dirty = true
}

// distanceField turns a coverage mask into signed distances in 0..1 with
// 0.5 on the edge, larger inside, saturating at spread pixels. It runs
// the 8SSEDT transform (Danielsson) once for the inside and once for the
// outside, which is linear in the number of pixels.
func distanceField(mask *image.Alpha, spread int) []float64 {
	w, h := mask.Rect.Dx(), mask.Rect.Dy()
	inside := make([]bool, w*h)
	for y := range h {
		for x := range w {
			inside[y*w+x] = mask.Pix[y*mask.Stride+x] >= 128
		}
	}
	dIn := edt(inside, w, h, false) // distance from inside pixels to the outside
	dOut := edt(inside, w, h, true) // distance from outside pixels to the inside
	out := make([]float64, w*h)
	for i := range out {
		var d float64
		if inside[i] {
			d = min(dIn[i], float64(spread)) / float64(spread)
			out[i] = 0.5 + d*0.5
		} else {
			d = min(dOut[i], float64(spread)) / float64(spread)
			out[i] = 0.5 - d*0.5
		}
	}
	return out
}

// edt computes, for every pixel where inside == !invert, the Euclidean
// distance to the nearest pixel of the other class, by 8SSEDT.
func edt(inside []bool, w, h int, invert bool) []float64 {
	type pt struct{ dx, dy int32 }
	const far = 1 << 20
	grid := make([]pt, w*h)
	for i, in := range inside {
		if in == invert {
			grid[i] = pt{0, 0} // this is the target class
		} else {
			grid[i] = pt{far, far}
		}
	}
	at := func(x, y int) pt {
		if x < 0 || y < 0 || x >= w || y >= h {
			return pt{far, far}
		}
		return grid[y*w+x]
	}
	dist2 := func(p pt) int64 { return int64(p.dx)*int64(p.dx) + int64(p.dy)*int64(p.dy) }
	compare := func(x, y int, cur *pt, ox, oy int) {
		o := at(x+ox, y+oy)
		if o.dx >= far {
			return
		}
		o.dx += int32(ox)
		o.dy += int32(oy)
		if dist2(o) < dist2(*cur) {
			*cur = o
		}
	}
	// Pass 1: top-left to bottom-right.
	for y := range h {
		for x := range w {
			p := &grid[y*w+x]
			compare(x, y, p, -1, 0)
			compare(x, y, p, 0, -1)
			compare(x, y, p, -1, -1)
			compare(x, y, p, 1, -1)
		}
		for x := w - 1; x >= 0; x-- {
			compare(x, y, &grid[y*w+x], 1, 0)
		}
	}
	// Pass 2: bottom-right to top-left.
	for y := h - 1; y >= 0; y-- {
		for x := w - 1; x >= 0; x-- {
			p := &grid[y*w+x]
			compare(x, y, p, 1, 0)
			compare(x, y, p, 0, 1)
			compare(x, y, p, -1, 1)
			compare(x, y, p, 1, 1)
		}
		for x := range w {
			compare(x, y, &grid[y*w+x], -1, 0)
		}
	}
	out := make([]float64, w*h)
	for i, p := range grid {
		out[i] = math.Sqrt(float64(dist2(p)))
	}
	return out
}

// DrawTextSized draws one line of an SDF font at size view units per em,
// rotated by angle radians about the text's top-left corner.
func (g *Graphics) DrawTextSized(f *Font, text string, x, y, size, angle float32, c Color) {
	k := size / f.Size
	sin, cos := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	pen := float32(0)
	base := f.Ascent * k
	var prev rune
	for _, r := range text {
		f.add(r)
		if prev != 0 {
			pen += fixedToFloat(f.face.Kern(prev, r)) / f.scale * k
		}
		gl := f.glyphs[r]
		if !gl.empty {
			lx, ly := pen+gl.bearing.X*k, base+gl.bearing.Y*k
			g.Draw(f.atlas, Sprite{
				Pos:  lin.V2(x+lx*cos-ly*sin, y+lx*sin+ly*cos),
				Size: gl.size.Mul(k),
				UV0:  gl.uv0, UV1: gl.uv1,
				Color:    c,
				Rotation: angle,
			})
		}
		pen += gl.advance * k
		prev = r
	}
	if f.dirty {
		_ = f.flush()
	}
}

// MeasureSized measures one line at the given em size.
func (f *Font) MeasureSized(text string, size float32) (w, h float32) {
	w, h = f.Measure(text)
	k := size / f.Size
	return w * k, h * k
}
