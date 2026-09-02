package phys

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Capsule2 is a segment along the body's local Y axis grown by Radius:
// HalfHeight from the centre to the centre of each round end.
type Capsule2 struct{ Radius, HalfHeight float32 }

// segment returns the world centres of the two ends.
func (c Capsule2) segment(pos lin.Vec2, rot float32) (lin.Vec2, lin.Vec2) {
	cs, sn := cosSin(rot)
	up := rotate2(lin.V2(0, c.HalfHeight), cs, sn)
	return pos.Sub(up), pos.Add(up)
}

func (c Capsule2) bounds(pos lin.Vec2, rot float32) (lin.Vec2, lin.Vec2) {
	a, b := c.segment(pos, rot)
	r := lin.V2(c.Radius, c.Radius)
	return a.Min(b).Sub(r), a.Max(b).Add(r)
}

func (c Capsule2) inertia(mass float32) float32 {
	return Box2{HalfW: c.Radius, HalfH: c.HalfHeight + c.Radius}.inertia(mass)
}

// Edge2 is a line segment in the body's frame with no inside: a piece
// of ground or wall that shapes collide with from either side.
type Edge2 struct{ A, B lin.Vec2 }

func (e Edge2) bounds(pos lin.Vec2, rot float32) (lin.Vec2, lin.Vec2) {
	cs, sn := cosSin(rot)
	a, b := rotate2(e.A, cs, sn).Add(pos), rotate2(e.B, cs, sn).Add(pos)
	return a.Min(b), a.Max(b)
}

func (e Edge2) inertia(mass float32) float32 {
	l := e.B.Sub(e.A).Len()
	return mass * l * l / 12
}

// Chain2 is a run of edges through Points, for terrain outlines; Loop
// joins the last point back to the first.
type Chain2 struct {
	Points []lin.Vec2
	Loop   bool
}

func (c Chain2) bounds(pos lin.Vec2, rot float32) (lin.Vec2, lin.Vec2) {
	lo := lin.V2(float32(math.Inf(1)), float32(math.Inf(1)))
	hi := lo.Neg()
	cs, sn := cosSin(rot)
	for _, p := range c.Points {
		w := rotate2(p, cs, sn).Add(pos)
		lo, hi = lo.Min(w), hi.Max(w)
	}
	if len(c.Points) == 0 {
		return pos, pos
	}
	return lo, hi
}

func (c Chain2) inertia(float32) float32 { return 0 }

// segments returns the chain's edges in the world.
func (c Chain2) segments(pos lin.Vec2, rot float32) [][2]lin.Vec2 {
	n := len(c.Points)
	if n < 2 {
		return nil
	}
	cs, sn := cosSin(rot)
	world := make([]lin.Vec2, n)
	for i, p := range c.Points {
		world[i] = rotate2(p, cs, sn).Add(pos)
	}
	count := n - 1
	if c.Loop {
		count = n
	}
	out := make([][2]lin.Vec2, count)
	for i := range count {
		out[i] = [2]lin.Vec2{world[i], world[(i+1)%n]}
	}
	return out
}

// edgeThickness is half the thickness edges are given when clipped
// against polygons, so the separating-axis test has a face to use.
const edgeThickness = 0.005

// prim2 is a placed collision primitive: a circle, a polygon, a capsule
// (segment a-b with radius r) or a bare segment.
type prim2 struct {
	kind int
	c    lin.Vec2
	r    float32
	a, b lin.Vec2
	pts  []lin.Vec2
}

const (
	primCircle = iota
	primPolygon
	primCapsule
	primSegment
)

// prims2 appends the primitives a placed shape breaks into to out; lo
// and hi are the other shape's bounds, used to skip chain edges that
// cannot touch it. A polygon's world points are appended to pts, which
// the caller keeps between calls so the split allocates nothing.
func prims2(out []prim2, pts *[]lin.Vec2, s Shape2, pos lin.Vec2, rot float32, lo, hi lin.Vec2) []prim2 {
	switch sh := s.(type) {
	case Circle:
		return append(out, prim2{kind: primCircle, c: pos, r: sh.Radius})
	case Box2:
		*pts = worldPolygon((*pts)[:0], sh.polygon(), pos, rot)
		return append(out, prim2{kind: primPolygon, pts: *pts})
	case Polygon2:
		if len(sh.Points) < 3 {
			return out
		}
		*pts = worldPolygon((*pts)[:0], sh, pos, rot)
		return append(out, prim2{kind: primPolygon, pts: *pts})
	case Capsule2:
		a, b := sh.segment(pos, rot)
		return append(out, prim2{kind: primCapsule, a: a, b: b, r: sh.Radius})
	case Edge2:
		cs, sn := cosSin(rot)
		return append(out, prim2{kind: primSegment, a: rotate2(sh.A, cs, sn).Add(pos), b: rotate2(sh.B, cs, sn).Add(pos)})
	case Chain2:
		for _, seg := range sh.segments(pos, rot) {
			slo, shi := seg[0].Min(seg[1]), seg[0].Max(seg[1])
			if slo.X > hi.X || lo.X > shi.X || slo.Y > hi.Y || lo.Y > shi.Y {
				continue
			}
			out = append(out, prim2{kind: primSegment, a: seg[0], b: seg[1]})
		}
		return out
	}
	return out
}

// closestOnSegment2 is the point of segment ab nearest p.
func closestOnSegment2(p, a, b lin.Vec2) lin.Vec2 {
	e := b.Sub(a)
	ee := e.Dot(e)
	if ee <= 1e-12 {
		return a
	}
	t := lin.Clamp(p.Sub(a).Dot(e)/ee, 0, 1)
	return a.Add(e.Mul(t))
}

// closestOnSegments2 finds the closest points between two segments.
func closestOnSegments2(a0, a1, b0, b1 lin.Vec2) (lin.Vec2, lin.Vec2) {
	da, db := a1.Sub(a0), b1.Sub(b0)
	la, lb := da.Len(), db.Len()
	if la < 1e-6 {
		return a0, closestOnSegment2(a0, b0, b1)
	}
	if lb < 1e-6 {
		return closestOnSegment2(b0, a0, a1), b0
	}
	// Segments that cross meet at a point.
	denom := cross2(da, db)
	if math.Abs(float64(denom)) > 1e-9 {
		d := b0.Sub(a0)
		s := cross2(d, db) / denom
		t := cross2(d, da) / denom
		if s >= 0 && s <= 1 && t >= 0 && t <= 1 {
			p := a0.Add(da.Mul(s))
			return p, p
		}
	}
	// Otherwise the closest pair involves an end point.
	best := float32(math.Inf(1))
	var pa, pb lin.Vec2
	try := func(p, q lin.Vec2) {
		if d := p.Sub(q).Len(); d < best {
			best, pa, pb = d, p, q
		}
	}
	try(a0, closestOnSegment2(a0, b0, b1))
	try(a1, closestOnSegment2(a1, b0, b1))
	try(closestOnSegment2(b0, a0, a1), b0)
	try(closestOnSegment2(b1, a0, a1), b1)
	return pa, pb
}

// segmentQuad appends a segment thickened into a thin rectangle to dst.
func segmentQuad(dst []lin.Vec2, a, b lin.Vec2) []lin.Vec2 {
	n := b.Sub(a).Norm().Perp().Mul(edgeThickness)
	return append(dst, a.Sub(n), b.Sub(n), b.Add(n), a.Add(n))
}

// flip2 reverses the normals of the contacts added from start onwards.
func flip2(cs []contact2, start int) {
	for i := start; i < len(cs); i++ {
		cs[i].normal = cs[i].normal.Neg()
	}
}

// spot appends a contact between two circles at the given centres.
func spot(out []contact2, pa lin.Vec2, ra float32, pb lin.Vec2, rb float32) []contact2 {
	return circleCircle(out, Circle{ra}, pa, Circle{rb}, pb)
}

// collidePrims appends the contacts between two primitives to out, with
// normals from a to b.
func collidePrims(sc *scratch2, out []contact2, a, b prim2) []contact2 {
	if a.kind > b.kind {
		start := len(out)
		out = collidePrims(sc, out, b, a)
		flip2(out, start)
		return out
	}
	switch a.kind {
	case primCircle:
		switch b.kind {
		case primCircle:
			return spot(out, a.c, a.r, b.c, b.r)
		case primPolygon:
			return circlePolygon(sc, out, Circle{a.r}, a.c, b.pts)
		case primCapsule:
			return spot(out, a.c, a.r, closestOnSegment2(a.c, b.a, b.b), b.r)
		case primSegment:
			return spot(out, a.c, a.r, closestOnSegment2(a.c, b.a, b.b), 0)
		}
	case primPolygon:
		switch b.kind {
		case primPolygon:
			return polygonPolygon(sc, out, a.pts, b.pts)
		case primCapsule:
			return polygonCapsule(sc, out, a.pts, b)
		case primSegment:
			sc.quad = segmentQuad(sc.quad[:0], b.a, b.b)
			return polygonPolygon(sc, out, a.pts, sc.quad)
		}
	case primCapsule:
		switch b.kind {
		case primCapsule:
			start := len(out)
			pa, pb := closestOnSegments2(a.a, a.b, b.a, b.b)
			out = spot(out, pa, a.r, pb, b.r)
			for _, e := range [4][2]lin.Vec2{
				{a.a, closestOnSegment2(a.a, b.a, b.b)},
				{a.b, closestOnSegment2(a.b, b.a, b.b)},
				{closestOnSegment2(b.a, a.a, a.b), b.a},
				{closestOnSegment2(b.b, a.a, a.b), b.b},
			} {
				sc.tmp = spot(sc.tmp[:0], e[0], a.r, e[1], b.r)
				out = appendUnique2(out, start, sc.tmp)
			}
			return out
		case primSegment:
			start := len(out)
			pa, pb := closestOnSegments2(a.a, a.b, b.a, b.b)
			out = spot(out, pa, a.r, pb, 0)
			for _, e := range [2][2]lin.Vec2{
				{a.a, closestOnSegment2(a.a, b.a, b.b)},
				{a.b, closestOnSegment2(a.b, b.a, b.b)},
			} {
				sc.tmp = spot(sc.tmp[:0], e[0], a.r, e[1], 0)
				out = appendUnique2(out, start, sc.tmp)
			}
			return out
		}
	}
	return out
}

// appendUnique2 adds contacts whose points are not already present from
// start onwards.
func appendUnique2(out []contact2, start int, cs []contact2) []contact2 {
	for _, c := range cs {
		dup := false
		for _, o := range out[start:] {
			if o.point.Sub(c.point).Len() < 1e-4 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, c)
		}
	}
	return out
}

// polygonCapsule collides a polygon with a capsule: the capsule's round
// ends against the polygon, and the polygon's corners against the
// capsule's side.
func polygonCapsule(sc *scratch2, out []contact2, poly []lin.Vec2, c prim2) []contact2 {
	start := len(out)
	for _, end := range [2]lin.Vec2{c.a, c.b} {
		sc.tmp = circlePolygon(sc, sc.tmp[:0], Circle{c.r}, end, poly)
		flip2(sc.tmp, 0)
		out = appendUnique2(out, start, sc.tmp)
	}
	for _, v := range poly {
		q := closestOnSegment2(v, c.a, c.b)
		d := q.Sub(v)
		dist := d.Len()
		if dist >= c.r || dist < 1e-6 {
			continue
		}
		n := d.Mul(1 / dist) // from the polygon corner into the capsule
		sc.tmp = append(sc.tmp[:0], contact2{point: v, normal: n, depth: c.r - dist})
		out = appendUnique2(out, start, sc.tmp)
	}
	return out
}

// raySegment intersects a ray with a segment, returning t along the ray
// and the segment's normal facing the ray.
func raySegment(r Ray2, a, b lin.Vec2) (float32, lin.Vec2, bool) {
	e := b.Sub(a)
	denom := cross2(r.Dir, e)
	if math.Abs(float64(denom)) < 1e-12 {
		return 0, lin.Vec2{}, false
	}
	d := a.Sub(r.Origin)
	t := cross2(d, e) / denom
	s := cross2(d, r.Dir) / denom
	if t < 0 || t > 1 || s < 0 || s > 1 {
		return 0, lin.Vec2{}, false
	}
	n := e.Perp().Norm()
	if n.Dot(r.Dir) > 0 {
		n = n.Neg()
	}
	return t, n, true
}

// rayCapsule2 casts a ray at a capsule: its two ends and its sides.
func rayCapsule2(r Ray2, a, b lin.Vec2, radius float32) (float32, lin.Vec2, bool) {
	best, found := float32(math.Inf(1)), false
	var bestN lin.Vec2
	consider := func(t float32, n lin.Vec2, ok bool) {
		if ok && t < best {
			best, bestN, found = t, n, true
		}
	}
	consider(rayShape2(r, Circle{radius}, a, 0))
	consider(rayShape2(r, Circle{radius}, b, 0))
	side := b.Sub(a).Norm().Perp().Mul(radius)
	consider(raySegment(r, a.Add(side), b.Add(side)))
	consider(raySegment(r, a.Sub(side), b.Sub(side)))
	return best, bestN, found
}

// closestPoint2 finds the point of a placed shape nearest p and the
// distance to it, zero when p is inside.
func closestPoint2(s Shape2, pos lin.Vec2, rot float32, p lin.Vec2) (lin.Vec2, float32) {
	best := float32(math.Inf(1))
	var bestP lin.Vec2
	r := lin.V2(best, best)
	var pts []lin.Vec2
	for _, pr := range prims2(nil, &pts, s, pos, rot, r.Neg(), r) {
		var q lin.Vec2
		var d float32
		switch pr.kind {
		case primCircle:
			dir := p.Sub(pr.c)
			l := dir.Len()
			if l <= pr.r {
				q, d = p, 0
			} else {
				q, d = pr.c.Add(dir.Mul(pr.r/l)), l-pr.r
			}
		case primPolygon:
			q, d = closestPointPolygon(p, pr.pts)
		case primCapsule:
			c := closestOnSegment2(p, pr.a, pr.b)
			dir := p.Sub(c)
			l := dir.Len()
			if l <= pr.r {
				q, d = p, 0
			} else {
				q, d = c.Add(dir.Mul(pr.r/l)), l-pr.r
			}
		case primSegment:
			q = closestOnSegment2(p, pr.a, pr.b)
			d = q.Sub(p).Len()
		}
		if d < best {
			best, bestP = d, q
		}
	}
	if best == float32(math.Inf(1)) {
		return p, best
	}
	return bestP, best
}

// closestPointPolygon is the polygon point nearest p, or p itself with
// distance zero when p is inside.
func closestPointPolygon(p lin.Vec2, poly []lin.Vec2) (lin.Vec2, float32) {
	var nbuf [8]lin.Vec2
	normals := polygonNormals(nbuf[:0], poly)
	inside := true
	best := float32(math.Inf(1))
	var bestP lin.Vec2
	for i := range poly {
		if normals[i].Dot(p.Sub(poly[i])) > 0 {
			inside = false
		}
		q := closestOnSegment2(p, poly[i], poly[(i+1)%len(poly)])
		if d := q.Sub(p).Len(); d < best {
			best, bestP = d, q
		}
	}
	if inside {
		return p, 0
	}
	return bestP, best
}
