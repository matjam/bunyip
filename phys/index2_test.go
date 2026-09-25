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

// The 2D brute-force queries: a walk over every collider in walk order,
// as the queries ran before the collider index.

func bruteEach2(w *ecs.World, lo, hi lin.Vec2, mask uint32, triggers bool, fn func(p placed2)) {
	stateOf2(w).colliders.Each(func(e ecs.Entity, t *gfx.Transform2, c *Collider2) {
		if c.Shape == nil || (c.Trigger && !triggers) || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		pos, clo, chi := placement2(t, c)
		if clo.X > hi.X || lo.X > chi.X || clo.Y > hi.Y || lo.Y > chi.Y {
			return
		}
		fn(placed2{e: e, c: c, pos: pos, rot: t.Rotation, lo: clo, hi: chi})
	})
}

func bruteRaycast2(w *ecs.World, r Ray2, mask uint32, exclude ecs.Entity) (Hit2, bool) {
	best := Hit2{Distance: float32(math.Inf(1))}
	found := false
	st := stateOf2(w)
	st.colliders.Each(func(e ecs.Entity, t *gfx.Transform2, c *Collider2) {
		if c.Shape == nil || c.Trigger || e == exclude || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		pos, lo, hi := placement2(t, c)
		if !raySlab2(r, lo, hi, min(best.Distance, 1)) {
			return
		}
		if tt, n, ok := rayShape2(&st.qs, r, c.Shape, pos, t.Rotation); ok && tt < best.Distance {
			best = Hit2{Entity: e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt}
			found = true
		}
	})
	return best, found
}

func bruteRaycastAll2(w *ecs.World, r Ray2, mask uint32) []Hit2 {
	var out []Hit2
	st := stateOf2(w)
	st.colliders.Each(func(e ecs.Entity, t *gfx.Transform2, c *Collider2) {
		if c.Shape == nil || c.Trigger || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		pos, lo, hi := placement2(t, c)
		if !raySlab2(r, lo, hi, 1) {
			return
		}
		if tt, n, ok := rayShape2(&st.qs, r, c.Shape, pos, t.Rotation); ok {
			out = append(out, Hit2{Entity: e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt})
		}
	})
	slices.SortStableFunc(out, func(a, b Hit2) int { return cmp.Compare(a.Distance, b.Distance) })
	return out
}

func bruteOverlap2(w *ecs.World, s Shape2, pos lin.Vec2, rot float32, mask uint32) []Hit2 {
	var out []Hit2
	lo, hi := s.bounds(pos, rot)
	st := stateOf2(w)
	bruteEach2(w, lo, hi, mask, true, func(p placed2) {
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

func bruteShapeCast2(w *ecs.World, s Shape2, pos lin.Vec2, rot float32, delta lin.Vec2, mask uint32) (Hit2, bool) {
	length := delta.Len()
	if length == 0 {
		return Hit2{}, false
	}
	lo, hi := s.bounds(pos, rot)
	slo, shi := lo.Min(lo.Add(delta)), hi.Max(hi.Add(delta))
	ext := hi.Sub(lo)
	minHalfA := min(ext.X, ext.Y) / 2
	st := stateOf2(w)
	var cands []candidate2
	bruteEach2(w, slo, shi, mask, false, func(p placed2) { cands = append(cands, candidate2{p: p}) })
	for i := range cands {
		cands[i].enter = sweepEnter2(lo, hi, delta, cands[i].p.lo, cands[i].p.hi)
	}
	slices.SortFunc(cands, func(a, b candidate2) int { return cmp.Compare(a.enter, b.enter) })
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

func bruteNearest2(w *ecs.World, point lin.Vec2, radius float32, mask uint32) (Hit2, bool) {
	r := lin.V2(radius, radius)
	best := Hit2{Distance: float32(math.Inf(1))}
	found := false
	st := stateOf2(w)
	bruteEach2(w, point.Sub(r), point.Add(r), mask, false, func(p placed2) {
		q, d := closestPoint2(&st.qs, p.c.Shape, p.pos, p.rot, point)
		if d <= radius && d < best.Distance {
			best, found = Hit2{Entity: p.e, Point: q, Normal: point.Sub(q).Norm(), Distance: d}, true
		}
	})
	return best, found
}

func (r *indexRand) shape2() Shape2 {
	switch r.intn(6) {
	case 0:
		return Circle{Radius: r.in(0.2, 2)}
	case 1:
		return Capsule2{Radius: r.in(0.1, 0.8), HalfHeight: r.in(0.1, 1.5)}
	case 2:
		return Polygon2{Points: []lin.Vec2{{X: -0.5, Y: -0.4}, {X: 0.6, Y: -0.3}, {X: 0.7, Y: 0.3}, {X: 0, Y: 0.8}, {X: -0.6, Y: 0.2}}}
	case 3:
		return Edge2{A: lin.V2(r.in(-2, 0), r.in(-1, 1)), B: lin.V2(r.in(0, 2), r.in(-1, 1))}
	case 4:
		if r.intn(3) == 0 {
			return Chain2{Points: []lin.Vec2{{X: -6, Y: 0}, {X: -2, Y: -1}, {X: 2, Y: 0.5}, {X: 6, Y: -0.5}}, Loop: r.intn(2) == 0}
		}
	}
	return Box2{HalfW: r.in(0.1, 2), HalfH: r.in(0.1, 2)}
}

func (r *indexRand) collider2() Collider2 {
	c := Collider2{Shape: r.shape2()}
	if r.intn(4) == 0 {
		c.Offset = lin.V2(r.in(-1, 1), r.in(-1, 1))
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

func (r *indexRand) transform2() gfx.Transform2 {
	return gfx.Transform2{Position: lin.V2(r.in(-40, 40), r.in(-15, 15)), Rotation: r.in(-3, 3)}
}

func sameHit2(a, b Hit2) bool {
	return a.Entity == b.Entity && sameBits2(a.Point, b.Point) && sameBits2(a.Normal, b.Normal) && sameBits(a.Distance, b.Distance)
}

// TestIndexedQueries2MatchBruteForce is the 2D form of the 3D test.
func TestIndexedQueries2MatchBruteForce(t *testing.T) {
	r := indexRand(2)
	w := ecs.NewWorld()
	w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	var ents []ecs.Entity
	for range 400 {
		ents = append(ents, w.SpawnWith(r.transform2(), r.collider2()))
	}
	for range 60 {
		ents = append(ents, w.SpawnWith(r.transform2(), Dynamic2(1), Collider2{Shape: Circle{Radius: r.in(0.2, 1)}}))
	}
	shapes := []Shape2{Circle{Radius: 0.7}, Box2{HalfW: 0.6, HalfH: 0.3}, Capsule2{Radius: 0.3, HalfHeight: 0.6}}
	queries := 0
	for round := range 20 {
		for i := range 500 {
			o := lin.V2(r.in(-50, 50), r.in(-20, 20))
			d := lin.V2(r.in(-30, 30), r.in(-20, 20))
			if i%3 == 0 {
				d = d.Mul(0.1)
			}
			mask := uint32(0)
			if i%7 == 0 {
				mask = 1 << r.intn(3)
			}
			ray := Ray2{Origin: o, Dir: d}
			exclude := ents[r.intn(len(ents))]
			a, aok := bruteRaycast2(w, ray, mask, exclude)
			if b, bok := raycast2(w, ray, mask, exclude); aok != bok || !sameHit2(a, b) {
				t.Fatalf("round %d: raycast %v: indexed %v %v, walk %v %v", round, ray, b, bok, a, aok)
			}
			if a, b := bruteRaycastAll2(w, ray, mask), RaycastAll2(w, ray, mask); !slices.EqualFunc(a, b, sameHit2) {
				t.Fatalf("round %d: raycast all %v: indexed %v, walk %v", round, ray, b, a)
			}
			s := shapes[i%len(shapes)]
			rot := r.in(-3, 3)
			if a, b := bruteOverlap2(w, s, o, rot, mask), OverlapShape2(w, s, o, rot, mask); !slices.EqualFunc(a, b, sameHit2) {
				t.Fatalf("round %d: overlap %T at %v: indexed %v, walk %v", round, s, o, b, a)
			}
			a, aok = bruteShapeCast2(w, s, o, rot, d.Mul(0.3), mask)
			if b, bok := ShapeCast2(w, s, o, rot, d.Mul(0.3), mask); aok != bok || !sameHit2(a, b) {
				t.Fatalf("round %d: shape cast %T from %v: indexed %v %v, walk %v %v", round, s, o, b, bok, a, aok)
			}
			radius := r.in(0.5, 6)
			a, aok = bruteNearest2(w, o, radius, mask)
			if b, bok := Nearest2(w, o, radius, mask); aok != bok || !sameHit2(a, b) {
				t.Fatalf("round %d: nearest %v: indexed %v %v, walk %v %v", round, o, b, bok, a, aok)
			}
			queries += 5
		}
		for range 25 {
			e := ents[r.intn(len(ents))]
			if !w.Alive(e) {
				continue
			}
			switch r.intn(6) {
			case 0:
				tr, _ := w.Get[gfx.Transform2](e)
				tr.Position = tr.Position.Add(lin.V2(r.in(-3, 3), r.in(-1, 1)))
			case 1:
				tr, _ := w.Get[gfx.Transform2](e)
				tr.Rotation = r.in(-3, 3)
			case 2:
				c, _ := w.Get[Collider2](e)
				*c = r.collider2()
			case 3:
				c, _ := w.Get[Collider2](e)
				if p, ok := c.Shape.(Polygon2); ok {
					p.Points[0] = p.Points[0].Mul(1.5)
				}
				if ch, ok := c.Shape.(Chain2); ok {
					ch.Points[1].Y += 1
				}
			case 4:
				w.Despawn(e)
				ents = append(ents, w.SpawnWith(r.transform2(), r.collider2()))
			default:
				if !w.Has[Body2](e) {
					w.Add(e, Dynamic2(1))
				}
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

// sweepAll is the broadphase as it ran before the still colliders got a
// list of their own: one sort-and-sweep over every interval.
func sweepAll(keys, ends []float32, fn func(i, j int)) {
	order := make([]int32, len(keys))
	for i := range order {
		order[i] = int32(i)
	}
	slices.SortFunc(order, func(a, b int32) int {
		switch {
		case axisLess(keys, a, b):
			return -1
		case axisLess(keys, b, a):
			return 1
		}
		return 0
	})
	for x, i := range order {
		for _, j := range order[x+1:] {
			if keys[j] > ends[i] {
				break
			}
			fn(int(i), int(j))
		}
	}
}

// TestSweepPairsMatchesOneSweep checks that sweeping the moving rows
// against each other and against the sorted still rows visits exactly
// the pairs one sweep over all of them would, less the pairs of two
// still rows, and in the same order, with ties on the axis included.
func TestSweepPairsMatchesOneSweep(t *testing.T) {
	r := indexRand(3)
	for trial := range 200 {
		n := 1 + r.intn(300)
		keys, ends := make([]float32, n), make([]float32, n)
		still := make([]bool, n)
		var moving, statics axisList
		for i := range n {
			// Coarse starts make ties common.
			keys[i] = float32(r.intn(40))
			ends[i] = keys[i] + float32(r.intn(6))
			if r.intn(8) == 0 {
				ends[i] += 30
			}
			still[i] = r.intn(3) != 0
			if still[i] {
				statics.order = append(statics.order, int32(i))
			} else {
				moving.order = append(moving.order, int32(i))
			}
		}
		statics.sort(keys)
		moving.sort(keys)
		var want, got [][2]int
		sweepAll(keys, ends, func(i, j int) {
			if !still[i] || !still[j] {
				want = append(want, [2]int{i, j})
			}
		})
		sweepPairs(keys, ends, moving.order, statics.order, func(i, j int) { got = append(got, [2]int{i, j}) })
		if !slices.Equal(want, got) {
			t.Fatalf("trial %d: %d pairs from the split sweep, want %d in the order of one sweep", trial, len(got), len(want))
		}
	}
}
