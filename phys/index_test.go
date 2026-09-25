package phys

import (
	"cmp"
	"math"
	"slices"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// The brute-force queries below are the queries as they were before the
// collider index: a walk over every collider, placing each one and
// testing it in walk order. They share the placement and the shape tests
// with the indexed queries, so the two must agree hit for hit, bit for
// bit and in the same order.

func bruteEach3(w *ecs.World, lo, hi lin.Vec3, mask uint32, triggers bool, fn func(p placed3)) {
	stateOf3(w).colliders.Each(func(e ecs.Entity, t *gfx.Transform, c *Collider3) {
		if c.Shape == nil || (c.Trigger && !triggers) || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		pos, rot, clo, chi := placement3(t, c)
		if clo.X > hi.X || lo.X > chi.X || clo.Y > hi.Y || lo.Y > chi.Y || clo.Z > hi.Z || lo.Z > chi.Z {
			return
		}
		fn(placed3{e: e, c: c, pos: pos, rot: rot, lo: clo, hi: chi})
	})
}

func bruteRaycast3(w *ecs.World, r Ray3, mask uint32, exclude ecs.Entity) (Hit3, bool) {
	best := Hit3{Distance: float32(math.Inf(1))}
	found := false
	st := stateOf3(w)
	st.colliders.Each(func(e ecs.Entity, t *gfx.Transform, c *Collider3) {
		if c.Shape == nil || c.Trigger || e == exclude || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		pos, rot, lo, hi := placement3(t, c)
		if !raySlab3(r, lo, hi, min(best.Distance, 1)) {
			return
		}
		if tt, n, ok := rayShape3(&st.qs, r, c.Shape, pos, rot); ok && tt < best.Distance {
			best = Hit3{Entity: e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt}
			found = true
		}
	})
	return best, found
}

func bruteRaycastAll3(w *ecs.World, r Ray3, mask uint32) []Hit3 {
	var out []Hit3
	st := stateOf3(w)
	st.colliders.Each(func(e ecs.Entity, t *gfx.Transform, c *Collider3) {
		if c.Shape == nil || c.Trigger || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		pos, rot, lo, hi := placement3(t, c)
		if !raySlab3(r, lo, hi, 1) {
			return
		}
		if tt, n, ok := rayShape3(&st.qs, r, c.Shape, pos, rot); ok {
			out = append(out, Hit3{Entity: e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt})
		}
	})
	slices.SortStableFunc(out, func(a, b Hit3) int { return cmp.Compare(a.Distance, b.Distance) })
	return out
}

func bruteOverlap3(w *ecs.World, s Shape3, pos lin.Vec3, r mat3, mask uint32) []Hit3 {
	var out []Hit3
	lo, hi := s.bounds(pos, r)
	st := stateOf3(w)
	bruteEach3(w, lo, hi, mask, true, func(p placed3) {
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

func bruteShapeCast3(w *ecs.World, s Shape3, pos lin.Vec3, rot mat3, delta lin.Vec3, mask uint32) (Hit3, bool) {
	st := stateOf3(w)
	parts := appendConvexParts(nil, s, pos, rot)
	if len(parts) == 0 {
		return Hit3{}, false
	}
	lo, hi := parts[0].lo, parts[0].hi
	for _, p := range parts[1:] {
		lo, hi = lo.Min(p.lo), hi.Max(p.hi)
	}
	slo, shi := lo.Min(lo.Add(delta)), hi.Max(hi.Add(delta))
	var cands []candidate3
	bruteEach3(w, slo, shi, mask, false, func(p placed3) { cands = append(cands, candidate3{p: p}) })
	for i := range cands {
		cands[i].enter = sweepEnter3(lo, hi, delta, cands[i].p.lo, cands[i].p.hi)
	}
	slices.SortFunc(cands, func(a, b candidate3) int { return cmp.Compare(a.enter, b.enter) })
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

func bruteNearest3(w *ecs.World, point lin.Vec3, radius float32, mask uint32) (Hit3, bool) {
	r := lin.V3(radius, radius, radius)
	st := stateOf3(w)
	best := Hit3{Distance: float32(math.Inf(1))}
	found := false
	bruteEach3(w, point.Sub(r), point.Add(r), mask, false, func(p placed3) {
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

// indexRand is a small deterministic generator for the index tests.
type indexRand uint64

func (r *indexRand) next() float32 {
	*r = *r*6364136223846793005 + 1442695040888963407
	return float32(uint32(*r>>40)) / float32(1<<24)
}

func (r *indexRand) in(lo, hi float32) float32 { return lo + (hi-lo)*r.next() }

func (r *indexRand) intn(n int) int { return int(r.next()*float32(n)) % n }

func (r *indexRand) quat() lin.Quat {
	return lin.AxisAngle(lin.V3(r.in(-1, 1), r.in(-1, 1), r.in(-1, 1)+0.01).Norm(), r.in(-3, 3))
}

func (r *indexRand) shape3(mesh MeshShape) Shape3 {
	switch r.intn(8) {
	case 0:
		return Sphere{Radius: r.in(0.2, 2)}
	case 1:
		return Capsule{Radius: r.in(0.1, 0.8), HalfHeight: r.in(0.1, 1.5)}
	case 2:
		return octahedron(r.in(0.3, 1.5))
	case 3:
		return dumpHullIndex(r.in(0.3, 1.5))
	case 4:
		return Compound3{Parts: []Part3{
			{Shape: Box3{Half: lin.V3(r.in(0.2, 1), 0.2, r.in(0.2, 1))}},
			{Shape: Sphere{Radius: r.in(0.2, 0.6)}, Offset: lin.V3(0, 0.7, 0), Rotation: r.quat()},
		}}
	case 5:
		if r.intn(4) == 0 {
			return mesh
		}
	}
	return Box3{Half: lin.V3(r.in(0.1, 2), r.in(0.1, 2), r.in(0.1, 2))}
}

func dumpHullIndex(s float32) ConvexHull {
	var pts []lin.Vec3
	for i := range 12 {
		a := float64(i) * math.Pi / 6
		pts = append(pts, lin.V3(s*float32(math.Cos(a)), s*0.5*float32(i%3-1), s*float32(math.Sin(a))))
	}
	return ConvexHull{Points: pts}
}

func (r *indexRand) collider3(mesh MeshShape) Collider3 {
	c := Collider3{Shape: r.shape3(mesh)}
	if r.intn(4) == 0 {
		c.Offset = lin.V3(r.in(-1, 1), r.in(-1, 1), r.in(-1, 1))
	}
	if r.intn(10) == 0 {
		c.Trigger = true
	}
	if r.intn(5) == 0 {
		c.Layers = Layers{Layer: 1 << r.intn(3), Mask: 1 << r.intn(3)}
	}
	if r.intn(40) == 0 {
		c.Shape = nil
	}
	return c
}

func (r *indexRand) transform3() gfx.Transform {
	return gfx.Transform{Position: lin.V3(r.in(-40, 40), r.in(-10, 10), r.in(-40, 40)), Rotation: r.quat()}
}

// sameHit3 compares two hits bit for bit.
func sameHit3(a, b Hit3) bool {
	return a.Entity == b.Entity && sameBits3(a.Point, b.Point) && sameBits3(a.Normal, b.Normal) && sameBits(a.Distance, b.Distance)
}

func sameHits3(a, b []Hit3) bool {
	return slices.EqualFunc(a, b, sameHit3)
}

// TestIndexedQueries3MatchBruteForce runs random rays, overlaps, shape
// casts and nearest-point queries against random scenes and compares
// every result with the brute-force walk, between edits that move,
// reshape, spawn, despawn and step colliders, so the index is checked
// both fresh and after each way it can go stale.
func TestIndexedQueries3MatchBruteForce(t *testing.T) {
	r := indexRand(1)
	mesh := flatMesh(20, 0, 8)
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	var ents []ecs.Entity
	for range 400 {
		ents = append(ents, w.SpawnWith(r.transform3(), r.collider3(mesh)))
	}
	for range 60 {
		body := Dynamic3(1)
		ents = append(ents, w.SpawnWith(r.transform3(), body, Collider3{Shape: r.shape3(mesh)}))
	}
	shapes := []Shape3{Sphere{Radius: 0.7}, Box3{Half: lin.V3(0.6, 0.3, 0.9)}, Capsule{Radius: 0.3, HalfHeight: 0.6}, dumpHullIndex(0.8),
		Compound3{Parts: []Part3{{Shape: Sphere{Radius: 0.4}}, {Shape: Box3{Half: lin.V3(0.2, 0.2, 0.2)}, Offset: lin.V3(0.5, 0, 0)}}}}
	queries := 0
	check := func(round int) {
		for i := range 500 {
			o := lin.V3(r.in(-50, 50), r.in(-15, 15), r.in(-50, 50))
			d := lin.V3(r.in(-30, 30), r.in(-15, 15), r.in(-30, 30))
			if i%3 == 0 {
				d = d.Mul(0.1) // short rays, the common case
			}
			mask := uint32(0)
			if i%7 == 0 {
				mask = 1 << r.intn(3)
			}
			ray := Ray3{Origin: o, Dir: d}
			exclude := ents[r.intn(len(ents))]
			if a, b := bruteRaycast3(w, ray, mask, exclude); true {
				c, ok := raycast3(w, ray, mask, exclude)
				if ok != b || !sameHit3(a, c) {
					t.Fatalf("round %d: raycast %v: indexed %v %v, walk %v %v", round, ray, c, ok, a, b)
				}
			}
			if a, b := bruteRaycastAll3(w, ray, mask), RaycastAll3(w, ray, mask); !sameHits3(a, b) {
				t.Fatalf("round %d: raycast all %v: indexed %v, walk %v", round, ray, b, a)
			}
			s := shapes[i%len(shapes)]
			q := r.quat()
			if a, b := bruteOverlap3(w, s, o, mat3FromQuat(q), mask), OverlapShape3(w, s, o, q, mask); !sameHits3(a, b) {
				t.Fatalf("round %d: overlap %T at %v: indexed %v, walk %v", round, s, o, b, a)
			}
			if a, ok := bruteShapeCast3(w, s, o, mat3FromQuat(q), d.Mul(0.3), mask); true {
				b, ok2 := ShapeCast3(w, s, o, q, d.Mul(0.3), mask)
				if ok != ok2 || !sameHit3(a, b) {
					t.Fatalf("round %d: shape cast %T from %v: indexed %v %v, walk %v %v", round, s, o, b, ok2, a, ok)
				}
			}
			radius := r.in(0.5, 6)
			if a, ok := bruteNearest3(w, o, radius, mask); true {
				b, ok2 := Nearest3(w, o, radius, mask)
				if ok != ok2 || !sameHit3(a, b) {
					t.Fatalf("round %d: nearest %v: indexed %v %v, walk %v %v", round, o, b, ok2, a, ok)
				}
			}
			queries += 5
		}
	}
	for round := range 20 {
		check(round)
		// Edit the scene the ways a game does between queries.
		for range 25 {
			e := ents[r.intn(len(ents))]
			if !w.Alive(e) {
				continue
			}
			switch r.intn(7) {
			case 0: // move
				tr, _ := w.Get[gfx.Transform](e)
				tr.Position = tr.Position.Add(lin.V3(r.in(-3, 3), r.in(-1, 1), r.in(-3, 3)))
			case 1: // turn
				tr, _ := w.Get[gfx.Transform](e)
				tr.Rotation = r.quat()
			case 2: // reshape or retrigger
				c, _ := w.Get[Collider3](e)
				*c = r.collider3(mesh)
			case 3: // edit a hull in place
				c, _ := w.Get[Collider3](e)
				if h, ok := c.Shape.(ConvexHull); ok {
					h.Points[0] = h.Points[0].Mul(1.5)
				}
			case 4: // despawn and spawn
				w.Despawn(e)
				ents = append(ents, w.SpawnWith(r.transform3(), r.collider3(mesh)))
			case 5: // give a still collider a body
				if !w.Has[Body3](e) {
					w.Add(e, Dynamic3(1))
				}
			default: // move its offset
				c, _ := w.Get[Collider3](e)
				c.Offset = lin.V3(r.in(-1, 1), 0, r.in(-1, 1))
			}
		}
		if round%3 == 2 {
			w.Update(step)
		}
	}
	if queries < 10000 {
		t.Fatalf("only %d queries ran", queries)
	}
}
