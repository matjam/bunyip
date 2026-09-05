package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// A colour glyph is drawn on the CPU into a colorCanvas and put in the
// atlas as premultiplied linear light, the way a bitmap strike is, so
// DrawGlyphs draws it untinted. The canvas is the shared half of the
// COLR and SVG glyph renderers: an affine transform, coverage masks from
// the outline rasteriser, colour lines and Porter-Duff compositing.

// affine maps a point: x' = a*x + c*y + tx, y' = b*x + d*y + ty. It is
// the layout of an OpenType Affine2x3 and of an SVG matrix().
type affine struct{ a, b, c, d, tx, ty float32 }

// identityAffine is the transform that moves nothing.
var identityAffine = affine{a: 1, d: 1}

// mul returns the transform that applies n and then m.
func (m affine) mul(n affine) affine {
	return affine{
		a:  m.a*n.a + m.c*n.b,
		b:  m.b*n.a + m.d*n.b,
		c:  m.a*n.c + m.c*n.d,
		d:  m.b*n.c + m.d*n.d,
		tx: m.a*n.tx + m.c*n.ty + m.tx,
		ty: m.b*n.tx + m.d*n.ty + m.ty,
	}
}

// apply maps a point.
func (m affine) apply(x, y float32) (float32, float32) {
	return m.a*x + m.c*y + m.tx, m.b*x + m.d*y + m.ty
}

// invert returns the inverse transform, or false when the transform
// flattens the plane.
func (m affine) invert() (affine, bool) {
	det := m.a*m.d - m.b*m.c
	if det == 0 || math.IsNaN(float64(det)) {
		return identityAffine, false
	}
	i := 1 / det
	return affine{
		a: m.d * i, b: -m.b * i, c: -m.c * i, d: m.a * i,
		tx: (m.c*m.ty - m.d*m.tx) * i,
		ty: (m.b*m.tx - m.a*m.ty) * i,
	}, true
}

// translate, scale, rotate and skew are the transforms COLR and SVG name
// separately, in the same space as the transform they compose with.
func translateAffine(dx, dy float32) affine { return affine{a: 1, d: 1, tx: dx, ty: dy} }

func scaleAffine(sx, sy float32) affine { return affine{a: sx, d: sy} }

func rotateAffine(radians float32) affine {
	s, c := float32(math.Sin(float64(radians))), float32(math.Cos(float64(radians)))
	return affine{a: c, b: s, c: -s, d: c}
}

func skewAffine(xRadians, yRadians float32) affine {
	return affine{a: 1, b: float32(math.Tan(float64(yRadians))), c: float32(math.Tan(float64(xRadians))), d: 1}
}

// around wraps a transform so that it acts about a centre point.
func around(m affine, cx, cy float32) affine {
	return translateAffine(cx, cy).mul(m).mul(translateAffine(-cx, -cy))
}

// box is a rectangle in the space a path was walked in.
type box struct{ minX, minY, maxX, maxY float32 }

// emptyBox is the box that grows to hold the first point added.
var emptyBox = box{math.MaxFloat32, math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}

func (b *box) add(x, y float32) {
	b.minX, b.minY = min(b.minX, x), min(b.minY, y)
	b.maxX, b.maxY = max(b.maxX, x), max(b.maxY, y)
}

func (b box) union(o box) box {
	if !o.valid() {
		return b
	}
	if !b.valid() {
		return o
	}
	return box{min(b.minX, o.minX), min(b.minY, o.minY), max(b.maxX, o.maxX), max(b.maxY, o.maxY)}
}

func (b box) valid() bool { return b.maxX >= b.minX && b.maxY >= b.minY }

// colorCanvas holds a colour glyph while it is drawn: linear light,
// premultiplied by alpha, four floats a pixel.
type colorCanvas struct {
	w, h int
	pix  []float32
}

func newColorCanvas(w, h int) *colorCanvas {
	return &colorCanvas{w: w, h: h, pix: make([]float32, w*h*4)}
}

// rgba is one colour in linear light, not premultiplied.
type rgba struct{ r, g, b, a float32 }

// fill composites a colour through a coverage mask, source over. at
// returns the colour at a pixel centre, or a constant for a solid fill.
func (c *colorCanvas) fill(mask []float32, at func(x, y float32) rgba) {
	for y := range c.h {
		for x := range c.w {
			cov := float32(1)
			if mask != nil {
				cov = mask[y*c.w+x]
				if cov <= 0 {
					continue
				}
			}
			col := at(float32(x)+0.5, float32(y)+0.5)
			a := col.a * cov
			if a <= 0 {
				continue
			}
			i := (y*c.w + x) * 4
			p := c.pix[i : i+4 : i+4]
			p[0] = col.r*a + p[0]*(1-a)
			p[1] = col.g*a + p[1]*(1-a)
			p[2] = col.b*a + p[2]*(1-a)
			p[3] = a + p[3]*(1-a)
		}
	}
}

// solid returns an at function for one colour.
func solid(c rgba) func(x, y float32) rgba {
	return func(float32, float32) rgba { return c }
}

// over composites another canvas onto this one, source over.
func (c *colorCanvas) over(src *colorCanvas) {
	for i := 0; i < len(c.pix); i += 4 {
		a := src.pix[i+3]
		if a <= 0 {
			continue
		}
		for k := range 4 {
			c.pix[i+k] = src.pix[i+k] + c.pix[i+k]*(1-a)
		}
	}
}

// empty reports whether nothing was drawn on the canvas.
func (c *colorCanvas) empty() bool {
	for i := 3; i < len(c.pix); i += 4 {
		if c.pix[i] > 0 {
			return false
		}
	}
	return true
}

// colorStop is one stop of a colour line, in linear light.
type colorStop struct {
	offset float32
	color  rgba
}

// extendMode says what a gradient does outside its colour line.
type extendMode uint8

const (
	extendPad extendMode = iota
	extendRepeat
	extendReflect
)

// colorLine samples a list of stops.
type colorLine struct {
	stops  []colorStop
	extend extendMode
}

// at returns the colour at a position along the line.
func (l colorLine) at(t float32) rgba {
	if len(l.stops) == 0 {
		return rgba{}
	}
	first, last := l.stops[0].offset, l.stops[len(l.stops)-1].offset
	switch {
	case last <= first:
		return l.stops[len(l.stops)-1].color
	case t < first || t > last:
		span := last - first
		switch l.extend {
		case extendRepeat:
			t = first + float32(math.Mod(float64(t-first), float64(span)))
			if t < first {
				t += span
			}
		case extendReflect:
			u := float32(math.Mod(float64(t-first), float64(2*span)))
			if u < 0 {
				u += 2 * span
			}
			if u > span {
				u = 2*span - u
			}
			t = first + u
		default:
			t = lin.Clamp(t, first, last)
		}
	}
	for i := 1; i < len(l.stops); i++ {
		a, b := l.stops[i-1], l.stops[i]
		if t > b.offset {
			continue
		}
		if b.offset <= a.offset {
			return b.color
		}
		u := (t - a.offset) / (b.offset - a.offset)
		return rgba{
			r: a.color.r + (b.color.r-a.color.r)*u,
			g: a.color.g + (b.color.g-a.color.g)*u,
			b: a.color.b + (b.color.b-a.color.b)*u,
			a: a.color.a + (b.color.a-a.color.a)*u,
		}
	}
	return l.stops[len(l.stops)-1].color
}

// linearGradient returns the colour at a point of a two-point linear
// gradient, in the space the points are given in.
func linearGradient(line colorLine, x0, y0, x1, y1 float32, inv affine) func(x, y float32) rgba {
	dx, dy := x1-x0, y1-y0
	den := dx*dx + dy*dy
	return func(px, py float32) rgba {
		x, y := inv.apply(px, py)
		if den <= 0 {
			return line.at(0)
		}
		return line.at(((x-x0)*dx + (y-y0)*dy) / den)
	}
}

// radialGradient returns the colour at a point of a two-circle radial
// gradient, the larger root of the circle that passes through it.
func radialGradient(line colorLine, x0, y0, r0, x1, y1, r1 float32, inv affine) func(x, y float32) rgba {
	cdx, cdy, dr := x1-x0, y1-y0, r1-r0
	a := cdx*cdx + cdy*cdy - dr*dr
	return func(px, py float32) rgba {
		x, y := inv.apply(px, py)
		pdx, pdy := x-x0, y-y0
		b := pdx*cdx + pdy*cdy + r0*dr
		cc := pdx*pdx + pdy*pdy - r0*r0
		var t float32
		if abs32(a) < 1e-6 {
			if abs32(b) < 1e-12 {
				return rgba{}
			}
			t = cc / (2 * b)
			if r0+t*dr < 0 {
				return rgba{}
			}
		} else {
			disc := b*b - a*cc
			if disc < 0 {
				return rgba{}
			}
			sq := float32(math.Sqrt(float64(disc)))
			t = (b + sq) / a
			if r0+t*dr < 0 {
				t = (b - sq) / a
				if r0+t*dr < 0 {
					return rgba{}
				}
			}
		}
		return line.at(t)
	}
}

// sweepGradient returns the colour at a point of a gradient that sweeps
// through an angle about a centre. The angles are in radians,
// counter-clockwise from the positive x axis of the gradient's space.
func sweepGradient(line colorLine, cx, cy, start, end float32, inv affine) func(x, y float32) rgba {
	span := end - start
	return func(px, py float32) rgba {
		x, y := inv.apply(px, py)
		if span == 0 {
			return line.at(0)
		}
		angle := float32(math.Atan2(float64(y-cy), float64(x-cx)))
		// The sweep is periodic: an angle is taken on the turn that
		// starts at the gradient's start angle.
		d := float32(math.Mod(float64(angle-start), 2*math.Pi))
		if d < 0 {
			d += 2 * math.Pi
		}
		if span < 0 {
			d -= 2 * math.Pi
		}
		return line.at(d / span)
	}
}

// addCanvas puts a rendered colour glyph in the atlas. ox and oy are the
// canvas's top-left in pixels from the glyph origin on the baseline, y
// down, the same convention as an outline glyph's bearing.
func (f *Font) addCanvas(c *colorCanvas, ox, oy int) (glyph, bool) {
	if c.w <= 0 || c.h <= 0 || c.empty() {
		return glyph{}, false
	}
	x, y, placed := f.packer.place(c.w, c.h)
	if !placed {
		return glyph{empty: true}, true // atlas full; drawn as nothing
	}
	for yy := range c.h {
		for xx := range c.w {
			i := (yy*c.w + xx) * 4
			p := c.pix[i : i+4 : i+4]
			f.pix.SetRGBA(x+xx, y+yy, rgbaBytes(p[0], p[1], p[2], p[3]))
		}
	}
	side := float32(f.packer.width)
	f.touched(x, y, c.w, c.h)
	return glyph{
		uv0:     lin.V2(float32(x)/side, float32(y)/side),
		uv1:     lin.V2(float32(x+c.w)/side, float32(y+c.h)/side),
		size:    lin.V2(float32(c.w)/f.scale, float32(c.h)/f.scale),
		bearing: lin.V2(float32(ox)/f.scale, float32(oy)/f.scale),
		color:   true,
	}, true
}
