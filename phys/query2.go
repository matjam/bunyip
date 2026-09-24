package phys

import (
	"cmp"
	"math"
	"slices"

	"github.com/matjam/bunyip/ecs"
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

// queryState2 is the world's 2D physics state with its collider index
// brought up to date, which every query starts from.
func queryState2(w *ecs.World) *state2 {
	st := stateOf2(w)
	st.cols.update(st.colliders)
	return st
}

// eachCollider2 calls fn for every collider whose bounds overlap the
// box, skipping triggers unless asked and colliders the mask excludes,
// in walk order. The index must be up to date.
func (st *state2) eachCollider2(lo, hi lin.Vec2, mask uint32, triggers bool, fn func(p placed2)) {
	x := &st.cols
	for _, k := range x.candidates(lo, hi) {
		r := &x.rows[k]
		c := r.c
		if (c.Trigger && !triggers) || !(Layers{Mask: mask}).collides(c.Layers) {
			continue
		}
		if r.lo.X > hi.X || lo.X > r.hi.X || r.lo.Y > hi.Y || lo.Y > r.hi.Y {
			continue
		}
		fn(placed2{e: r.e, c: c, pos: r.pos, rot: r.t.Rotation, lo: r.lo, hi: r.hi})
	}
}

// OverlapShape2 returns every collider the shape overlaps when placed at
// pos with rotation rot. Each hit carries the deepest contact: Point on
// the collider, Normal pointing from the collider back toward the shape
// and Distance the penetration depth. Triggers are included. s must be
// non-nil. rot is radians; mask zero includes every collider layer.
func OverlapShape2(w *ecs.World, s Shape2, pos lin.Vec2, rot float32, mask uint32) []Hit2 {
	return OverlapShape2Into(nil, w, s, pos, rot, mask)
}

// OverlapShape2Into appends every collider the shape overlaps to out and
// returns out. Pass the previous result truncated with [:0] to reuse its
// storage; pass nil for a fresh slice. Result and scratch buffers may
// allocate on initial use or growth. OverlapShape2's contracts apply.
func OverlapShape2Into(out []Hit2, w *ecs.World, s Shape2, pos lin.Vec2, rot float32, mask uint32) []Hit2 {
	return queryState2(w).overlapShape2(out, s, pos, rot, mask, true, ecs.None)
}

func (st *state2) overlapShape2(out []Hit2, s Shape2, pos lin.Vec2, rot float32, mask uint32, triggers bool, exclude ecs.Entity) []Hit2 {
	lo, hi := s.bounds(pos, rot)
	st.eachCollider2(lo, hi, mask, triggers, func(p placed2) {
		if p.e == exclude {
			return
		}
		st.qs.contacts = collide2(&st.qs, st.qs.contacts[:0], s, pos, rot, p.c.Shape, p.pos, p.rot)
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
	return queryState2(w).shapeCast2(s, pos, rot, delta, mask, ecs.None)
}

func (st *state2) shapeCast2(s Shape2, pos lin.Vec2, rot float32, delta lin.Vec2, mask uint32, exclude ecs.Entity) (Hit2, bool) {
	length := delta.Len()
	if length == 0 {
		return Hit2{}, false
	}
	lo, hi := s.bounds(pos, rot)
	slo, shi := lo.Min(lo.Add(delta)), hi.Max(hi.Add(delta))
	ext := hi.Sub(lo)
	minHalfA := min(ext.X, ext.Y) / 2
	// Order the candidates along the sweep. Marching a shape along its
	// path is the expensive part, so finding the nearest collider first
	// and cutting every later march short at that distance is what makes
	// a long cast affordable.
	cands := st.gatherColliders2(st.qcands[:0], slo, shi, mask, false, exclude)
	for i := range cands {
		cands[i].enter = sweepEnter2(lo, hi, delta, cands[i].p.lo, cands[i].p.hi)
	}
	slices.SortFunc(cands, func(a, b candidate2) int { return cmp.Compare(a.enter, b.enter) })
	st.qcands = cands
	best := Hit2{Distance: float32(math.Inf(1))}
	found := false
	for ci := range cands {
		if cands[ci].enter > best.Distance {
			break
		}
		p := &cands[ci].p
		cext := p.hi.Sub(p.lo)
		t, cs, ok := marchSweep2(&st.qs, s, pos, rot, delta, minHalfA, p.c.Shape, p.pos, p.rot, min(cext.X, cext.Y)/2, best.Distance)
		if !ok {
			continue
		}
		deepest := cs[0]
		for _, c := range cs[1:] {
			if c.depth > deepest.depth {
				deepest = c
			}
		}
		best, found = Hit2{Entity: p.e, Point: deepest.point, Normal: deepest.normal.Neg(), Distance: t}, true
	}
	return best, found
}

// candidate2 is one collider a query still has to test, with how far
// along a sweep its bounds are first reached so the nearest are tried
// first and the rest fall away as the best hit shortens.
type candidate2 struct {
	p     placed2
	enter float32
}

// gatherColliders2 appends every collider whose bounds overlap the box
// to dst, skipping triggers unless asked, the excluded entity and
// colliders the mask leaves out. Passing back a slice a previous call
// filled reuses its storage.
func (st *state2) gatherColliders2(dst []candidate2, lo, hi lin.Vec2, mask uint32, triggers bool, exclude ecs.Entity) []candidate2 {
	st.eachCollider2(lo, hi, mask, triggers, func(p placed2) {
		if p.e != exclude {
			dst = append(dst, candidate2{p: p})
		}
	})
	return dst
}

// sweepEnter2 is the earliest fraction of delta at which a moving box
// first overlaps a still one: zero when they already overlap, and
// infinity when they never do.
func sweepEnter2(lo, hi, delta, olo, ohi lin.Vec2) float32 {
	enter, exit := float32(0), float32(1)
	if !slabSweep(lo.X, hi.X, delta.X, olo.X, ohi.X, &enter, &exit) ||
		!slabSweep(lo.Y, hi.Y, delta.Y, olo.Y, ohi.Y, &enter, &exit) {
		return float32(math.Inf(1))
	}
	return enter
}

// marchSweep2 moves a shape along delta in steps smaller than the
// thinnest feature of either shape and bisects the first overlapping
// step, returning the fraction and the contacts there. Sweeps that
// start overlapping, or hit past limit, report nothing.
func marchSweep2(sc *scratch2, s Shape2, pos lin.Vec2, rot float32, delta lin.Vec2, minHalfA float32, other Shape2, opos lin.Vec2, orot float32, minHalfB, limit float32) (float32, []contact2, bool) {
	length := delta.Len()
	step := 0.5 * (minHalfA + minHalfB)
	if step <= 0 {
		step = length
	}
	steps := max(1, min(256, int(math.Ceil(float64(length/step)))))
	overlaps := func(t float32) []contact2 {
		sc.contacts = collide2(sc, sc.contacts[:0], s, pos.Add(delta.Mul(t)), rot, other, opos, orot)
		return sc.contacts
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
	// The contacts of the last overlapping probe are copied out, because
	// the next probe reuses the scratch they were written into.
	cs, kept := sc.keep[:0], false
	for range 12 {
		mid := (free + hit) / 2
		if c := overlaps(mid); len(c) > 0 {
			hit, cs, kept = mid, append(cs[:0], c...), true
		} else {
			free = mid
		}
	}
	if !kept {
		cs = append(cs[:0], overlaps(hit)...)
	}
	sc.keep = cs
	return hit, cs, true
}

// Nearest2 finds the collider closest to a point within radius: Point is
// the nearest point on its outline, Normal points from there toward the
// query point (zero when the point is inside) and Distance is how far.
func Nearest2(w *ecs.World, point lin.Vec2, radius float32, mask uint32) (Hit2, bool) {
	r := lin.V2(radius, radius)
	best := Hit2{Distance: float32(math.Inf(1))}
	found := false
	st := queryState2(w)
	st.eachCollider2(point.Sub(r), point.Add(r), mask, false, func(p placed2) {
		q, d := closestPoint2(&st.qs, p.c.Shape, p.pos, p.rot, point)
		if d <= radius && d < best.Distance {
			best, found = Hit2{Entity: p.e, Point: q, Normal: point.Sub(q).Norm(), Distance: d}, true
		}
	})
	return best, found
}

// RaycastAll2 returns every collider along the ray, nearest first,
// ignoring triggers and colliders the mask excludes. To cast repeatedly
// without allocating a result each time, call RaycastAll2Into.
func RaycastAll2(w *ecs.World, r Ray2, mask uint32) []Hit2 {
	return RaycastAll2Into(nil, w, r, mask)
}

// RaycastAll2Into appends every collider along the ray to out, nearest
// first, and returns out. Pass the previous result truncated with [:0]
// to reuse its storage; pass nil for a fresh slice. The appended hits
// are sorted among themselves, not against what out already held.
func RaycastAll2Into(out []Hit2, w *ecs.World, r Ray2, mask uint32) []Hit2 {
	start := len(out)
	st := queryState2(w)
	x := &st.cols
	for _, k := range x.rayCandidates(r) {
		p := &x.rows[k]
		if p.c.Trigger || !(Layers{Mask: mask}).collides(p.c.Layers) {
			continue
		}
		if !raySlab2(r, p.lo, p.hi, 1) {
			continue
		}
		if tt, n, ok := rayShape2(&st.qs, r, p.c.Shape, p.pos, p.t.Rotation); ok {
			out = append(out, Hit2{Entity: p.e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt})
		}
	}
	slices.SortStableFunc(out[start:], func(a, b Hit2) int { return cmp.Compare(a.Distance, b.Distance) })
	return out
}

// raySlab2 reports whether the ray reaches the box within maxT, as a
// cheap reject before the shape's own test.
func raySlab2(r Ray2, lo, hi lin.Vec2, maxT float32) bool {
	tmin, tmax := float32(0), maxT
	return slabAxis(r.Origin.X, r.Dir.X, lo.X, hi.X, &tmin, &tmax) &&
		slabAxis(r.Origin.Y, r.Dir.Y, lo.Y, hi.Y, &tmin, &tmax)
}
