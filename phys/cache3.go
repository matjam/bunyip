package phys

import (
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/lin"
)

// shapeCache3 keeps each collider's convex parts in world space between
// queries, indexed by the entity's slot the way slotMap is. A sweep, a
// shape cast or a character controller that runs against the same static
// geometry every frame places each of those shapes once instead of once
// per query.
//
// A row is placed again when the collider's position, rotation, kind or
// world bounds change, which covers moving it, turning it and resizing
// it. A shape whose points move without any of those changing, such as a
// ConvexHull edited in place, must be assigned to the collider again for
// the cache to notice.
type shapeCache3 struct {
	rows []cachedParts3
}

type cachedParts3 struct {
	parts  []convexPart
	pos    lin.Vec3
	rot    mat3
	lo, hi lin.Vec3
	kind   uint8
	valid  bool
}

// shapeKinds are the concrete shapes a cached row may hold. The kind is
// stored so swapping one shape for another of the same size and place is
// noticed.
func shapeKind3(s Shape3) uint8 {
	switch s.(type) {
	case Sphere:
		return 1
	case Box3:
		return 2
	case Capsule:
		return 3
	case ConvexHull:
		return 4
	case Compound3:
		return 5
	case MeshShape:
		return 6
	}
	return 0
}

// parts returns the collider's world-space convex parts, placing them
// again only when the collider has moved, turned or changed. The result
// points into the cache and stays valid until the same entity is asked
// for again.
func (c *shapeCache3) parts(e ecs.Entity, s Shape3, pos lin.Vec3, rot mat3, lo, hi lin.Vec3) []convexPart {
	i := slot(e)
	for len(c.rows) <= i {
		c.rows = append(c.rows, cachedParts3{})
	}
	r := &c.rows[i]
	kind := shapeKind3(s)
	if r.valid && r.kind == kind && r.pos == pos && r.rot == rot && r.lo == lo && r.hi == hi {
		return r.parts
	}
	r.parts = appendConvexParts(r.parts[:0], s, pos, rot)
	r.pos, r.rot, r.lo, r.hi, r.kind, r.valid = pos, rot, lo, hi, kind, true
	return r.parts
}
