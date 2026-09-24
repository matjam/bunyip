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

// PlacedShape3 is a shape placed in the world once, so that many points
// can be measured against it without placing it again for each one: the
// rotation, a box's inverse rotation and a capsule's segment are worked
// out when it is placed. The zero value measures nothing.
type PlacedShape3 struct {
	shape  Shape3
	pos    lin.Vec3
	rot    mat3
	inv    mat3     // the inverse rotation, for a box
	a, b   lin.Vec3 // a capsule's cap centres
	lo, hi lin.Vec3
	ok     bool
}

// PlaceShape3 places a shape at pos with rotation rot for measuring. A
// zero rot is the identity. To measure many points against one collider,
// such as every particle of a cloth against a solid, call it once and
// call SignedDistance for each point. A compound still places its parts
// on each call.
func PlaceShape3(s Shape3, pos lin.Vec3, rot lin.Quat) PlacedShape3 {
	p := PlacedShape3{shape: s, pos: pos, rot: mat3FromQuat(rot)}
	switch sh := s.(type) {
	case Sphere, Compound3:
		p.ok = true
	case Box3:
		p.inv, p.ok = p.rot.transpose(), true
	case Capsule:
		p.a, p.b = sh.segment(pos, p.rot)
		p.ok = true
	}
	if s != nil {
		p.lo, p.hi = s.bounds(pos, p.rot)
	}
	return p
}

// SignedDistance measures a point against the placed shape. It returns
// exactly what SignedDistance3 returns for the shape, position and
// rotation the shape was placed with, and ok false for the same shapes.
func (p *PlacedShape3) SignedDistance(point lin.Vec3) (dist float32, normal lin.Vec3, ok bool) {
	if !p.ok {
		return 0, lin.Vec3{}, false
	}
	switch sh := p.shape.(type) {
	case Box3:
		return boxDistance3(point, p.pos, p.rot, p.inv, sh.Half)
	case Capsule:
		return capsuleDistance3(point, p.a, p.b, sh.Radius, p.rot)
	}
	return signedDistance3(p.shape, p.pos, p.rot, point)
}

// Bounds returns the world box around the placed shape, the region
// outside which a point is at least as far from the shape as it is from
// the box. A nil shape has empty bounds at the origin.
func (p *PlacedShape3) Bounds() (lo, hi lin.Vec3) { return p.lo, p.hi }

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
		return boxDistance3(p, pos, rot, rot.transpose(), sh.Half)
	case Capsule:
		a, b := sh.segment(pos, rot)
		return capsuleDistance3(p, a, b, sh.Radius, rot)
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

// capsuleDistance3 is the signed distance to a capsule whose cap centres
// are a and b, negative inside.
func capsuleDistance3(p, a, b lin.Vec3, radius float32, rot mat3) (float32, lin.Vec3, bool) {
	q := segmentPoint3(p, a, b)
	d := p.Sub(q)
	l := d.Len()
	if l < 1e-9 {
		return -radius, rot.axis(0), true
	}
	return l - radius, d.Mul(1 / l), true
}

// boxDistance3 is the exact signed distance to a box, negative inside;
// inv is the transpose of rot.
func boxDistance3(p, pos lin.Vec3, rot, inv mat3, half lin.Vec3) (float32, lin.Vec3, bool) {
	local := inv.mulVec(p.Sub(pos))
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
		return circleDistance2(point, pos, sh.Radius)
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
		return capsuleDistance2(point, a, b, sh.Radius)
	}
	return 0, lin.Vec2{}, false
}

// PlacedShape2 is a 2D shape placed in the world once, so that many
// points can be measured against it without placing it again for each
// one: a polygon's world points and edge normals and a capsule's segment
// are worked out when it is placed. The zero value measures nothing.
type PlacedShape2 struct {
	shape Shape2
	pos   lin.Vec2
	rot   float32
	// A box's or polygon's world points and outward edge normals: the
	// first n of each half of buf, or poly and normals past the size buf
	// holds.
	n             int
	buf           [2 * placedPolyBuf]lin.Vec2
	poly, normals []lin.Vec2
	a, b          lin.Vec2 // a capsule's end centres
	lo, hi        lin.Vec2
	ok            bool
}

// placedPolyBuf is how many polygon points a PlacedShape2 holds without
// allocating.
const placedPolyBuf = 16

// PlaceShape2 places a shape at pos with rotation rot, in radians, for
// measuring. To measure many points against one collider, such as every
// particle of a fluid against a wall, call it once and call
// SignedDistance for each point. A polygon of more than sixteen points
// allocates when placed.
func PlaceShape2(s Shape2, pos lin.Vec2, rot float32) PlacedShape2 {
	p := PlacedShape2{shape: s, pos: pos, rot: rot}
	switch sh := s.(type) {
	case Circle:
		p.ok = true
	case Box2:
		p.placePolygon(sh.polygon())
	case Polygon2:
		if len(sh.Points) >= 3 {
			p.placePolygon(sh)
		}
	case Capsule2:
		p.a, p.b = sh.segment(pos, rot)
		p.ok = true
	}
	if s != nil {
		p.lo, p.hi = s.bounds(pos, rot)
	}
	return p
}

// placePolygon places a polygon's points and normals, in the value's
// own buffer when they fit. No slice of the buffer is kept, so a copy of
// the value measures from its own copy of the points.
func (p *PlacedShape2) placePolygon(poly Polygon2) {
	p.n = len(poly.Points)
	if p.n <= placedPolyBuf {
		pts := worldPolygon(p.buf[:0:placedPolyBuf], poly, p.pos, p.rot)
		polygonNormals(p.buf[placedPolyBuf:placedPolyBuf], pts)
	} else {
		p.poly = worldPolygon(make([]lin.Vec2, 0, p.n), poly, p.pos, p.rot)
		p.normals = polygonNormals(make([]lin.Vec2, 0, p.n), p.poly)
	}
	p.ok = true
}

// SignedDistance measures a point against the placed shape. It returns
// exactly what SignedDistance2 returns for the shape, position and
// rotation the shape was placed with, and ok false for the same shapes.
func (p *PlacedShape2) SignedDistance(point lin.Vec2) (dist float32, normal lin.Vec2, ok bool) {
	if !p.ok {
		return 0, lin.Vec2{}, false
	}
	switch sh := p.shape.(type) {
	case Circle:
		return circleDistance2(point, p.pos, sh.Radius)
	case Capsule2:
		return capsuleDistance2(point, p.a, p.b, sh.Radius)
	}
	if p.n <= placedPolyBuf {
		return polygonDistanceNormals2(p.buf[:p.n], p.buf[placedPolyBuf:placedPolyBuf+p.n], point)
	}
	return polygonDistanceNormals2(p.poly, p.normals, point)
}

// Bounds returns the world box around the placed shape.
func (p *PlacedShape2) Bounds() (lo, hi lin.Vec2) { return p.lo, p.hi }

func circleDistance2(point, pos lin.Vec2, radius float32) (float32, lin.Vec2, bool) {
	d := point.Sub(pos)
	l := d.Len()
	if l < 1e-9 {
		return -radius, lin.V2(0, -1), true
	}
	return l - radius, d.Mul(1 / l), true
}

func capsuleDistance2(point, a, b lin.Vec2, radius float32) (float32, lin.Vec2, bool) {
	q := closestOnSegment2(point, a, b)
	d := point.Sub(q)
	l := d.Len()
	if l < 1e-9 {
		return -radius, lin.V2(0, -1), true
	}
	return l - radius, d.Mul(1 / l), true
}

// polygonDistance2 is the signed distance to a convex polygon in world
// space, negative inside.
func polygonDistance2(poly []lin.Vec2, p lin.Vec2) (float32, lin.Vec2, bool) {
	var nbuf [16]lin.Vec2
	return polygonDistanceNormals2(poly, polygonNormals(nbuf[:0], poly), p)
}

// polygonDistanceNormals2 is polygonDistance2 with the edge normals
// already worked out.
func polygonDistanceNormals2(poly, normals []lin.Vec2, p lin.Vec2) (float32, lin.Vec2, bool) {
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
