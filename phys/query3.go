package phys

import (
	"cmp"
	"math"
	"slices"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// placed3 is one collider with its world placement and bounds.
type placed3 struct {
	e      ecs.Entity
	c      *Collider3
	pos    lin.Vec3
	rot    mat3
	lo, hi lin.Vec3
}

// eachCollider3 calls fn for every collider whose bounds overlap the
// box, skipping triggers unless asked and colliders the mask excludes.
func eachCollider3(w *ecs.World, lo, hi lin.Vec3, mask uint32, triggers bool, fn func(p placed3)) {
	stateOf3(w).colliders.Each(func(e ecs.Entity, t *gfx.Transform, c *Collider3) {
		if c.Shape == nil || (c.Trigger && !triggers) || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		rot := mat3FromQuat(t.Rotation)
		pos := t.Position.Add(rot.mulVec(c.Offset))
		clo, chi := c.Shape.bounds(pos, rot)
		if clo.X > hi.X || lo.X > chi.X || clo.Y > hi.Y || lo.Y > chi.Y || clo.Z > hi.Z || lo.Z > chi.Z {
			return
		}
		fn(placed3{e: e, c: c, pos: pos, rot: rot, lo: clo, hi: chi})
	})
}

// convexParts breaks a placed shape into its convex pieces with their
// bounds; a mesh has none.
func convexParts(s Shape3, pos lin.Vec3, rot mat3) []convexPart {
	if c, ok := s.(Compound3); ok {
		var out []convexPart
		for _, p := range c.Parts {
			if p.Shape == nil {
				continue
			}
			pp, pr := p.place(pos, rot)
			out = append(out, convexParts(p.Shape, pp, pr)...)
		}
		return out
	}
	c, ok := placeConvex(s, pos, rot)
	if !ok {
		return nil
	}
	lo, hi := s.bounds(pos, rot)
	return []convexPart{{conv: c, lo: lo, hi: hi}}
}

type convexPart struct {
	conv   convex
	lo, hi lin.Vec3
}

// OverlapShape3 returns every collider the shape overlaps when placed at
// pos with rotation rot. Each hit carries the deepest contact: Point on
// the collider, Normal pointing from the collider back toward the shape
// and Distance the penetration depth. Triggers are included.
func OverlapShape3(w *ecs.World, s Shape3, pos lin.Vec3, rot lin.Quat, mask uint32) []Hit3 {
	return overlapShape3(w, s, pos, mat3FromQuat(rot), mask, true, ecs.None)
}

func overlapShape3(w *ecs.World, s Shape3, pos lin.Vec3, r mat3, mask uint32, triggers bool, exclude ecs.Entity) []Hit3 {
	lo, hi := s.bounds(pos, r)
	st := stateOf3(w)
	var out []Hit3
	eachCollider3(w, lo, hi, mask, triggers, func(p placed3) {
		if p.e == exclude {
			return
		}
		st.qs.contacts = collide3(&st.qs, st.qs.contacts[:0], s, pos, r, p.c.Shape, p.pos, p.rot)
		cs := st.qs.contacts
		if len(cs) == 0 {
			return
		}
		deepest := cs[0]
		for _, c := range cs[1:] {
			if c.depth > deepest.depth {
				deepest = c
			}
		}
		out = append(out, Hit3{Entity: p.e, Point: deepest.point, Normal: deepest.normal.Neg(), Distance: deepest.depth})
	})
	return out
}

// OverlapSphere3 returns every collider a sphere overlaps.
func OverlapSphere3(w *ecs.World, center lin.Vec3, radius float32, mask uint32) []Hit3 {
	return OverlapShape3(w, Sphere{Radius: radius}, center, lin.Quat{}, mask)
}

// OverlapBox3 returns every collider an oriented box overlaps.
func OverlapBox3(w *ecs.World, center, half lin.Vec3, rot lin.Quat, mask uint32) []Hit3 {
	return OverlapShape3(w, Box3{Half: half}, center, rot, mask)
}

// ShapeCast3 sweeps a shape from pos along delta and returns the first
// collider it touches: Distance is the fraction of delta travelled,
// Point where the surfaces meet and Normal the collider's surface
// normal there. Colliders already overlapping the shape at the start
// and triggers are ignored.
func ShapeCast3(w *ecs.World, s Shape3, pos lin.Vec3, rot lin.Quat, delta lin.Vec3, mask uint32) (Hit3, bool) {
	return shapeCast3(w, s, pos, mat3FromQuat(rot), delta, mask, ecs.None)
}

func shapeCast3(w *ecs.World, s Shape3, pos lin.Vec3, rot mat3, delta lin.Vec3, mask uint32, exclude ecs.Entity) (Hit3, bool) {
	parts := convexParts(s, pos, rot)
	if len(parts) == 0 {
		return Hit3{}, false
	}
	lo, hi := parts[0].lo, parts[0].hi
	for _, p := range parts[1:] {
		lo, hi = lo.Min(p.lo), hi.Max(p.hi)
	}
	slo, shi := lo.Min(lo.Add(delta)), hi.Max(hi.Add(delta))
	best := Hit3{Distance: float32(math.Inf(1))}
	found := false
	eachCollider3(w, slo, shi, mask, false, func(p placed3) {
		if p.e == exclude {
			return
		}
		for i := range parts {
			a := &parts[i].conv
			if m, ok := p.c.Shape.(MeshShape); ok {
				if t, n, pt, hit := sweepMesh(m, p.pos, p.rot, a, parts[i].lo, parts[i].hi, delta); hit && t < best.Distance {
					best, found = Hit3{Entity: p.e, Point: pt, Normal: n, Distance: t}, true
				}
				continue
			}
			for _, target := range convexParts(p.c.Shape, p.pos, p.rot) {
				if t, n, pt, hit := sweepConvex(a, &target.conv, delta); hit && t < best.Distance {
					best, found = Hit3{Entity: p.e, Point: pt, Normal: n, Distance: t}, true
				}
			}
		}
	})
	return best, found
}

// Nearest3 finds the collider closest to a point within radius: Point is
// the nearest point on its surface, Normal points from there toward the
// query point (zero when the point is inside) and Distance is how far.
func Nearest3(w *ecs.World, point lin.Vec3, radius float32, mask uint32) (Hit3, bool) {
	r := lin.V3(radius, radius, radius)
	best := Hit3{Distance: float32(math.Inf(1))}
	found := false
	eachCollider3(w, point.Sub(r), point.Add(r), mask, false, func(p placed3) {
		if m, ok := p.c.Shape.(MeshShape); ok {
			if q, d, hit := closestPointMesh(m, p.pos, p.rot, point, radius); hit && d < best.Distance {
				best, found = Hit3{Entity: p.e, Point: q, Normal: point.Sub(q).Norm(), Distance: d}, true
			}
			return
		}
		for _, part := range convexParts(p.c.Shape, p.pos, p.rot) {
			q, d := closestPointConvex(&part.conv, point)
			if d <= radius && d < best.Distance {
				best, found = Hit3{Entity: p.e, Point: q, Normal: point.Sub(q).Norm(), Distance: d}, true
			}
		}
	})
	return best, found
}

// RaycastAll3 returns every collider along the ray, nearest first,
// ignoring triggers and colliders the mask excludes. To cast repeatedly
// without allocating a result each time, call RaycastAll3Into.
func RaycastAll3(w *ecs.World, r Ray3, mask uint32) []Hit3 {
	return RaycastAll3Into(nil, w, r, mask)
}

// RaycastAll3Into appends every collider along the ray to out, nearest
// first, and returns out. Pass the previous result truncated with [:0]
// to reuse its storage; pass nil for a fresh slice. The appended hits
// are sorted among themselves, not against what out already held.
func RaycastAll3Into(out []Hit3, w *ecs.World, r Ray3, mask uint32) []Hit3 {
	start := len(out)
	stateOf3(w).colliders.Each(func(e ecs.Entity, t *gfx.Transform, c *Collider3) {
		if c.Shape == nil || c.Trigger || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		rot := mat3FromQuat(t.Rotation)
		pos := t.Position.Add(rot.mulVec(c.Offset))
		lo, hi := c.Shape.bounds(pos, rot)
		if !raySlab3(r, lo, hi, 1) {
			return
		}
		if tt, n, ok := rayShape3(r, c.Shape, pos, rot); ok {
			out = append(out, Hit3{Entity: e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt})
		}
	})
	slices.SortStableFunc(out[start:], func(a, b Hit3) int { return cmp.Compare(a.Distance, b.Distance) })
	return out
}
