package phys

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// SignedDistance3 measures a point against a shape placed at pos with
// rotation rot. It returns the distance from the point to the nearest
// surface, negative when the point is inside, and the unit normal there,
// pointing out of the shape. Use it to push a point out of a solid:
// move it along the normal until the distance reaches the clearance
// wanted.
//
// The shapes it understands are Sphere, Box3, Capsule and a Compound3 of
// those; ok is false for ConvexHull, MeshShape and a nil shape, which
// have no cheap signed distance. It allocates nothing, so it suits a
// per-particle inner loop.
func SignedDistance3(s Shape3, pos lin.Vec3, rot lin.Quat, point lin.Vec3) (dist float32, normal lin.Vec3, ok bool) {
	return signedDistance3(s, pos, mat3FromQuat(rot), point)
}

func signedDistance3(s Shape3, pos lin.Vec3, rot mat3, p lin.Vec3) (float32, lin.Vec3, bool) {
	switch sh := s.(type) {
	case Sphere:
		d := p.Sub(pos)
		l := d.Len()
		if l < 1e-9 {
			return -sh.Radius, lin.V3(0, 1, 0), true
		}
		return l - sh.Radius, d.Mul(1 / l), true
	case Box3:
		return boxDistance3(p, pos, rot, sh.Half)
	case Capsule:
		a, b := sh.segment(pos, rot)
		q := segmentPoint3(p, a, b)
		d := p.Sub(q)
		l := d.Len()
		if l < 1e-9 {
			return -sh.Radius, rot.axis(0), true
		}
		return l - sh.Radius, d.Mul(1 / l), true
	case Compound3:
		best, bestN, found := float32(math.Inf(1)), lin.Vec3{}, false
		for _, part := range sh.Parts {
			if part.Shape == nil {
				continue
			}
			pp, pr := part.place(pos, rot)
			if d, n, ok := signedDistance3(part.Shape, pp, pr, p); ok && d < best {
				best, bestN, found = d, n, true
			}
		}
		return best, bestN, found
	}
	return 0, lin.Vec3{}, false
}

// boxDistance3 is the exact signed distance to a box, negative inside.
func boxDistance3(p, pos lin.Vec3, rot mat3, half lin.Vec3) (float32, lin.Vec3, bool) {
	local := rot.transpose().mulVec(p.Sub(pos))
	q := local.Abs().Sub(half)
	out := q.Max(lin.Vec3{})
	if l := out.Len(); l > 0 {
		n := lin.V3(sign(local.X)*out.X, sign(local.Y)*out.Y, sign(local.Z)*out.Z).Mul(1 / l)
		return l, rot.mulVec(n), true
	}
	// Inside: leave through the face it is nearest to.
	axis, best := 0, q.X
	if q.Y > best {
		axis, best = 1, q.Y
	}
	if q.Z > best {
		axis, best = 2, q.Z
	}
	var n lin.Vec3
	switch axis {
	case 0:
		n = lin.V3(sign(local.X), 0, 0)
	case 1:
		n = lin.V3(0, sign(local.Y), 0)
	default:
		n = lin.V3(0, 0, sign(local.Z))
	}
	return best, rot.mulVec(n), true
}

// segmentPoint3 is the point of the segment ab nearest p.
func segmentPoint3(p, a, b lin.Vec3) lin.Vec3 {
	e := b.Sub(a)
	den := e.Dot(e)
	if den < 1e-12 {
		return a
	}
	return a.Add(e.Mul(lin.Clamp(p.Sub(a).Dot(e)/den, 0, 1)))
}

// SignedDistance2 measures a point against a shape placed at pos with
// rotation rot. It returns the distance from the point to the nearest
// outline, negative when the point is inside, and the unit normal there,
// pointing out of the shape. Use it to push a point out of a solid:
// move it along the normal until the distance reaches the clearance
// wanted.
//
// The shapes it understands are Circle, Box2, Polygon2 and Capsule2; ok
// is false for Edge2, Chain2 and a nil shape, which have no inside. It
// allocates nothing for polygons of up to sixteen points, so it suits a
// per-particle inner loop.
func SignedDistance2(s Shape2, pos lin.Vec2, rot float32, point lin.Vec2) (dist float32, normal lin.Vec2, ok bool) {
	switch sh := s.(type) {
	case Circle:
		d := point.Sub(pos)
		l := d.Len()
		if l < 1e-9 {
			return -sh.Radius, lin.V2(0, -1), true
		}
		return l - sh.Radius, d.Mul(1 / l), true
	case Box2:
		var buf [4]lin.Vec2
		return polygonDistance2(worldPolygon(buf[:0], sh.polygon(), pos, rot), point)
	case Polygon2:
		if len(sh.Points) < 3 {
			return 0, lin.Vec2{}, false
		}
		var buf [16]lin.Vec2
		return polygonDistance2(worldPolygon(buf[:0], sh, pos, rot), point)
	case Capsule2:
		a, b := sh.segment(pos, rot)
		q := closestOnSegment2(point, a, b)
		d := point.Sub(q)
		l := d.Len()
		if l < 1e-9 {
			return -sh.Radius, lin.V2(0, -1), true
		}
		return l - sh.Radius, d.Mul(1 / l), true
	}
	return 0, lin.Vec2{}, false
}

// polygonDistance2 is the signed distance to a convex polygon in world
// space, negative inside.
func polygonDistance2(poly []lin.Vec2, p lin.Vec2) (float32, lin.Vec2, bool) {
	var nbuf [16]lin.Vec2
	normals := polygonNormals(nbuf[:0], poly)
	face, sep := 0, float32(math.Inf(-1))
	for i := range poly {
		if d := normals[i].Dot(p.Sub(poly[i])); d > sep {
			face, sep = i, d
		}
	}
	if sep <= 0 {
		return sep, normals[face], true
	}
	// Outside: the nearest point on the outline decides the normal, which
	// is the face normal beside a face and a corner direction past one.
	best, bestQ := float32(math.Inf(1)), p
	for i := range poly {
		q := closestOnSegment2(p, poly[i], poly[(i+1)%len(poly)])
		if d := q.Sub(p).Len(); d < best {
			best, bestQ = d, q
		}
	}
	if best < 1e-9 {
		return best, normals[face], true
	}
	return best, p.Sub(bestQ).Mul(1 / best), true
}
