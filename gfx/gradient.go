package gfx

import (
	"math"
	"sort"

	"github.com/matjam/bunyip/lin"
)

// GradientStop is a colour at a position along a gradient, 0 to 1.
type GradientStop struct {
	T     float32
	Color Color
}

// Gradient colours a fill or stroke by position: linear from one point to
// another, or radial out from a centre. Graphics releases its small
// texture at shutdown, or Destroy releases it earlier.
type Gradient struct {
	from, to lin.Vec2
	radius   float32
	radial   bool
	tex      *Texture
}

// NewGradient bakes stops into a gradient; stops need not be sorted and
// a single stop is a flat colour. Give it a direction with Linear or
// Radial before drawing with it.
func (g *Graphics) NewGradient(stops ...GradientStop) (*Gradient, error) {
	stops = append([]GradientStop(nil), stops...)
	sort.SliceStable(stops, func(i, j int) bool { return stops[i].T < stops[j].T })
	const n = 256
	pix := make([]byte, n*4)
	for i := range n {
		t := float32(i) / (n - 1)
		c := sampleStops(stops, t).premultiplied()
		for k := range 4 {
			pix[i*4+k] = uint8(lin.Clamp(c[k], 0, 1)*255 + 0.5)
		}
	}
	tex, err := g.newTexture(n, 1, pix, TextureOptions{Linear: true, NoMipmaps: true, Data: true})
	if err != nil {
		return nil, err
	}
	return &Gradient{tex: tex, to: lin.V2(1, 0)}, nil
}

func sampleStops(stops []GradientStop, t float32) Color {
	if len(stops) == 0 {
		return White
	}
	if t <= stops[0].T {
		return stops[0].Color
	}
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].T {
			a, b := stops[i-1], stops[i]
			if b.T <= a.T {
				return b.Color
			}
			return a.Color.Lerp(b.Color, (t-a.T)/(b.T-a.T))
		}
	}
	return stops[len(stops)-1].Color
}

// Linear runs the gradient from one point to another, in the units of
// the drawing it is used in; beyond the ends the end colours hold.
func (gr *Gradient) Linear(from, to lin.Vec2) *Gradient {
	gr.from, gr.to, gr.radial = from, to, false
	return gr
}

// Radial runs the gradient out from a centre to a radius.
func (gr *Gradient) Radial(center lin.Vec2, radius float32) *Gradient {
	gr.from, gr.radius, gr.radial = center, radius, true
	return gr
}

// uv maps a point to the gradient texture.
func (gr *Gradient) uv(p lin.Vec2) lin.Vec2 {
	if gr.radial {
		if gr.radius <= 0 {
			return lin.V2(1, 0.5)
		}
		return lin.V2(p.Sub(gr.from).Len()/gr.radius, 0.5)
	}
	d := gr.to.Sub(gr.from)
	l2 := d.Dot(d)
	if l2 == 0 {
		return lin.V2(0, 0.5)
	}
	return lin.V2(p.Sub(gr.from).Dot(d)/l2, 0.5)
}

// Destroy frees the gradient's texture.
func (gr *Gradient) Destroy() {
	if gr.tex != nil {
		gr.tex.Destroy()
		gr.tex = nil
	}
}

// FillGradient fills a rectangle with a gradient.
func (g *Graphics) FillGradient(r lin.Rect, gr *Gradient) {
	var p Path
	p.Rect(r.X, r.Y, r.W, r.H)
	g.FillPath(&p, White, FillOptions{Gradient: gr})
}

// DrawTextOnPath lays one line of text along a path, each glyph turned
// to follow it, starting offset units from the path's start: labels on
// arcs, text around a badge, a river's name along its course. Glyphs
// past the end of the path are not drawn; only the path's first
// sub-path is used.
func (g *Graphics) DrawTextOnPath(f *Font, text string, p *Path, offset float32, opts TextOptions, c Color) {
	subs := p.flatten(g.fringe() * 0.25)
	if len(subs) == 0 || len(subs[0].pts) < 2 {
		return
	}
	pts := subs[0].pts
	if subs[0].closed {
		pts = append(pts, pts[0])
	}
	// Cumulative lengths for walking the polyline by distance.
	lengths := make([]float32, len(pts))
	for i := 1; i < len(pts); i++ {
		lengths[i] = lengths[i-1] + pts[i].Distance(pts[i-1])
	}
	total := lengths[len(lengths)-1]
	at := func(s float32) (point, tangent lin.Vec2, ok bool) {
		if s < 0 || s > total {
			return lin.Vec2{}, lin.Vec2{}, false
		}
		i := sort.Search(len(lengths), func(i int) bool { return lengths[i] >= s })
		i = max(i, 1)
		a, b := pts[i-1], pts[i]
		seg := lengths[i] - lengths[i-1]
		t := float32(0)
		if seg > 0 {
			t = (s - lengths[i-1]) / seg
		}
		return a.Lerp(b, t), b.Sub(a).Norm(), true
	}
	scale := f.sizeScale(opts.Size)
	if c == (Color{}) {
		c = White
	}
	for _, gl := range f.Shape(text, opts) {
		if gl.Empty {
			continue
		}
		w := gl.Size.X * scale
		centre := offset + (gl.Pos.X+gl.Size.X/2)*scale
		point, tangent, ok := at(centre)
		if !ok {
			continue
		}
		angle := tangent.Angle()
		// The glyph's top-left relative to its centre on the baseline.
		local := lin.V2(-w/2, (gl.Pos.Y-f.Ascent)*scale)
		g.Draw(f.atlas, Sprite{
			Pos: point.Add(local.Rotate(angle)), Size: gl.Size.Mul(scale),
			UV0: gl.UV0, UV1: gl.UV1, Color: c, Rotation: angle,
		})
	}
	if f.dirty {
		_ = f.flush()
	}
}

// dashed cuts a flattened sub-path into the "on" pieces of a dash
// pattern, starting the pattern offset units in.
func dashed(sub subpath, pattern []float32, offset float32) []subpath {
	// A negative or zero dash would never advance along the path; the
	// pattern is sanitised once and used in that form throughout.
	clean := make([]float32, len(pattern))
	total := float32(0)
	for i, d := range pattern {
		clean[i] = max(d, 0.001)
		total += clean[i]
	}
	pattern = clean
	if len(pattern) == 0 || total <= 0 {
		return []subpath{sub}
	}
	pts := sub.pts
	if sub.closed && len(pts) > 1 {
		pts = append(append([]lin.Vec2(nil), pts...), pts[0])
	}
	// Advance the pattern to the offset.
	idx, remain := 0, pattern[0]
	on := true
	for o := float32(math.Mod(float64(offset), float64(total))); o > 0; {
		if o < remain {
			remain -= o
			break
		}
		o -= remain
		idx = (idx + 1) % len(pattern)
		remain = pattern[idx]
		on = !on
	}
	var out []subpath
	var cur []lin.Vec2
	if on {
		cur = []lin.Vec2{pts[0]}
	}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		seg := b.Sub(a).Len()
		pos := float32(0)
		for seg-pos > remain {
			pos += remain
			p := a.Lerp(b, pos/seg)
			if on {
				cur = append(cur, p)
				out = append(out, subpath{pts: cur})
				cur = nil
			} else {
				cur = []lin.Vec2{p}
			}
			on = !on
			idx = (idx + 1) % len(pattern)
			remain = max(pattern[idx], 0.001)
		}
		remain -= seg - pos
		if on {
			cur = append(cur, b)
		}
	}
	if on && len(cur) > 1 {
		out = append(out, subpath{pts: cur})
	}
	return out
}
