package phys

import (
	"slices"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/lin"
)

// shapeCache3 keeps each collider's convex parts in world space between
// queries, indexed by the entity's slot the way slotMap is. A sweep, a
// shape cast or a character controller that runs against the same static
// geometry every frame places each of those shapes once instead of once
// per query.
//
// A row holds an owned snapshot of its local geometry. Exact comparisons
// notice replacement shapes and edits to hull points or nested compound
// parts even when the world bounds stay the same. The full entity handle
// prevents a new occupant of a reused slot from inheriting the old row.
type shapeCache3 struct {
	rows []cachedParts3
}

type cachedParts3 struct {
	parts  []convexPart
	pos    lin.Vec3
	rot    mat3
	lo, hi lin.Vec3
	entity ecs.Entity
	shape  Shape3
	valid  bool
}

// sameGeometry3 compares against a snapshot, never against the caller's
// mutable slices. Geometry comparison allocates nothing and avoids the
// world-space transforms and convex construction on an unchanged query.
func sameGeometry3(a, b Shape3) bool {
	switch a := a.(type) {
	case nil:
		return b == nil
	case Sphere:
		b, ok := b.(Sphere)
		return ok && a == b
	case Box3:
		b, ok := b.(Box3)
		return ok && a == b
	case Capsule:
		b, ok := b.(Capsule)
		return ok && a == b
	case ConvexHull:
		b, ok := b.(ConvexHull)
		return ok && slices.Equal(a.Points, b.Points)
	case Compound3:
		b, ok := b.(Compound3)
		if !ok || len(a.Parts) != len(b.Parts) {
			return false
		}
		for i, p := range a.Parts {
			q := b.Parts[i]
			if p.Offset != q.Offset || p.Rotation != q.Rotation || !sameGeometry3(p.Shape, q.Shape) {
				return false
			}
		}
		return true
	case MeshShape:
		// Meshes contribute no convex parts; mesh queries use their own
		// triangle tree and retain the immutable-slice contract.
		_, ok := b.(MeshShape)
		return ok
	}
	return false
}

// snapshotGeometry3 owns every mutable slice that sameGeometry3 reads.
// It is called only when geometry changes, not when its placement changes.
func snapshotGeometry3(s Shape3) Shape3 {
	switch s := s.(type) {
	case ConvexHull:
		return ConvexHull{Points: slices.Clone(s.Points)}
	case Compound3:
		parts := slices.Clone(s.Parts)
		for i := range parts {
			parts[i].Shape = snapshotGeometry3(parts[i].Shape)
		}
		return Compound3{Parts: parts}
	case MeshShape:
		return MeshShape{}
	}
	return s
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
	same := sameGeometry3(s, r.shape)
	if r.valid && r.entity == e && same && r.pos == pos && r.rot == rot && r.lo == lo && r.hi == hi {
		return r.parts
	}
	r.parts = appendConvexParts(r.parts[:0], s, pos, rot)
	if !same {
		r.shape = snapshotGeometry3(s)
	}
	r.pos, r.rot, r.lo, r.hi, r.entity, r.valid = pos, rot, lo, hi, e, true
	return r.parts
}
