package phys

import (
	"math"
	"sort"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// placed2 is one collider with its world placement and bounds.
type placed2 struct {
	e      ecs.Entity
	c      *Collider2
	pos    lin.Vec2
	rot    float32
	lo, hi lin.Vec2
}

// eachCollider2 calls fn for every collider whose bounds overlap the
// box, skipping triggers unless asked and colliders the mask excludes.
func eachCollider2(w *ecs.World, lo, hi lin.Vec2, mask uint32, triggers bool, fn func(p placed2)) {
	stateOf2(w).colliders.Each(func(e ecs.Entity, t *gfx.Transform2, c *Collider2) {
		if c.Shape == nil || (c.Trigger && !triggers) || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		cs, sn := cosSin(t.Rotation)
		pos := t.Position.Add(rotate2(c.Offset, cs, sn))
		clo, chi := c.Shape.bounds(pos, t.Rotation)
		if clo.X > hi.X || lo.X > chi.X || clo.Y > hi.Y || lo.Y > chi.Y {
			return
		}
		fn(placed2{e: e, c: c, pos: pos, rot: t.Rotation, lo: clo, hi: chi})
	})
}

// OverlapShape2 returns every collider the shape overlaps when placed at
// pos with rotation rot. Each hit carries the deepest contact: Point on
// the collider, Normal pointing from the collider back toward the shape
// and Distance the penetration depth. Triggers are included.
func OverlapShape2(w *ecs.World, s Shape2, pos lin.Vec2, rot float32, mask uint32) []Hit2 {
	return overlapShape2(w, s, pos, rot, mask, true, ecs.None)
}

func overlapShape2(w *ecs.World, s Shape2, pos lin.Vec2, rot float32, mask uint32, triggers bool, exclude ecs.Entity) []Hit2 {
	lo, hi := s.bounds(pos, rot)
	var out []Hit2
	eachCollider2(w, lo, hi, mask, triggers, func(p placed2) {
		if p.e == exclude {
			return
		}
		cs := collide2(s, pos, rot, p.c.Shape, p.pos, p.rot)
		if len(cs) == 0 {
			return
		}
		deepest := cs[0]
		for _, c := range cs[1:] {
			if c.depth > deepest.depth {
				deepest = c
			}
		}
		out = append(out, Hit2{Entity: p.e, Point: deepest.point, Normal: deepest.normal.Neg(), Distance: deepest.depth})
	})
	return out
}

// OverlapCircle2 returns every collider a circle overlaps.
func OverlapCircle2(w *ecs.World, center lin.Vec2, radius float32, mask uint32) []Hit2 {
	return OverlapShape2(w, Circle{Radius: radius}, center, 0, mask)
}

// OverlapBox2 returns every collider a rotated rectangle overlaps.
func OverlapBox2(w *ecs.World, center lin.Vec2, halfW, halfH, rot float32, mask uint32) []Hit2 {
	return OverlapShape2(w, Box2{HalfW: halfW, HalfH: halfH}, center, rot, mask)
}

// ShapeCast2 sweeps a shape from pos along delta and returns the first
// collider it touches: Distance is the fraction of delta travelled,
// Point where the surfaces meet and Normal the collider's surface
// normal there. Colliders already overlapping the shape at the start
// and triggers are ignored.
func ShapeCast2(w *ecs.World, s Shape2, pos lin.Vec2, rot float32, delta lin.Vec2, mask uint32) (Hit2, bool) {
	return shapeCast2(w, s, pos, rot, delta, mask, ecs.None)
}

func shapeCast2(w *ecs.World, s Shape2, pos lin.Vec2, rot float32, delta lin.Vec2, mask uint32, exclude ecs.Entity) (Hit2, bool) {
	length := delta.Len()
	if length == 0 {
		return Hit2{}, false
	}
	lo, hi := s.bounds(pos, rot)
	slo, shi := lo.Min(lo.Add(delta)), hi.Max(hi.Add(delta))
	ext := hi.Sub(lo)
	minHalfA := min(ext.X, ext.Y) / 2
	best := Hit2{Distance: float32(math.Inf(1))}
	found := false
	eachCollider2(w, slo, shi, mask, false, func(p placed2) {
		if p.e == exclude {
			return
		}
		cext := p.hi.Sub(p.lo)
		t, cs, ok := marchSweep2(s, pos, rot, delta, minHalfA, p.c.Shape, p.pos, p.rot, min(cext.X, cext.Y)/2, best.Distance)
		if !ok {
			return
		}
		deepest := cs[0]
		for _, c := range cs[1:] {
			if c.depth > deepest.depth {
				deepest = c
			}
		}
		best, found = Hit2{Entity: p.e, Point: deepest.point, Normal: deepest.normal.Neg(), Distance: t}, true
	})
	return best, found
}

// marchSweep2 moves a shape along delta in steps smaller than the
// thinnest feature of either shape and bisects the first overlapping
// step, returning the fraction and the contacts there. Sweeps that
// start overlapping, or hit past limit, report nothing.
func marchSweep2(s Shape2, pos lin.Vec2, rot float32, delta lin.Vec2, minHalfA float32, other Shape2, opos lin.Vec2, orot float32, minHalfB, limit float32) (float32, []contact2, bool) {
	length := delta.Len()
	step := 0.5 * (minHalfA + minHalfB)
	if step <= 0 {
		step = length
	}
	steps := max(1, min(256, int(math.Ceil(float64(length/step)))))
	overlaps := func(t float32) []contact2 {
		return collide2(s, pos.Add(delta.Mul(t)), rot, other, opos, orot)
	}
	if len(overlaps(0)) > 0 {
		return 0, nil, false
	}
	free, hit := float32(0), float32(-1)
	for i := 1; i <= steps; i++ {
		t := float32(i) / float32(steps)
		if t > limit {
			return 0, nil, false
		}
		if len(overlaps(t)) > 0 {
			hit = t
			break
		}
		free = t
	}
	if hit < 0 {
		return 0, nil, false
	}
	var cs []contact2
	for range 12 {
		mid := (free + hit) / 2
		if c := overlaps(mid); len(c) > 0 {
			hit, cs = mid, c
		} else {
			free = mid
		}
	}
	if cs == nil {
		cs = overlaps(hit)
	}
	return hit, cs, true
}

// Nearest2 finds the collider closest to a point within radius: Point is
// the nearest point on its outline, Normal points from there toward the
// query point (zero when the point is inside) and Distance is how far.
func Nearest2(w *ecs.World, point lin.Vec2, radius float32, mask uint32) (Hit2, bool) {
	r := lin.V2(radius, radius)
	best := Hit2{Distance: float32(math.Inf(1))}
	found := false
	eachCollider2(w, point.Sub(r), point.Add(r), mask, false, func(p placed2) {
		q, d := closestPoint2(p.c.Shape, p.pos, p.rot, point)
		if d <= radius && d < best.Distance {
			best, found = Hit2{Entity: p.e, Point: q, Normal: point.Sub(q).Norm(), Distance: d}, true
		}
	})
	return best, found
}

// RaycastAll2 returns every collider along the ray, nearest first,
// ignoring triggers and colliders the mask excludes.
func RaycastAll2(w *ecs.World, r Ray2, mask uint32) []Hit2 {
	var out []Hit2
	stateOf2(w).colliders.Each(func(e ecs.Entity, t *gfx.Transform2, c *Collider2) {
		if c.Shape == nil || c.Trigger || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		cs, sn := cosSin(t.Rotation)
		pos := t.Position.Add(rotate2(c.Offset, cs, sn))
		if tt, n, ok := rayShape2(r, c.Shape, pos, t.Rotation); ok {
			out = append(out, Hit2{Entity: e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt})
		}
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Distance < out[j].Distance })
	return out
}
