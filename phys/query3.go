package phys

import (
	"cmp"
	"math"
	"slices"

	"github.com/matjam/bunyip/ecs"
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

// queryState3 is the world's physics state with its collider index
// brought up to date, which every query starts from. A query that runs
// several searches in a row, such as a character controller's move,
// calls it once and then searches the index directly.
func queryState3(w *ecs.World) *state3 {
	st := stateOf3(w)
	st.cols.update(st.colliders)
	return st
}

// eachCollider3 calls fn for every collider whose bounds overlap the
// box, skipping triggers unless asked and colliders the mask excludes,
// in walk order. The index must be up to date.
func (st *state3) eachCollider3(lo, hi lin.Vec3, mask uint32, triggers bool, fn func(p placed3)) {
	x := &st.cols
	for _, k := range x.candidates(lo, hi) {
		r := &x.rows[k]
		c := r.c
		if (c.Trigger && !triggers) || !(Layers{Mask: mask}).collides(c.Layers) {
			continue
		}
		if r.lo.X > hi.X || lo.X > r.hi.X || r.lo.Y > hi.Y || lo.Y > r.hi.Y || r.lo.Z > hi.Z || lo.Z > r.hi.Z {
			continue
		}
		fn(placed3{e: r.e, c: c, pos: r.pos, rot: r.rot, lo: r.lo, hi: r.hi})
	}
}

// candidate3 is one collider a query still has to test, with how far
// along a sweep its bounds are first reached so the nearest are tried
// first and the rest fall away as the best hit shortens.
type candidate3 struct {
	p     placed3
	enter float32
}

// gatherColliders3 appends every collider whose bounds overlap the box
// to dst, skipping triggers unless asked, the excluded entity and
// colliders the mask leaves out. Passing back a slice a previous call
// filled reuses its storage.
func (st *state3) gatherColliders3(dst []candidate3, lo, hi lin.Vec3, mask uint32, triggers bool, exclude ecs.Entity) []candidate3 {
	st.eachCollider3(lo, hi, mask, triggers, func(p placed3) {
		if p.e != exclude {
			dst = append(dst, candidate3{p: p})
		}
	})
	return dst
}

// sweepEnter3 is the earliest fraction of delta at which a moving box
// first overlaps a still one: zero when they already overlap, and
// infinity when they never do.
func sweepEnter3(lo, hi, delta, olo, ohi lin.Vec3) float32 {
	enter, exit := float32(0), float32(1)
	if !slabSweep(lo.X, hi.X, delta.X, olo.X, ohi.X, &enter, &exit) ||
		!slabSweep(lo.Y, hi.Y, delta.Y, olo.Y, ohi.Y, &enter, &exit) ||
		!slabSweep(lo.Z, hi.Z, delta.Z, olo.Z, ohi.Z, &enter, &exit) {
		return float32(math.Inf(1))
	}
	return enter
}

// slabSweep narrows [enter, exit] to the span in which a moving interval
// overlaps a still one, and reports false when they never do.
func slabSweep(lo, hi, d, olo, ohi float32, enter, exit *float32) bool {
	if d > -1e-20 && d < 1e-20 {
		return lo <= ohi && olo <= hi
	}
	t0, t1 := (olo-hi)/d, (ohi-lo)/d
	if t0 > t1 {
		t0, t1 = t1, t0
	}
	*enter = max(*enter, t0)
	*exit = min(*exit, t1)
	return *enter <= *exit
}

// appendConvexParts breaks a placed shape into its convex pieces with
// their bounds and appends them to dst; a mesh has none. Each piece
// keeps the hull buffer it was given, so passing back a slice a previous
// call filled places the same shapes again without allocating.
func appendConvexParts(dst []convexPart, s Shape3, pos lin.Vec3, rot mat3) []convexPart {
	if c, ok := s.(Compound3); ok {
		for _, p := range c.Parts {
			if p.Shape == nil {
				continue
			}
			pp, pr := p.place(pos, rot)
			dst = appendConvexParts(dst, p.Shape, pp, pr)
		}
		return dst
	}
	n := len(dst)
	if n < cap(dst) {
		dst = dst[:n+1] // keep the hull buffer this row already holds
	} else {
		dst = append(dst, convexPart{})
	}
	part := &dst[n]
	var ok bool
	part.conv, part.hull, ok = placeConvex(part.hull, s, pos, rot)
	if !ok {
		return dst[:n]
	}
	part.lo, part.hi = s.bounds(pos, rot)
	return dst
}

type convexPart struct {
	conv   convex
	hull   []lin.Vec3 // reused world points for a hull the convex cannot hold
	lo, hi lin.Vec3
}

// OverlapShape3 returns every collider the shape overlaps when placed at
// pos with rotation rot. Each hit carries the deepest contact: Point on
// the collider, Normal pointing from the collider back toward the shape
// and Distance the penetration depth. Triggers are included. s must be
// non-nil. A zero rot is identity; mask zero includes every collider layer.
func OverlapShape3(w *ecs.World, s Shape3, pos lin.Vec3, rot lin.Quat, mask uint32) []Hit3 {
	return OverlapShape3Into(nil, w, s, pos, rot, mask)
}

// OverlapShape3Into appends every collider the shape overlaps to out and
// returns out. Pass the previous result truncated with [:0] to reuse its
// storage; pass nil for a fresh slice. Result and scratch buffers may
// allocate on initial use or growth. OverlapShape3's contracts apply.
func OverlapShape3Into(out []Hit3, w *ecs.World, s Shape3, pos lin.Vec3, rot lin.Quat, mask uint32) []Hit3 {
	return queryState3(w).overlapShape3(out, s, pos, mat3FromQuat(rot), mask, true, ecs.None)
}

func (st *state3) overlapShape3(out []Hit3, s Shape3, pos lin.Vec3, r mat3, mask uint32, triggers bool, exclude ecs.Entity) []Hit3 {
	lo, hi := s.bounds(pos, r)
	st.eachCollider3(lo, hi, mask, triggers, func(p placed3) {
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
	return queryState3(w).shapeCast3(s, pos, mat3FromQuat(rot), delta, mask, ecs.None)
}

func (st *state3) shapeCast3(s Shape3, pos lin.Vec3, rot mat3, delta lin.Vec3, mask uint32, exclude ecs.Entity) (Hit3, bool) {
	st.castParts = appendConvexParts(st.castParts[:0], s, pos, rot)
	parts := st.castParts
	if len(parts) == 0 {
		return Hit3{}, false
	}
	lo, hi := parts[0].lo, parts[0].hi
	for _, p := range parts[1:] {
		lo, hi = lo.Min(p.lo), hi.Max(p.hi)
	}
	slo, shi := lo.Min(lo.Add(delta)), hi.Max(hi.Add(delta))
	// Order the candidates along the sweep, so the first collider hit
	// shortens the cast and everything behind it is dropped on its
	// bounds alone.
	cands := st.gatherColliders3(st.qcands[:0], slo, shi, mask, false, exclude)
	for i := range cands {
		cands[i].enter = sweepEnter3(lo, hi, delta, cands[i].p.lo, cands[i].p.hi)
	}
	slices.SortFunc(cands, func(a, b candidate3) int { return cmp.Compare(a.enter, b.enter) })
	st.qcands = cands
	best := Hit3{Distance: float32(math.Inf(1))}
	found := false
	for ci := range cands {
		p := &cands[ci].p
		if cands[ci].enter > best.Distance {
			break
		}
		for i := range parts {
			a := &parts[i].conv
			if m, ok := p.c.Shape.(MeshShape); ok {
				if t, n, pt, hit := sweepMesh(&st.qs, m, p.pos, p.rot, a, parts[i].lo, parts[i].hi, delta); hit && t < best.Distance {
					best, found = Hit3{Entity: p.e, Point: pt, Normal: n, Distance: t}, true
				}
				continue
			}
			targets := st.shapes.parts(p.e, p.c.Shape, p.pos, p.rot, p.lo, p.hi)
			for j := range targets {
				if t, n, pt, hit := sweepConvex(a, &targets[j].conv, delta); hit && t < best.Distance {
					best, found = Hit3{Entity: p.e, Point: pt, Normal: n, Distance: t}, true
				}
			}
		}
	}
	return best, found
}

// Nearest3 finds the collider closest to a point within radius: Point is
// the nearest point on its surface, Normal points from there toward the
// query point (zero when the point is inside) and Distance is how far.
func Nearest3(w *ecs.World, point lin.Vec3, radius float32, mask uint32) (Hit3, bool) {
	r := lin.V3(radius, radius, radius)
	st := queryState3(w)
	best := Hit3{Distance: float32(math.Inf(1))}
	found := false
	st.eachCollider3(point.Sub(r), point.Add(r), mask, false, func(p placed3) {
		if m, ok := p.c.Shape.(MeshShape); ok {
			if q, d, hit := closestPointMesh(m, p.pos, p.rot, point, radius); hit && d < best.Distance {
				best, found = Hit3{Entity: p.e, Point: q, Normal: point.Sub(q).Norm(), Distance: d}, true
			}
			return
		}
		parts := st.shapes.parts(p.e, p.c.Shape, p.pos, p.rot, p.lo, p.hi)
		for i := range parts {
			q, d := closestPointConvex(&parts[i].conv, point)
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
	st := queryState3(w)
	x := &st.cols
	for _, k := range x.rayCandidates(r) {
		p := &x.rows[k]
		if p.c.Trigger || !(Layers{Mask: mask}).collides(p.c.Layers) {
			continue
		}
		if !raySlab3(r, p.lo, p.hi, 1) {
			continue
		}
		if tt, n, ok := rayShape3(&st.qs, r, p.c.Shape, p.pos, p.rot); ok {
			out = append(out, Hit3{Entity: p.e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt})
		}
	}
	slices.SortStableFunc(out[start:], func(a, b Hit3) int { return cmp.Compare(a.Distance, b.Distance) })
	return out
}
