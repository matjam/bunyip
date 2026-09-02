package phys

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Capsule is a cylinder along the body's local Y axis with hemispherical
// ends: Radius around the axis, HalfHeight from the centre to the centre
// of each cap, so the whole is 2·(HalfHeight+Radius) tall.
type Capsule struct{ Radius, HalfHeight float32 }

func (c Capsule) bounds(pos lin.Vec3, rot mat3) (lin.Vec3, lin.Vec3) {
	a, b := c.segment(pos, rot)
	r := lin.V3(c.Radius, c.Radius, c.Radius)
	return a.Min(b).Sub(r), a.Max(b).Add(r)
}

func (c Capsule) inertia(mass float32) lin.Vec3 {
	r, h := c.Radius, 2*c.HalfHeight
	vc := math.Pi * r * r * h
	vs := 4.0 / 3.0 * math.Pi * r * r * r
	if vc+vs == 0 {
		return lin.Vec3{}
	}
	mc, ms := mass*vc/(vc+vs), mass*vs/(vc+vs)
	iy := mc*r*r/2 + ms*0.4*r*r
	ix := mc*(r*r/4+h*h/12) + ms*(0.4*r*r+(h/2+3*r/8)*(h/2+3*r/8))
	return lin.V3(ix, iy, ix)
}

// segment returns the world centres of the two caps.
func (c Capsule) segment(pos lin.Vec3, rot mat3) (lin.Vec3, lin.Vec3) {
	up := rot.axis(1).Mul(c.HalfHeight)
	return pos.Sub(up), pos.Add(up)
}

// ConvexHull is the convex volume around a set of points in the body's
// frame. Only the extreme points matter; interior ones are ignored by
// collision and may be left out. The points should surround the body's
// origin, which is its centre of mass.
type ConvexHull struct{ Points []lin.Vec3 }

func (h ConvexHull) bounds(pos lin.Vec3, rot mat3) (lin.Vec3, lin.Vec3) {
	lo := lin.V3(float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1)))
	hi := lo.Neg()
	for _, p := range h.Points {
		w := pos.Add(rot.mulVec(p))
		lo, hi = lo.Min(w), hi.Max(w)
	}
	return lo, hi
}

func (h ConvexHull) inertia(mass float32) lin.Vec3 {
	// The box around the points is a fair stand-in for a compact hull.
	var half lin.Vec3
	for _, p := range h.Points {
		half = half.Max(p.Abs())
	}
	return Box3{Half: half}.inertia(mass)
}

// world transforms the hull's points into place.
func (h ConvexHull) world(pos lin.Vec3, rot mat3) []lin.Vec3 {
	out := make([]lin.Vec3, len(h.Points))
	for i, p := range h.Points {
		out[i] = pos.Add(rot.mulVec(p))
	}
	return out
}

// placeConvex builds the support description of a placed shape, or false
// for shapes that are not one convex volume (meshes and compounds).
func placeConvex(s Shape3, pos lin.Vec3, rot mat3) (convex, bool) {
	switch sh := s.(type) {
	case Sphere:
		return pointConvex(pos, sh.Radius), true
	case Capsule:
		a, b := sh.segment(pos, rot)
		return segmentConvex(a, b, sh.Radius), true
	case Box3:
		o := obb{pos, rot, sh.Half}
		var pts []lin.Vec3
		for i := range 8 {
			pts = append(pts, o.corner(i))
		}
		return pointsConvex(pts, pos), true
	case ConvexHull:
		if len(sh.Points) == 0 {
			return convex{}, false
		}
		return pointsConvex(sh.world(pos, rot), pos), true
	}
	return convex{}, false
}

// corner returns one of the box's eight corners.
func (o obb) corner(i int) lin.Vec3 {
	sx, sy, sz := float32(1), float32(1), float32(1)
	if i&1 != 0 {
		sx = -1
	}
	if i&2 != 0 {
		sy = -1
	}
	if i&4 != 0 {
		sz = -1
	}
	return o.center.Add(o.rot.axis(0).Mul(sx * o.half.X)).Add(o.rot.axis(1).Mul(sy * o.half.Y)).Add(o.rot.axis(2).Mul(sz * o.half.Z))
}

// triangleConvex describes one world-space triangle.
func triangleConvex(a, b, c lin.Vec3) convex {
	return pointsConvex([]lin.Vec3{a, b, c}, a.Add(b).Add(c).Mul(1.0/3))
}

// penetration finds how far two grown convex shapes overlap: the normal
// from a to b, the depth, and a point midway between the deepest points.
func penetration(a, b *convex) (n lin.Vec3, depth float32, point lin.Vec3, ok bool) {
	r := gjk(a, b)
	total := a.margin + b.margin
	if !r.overlap {
		if r.dist >= total {
			return lin.Vec3{}, 0, lin.Vec3{}, false
		}
		if r.dist > 1e-6 {
			n = r.pb.Sub(r.pa).Mul(1 / r.dist)
		} else {
			n = b.center.Sub(a.center).Norm()
			if n == (lin.Vec3{}) {
				n = lin.V3(0, 1, 0)
			}
		}
		depth = total - r.dist
		point = r.pa.Add(n.Mul(a.margin)).Add(r.pb.Sub(n.Mul(b.margin))).Mul(0.5)
		return n, depth, point, true
	}
	en, ed, pa, pb, eok := epa(a, b, r)
	if !eok {
		n = b.center.Sub(a.center).Norm()
		if n == (lin.Vec3{}) {
			n = lin.V3(0, 1, 0)
		}
		return n, total, a.center.Add(b.center).Mul(0.5), true
	}
	// A slightly negative EPA distance means the cores just miss; the
	// margins may still meet.
	n, depth = en, ed+total
	if depth <= 0 {
		return lin.Vec3{}, 0, lin.Vec3{}, false
	}
	point = pa.Add(n.Mul(a.margin)).Add(pb.Sub(n.Mul(b.margin))).Mul(0.5)
	return n, depth, point, true
}

// convexContacts generates contacts between two grown convex shapes.
// With a forced normal (a mesh triangle's face) the depth is measured
// along it from the plane through planePoint instead.
func convexContacts(a, b *convex, forced *lin.Vec3, planePoint lin.Vec3) []contact3 {
	n, depth, point, ok := penetration(a, b)
	if !ok {
		return nil
	}
	if forced != nil {
		n = *forced
		deepest := a.support(n)
		depth = n.Dot(deepest.Sub(planePoint))
		if depth <= 0 {
			return nil
		}
		point = deepest.Sub(n.Mul(depth / 2))
	}
	return manifold(a, b, n, depth, point)
}

// manifold turns one penetration into a set of contacts: the incident
// face of one shape clipped to the reference face of the other when both
// have flat faces there, the closest points of two edges, or the single
// deepest point otherwise.
func manifold(a, b *convex, n lin.Vec3, depth float32, point lin.Vec3) []contact3 {
	single := []contact3{{point: point, normal: n, depth: depth}}
	fa, fb := a.face(n), b.face(n.Neg())
	if len(fa) < 2 || len(fb) < 2 {
		return single
	}
	if len(fa) == 2 && len(fb) == 2 {
		da, db := fa[1].Sub(fa[0]), fb[1].Sub(fb[0])
		la, lb := da.Len(), db.Len()
		if la < 1e-6 || lb < 1e-6 {
			return single
		}
		ca, cb := closestOnSegments(fa[0].Add(fa[1]).Mul(0.5), da.Mul(1/la), la/2, fb[0].Add(fb[1]).Mul(0.5), db.Mul(1/lb), lb/2)
		d := ca.Sub(cb).Dot(n) + a.margin + b.margin
		if d < 0 {
			return single
		}
		return []contact3{{point: ca.Add(cb).Mul(0.5), normal: n, depth: d}}
	}
	ref, inc, refN := fa, fb, n
	if len(fa) < 3 {
		ref, inc, refN = fb, fa, n.Neg()
	}
	poly := append([]lin.Vec3(nil), inc...)
	for i := range ref {
		e := ref[(i+1)%len(ref)].Sub(ref[i])
		side := e.Cross(refN)
		if side.Len() < 1e-9 {
			continue
		}
		side = side.Norm()
		poly = clipPolygon(poly, side, side.Dot(ref[i]))
	}
	planeD := refN.Dot(ref[0])
	var out []contact3
	deepest := contact3{depth: float32(math.Inf(-1))}
	for _, p := range poly {
		d := planeD - refN.Dot(p) + a.margin + b.margin
		c := contact3{point: p, normal: n, depth: d}
		if d > deepest.depth {
			deepest = c
		}
		if d >= 0 && len(out) < 8 {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		if deepest.depth == float32(math.Inf(-1)) {
			return single
		}
		return []contact3{deepest}
	}
	return out
}

// pairContacts collides two convex shapes, adding each capsule's end
// spheres so a capsule lying on a face rests on two points.
func pairContacts(a, b *convex, forced *lin.Vec3, planePoint lin.Vec3) []contact3 {
	out := convexContacts(a, b, forced, planePoint)
	addUnique := func(cs []contact3) {
		for _, c := range cs {
			dup := false
			for _, o := range out {
				if o.point.Sub(c.point).Len() < 1e-3*(a.size+b.size) {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, c)
			}
		}
	}
	for _, e := range a.ends {
		end := pointConvex(e, a.margin)
		addUnique(convexContacts(&end, b, forced, planePoint))
	}
	for _, e := range b.ends {
		end := pointConvex(e, b.margin)
		addUnique(convexContacts(a, &end, forced, planePoint))
	}
	return out
}

// convexPair collides two placed shapes through their support functions.
func convexPair(sa Shape3, pa lin.Vec3, ra mat3, sb Shape3, pb lin.Vec3, rb mat3) []contact3 {
	a, ok := placeConvex(sa, pa, ra)
	if !ok {
		return nil
	}
	b, ok := placeConvex(sb, pb, rb)
	if !ok {
		return nil
	}
	return pairContacts(&a, &b, nil, lin.Vec3{})
}

// rayConvex casts a ray at a placed convex shape.
func rayConvex(r Ray3, c *convex) (float32, lin.Vec3, bool) {
	t, n, ok := castRay(c.support, r.Origin, r.Dir, c.size)
	if !ok || n == (lin.Vec3{}) {
		return 0, lin.Vec3{}, false
	}
	return t, n, true
}

// sweepConvex moves shape a by delta and reports the fraction at which it
// first touches b, with b's surface normal there and the touching point.
func sweepConvex(a, b *convex, delta lin.Vec3) (t float32, normal, point lin.Vec3, ok bool) {
	sup := func(dir lin.Vec3) lin.Vec3 { return b.support(dir).Sub(a.support(dir.Neg())) }
	t, normal, ok = castRay(sup, lin.Vec3{}, delta, a.size+b.size)
	if !ok || normal == (lin.Vec3{}) {
		return 0, lin.Vec3{}, lin.Vec3{}, false
	}
	moved := *a
	base := a.sup
	moved.sup = func(dir lin.Vec3) lin.Vec3 { return base(dir).Add(delta.Mul(t)) }
	moved.center = a.center.Add(delta.Mul(t))
	r := gjk(&moved, b)
	point = r.pb.Add(normal.Mul(b.margin))
	if r.overlap {
		point = moved.center.Sub(normal.Mul(a.margin))
	}
	return t, normal, point, true
}

// closestPointConvex finds the point of the grown shape nearest p and
// the distance to it, zero when p is inside.
func closestPointConvex(c *convex, p lin.Vec3) (lin.Vec3, float32) {
	pt := pointConvex(p, 0)
	r := gjk(&pt, c)
	if r.overlap || r.dist <= c.margin {
		return p, 0
	}
	n := r.pb.Sub(r.pa).Mul(1 / r.dist)
	return r.pb.Sub(n.Mul(c.margin)), r.dist - c.margin
}
