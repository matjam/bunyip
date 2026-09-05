package orbit

import (
	"math"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

func sqrtFloat(v float64) float64 { return math.Sqrt(v) }

// Settings is the world resource for the orbit system.
type Settings struct {
	G         float64 // zero means the real constant
	TimeScale float64 // simulation time units per real second; zero means 1
	Substeps  int     // minimum integration steps per update for ships; nonpositive means 8
	Softening float64 // distance softening: squared and added to squared separations; zero disables it
	// Rendering: scene units per simulation distance unit, and the floating origin that is
	// subtracted from every position before scaling so the scene stays
	// near zero where float32 is precise. Move Origin with the camera.
	Scale  float64 // zero means 1
	Origin Vec3
}

// Kepler puts an entity on an analytic two-body orbit around Primary:
// planets around a star, moons around a planet. Its Body is written
// each update from the elements; Mu zero means G times the primary's
// mass. JSON encoding preserves the elapsed orbital phase for saves
// and prefabs; older JSON without elapsed time starts at the elements'
// epoch.
type Kepler struct {
	Primary  ecs.Entity
	Elements Elements
	Mu       float64
	elapsed  float64
}

// Thrust is the constant acceleration applied to a Ship, in simulation
// distance units per simulation time unit squared, in world axes.
type Thrust struct{ Accel Vec3 }

// Ship is a marker for free bodies: they are integrated numerically
// under massive non-Ship bodies' gravity plus their own Thrust. Ships
// do not exert gravity on one another, regardless of Mass. Without it,
// a Body with no Kepler stays put (a star at the origin). Use either
// Ship or Kepler on an entity, not both.
type Ship struct{}

type state struct {
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

// relState is a Kepler body's state relative to its primary this update.
type relState struct {
	e, primary ecs.Entity
	mass       float64
	st         State
}

func stateOf(w *ecs.World) *state {
	s := ecs.Resource[state](w)
	if s == nil {
		ecs.SetResource(w, state{keplers: ecs.NewQuery2[Kepler, Body](w), ships: ecs.NewQuery2[Ship, Body](w),
			bodies: ecs.NewQuery1[Body](w), placed: ecs.NewQuery2[Body, gfx.Transform](w)})
		s = ecs.Resource[state](w)
	}
	return s
}

// System advances orbits by dt real seconds (times Settings.TimeScale)
// and writes scaled positions into existing gfx.Transform components.
// It creates default Settings when absent. Use a nonnegative scaled
// timestep: the ship integrator does not integrate backwards. Kepler
// primary chains should be acyclic and no more than sixteen links deep.
func System(w *ecs.World, dt float64) {
	settings := ecs.Resource[Settings](w)
	if settings == nil {
		ecs.SetResource(w, Settings{})
		settings = ecs.Resource[Settings](w)
	}
	scale := settings.TimeScale
	if scale == 0 {
		scale = 1
	}
	sim := dt * scale
	s := stateOf(w)
	g := settings.G
	if g == 0 {
		g = G
	}
	// Kepler bodies: exact states relative to their primaries, then
	// absolute positions by walking each chain (star, planet, moon) so
	// every body sees its primary at the same instant.
	// Free bodies first: integrated across this update under the field of
	// every massive body, with the Kepler bodies moved along their orbits
	// at each step, so a ship in a tight orbit stays accurate at high
	// time warp.
	substeps := settings.Substeps
	if substeps <= 0 {
		substeps = 8
	}
	s.sim.G, s.sim.Softening = g, settings.Softening
	fixed := s.fixed[:0]
	s.bodies.Each(func(e ecs.Entity, b *Body) {
		if b.Mass > 0 && !ecs.Has[Ship](w, e) && !ecs.Has[Kepler](w, e) {
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
		if t, ok := ecs.Get[Thrust](w, e); ok {
			thrust = t.Accel
		}
		accel := func(p, _ Vec3) Vec3 { return s.sim.FieldAt(p).Add(thrust) }
		place(0)
		b.Pos, b.Vel = integrate(b.Pos, b.Vel, 0, sim, substeps, place, accel)
	})
	// Then the Kepler bodies: exact states relative to their primaries,
	// with absolute positions by walking each chain (star, planet, moon)
	// so every body sees its primary at the same instant.
	s.keplers.Each(func(e ecs.Entity, k *Kepler, _ *Body) { k.elapsed += sim })
	s.keplerStates(w, g, 0, ecs.None, true, func(e ecs.Entity, st State, _ float64) {
		if b, ok := ecs.Get[Body](w, e); ok {
			b.Pos, b.Vel = st.Pos, st.Vel
		}
	})
	// Place everything that has a transform.
	unit := settings.Scale
	if unit == 0 {
		unit = 1
	}
	s.placed.Each(func(e ecs.Entity, b *Body, t *gfx.Transform) {
		t.Position = b.Pos.Sub(settings.Origin).Mul(unit).Lin()
	})
}

// integrate advances a free body from time t0 to t1 by RK4 in adaptive
// steps: at least minSteps of them, and none longer than a small fraction
// of the local orbital timescale |v|/|a|, so a tight orbit around a
// fast-moving world is followed rather than spiralled out of. place is
// called with each step's midpoint time so the massive bodies can be
// moved along their own orbits first; the caller places them at t0.
func integrate(pos, vel Vec3, t0, t1 float64, minSteps int, place func(t float64), accel func(p, v Vec3) Vec3) (Vec3, Vec3) {
	if minSteps < 1 {
		minSteps = 1
	}
	maxStep := (t1 - t0) / float64(minSteps)
	t := t0
	for t1-t > 1e-9 {
		step := min(t1-t, maxStep)
		a := accel(pos, vel).Len()
		if v := vel.Len(); a > 0 && v > 0 {
			step = min(step, max(0.01*v/a, maxStep/64))
		}
		place(t + step/2)
		pos, vel = RK4(pos, vel, step, accel)
		t += step
	}
	return pos, vel
}

// keplerStates computes every Kepler body's absolute state ahead seconds
// past where it is now, resolving primary chains so a moon follows its
// planet, and calls fn with each entity, its state and its mass. With
// all false only massive bodies (and ref) are reported, which keeps an
// asteroid belt of massless bodies out of gravity calculations. Bodies
// with no Kepler component stay where they are.
func (s *state) keplerStates(w *ecs.World, g, ahead float64, ref ecs.Entity, all bool, fn func(e ecs.Entity, st State, mass float64)) {
	s.rel = s.rel[:0]
	s.keplers.Each(func(e ecs.Entity, k *Kepler, b *Body) {
		if !all && b.Mass <= 0 && e != ref {
			return
		}
		mu := k.Mu
		if mu == 0 {
			if p, ok := ecs.Get[Body](w, k.Primary); ok {
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
		} else if p, ok := ecs.Get[Body](w, r.primary); ok {
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

// Predict integrates a copy of a ship forward and returns its path in
// world units (scaled, relative to the origin), for drawing where it is
// heading. seconds is a nonnegative duration in simulation time units,
// independent of Settings.TimeScale. The result contains exactly samples points,
// excluding the starting point and including the end of the duration.
// Kepler bodies move during prediction; other Ships are excluded from
// gravity, and massive bodies with neither Ship nor Kepler stay fixed.
// Settings and the ship's Body must exist and samples must be positive,
// otherwise it returns nil. Current Thrust is held constant throughout.
func Predict(w *ecs.World, ship ecs.Entity, seconds float64, samples int) []lin.Vec3 {
	return PredictRelative(w, ship, ecs.None, seconds, samples)
}

// PredictRelative is Predict in the moving frame of another body: each
// point is shifted by how far that body will have moved by then, so the
// path of a ship orbiting a planet draws as a loop around the planet
// where it is now rather than a streak across the sky. With ref None it
// is the inertial path. Only a reference with Kepler is advanced; a
// free reference body supplies no moving-frame correction. Requirements,
// units and sample placement are the same as Predict.
func PredictRelative(w *ecs.World, ship, ref ecs.Entity, seconds float64, samples int) []lin.Vec3 {
	settings := ecs.Resource[Settings](w)
	if settings == nil || samples <= 0 {
		return nil
	}
	refNow := Vec3{}
	if rb, ok := ecs.Get[Body](w, ref); ok {
		refNow = rb.Pos
	}
	s := stateOf(w)
	b, ok := ecs.Get[Body](w, ship)
	if !ok {
		return nil
	}
	g := settings.G
	if g == 0 {
		g = G
	}
	var thrust Vec3
	if t, ok := ecs.Get[Thrust](w, ship); ok {
		thrust = t.Accel
	}
	unit := settings.Scale
	if unit == 0 {
		unit = 1
	}
	// Bodies that never move: everything massive that is neither a ship
	// nor on a Kepler orbit.
	var fixed []Body
	s.bodies.Each(func(e ecs.Entity, o *Body) {
		if o.Mass > 0 && !ecs.Has[Ship](w, e) && !ecs.Has[Kepler](w, e) {
			fixed = append(fixed, *o)
		}
	})
	sim := Simulation{G: g, Softening: settings.Softening}
	pos, vel := b.Pos, b.Vel
	h := seconds / float64(samples)
	accel := func(p, _ Vec3) Vec3 { return sim.FieldAt(p).Add(thrust) }
	out := make([]lin.Vec3, 0, samples)
	// place moves every massive Kepler body to where it will be at time t
	// and notes how far the reference body has moved by then.
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

// Around describes a ship's orbit relative to the massive body whose
// gravity dominates it, in classical elements, for a readout.
func Around(w *ecs.World, ship ecs.Entity) (primary ecs.Entity, el Elements, mu float64, ok bool) {
	b, has := ecs.Get[Body](w, ship)
	if !has {
		return ecs.None, Elements{}, 0, false
	}
	settings := ecs.Resource[Settings](w)
	g := G
	if settings != nil && settings.G != 0 {
		g = settings.G
	}
	best := 0.0
	var bestBody *Body
	stateOf(w).bodies.Each(func(e ecs.Entity, o *Body) {
		if o.Mass == 0 || e == ship {
			return
		}
		d := o.Pos.Sub(b.Pos).Len()
		if d == 0 {
			return
		}
		if pull := o.Mass / (d * d); pull > best {
			best, bestBody, primary = pull, o, e
		}
	})
	if bestBody == nil {
		return ecs.None, Elements{}, 0, false
	}
	mu = g * bestBody.Mass
	rel := State{Pos: b.Pos.Sub(bestBody.Pos), Vel: b.Vel.Sub(bestBody.Vel)}
	return primary, ElementsOf(rel, mu), mu, true
}
