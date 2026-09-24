package orbit

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// refState and the functions after it are the orbit system as it was
// before ships advanced together and placements were shared: every ship
// integrated alone, re-solving every Kepler body at each step through
// two maps. The tests hold the current system to its results bit for
// bit.
type refState struct {
	keplers *ecs.Query2[Kepler, Body]
	ships   *ecs.Query2[Ship, Body]
	bodies  *ecs.Query1[Body]
	placed  *ecs.Query2[Body, gfx.Transform]
	sim     Simulation
	fixed   []Body
	rel     []relState
	index   map[ecs.Entity]int
	solved  map[ecs.Entity]State
}

type relState struct {
	e, primary ecs.Entity
	mass       float64
	st         State
}

func newRefState(w *ecs.World) *refState {
	return &refState{keplers: w.Query2[Kepler, Body](), ships: w.Query2[Ship, Body](),
		bodies: w.Query1[Body](), placed: w.Query2[Body, gfx.Transform]()}
}

func (s *refState) system(w *ecs.World, dt float64) {
	settings := w.Resource[Settings]()
	scale := settings.TimeScale
	if scale == 0 {
		scale = 1
	}
	sim := dt * scale
	g := settings.G
	if g == 0 {
		g = G
	}
	substeps := settings.Substeps
	if substeps <= 0 {
		substeps = 8
	}
	s.sim.G, s.sim.Softening = g, settings.Softening
	fixed := s.fixed[:0]
	s.bodies.Each(func(e ecs.Entity, b *Body) {
		if b.Mass > 0 && !w.Has[Ship](e) && !w.Has[Kepler](e) {
			fixed = append(fixed, *b)
		}
	})
	s.fixed = fixed
	place := func(t float64) {
		s.sim.Bodies = append(s.sim.Bodies[:0], fixed...)
		s.keplerStates(w, g, t, ecs.None, false, func(_ ecs.Entity, st State, mass float64) {
			s.sim.Bodies = append(s.sim.Bodies, Body{Pos: st.Pos, Vel: st.Vel, Mass: mass})
		})
	}
	s.ships.Each(func(e ecs.Entity, _ *Ship, b *Body) {
		var thrust Vec3
		if t, ok := w.Get[Thrust](e); ok {
			thrust = t.Accel
		}
		accel := func(p, _ Vec3) Vec3 { return s.sim.FieldAt(p).Add(thrust) }
		place(0)
		b.Pos, b.Vel = integrate(b.Pos, b.Vel, 0, sim, substeps, place, accel)
	})
	s.keplers.Each(func(e ecs.Entity, k *Kepler, _ *Body) { k.elapsed += sim })
	s.keplerStates(w, g, 0, ecs.None, true, func(e ecs.Entity, st State, _ float64) {
		if b, ok := w.Get[Body](e); ok {
			b.Pos, b.Vel = st.Pos, st.Vel
		}
	})
	unit := settings.Scale
	if unit == 0 {
		unit = 1
	}
	s.placed.Each(func(e ecs.Entity, b *Body, t *gfx.Transform) {
		t.Position = b.Pos.Sub(settings.Origin).Mul(unit).Lin()
	})
}

func (s *refState) keplerStates(w *ecs.World, g, ahead float64, ref ecs.Entity, all bool, fn func(e ecs.Entity, st State, mass float64)) {
	s.rel = s.rel[:0]
	s.keplers.Each(func(e ecs.Entity, k *Kepler, b *Body) {
		if !all && b.Mass <= 0 && e != ref {
			return
		}
		mu := k.Mu
		if mu == 0 {
			if p, ok := w.Get[Body](k.Primary); ok {
				mu = g * p.Mass
			}
		}
		if mu == 0 {
			return
		}
		s.rel = append(s.rel, relState{e: e, primary: k.Primary, mass: b.Mass, st: k.Elements.AtTime(mu, k.elapsed+ahead).State(mu)})
	})
	if s.index == nil {
		s.index, s.solved = map[ecs.Entity]int{}, map[ecs.Entity]State{}
	}
	clear(s.index)
	clear(s.solved)
	for i, r := range s.rel {
		s.index[r.e] = i
	}
	var resolve func(i int, depth int) State
	resolve = func(i int, depth int) State {
		r := s.rel[i]
		if st, ok := s.solved[r.e]; ok {
			return st
		}
		var base State
		if j, ok := s.index[r.primary]; ok && depth < 16 {
			base = resolve(j, depth+1)
		} else if p, ok := w.Get[Body](r.primary); ok {
			base = State{Pos: p.Pos, Vel: p.Vel}
		}
		st := State{Pos: base.Pos.Add(r.st.Pos), Vel: base.Vel.Add(r.st.Vel)}
		s.solved[r.e] = st
		return st
	}
	for i, r := range s.rel {
		fn(r.e, resolve(i, 0), r.mass)
	}
}

func (s *refState) predictRelative(w *ecs.World, ship, ref ecs.Entity, seconds float64, samples int) []lin.Vec3 {
	settings := w.Resource[Settings]()
	refNow := Vec3{}
	if rb, ok := w.Get[Body](ref); ok {
		refNow = rb.Pos
	}
	b, _ := w.Get[Body](ship)
	g := settings.G
	if g == 0 {
		g = G
	}
	var thrust Vec3
	if t, ok := w.Get[Thrust](ship); ok {
		thrust = t.Accel
	}
	unit := settings.Scale
	if unit == 0 {
		unit = 1
	}
	var fixed []Body
	s.bodies.Each(func(e ecs.Entity, o *Body) {
		if o.Mass > 0 && !w.Has[Ship](e) && !w.Has[Kepler](e) {
			fixed = append(fixed, *o)
		}
	})
	sim := Simulation{G: g, Softening: settings.Softening}
	pos, vel := b.Pos, b.Vel
	h := seconds / float64(samples)
	accel := func(p, _ Vec3) Vec3 { return sim.FieldAt(p).Add(thrust) }
	out := make([]lin.Vec3, 0, samples)
	var shift Vec3
	place := func(t float64) {
		sim.Bodies = append(sim.Bodies[:0], fixed...)
		s.keplerStates(w, g, t, ref, false, func(e ecs.Entity, st State, mass float64) {
			if mass > 0 {
				sim.Bodies = append(sim.Bodies, Body{Pos: st.Pos, Vel: st.Vel, Mass: mass})
			}
			if e == ref {
				shift = st.Pos.Sub(refNow)
			}
		})
	}
	place(0)
	for i := range samples {
		pos, vel = integrate(pos, vel, float64(i)*h, float64(i+1)*h, 1, place, accel)
		place(float64(i+1) * h)
		out = append(out, pos.Sub(shift).Sub(settings.Origin).Mul(unit).Lin())
	}
	return out
}

// lockstepWorld is a star with planets and moons, a massless belt, a fixed
// second star, and ships in wide orbits (whose steps agree) and in tight
// orbits around planets (whose adaptive steps differ), some thrusting.
func lockstepWorld() (*ecs.World, []ecs.Entity) {
	w := ecs.NewWorld()
	w.SetResource(Settings{G: 1, TimeScale: 20, Substeps: 8, Softening: 0.001})
	star := w.SpawnWith(Body{Mass: 1e6}, gfx.Transform{})
	w.SpawnWith(Body{Pos: V3(5000, 0, 0), Mass: 10}, gfx.Transform{})
	var planets []ecs.Entity
	for p := range 6 {
		planet := w.SpawnWith(Body{Mass: 100 + float64(p)}, gfx.Transform{},
			Kepler{Primary: star, Elements: Elements{SemiMajorAxis: 100 + float64(p)*40, Eccentricity: 0.05, Inclination: 0.02 * float64(p), TrueAnomaly: float64(p)}})
		planets = append(planets, planet)
		for m := range 3 {
			moon := w.SpawnWith(Body{Mass: 0.01}, gfx.Transform{},
				Kepler{Primary: planet, Elements: Elements{SemiMajorAxis: 3 + float64(m), Eccentricity: 0.01, Inclination: 0.1, TrueAnomaly: float64(m)}})
			// A moonlet around the moon: a chain three deep.
			if m == 0 {
				w.SpawnWith(Body{Mass: 0.0001}, Kepler{Primary: moon, Elements: Elements{SemiMajorAxis: 0.2}})
			}
		}
	}
	for i := range 20 {
		w.SpawnWith(Body{}, gfx.Transform{}, Kepler{Primary: star, Elements: Elements{SemiMajorAxis: 300 + float64(i), TrueAnomaly: float64(i)}})
	}
	w.Update(0)
	var ships []ecs.Entity
	for i := range 6 {
		r := 170.0 + float64(i)*7
		v := math.Sqrt(1e6 / r)
		e := w.SpawnWith(Body{Pos: V3(r, 0, 0), Vel: V3(0, v, 0), Mass: 1}, Ship{}, gfx.Transform{})
		if i%2 == 1 {
			w.Add(e, Thrust{Accel: V3(0.5, -0.25, 0.1)})
		}
		ships = append(ships, e)
	}
	for i, planet := range planets[:4] {
		pb, _ := w.Get[Body](planet)
		rel := Elements{SemiMajorAxis: 1.5 + 0.3*float64(i), TrueAnomaly: float64(i)}.State(pb.Mass)
		ships = append(ships, w.SpawnWith(Body{Pos: pb.Pos.Add(rel.Pos), Vel: pb.Vel.Add(rel.Vel), Mass: 1}, Ship{}, gfx.Transform{}))
	}
	return w, ships
}

// allBodies lists every entity's Body in entity order.
func allBodies(w *ecs.World) []Body {
	var out []Body
	for _, e := range w.Entities() {
		if b, ok := w.Get[Body](e); ok {
			out = append(out, *b)
		}
	}
	return out
}

func TestShipsMatchSequentialIntegration(t *testing.T) {
	got, _ := lockstepWorld()
	got.AddSystem("orbit", System)
	want, _ := lockstepWorld()
	ref := newRefState(want)
	for step := range 40 {
		got.Update(1.0 / 60)
		ref.system(want, 1.0/60)
		g, w := allBodies(got), allBodies(want)
		if len(g) != len(w) {
			t.Fatalf("body counts %d and %d", len(g), len(w))
		}
		for i := range g {
			if g[i] != w[i] {
				t.Fatalf("update %d, body %d: %+v, want %+v", step, i, g[i], w[i])
			}
		}
	}
	// The transforms follow the bodies.
	for _, e := range got.Entities() {
		gt, ok := got.Get[gfx.Transform](e)
		wt, _ := want.Get[gfx.Transform](e)
		if ok && gt.Position != wt.Position {
			t.Fatalf("transform of %v: %v, want %v", e, gt.Position, wt.Position)
		}
	}
}

func TestPredictMatchesSequentialPlacement(t *testing.T) {
	w, ships := lockstepWorld()
	w.AddSystem("orbit", System)
	w.Update(1.0 / 60)
	ref := newRefState(w)
	planet, _, _, _ := Around(w, ships[len(ships)-1])
	for _, tc := range []struct {
		ship, ref ecs.Entity
	}{{ships[0], ecs.None}, {ships[1], planet}, {ships[len(ships)-1], planet}} {
		want := ref.predictRelative(w, tc.ship, tc.ref, 5, 40)
		got := PredictRelative(w, tc.ship, tc.ref, 5, 40)
		if len(got) != len(want) {
			t.Fatalf("%d points, want %d", len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("ship %v point %d: %v, want %v", tc.ship, i, got[i], want[i])
			}
		}
	}
}

// Predicting every frame into a kept slice allocates nothing.
func TestAppendPredictAllocs(t *testing.T) {
	w, ships := lockstepWorld()
	w.AddSystem("orbit", System)
	w.Update(1.0 / 60)
	planet, _, _, _ := Around(w, ships[len(ships)-1])
	path := AppendPredictRelative(nil, w, ships[len(ships)-1], planet, 5, 40)
	allocs := testing.AllocsPerRun(20, func() {
		path = AppendPredictRelative(path[:0], w, ships[len(ships)-1], planet, 5, 40)
	})
	if allocs != 0 {
		t.Fatalf("AppendPredictRelative allocated %v times", allocs)
	}
	if len(path) != 40 {
		t.Fatalf("%d points", len(path))
	}
	if got := AppendPredict(path[:0], w, ships[0], 5, 40); len(got) != 40 {
		t.Fatalf("AppendPredict gave %d points", len(got))
	}
}
