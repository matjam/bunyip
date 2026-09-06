package gfx

import (
	"fmt"
	"image"
	"math"

	"github.com/go-text/typesetting/font"

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
// units used by DrawText; TextOptions.Size draws at any other size and
// stays sharp, where a bitmap font would blur.
func (g *Graphics) NewSDFFont(ttf []byte, size float32, opts FontOptions) (*Font, error) {
	// scale is face pixels per view unit at the nominal size; the atlas is
	// sdfOversample times coarser than the face.
	scale := float32(sdfEmPixels*sdfOversample) / size
	return g.newFont(ttf, size, opts, scale, sdfEmPixels*sdfOversample, true)
}

// addSDF rasterises a glyph mask at sdfOversample times the atlas size,
// converts it to distances with a linear-time transform and downsamples,
// so the stored edge position is accurate to a fraction of an atlas pixel.
func (f *Font) addSDF(face uint8, gid font.GID) glyph {
	const os = sdfOversample
	k := f.pxPerEm / f.faces[face].upem
	mask, ox, oy, ok := f.rasterise(face, gid, k, sdfSpread*os)
	if !ok {
		return glyph{empty: true}
	}
	// Round the mask up to whole atlas cells.
	mw, mh := mask.Rect.Dx(), mask.Rect.Dy()
	w, h := (mw+os-1)/os, (mh+os-1)/os
	x, y, placed := f.packer.place(w, h)
	if !placed {
		f.glyphErr = fmt.Errorf("gfx: glyph atlas is full (%d by %d); increase FontOptions.AtlasSize", f.packer.width, f.packer.height)
		return glyph{empty: true}
	}
	field := distanceField(mask, sdfSpread*os)
	for yy := range h {
		for xx := range w {
			var sum float64
			n := 0
			for sy := range os {
				for sx := range os {
					px, py := xx*os+sx, yy*os+sy
					if px < mw && py < mh {
						sum += field[py*mw+px]
						n++
					}
				}
			}
			v := 0.0
			if n > 0 {
				v = sum / float64(n)
			}
			f.pix.SetRGBA(x+xx, y+yy, rgbaPremul(uint8(math.Round(v*255))))
		}
	}
	side := float32(f.packer.width)
	f.touched(x, y, w, h)
	return glyph{
		uv0:     lin.V2(float32(x)/side, float32(y)/side),
		uv1:     lin.V2(float32(x+w)/side, float32(y+h)/side),
		size:    lin.V2(float32(w*os)/f.scale, float32(h*os)/f.scale),
		bearing: lin.V2(float32(ox)/f.scale, float32(oy)/f.scale),
	}
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
