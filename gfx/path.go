package gfx

import (
	"math"
	"slices"

	"github.com/matjam/bunyip/lin"
)

// Path is a sequence of lines and curves in view units, built by
// chaining calls: MoveTo starts a sub-path, the others extend it, Close
// joins it back to its start. A path can be filled, stroked, or both,
// as many times as you like; it holds no GPU state.
type Path struct {
	cmds  []pathCmd
	start lin.Vec2
	cur   lin.Vec2
	open  bool
}

type pathOp uint8

const (
	opMove pathOp = iota
	opLine
	opQuad
	opCubic
	opClose
)

type pathCmd struct {
	op pathOp
	p  [3]lin.Vec2
}

// MoveTo starts a new sub-path at (x, y).
func (p *Path) MoveTo(x, y float32) *Path {
	p.cmds = append(p.cmds, pathCmd{op: opMove, p: [3]lin.Vec2{{X: x, Y: y}}})
	p.start, p.cur, p.open = lin.V2(x, y), lin.V2(x, y), true
	return p
}

// LineTo adds a straight segment to (x, y).
func (p *Path) LineTo(x, y float32) *Path {
	p.ensureOpen()
	p.cmds = append(p.cmds, pathCmd{op: opLine, p: [3]lin.Vec2{{X: x, Y: y}}})
	p.cur = lin.V2(x, y)
	return p
}

// QuadTo adds a quadratic Bézier curve to (x, y) with control point (cx, cy).
func (p *Path) QuadTo(cx, cy, x, y float32) *Path {
	p.ensureOpen()
	p.cmds = append(p.cmds, pathCmd{op: opQuad, p: [3]lin.Vec2{{X: cx, Y: cy}, {X: x, Y: y}}})
	p.cur = lin.V2(x, y)
	return p
}

// CubicTo adds a cubic Bézier curve to (x, y) with two control points.
func (p *Path) CubicTo(c1x, c1y, c2x, c2y, x, y float32) *Path {
	p.ensureOpen()
	p.cmds = append(p.cmds, pathCmd{op: opCubic, p: [3]lin.Vec2{{X: c1x, Y: c1y}, {X: c2x, Y: c2y}, {X: x, Y: y}}})
	p.cur = lin.V2(x, y)
	return p
}

// Close joins the sub-path back to its start with a straight segment.
func (p *Path) Close() *Path {
	if p.open {
		p.cmds = append(p.cmds, pathCmd{op: opClose})
		p.cur, p.open = p.start, false
	}
	return p
}

// Reset empties the path for reuse without freeing its memory.
func (p *Path) Reset() {
	p.cmds = p.cmds[:0]
	p.open = false
}

// Empty reports whether the path has no segments.
func (p *Path) Empty() bool { return len(p.cmds) == 0 }

// Arc adds an arc of a circle centred at (cx, cy) with radius r, from
// angle start sweeping by sweep radians (positive turns the way angles
// increase: clockwise on a y-down screen). It joins the current point to
// the arc's start with a line when the sub-path is open.
func (p *Path) Arc(cx, cy, r, start, sweep float32) *Path {
	if sweep == 0 || r <= 0 {
		return p
	}
	first := lin.V2(cx+r*cos32(start), cy+r*sin32(start))
	if p.open {
		p.LineTo(first.X, first.Y)
	} else {
		p.MoveTo(first.X, first.Y)
	}
	// Split into pieces of at most a quarter turn, each a cubic.
	n := int(math.Ceil(math.Abs(float64(sweep)) / (math.Pi / 2)))
	step := sweep / float32(n)
	k := float32(4.0/3.0) * float32(math.Tan(float64(step)/4))
	a := start
	for range n {
		b := a + step
		p0 := lin.V2(cos32(a), sin32(a))
		p3 := lin.V2(cos32(b), sin32(b))
		c1 := lin.V2(p0.X-k*p0.Y, p0.Y+k*p0.X)
		c2 := lin.V2(p3.X+k*p3.Y, p3.Y-k*p3.X)
		p.CubicTo(cx+r*c1.X, cy+r*c1.Y, cx+r*c2.X, cy+r*c2.Y, cx+r*p3.X, cy+r*p3.Y)
		a = b
	}
	return p
}

// ArcTo adds an arc of radius r tangent to the lines from the current
// point to (x1, y1) and from there to (x2, y2), as a rounded corner.
func (p *Path) ArcTo(x1, y1, x2, y2, r float32) *Path {
	p.ensureOpen()
	p0, p1, p2 := p.cur, lin.V2(x1, y1), lin.V2(x2, y2)
	d0, d1 := p0.Sub(p1), p2.Sub(p1)
	l0, l1 := d0.Len(), d1.Len()
	if r <= 0 || l0 == 0 || l1 == 0 {
		return p.LineTo(x1, y1)
	}
	d0, d1 = d0.Mul(1/l0), d1.Mul(1/l1)
	cosA := d0.Dot(d1)
	if cosA <= -0.9999 || cosA >= 0.9999 {
		return p.LineTo(x1, y1)
	}
	half := float32(math.Acos(float64(cosA))) / 2
	t := r / float32(math.Tan(float64(half)))
	t = min(t, l0, l1)
	a := p1.Add(d0.Mul(t))
	b := p1.Add(d1.Mul(t))
	// The centre lies along the bisector at distance r / sin(half).
	bis := d0.Add(d1).Norm()
	c := p1.Add(bis.Mul(r / float32(math.Sin(float64(half)))))
	p.LineTo(a.X, a.Y)
	sa := float32(math.Atan2(float64(a.Y-c.Y), float64(a.X-c.X)))
	sb := float32(math.Atan2(float64(b.Y-c.Y), float64(b.X-c.X)))
	sweep := sb - sa
	for sweep > math.Pi {
		sweep -= 2 * math.Pi
	}
	for sweep < -math.Pi {
		sweep += 2 * math.Pi
	}
	return p.Arc(c.X, c.Y, r, sa, sweep)
}

// Rect adds a closed rectangle as its own sub-path.
func (p *Path) Rect(x, y, w, h float32) *Path {
	return p.MoveTo(x, y).LineTo(x+w, y).LineTo(x+w, y+h).LineTo(x, y+h).Close()
}

// RoundRect adds a closed rectangle with rounded corners of radius r.
func (p *Path) RoundRect(x, y, w, h, r float32) *Path {
	r = min(r, w/2, h/2)
	if r <= 0 {
		return p.Rect(x, y, w, h)
	}
	p.MoveTo(x+r, y)
	p.LineTo(x+w-r, y).Arc(x+w-r, y+r, r, -math.Pi/2, math.Pi/2)
	p.LineTo(x+w, y+h-r).Arc(x+w-r, y+h-r, r, 0, math.Pi/2)
	p.LineTo(x+r, y+h).Arc(x+r, y+h-r, r, math.Pi/2, math.Pi/2)
	p.LineTo(x, y+r).Arc(x+r, y+r, r, math.Pi, math.Pi/2)
	return p.Close()
}

// Circle adds a closed circle as its own sub-path.
func (p *Path) Circle(cx, cy, r float32) *Path { return p.Ellipse(cx, cy, r, r) }

// Ellipse adds a closed axis-aligned ellipse as its own sub-path.
func (p *Path) Ellipse(cx, cy, rx, ry float32) *Path {
	const k = 0.5522847498 // 4/3·tan(π/8): a quarter circle as a cubic
	p.MoveTo(cx+rx, cy)
	p.CubicTo(cx+rx, cy+ry*k, cx+rx*k, cy+ry, cx, cy+ry)
	p.CubicTo(cx-rx*k, cy+ry, cx-rx, cy+ry*k, cx-rx, cy)
	p.CubicTo(cx-rx, cy-ry*k, cx-rx*k, cy-ry, cx, cy-ry)
	p.CubicTo(cx+rx*k, cy-ry, cx+rx, cy-ry*k, cx+rx, cy)
	return p.Close()
}

// Polygon adds a closed polygon through the points.
func (p *Path) Polygon(points ...lin.Vec2) *Path {
	if len(points) == 0 {
		return p
	}
	p.MoveTo(points[0].X, points[0].Y)
	for _, q := range points[1:] {
		p.LineTo(q.X, q.Y)
	}
	return p.Close()
}

func (p *Path) ensureOpen() {
	if !p.open {
		p.MoveTo(p.cur.X, p.cur.Y)
	}
}

// subpath is a flattened run of points.
type subpath struct {
	pts    []lin.Vec2
	closed bool
}

// flatten turns the path into polylines, approximating curves to within
// tol view units.
func (p *Path) flatten(tol float32) []subpath {
	tol = max(tol, 1e-3)
	var out []subpath
	var cur *subpath
	begin := func(at lin.Vec2) {
		out = append(out, subpath{pts: []lin.Vec2{at}})
		cur = &out[len(out)-1]
	}
	last := func() lin.Vec2 {
		if cur == nil || len(cur.pts) == 0 {
			return lin.Vec2{}
		}
		return cur.pts[len(cur.pts)-1]
	}
	for _, c := range p.cmds {
		switch c.op {
		case opMove:
			begin(c.p[0])
		case opLine:
			if cur == nil {
				begin(c.p[0])
			} else {
				cur.pts = append(cur.pts, c.p[0])
			}
		case opQuad:
			if cur == nil {
				begin(c.p[0])
			}
			p0, p1, p2 := last(), c.p[0], c.p[1]
			dd := p0.Sub(p1.Mul(2)).Add(p2).Len()
			n := max(1, min(100, int(math.Ceil(math.Sqrt(float64(dd/tol*0.25))))))
			for i := 1; i <= n; i++ {
				t := float32(i) / float32(n)
				u := 1 - t
				cur.pts = append(cur.pts, p0.Mul(u*u).Add(p1.Mul(2*u*t)).Add(p2.Mul(t*t)))
			}
		case opCubic:
			if cur == nil {
				begin(c.p[0])
			}
			p0, p1, p2, p3 := last(), c.p[0], c.p[1], c.p[2]
			dd := max(p0.Sub(p1.Mul(2)).Add(p2).Len(), p1.Sub(p2.Mul(2)).Add(p3).Len())
			n := max(1, min(100, int(math.Ceil(math.Sqrt(float64(dd/tol*0.75))))))
			for i := 1; i <= n; i++ {
				t := float32(i) / float32(n)
				u := 1 - t
				cur.pts = append(cur.pts, p0.Mul(u*u*u).Add(p1.Mul(3*u*u*t)).Add(p2.Mul(3*u*t*t)).Add(p3.Mul(t*t*t)))
			}
		case opClose:
			if cur != nil {
				cur.closed = true
				cur = nil
			}
		}
	}
	// Drop repeated points, which would make zero-length segments.
	for i := range out {
		pts := out[i].pts[:1]
		for _, q := range out[i].pts[1:] {
			if q.Sub(pts[len(pts)-1]).Len() > 1e-5 {
				pts = append(pts, q)
			}
		}
		if out[i].closed && len(pts) > 1 && pts[len(pts)-1].Sub(pts[0]).Len() <= 1e-5 {
			pts = pts[:len(pts)-1]
		}
		out[i].pts = pts
	}
	return out
}

// FillRule decides which regions of a self-overlapping path are inside.
type FillRule uint8

const (
	// FillNonZero fills where the winding number is not zero: a shape
	// drawn twice the same way stays filled.
	FillNonZero FillRule = iota
	// FillEvenOdd fills where an odd number of edges are crossed: a shape
	// inside another becomes a hole.
	FillEvenOdd
)

// FillOptions controls FillPath.
type FillOptions struct {
	Rule FillRule
	// Texture maps an image over the path: view point p gets texture
	// coordinate (p - TextureOrigin) / TextureSize. Zero size means the
	// path's bounds.
	Texture       *Texture
	TextureOrigin lin.Vec2
	TextureSize   lin.Vec2
	// Gradient colours the fill by position instead of a texture; the
	// colour argument then tints it.
	Gradient    *Gradient
	NoAntiAlias bool
}

// LineCap is how a stroke ends.
type LineCap uint8

const (
	CapButt   LineCap = iota // stops at the end point
	CapRound                 // a half circle past it
	CapSquare                // half the width past it
)

// LineJoin is how a stroke turns a corner.
type LineJoin uint8

const (
	JoinMiter LineJoin = iota // a sharp corner, up to MiterLimit
	JoinRound                 // a rounded corner
	JoinBevel                 // a cut corner
)

// StrokeOptions controls StrokePath.
type StrokeOptions struct {
	Width      float32 // zero means 1
	Cap        LineCap
	Join       LineJoin
	MiterLimit float32 // miter length over width beyond which corners bevel; zero means 4
	// Dash is a pattern of on and off lengths in view units, repeated
	// along the path, starting DashOffset in; empty strokes solid.
	Dash       []float32
	DashOffset float32
	// Gradient colours the stroke by position; the colour then tints it.
	Gradient    *Gradient
	NoAntiAlias bool
}

// fringe is the anti-aliasing ramp width in view units for the current
// queue: one framebuffer pixel through the camera and transform.
func (g *Graphics) fringe() float32 {
	q := g.cur
	px := q.pixelW / q.viewW
	if q == g.main {
		px = g.pixelScale()
	}
	zoom := float32(1)
	if q.hasCam2D && q.cam2D.Zoom != 0 {
		zoom = q.cam2D.Zoom
	}
	scale := q.xform.Scale()
	if scale <= 0 {
		scale = 1
	}
	return 1 / (px * zoom * scale)
}

// FillPath fills the path's interior with a colour.
func (g *Graphics) FillPath(p *Path, c Color, opts FillOptions) {
	fr := g.fringe()
	subs := p.flatten(fr * 0.25)
	var b filler
	b.rule = opts.Rule
	b.color = c.premultiplied()
	b.fringe = fr
	if opts.NoAntiAlias {
		b.fringe = 0
	}
	if opts.Gradient != nil && opts.Gradient.tex != nil {
		b.tex, b.grad = opts.Gradient.tex, opts.Gradient
	} else if opts.Texture != nil {
		b.tex = opts.Texture
		b.uvOrigin, b.uvSize = opts.TextureOrigin, opts.TextureSize
		if b.uvSize == (lin.Vec2{}) {
			b.uvOrigin, b.uvSize = bounds(subs)
		}
	}
	b.run(subs)
	g.emit(b.tex, b.verts)
}

// bounds is the box around flattened sub-paths.
func bounds(subs []subpath) (origin, size lin.Vec2) {
	lo, hi := lin.V2(math.MaxFloat32, math.MaxFloat32), lin.V2(-math.MaxFloat32, -math.MaxFloat32)
	for _, s := range subs {
		for _, q := range s.pts {
			lo = lin.V2(min(lo.X, q.X), min(lo.Y, q.Y))
			hi = lin.V2(max(hi.X, q.X), max(hi.Y, q.Y))
		}
	}
	if hi.X <= lo.X || hi.Y <= lo.Y {
		return lo, lin.V2(1, 1)
	}
	return lo, hi.Sub(lo)
}

// fillEdge is one non-horizontal polygon edge, oriented top to bottom
// with the winding direction of the original.
type fillEdge struct {
	x0, y0, x1, y1 float32
	dir            int32 // +1 when the original ran downward
	dxdy           float32
}

func (e *fillEdge) at(y float32) float32 { return e.x0 + (y-e.y0)*e.dxdy }

// filler decomposes flattened sub-paths into trapezoids by a scanline
// sweep, with the fill rule applied between crossings, plus anti-alias
// fringes along boundary edges.
type filler struct {
	rule     FillRule
	color    [4]float32
	fringe   float32
	tex      *Texture
	grad     *Gradient
	uvOrigin lin.Vec2
	uvSize   lin.Vec2
	verts    []vertex2D
	edges    []fillEdge
	active   []int
	ys       []float32
}

func (b *filler) inside(w int32) bool {
	if b.rule == FillEvenOdd {
		return w&1 != 0
	}
	return w != 0
}

func (b *filler) vertex(p lin.Vec2, alpha float32) vertex2D {
	c := b.color
	if alpha != 1 {
		for i := range c {
			c[i] *= alpha
		}
	}
	var uv lin.Vec2
	if b.grad != nil {
		uv = b.grad.uv(p)
	} else if b.tex != nil {
		uv = lin.V2((p.X-b.uvOrigin.X)/b.uvSize.X, (p.Y-b.uvOrigin.Y)/b.uvSize.Y)
	}
	return vertex2D{pos: p, uv: uv, color: c}
}

func (b *filler) tri(p0, p1, p2 lin.Vec2) {
	b.verts = append(b.verts, b.vertex(p0, 1), b.vertex(p1, 1), b.vertex(p2, 1))
}

// fringeQuad draws the alpha ramp from edge a-b (opaque) outward along n.
func (b *filler) fringeQuad(a, bb, n lin.Vec2) {
	oa, ob := a.Add(n), bb.Add(n)
	b.verts = append(b.verts,
		b.vertex(a, 1), b.vertex(bb, 1), b.vertex(ob, 0),
		b.vertex(a, 1), b.vertex(ob, 0), b.vertex(oa, 0))
}

func (b *filler) run(subs []subpath) {
	b.edges = b.edges[:0]
	b.ys = b.ys[:0]
	for _, s := range subs {
		n := len(s.pts)
		if n < 2 {
			continue
		}
		for i := range n {
			p, q := s.pts[i], s.pts[(i+1)%n]
			if i == n-1 && n == 2 {
				break // a two-point sub-path has one edge each way; skip the return
			}
			if p.Y == q.Y {
				continue
			}
			e := fillEdge{dir: 1}
			if p.Y > q.Y {
				p, q = q, p
				e.dir = -1
			}
			e.x0, e.y0, e.x1, e.y1 = p.X, p.Y, q.X, q.Y
			e.dxdy = (q.X - p.X) / (q.Y - p.Y)
			b.edges = append(b.edges, e)
			b.ys = append(b.ys, p.Y, q.Y)
		}
	}
	if len(b.edges) == 0 {
		return
	}
	slices.Sort(b.ys)
	b.ys = slices.Compact(b.ys)
	for i := 0; i+1 < len(b.ys); i++ {
		ya, yb := b.ys[i], b.ys[i+1]
		if yb-ya < 1e-6 {
			continue
		}
		// Edges spanning this slab, ordered by x at its middle.
		b.active = b.active[:0]
		ym := (ya + yb) / 2
		for j := range b.edges {
			e := &b.edges[j]
			if e.y0 <= ya && e.y1 >= yb {
				b.active = append(b.active, j)
			}
		}
		slices.SortFunc(b.active, func(p, q int) int {
			xp, xq := b.edges[p].at(ym), b.edges[q].at(ym)
			if xp < xq {
				return -1
			}
			if xp > xq {
				return 1
			}
			return 0
		})
		// Two adjacent edges that cross inside the slab swap order; split
		// the slab at the crossing and redo it.
		split := false
		for k := 0; k+1 < len(b.active); k++ {
			e, f := &b.edges[b.active[k]], &b.edges[b.active[k+1]]
			if e.at(ya) > f.at(ya)+1e-6 || e.at(yb) > f.at(yb)+1e-6 {
				// x_e(y) = x_f(y) somewhere in (ya, yb).
				d := e.dxdy - f.dxdy
				if d != 0 {
					yc := ya + ((f.at(ya) - e.at(ya)) / d)
					if yc > ya+1e-6 && yc < yb-1e-6 {
						b.ys = slices.Insert(b.ys, i+1, yc)
						split = true
						break
					}
				}
			}
		}
		if split {
			i--
			continue
		}
		var w int32
		for k := 0; k+1 < len(b.active); k++ {
			e := &b.edges[b.active[k]]
			f := &b.edges[b.active[k+1]]
			w += e.dir
			if !b.inside(w) {
				continue
			}
			xa0, xa1 := e.at(ya), f.at(ya)
			xb0, xb1 := e.at(yb), f.at(yb)
			p0, p1 := lin.V2(xa0, ya), lin.V2(xa1, ya)
			p2, p3 := lin.V2(xb1, yb), lin.V2(xb0, yb)
			b.tri(p0, p1, p2)
			b.tri(p0, p2, p3)
		}
		if b.fringe > 0 {
			// Boundary edges are where insideness changes; the fringe goes
			// on the outside.
			w = 0
			for k := range b.active {
				e := &b.edges[b.active[k]]
				left := b.inside(w)
				w += e.dir
				right := b.inside(w)
				if left == right {
					continue
				}
				a, c := lin.V2(e.at(ya), ya), lin.V2(e.at(yb), yb)
				d := c.Sub(a)
				n := lin.V2(d.Y, -d.X).Norm().Mul(b.fringe) // points to the left (-x side) of a downward edge
				if n.X > 0 == left {
					n = n.Mul(-1) // outward is away from the inside
				}
				// Stretch the fringe along the edge so the ramp is measured
				// perpendicular to it.
				b.fringeQuad(a, c, n)
			}
		}
	}
}

// StrokePath outlines the path with a colour.
func (g *Graphics) StrokePath(p *Path, c Color, opts StrokeOptions) {
	fr := g.fringe()
	subs := p.flatten(fr * 0.25)
	s := stroker{color: c.premultiplied(), fringe: fr, opts: opts}
	if opts.NoAntiAlias {
		s.fringe = 0
	}
	if s.opts.Width <= 0 {
		s.opts.Width = 1
	}
	if s.opts.MiterLimit <= 0 {
		s.opts.MiterLimit = 4
	}
	var tex *Texture
	if opts.Gradient != nil && opts.Gradient.tex != nil {
		s.grad, tex = opts.Gradient, opts.Gradient.tex
	}
	for _, sub := range subs {
		if len(opts.Dash) > 0 {
			for _, piece := range dashed(sub, opts.Dash, opts.DashOffset) {
				s.run(piece)
			}
			continue
		}
		s.run(sub)
	}
	g.emit(tex, s.verts)
}

// stroker expands polylines into triangles with joins and caps.
type stroker struct {
	color  [4]float32
	fringe float32
	opts   StrokeOptions
	grad   *Gradient
	verts  []vertex2D
}

func (s *stroker) vertex(p lin.Vec2, alpha float32) vertex2D {
	c := s.color
	if alpha != 1 {
		for i := range c {
			c[i] *= alpha
		}
	}
	var uv lin.Vec2
	if s.grad != nil {
		uv = s.grad.uv(p)
	}
	return vertex2D{pos: p, uv: uv, color: c}
}

func (s *stroker) tri(p0, p1, p2 lin.Vec2) {
	s.verts = append(s.verts, s.vertex(p0, 1), s.vertex(p1, 1), s.vertex(p2, 1))
}

// edge draws an opaque-to-clear fringe from segment a-b outward along n.
func (s *stroker) edge(a, b, n lin.Vec2) {
	if s.fringe <= 0 {
		return
	}
	oa, ob := a.Add(n), b.Add(n)
	s.verts = append(s.verts,
		s.vertex(a, 1), s.vertex(b, 1), s.vertex(ob, 0),
		s.vertex(a, 1), s.vertex(ob, 0), s.vertex(oa, 0))
}

// fan draws a pie of radius r around c from angle a0 sweeping to a1,
// with a fringe around its arc.
func (s *stroker) fan(c lin.Vec2, r, a0, a1 float32) {
	sweep := a1 - a0
	n := max(2, int(math.Ceil(math.Abs(float64(sweep))/(math.Pi/8))))
	prev := c.Add(lin.V2(cos32(a0), sin32(a0)).Mul(r))
	for i := 1; i <= n; i++ {
		a := a0 + sweep*float32(i)/float32(n)
		dir := lin.V2(cos32(a), sin32(a))
		next := c.Add(dir.Mul(r))
		s.tri(c, prev, next)
		s.edge(prev, next, prev.Add(next).Mul(0.5).Sub(c).Norm().Mul(s.fringe))
		prev = next
	}
}

func (s *stroker) run(sub subpath) {
	pts := sub.pts
	if len(pts) < 2 {
		if len(pts) == 1 && s.opts.Cap == CapRound {
			s.fan(pts[0], s.opts.Width/2, 0, 2*math.Pi) // a dot
		}
		return
	}
	hw := s.opts.Width / 2
	n := len(pts)
	segs := n - 1
	if sub.closed {
		segs = n
	}
	// Segment bodies.
	for i := range segs {
		a, b := pts[i], pts[(i+1)%n]
		d := b.Sub(a).Norm()
		if !sub.closed && s.opts.Cap == CapSquare {
			if i == 0 {
				a = a.Sub(d.Mul(hw))
			}
			if i == segs-1 {
				b = b.Add(d.Mul(hw))
			}
		}
		nrm := lin.V2(-d.Y, d.X).Mul(hw)
		p0, p1, p2, p3 := a.Add(nrm), b.Add(nrm), b.Sub(nrm), a.Sub(nrm)
		s.tri(p0, p1, p2)
		s.tri(p0, p2, p3)
		fn := nrm.Norm().Mul(s.fringe)
		s.edge(p0, p1, fn)
		s.edge(p2, p3, fn.Mul(-1))
		if !sub.closed && s.opts.Cap != CapRound {
			if i == 0 {
				s.edge(p3, p0, d.Mul(-s.fringe))
			}
			if i == segs-1 {
				s.edge(p1, p2, d.Mul(s.fringe))
			}
		}
	}
	// Joins at interior corners (every corner when closed).
	first, last := 1, n-1
	if sub.closed {
		first, last = 0, n
	}
	for i := first; i < last; i++ {
		prev, cur, next := pts[(i-1+n)%n], pts[i%n], pts[(i+1)%n]
		s.join(prev, cur, next, hw)
	}
	if !sub.closed && s.opts.Cap == CapRound {
		d := pts[1].Sub(pts[0]).Norm()
		a := float32(math.Atan2(float64(d.Y), float64(d.X)))
		s.fan(pts[0], hw, a+math.Pi/2, a+3*math.Pi/2)
		d = pts[n-1].Sub(pts[n-2]).Norm()
		a = float32(math.Atan2(float64(d.Y), float64(d.X)))
		s.fan(pts[n-1], hw, a-math.Pi/2, a+math.Pi/2)
	}
}

// join fills the wedge on the outside of a corner.
func (s *stroker) join(prev, cur, next lin.Vec2, hw float32) {
	d0 := cur.Sub(prev).Norm()
	d1 := next.Sub(cur).Norm()
	cross := d0.X*d1.Y - d0.Y*d1.X
	if abs32(cross) < 1e-6 && d0.Dot(d1) > 0 {
		return // straight on
	}
	n0 := lin.V2(-d0.Y, d0.X).Mul(hw)
	n1 := lin.V2(-d1.Y, d1.X).Mul(hw)
	// The outer side is opposite the turn direction.
	if cross > 0 {
		n0, n1 = n0.Mul(-1), n1.Mul(-1)
	}
	a, b := cur.Add(n0), cur.Add(n1)
	switch s.opts.Join {
	case JoinRound:
		a0 := float32(math.Atan2(float64(n0.Y), float64(n0.X)))
		a1 := float32(math.Atan2(float64(n1.Y), float64(n1.X)))
		sweep := a1 - a0
		for sweep > math.Pi {
			sweep -= 2 * math.Pi
		}
		for sweep < -math.Pi {
			sweep += 2 * math.Pi
		}
		s.fan(cur, hw, a0, a0+sweep)
	case JoinMiter:
		// Miter point: along the bisector of the outer normals.
		bis := n0.Add(n1)
		cosHalf := float32(math.Sqrt(float64(max(0, (1+d0.Dot(d1))/2))))
		if bis.Len() > 1e-6 && cosHalf > 1e-4 && 1/cosHalf <= s.opts.MiterLimit {
			m := cur.Add(bis.Norm().Mul(hw / cosHalf))
			s.tri(cur, a, m)
			s.tri(cur, m, b)
			s.edge(a, m, n0.Norm().Mul(s.fringe))
			s.edge(m, b, n1.Norm().Mul(s.fringe))
			return
		}
		fallthrough
	default: // bevel
		s.tri(cur, a, b)
		s.edge(a, b, a.Add(b).Mul(0.5).Sub(cur).Norm().Mul(s.fringe))
	}
}

// FillCircle fills a circle.
func (g *Graphics) FillCircle(cx, cy, r float32, c Color) {
	var p Path
	g.FillPath(p.Circle(cx, cy, r), c, FillOptions{})
}

// StrokeCircle outlines a circle with a line width.
func (g *Graphics) StrokeCircle(cx, cy, r, width float32, c Color) {
	var p Path
	g.StrokePath(p.Circle(cx, cy, r), c, StrokeOptions{Width: width})
}

// StrokeRect outlines a rectangle with a line width.
func (g *Graphics) StrokeRect(x, y, w, h, width float32, c Color) {
	var p Path
	g.StrokePath(p.Rect(x, y, w, h), c, StrokeOptions{Width: width})
}

// StrokeLine draws a line segment with a width and butt caps.
func (g *Graphics) StrokeLine(x0, y0, x1, y1, width float32, c Color) {
	var p Path
	g.StrokePath(p.MoveTo(x0, y0).LineTo(x1, y1), c, StrokeOptions{Width: width})
}

// FillPolygon fills a polygon through the points.
func (g *Graphics) FillPolygon(points []lin.Vec2, c Color) {
	var p Path
	g.FillPath(p.Polygon(points...), c, FillOptions{})
}
