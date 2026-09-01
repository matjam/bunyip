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
	sdfEmPixels = 48 // rasterisation size of one em
	sdfSpread   = 8  // pixels of distance encoded either side of the edge
)

// NewSDFFont prepares a scalable font. Size is a nominal em size in view
// units used by DrawText; DrawTextSized draws at any size.
func (g *Graphics) NewSDFFont(ttf []byte, size float32, opts FontOptions) (*Font, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: sdfEmPixels, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, err
	}
	side := opts.AtlasSize
	if side <= 0 {
		side = 1024
	}
	metrics := face.Metrics()
	scale := float32(sdfEmPixels) / size // atlas pixels per view unit at the nominal size
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

// addSDF rasterises a glyph mask with padding and converts it to distances.
func (f *Font) addSDF(r rune) {
	bounds, advance, ok := f.face.GlyphBounds(r)
	if !ok {
		bounds, advance, _ = f.face.GlyphBounds(0xFFFD)
	}
	gl := glyph{advance: fixedToFloat(advance) / f.scale}
	gw := (bounds.Max.X - bounds.Min.X).Ceil()
	gh := (bounds.Max.Y - bounds.Min.Y).Ceil()
	if gw <= 0 || gh <= 0 {
		gl.empty = true
		f.glyphs[r] = gl
		return
	}
	w, h := gw+2*sdfSpread, gh+2*sdfSpread
	x, y, ok := f.packer.place(w, h)
	if !ok {
		gl.empty = true
		f.glyphs[r] = gl
		return
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	dot := fixed.Point26_6{X: -bounds.Min.X + fixed.I(sdfSpread), Y: -bounds.Min.Y + fixed.I(sdfSpread)}
	dr, maskImg, maskPt, _, _ := f.face.Glyph(dot, r)
	if maskImg != nil {
		draw.Draw(mask, dr, maskImg, maskPt, draw.Src)
	}
	field := distanceField(mask)
	for yy := range h {
		for xx := range w {
			v := field[yy*w+xx]
			f.pix.SetRGBA(x+xx, y+yy, rgbaPremul(v))
		}
	}
	side := float32(f.packer.width)
	gl.uv0 = lin.V2(float32(x)/side, float32(y)/side)
	gl.uv1 = lin.V2(float32(x+w)/side, float32(y+h)/side)
	gl.size = lin.V2(float32(w)/f.scale, float32(h)/f.scale)
	gl.bearing = lin.V2((fixedToFloat(bounds.Min.X)-sdfSpread)/f.scale, (fixedToFloat(bounds.Min.Y)-sdfSpread)/f.scale)
	f.glyphs[r] = gl
	f.dirty = true
}

// distanceField turns a coverage mask into 8-bit signed distances: 128 on
// the edge, larger inside, clamped at sdfSpread pixels either way. It is a
// direct search within the spread radius, which is fast enough for the
// glyph sizes involved and exact.
func distanceField(mask *image.Alpha) []uint8 {
	w, h := mask.Rect.Dx(), mask.Rect.Dy()
	inside := func(x, y int) bool {
		if x < 0 || y < 0 || x >= w || y >= h {
			return false
		}
		return mask.Pix[y*mask.Stride+x] >= 128
	}
	out := make([]uint8, w*h)
	for y := range h {
		for x := range w {
			in := inside(x, y)
			best := float64(sdfSpread)
			for dy := -sdfSpread; dy <= sdfSpread; dy++ {
				for dx := -sdfSpread; dx <= sdfSpread; dx++ {
					if inside(x+dx, y+dy) != in {
						if d := math.Hypot(float64(dx), float64(dy)); d < best {
							best = d
						}
					}
				}
			}
			d := best / sdfSpread // 0..1
			v := 0.5 - d*0.5
			if in {
				v = 0.5 + d*0.5
			}
			out[y*w+x] = uint8(math.Round(v * 255))
		}
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
