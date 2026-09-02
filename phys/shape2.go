package phys

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// Shape2 is a collider outline in the body's local frame.
type Shape2 interface {
	// bounds returns the axis-aligned box of the shape placed in the world.
	bounds(pos lin.Vec2, rot float32) (lo, hi lin.Vec2)
	// inertia returns the moment of inertia for the given mass.
	inertia(mass float32) float32
}

// Circle is a disc centred on the body.
type Circle struct{ Radius float32 }

func (c Circle) bounds(pos lin.Vec2, _ float32) (lin.Vec2, lin.Vec2) {
	r := lin.V2(c.Radius, c.Radius)
	return pos.Sub(r), pos.Add(r)
}

func (c Circle) inertia(mass float32) float32 { return 0.5 * mass * c.Radius * c.Radius }

// Box2 is a rectangle centred on the body with the given half extents.
type Box2 struct{ HalfW, HalfH float32 }

func (b Box2) polygon() Polygon2 {
	return Polygon2{Points: []lin.Vec2{{X: -b.HalfW, Y: -b.HalfH}, {X: b.HalfW, Y: -b.HalfH}, {X: b.HalfW, Y: b.HalfH}, {X: -b.HalfW, Y: b.HalfH}}}
}

func (b Box2) bounds(pos lin.Vec2, rot float32) (lin.Vec2, lin.Vec2) {
	return b.polygon().bounds(pos, rot)
}

func (b Box2) inertia(mass float32) float32 {
	w, h := 2*b.HalfW, 2*b.HalfH
	return mass * (w*w + h*h) / 12
}

// Polygon2 is a convex polygon with points in the body's frame, in any
// winding order.
type Polygon2 struct{ Points []lin.Vec2 }

func (p Polygon2) bounds(pos lin.Vec2, rot float32) (lin.Vec2, lin.Vec2) {
	lo := lin.V2(float32(math.Inf(1)), float32(math.Inf(1)))
	hi := lo.Mul(-1)
	c, s := cosSin(rot)
	for _, pt := range p.Points {
		w := rotate2(pt, c, s).Add(pos)
		lo = lin.V2(min(lo.X, w.X), min(lo.Y, w.Y))
		hi = lin.V2(max(hi.X, w.X), max(hi.Y, w.Y))
	}
	return lo, hi
}

func (p Polygon2) inertia(mass float32) float32 {
	// Polygon inertia about the origin (Green's theorem).
	var num, den float32
	n := len(p.Points)
	for i := range n {
		a, b := p.Points[i], p.Points[(i+1)%n]
		cross := cross2(a, b)
		num += cross * (a.Dot(a) + a.Dot(b) + b.Dot(b))
		den += cross
	}
	if den == 0 {
		return 0
	}
	return mass * num / (6 * den)
}

func cosSin(rot float32) (float32, float32) {
	return float32(math.Cos(float64(rot))), float32(math.Sin(float64(rot)))
}

func rotate2(v lin.Vec2, c, s float32) lin.Vec2 {
	return lin.V2(c*v.X-s*v.Y, s*v.X+c*v.Y)
}

func cross2(a, b lin.Vec2) float32 { return a.X*b.Y - a.Y*b.X }

// crossSV is ω × v for a scalar angular velocity.
func crossSV(w float32, v lin.Vec2) lin.Vec2 { return lin.V2(-w*v.Y, w*v.X) }

// contact2 is one contact point between two colliders.
type contact2 struct {
	point  lin.Vec2
	normal lin.Vec2 // from A to B
	depth  float32
}

// worldPolygon transforms polygon points into the world.
func worldPolygon(p Polygon2, pos lin.Vec2, rot float32) []lin.Vec2 {
	c, s := cosSin(rot)
	out := make([]lin.Vec2, len(p.Points))
	for i, pt := range p.Points {
		out[i] = rotate2(pt, c, s).Add(pos)
	}
	return out
}

// polygonNormals returns outward unit normals for each edge.
func polygonNormals(pts []lin.Vec2) []lin.Vec2 {
	var centroid lin.Vec2
	for _, p := range pts {
		centroid = centroid.Add(p)
	}
	centroid = centroid.Mul(1 / float32(len(pts)))
	out := make([]lin.Vec2, len(pts))
	for i := range pts {
		a, b := pts[i], pts[(i+1)%len(pts)]
		e := b.Sub(a)
		n := lin.V2(e.Y, -e.X).Norm()
		if n.Dot(a.Sub(centroid)) < 0 {
			n = n.Mul(-1)
		}
		out[i] = n
	}
	return out
}

// collide2 generates contacts between two placed shapes; normals point
// from A to B. Each shape becomes circles, polygons, capsules and
// segments (a chain contributes only the edges near the other shape)
// and every pair of those is tested.
func collide2(sa Shape2, pa lin.Vec2, ra float32, sb Shape2, pb lin.Vec2, rb float32) []contact2 {
	alo, ahi := sa.bounds(pa, ra)
	blo, bhi := sb.bounds(pb, rb)
	pa2 := prims2(sa, pa, ra, blo, bhi)
	pb2 := prims2(sb, pb, rb, alo, ahi)
	if len(pa2) == 1 && len(pb2) == 1 {
		return collidePrims(pa2[0], pb2[0])
	}
	var out []contact2
	for _, a := range pa2 {
		for _, b := range pb2 {
			out = append(out, collidePrims(a, b)...)
		}
	}
	return out
}

func circleCircle(a Circle, pa lin.Vec2, b Circle, pb lin.Vec2) []contact2 {
	d := pb.Sub(pa)
	dist := d.Len()
	r := a.Radius + b.Radius
	if dist >= r {
		return nil
	}
	n := lin.V2(0, 1)
	if dist > 1e-6 {
		n = d.Mul(1 / dist)
	}
	return []contact2{{point: pa.Add(n.Mul(a.Radius)), normal: n, depth: r - dist}}
}

func circlePolygon(c Circle, pc lin.Vec2, poly []lin.Vec2) []contact2 {
	normals := polygonNormals(poly)
	// The face the centre is furthest outside of.
	best, bestSep := 0, float32(math.Inf(-1))
	for i := range poly {
		sep := normals[i].Dot(pc.Sub(poly[i]))
		if sep > c.Radius {
			return nil
		}
		if sep > bestSep {
			best, bestSep = i, sep
		}
	}
	a, b := poly[best], poly[(best+1)%len(poly)]
	if bestSep < 0 {
		// Centre inside: push out along that face's normal.
		n := normals[best]
		return []contact2{{point: pc.Sub(n.Mul(c.Radius)), normal: n.Mul(-1), depth: c.Radius - bestSep}}
	}
	// Outside: closest point on the face segment.
	e := b.Sub(a)
	t := lin.Clamp(pc.Sub(a).Dot(e)/e.Dot(e), 0, 1)
	closest := a.Add(e.Mul(t))
	d := pc.Sub(closest)
	dist := d.Len()
	if dist >= c.Radius || dist < 1e-6 {
		return nil
	}
	n := d.Mul(1 / dist)
	// Normal from the circle (A) to the polygon (B).
	return []contact2{{point: closest, normal: n.Mul(-1), depth: c.Radius - dist}}
}

// polygonPolygon is the separating-axis test with reference-face
// clipping, producing up to two contact points.
func polygonPolygon(pa, pb []lin.Vec2) []contact2 {
	na, nb := polygonNormals(pa), polygonNormals(pb)
	faceA, sepA := bestFace(pa, na, pb)
	if sepA > 0 {
		return nil
	}
	faceB, sepB := bestFace(pb, nb, pa)
	if sepB > 0 {
		return nil
	}
	// The reference polygon is the one with the larger separation, with
	// a small preference for A so the result is stable frame to frame.
	ref, inc, refNormals, refFace, flip := pa, pb, na, faceA, false
	if sepB > sepA+1e-4 {
		ref, inc, refNormals, refFace, flip = pb, pa, nb, faceB, true
	}
	n := refNormals[refFace]
	// Incident face: the one on inc most opposed to n.
	incNormals := polygonNormals(inc)
	incFace, best := 0, float32(math.Inf(1))
	for i, in := range incNormals {
		if d := in.Dot(n); d < best {
			incFace, best = i, d
		}
	}
	v1, v2 := inc[incFace], inc[(incFace+1)%len(inc)]
	r1, r2 := ref[refFace], ref[(refFace+1)%len(ref)]
	side := r2.Sub(r1).Norm()
	// Clip the incident edge to the reference face's side planes.
	pts := []lin.Vec2{v1, v2}
	pts = clipSegment(pts, side.Mul(-1), -side.Dot(r1))
	if len(pts) < 2 {
		return nil
	}
	pts = clipSegment(pts, side, side.Dot(r2))
	if len(pts) < 2 {
		return nil
	}
	var out []contact2
	for _, p := range pts {
		depth := -n.Dot(p.Sub(r1))
		if depth >= 0 {
			normal := n
			if flip {
				normal = n.Mul(-1)
			}
			out = append(out, contact2{point: p, normal: normal, depth: depth})
		}
	}
	return out
}

// bestFace finds the face of poly whose normal gives the largest
// separation from other (least penetration).
func bestFace(poly, normals, other []lin.Vec2) (int, float32) {
	best, bestSep := 0, float32(math.Inf(-1))
	for i := range poly {
		sep := float32(math.Inf(1))
		for _, o := range other {
			sep = min(sep, normals[i].Dot(o.Sub(poly[i])))
		}
		if sep > bestSep {
			best, bestSep = i, sep
		}
	}
	return best, bestSep
}

// clipSegment keeps the part of a two-point segment with n·p <= c.
func clipSegment(pts []lin.Vec2, n lin.Vec2, c float32) []lin.Vec2 {
	var out []lin.Vec2
	d0 := n.Dot(pts[0]) - c
	d1 := n.Dot(pts[1]) - c
	if d0 <= 0 {
		out = append(out, pts[0])
	}
	if d1 <= 0 {
		out = append(out, pts[1])
	}
	if d0*d1 < 0 {
		t := d0 / (d0 - d1)
		out = append(out, pts[0].Add(pts[1].Sub(pts[0]).Mul(t)))
	}
	return out
}

// Ray2 is a ray for casts.
type Ray2 struct {
	Origin, Dir lin.Vec2 // Dir need not be unit length
}

// rayShape2 intersects a ray with a placed shape, returning the nearest
// t along the ray in [0, 1] (for Dir as the full extent) and the normal.
func rayShape2(r Ray2, s Shape2, pos lin.Vec2, rot float32) (t float32, normal lin.Vec2, ok bool) {
	switch sh := s.(type) {
	case Circle:
		m := r.Origin.Sub(pos)
		a := r.Dir.Dot(r.Dir)
		b := 2 * m.Dot(r.Dir)
		c := m.Dot(m) - sh.Radius*sh.Radius
		disc := b*b - 4*a*c
		if disc < 0 || a == 0 {
			return 0, lin.Vec2{}, false
		}
		t = (-b - float32(math.Sqrt(float64(disc)))) / (2 * a)
		if t < 0 || t > 1 {
			return 0, lin.Vec2{}, false
		}
		hit := r.Origin.Add(r.Dir.Mul(t))
		return t, hit.Sub(pos).Norm(), true
	case Box2:
		return rayPolygon(r, worldPolygon(sh.polygon(), pos, rot))
	case Polygon2:
		return rayPolygon(r, worldPolygon(sh, pos, rot))
	case Capsule2:
		a, b := sh.segment(pos, rot)
		return rayCapsule2(r, a, b, sh.Radius)
	case Edge2:
		cs, sn := cosSin(rot)
		return raySegment(r, rotate2(sh.A, cs, sn).Add(pos), rotate2(sh.B, cs, sn).Add(pos))
	case Chain2:
		best, found := float32(math.Inf(1)), false
		var bestN lin.Vec2
		for _, seg := range sh.segments(pos, rot) {
			if t, n, ok := raySegment(r, seg[0], seg[1]); ok && t < best {
				best, bestN, found = t, n, true
			}
		}
		return best, bestN, found
	}
	return 0, lin.Vec2{}, false
}

func rayPolygon(r Ray2, poly []lin.Vec2) (float32, lin.Vec2, bool) {
	normals := polygonNormals(poly)
	tEnter, tExit := float32(0), float32(1)
	var enterNormal lin.Vec2
	for i, n := range normals {
		denom := n.Dot(r.Dir)
		dist := n.Dot(poly[i].Sub(r.Origin))
		if math.Abs(float64(denom)) < 1e-8 {
			if dist < 0 {
				return 0, lin.Vec2{}, false
			}
			continue
		}
		t := dist / denom
		if denom < 0 {
			if t > tEnter {
				tEnter, enterNormal = t, n
			}
		} else {
			tExit = min(tExit, t)
		}
		if tEnter > tExit {
			return 0, lin.Vec2{}, false
		}
	}
	if enterNormal == (lin.Vec2{}) {
		return 0, lin.Vec2{}, false // started inside
	}
	return tEnter, enterNormal, true
}
