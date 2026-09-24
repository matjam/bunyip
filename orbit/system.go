package orbit

import (
	"math"
	"runtime"
	"slices"
	"sync"

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
	plan    keplerPlan

	// Ships advance together: runs holds each ship's integration, and
	// placements the massive bodies at each distinct midpoint time of
	// the current round of steps, shared by every ship stepping there.
	runs       []shipRun
	placements []placement

	// Prediction's own scratch, so it can run between updates without
	// disturbing the system's and without allocating.
	pred predictor
}

// placement is where the massive bodies are at one time.
type placement struct {
	t      float64
	bodies []Body
}

// shipRun is one ship's integration across an update.
type shipRun struct {
	pos, vel, thrust Vec3
	t                float64
	a                float64 // |acceleration| at pos under the latest placement, for the step size
	step             float64
	at               int // this round's placement
}

// relEntry is a Kepler body in a plan: what is needed to place it at any
// time in this update, and where its primary is.
type relEntry struct {
	e       ecs.Entity
	mass    float64
	el      Elements
	mu      float64
	elapsed float64
	prim    int   // the primary's entry, or -1 when it has none
	base    State // the primary's Body, used when prim is -1 or the chain is too deep
}

// keplerPlan is the Kepler bodies of one update in query order, built once
// and then solved at as many times as the integration needs. Positions
// in the plan index everything, so solving uses no maps.
type keplerPlan struct {
	rel    []relEntry
	local  []State // each entry's state relative to its primary
	abs    []State // each entry's absolute state
	solved []bool
	// primaries is each entry's primary entity while the plan is built,
	// and order the entry positions sorted by entity, to find them.
	primaries []ecs.Entity
	order     []int
}

// build collects the Kepler bodies. With all false only massive bodies
// (and ref) are kept, which keeps an asteroid belt of massless bodies out
// of gravity calculations. Bodies whose mu is zero are left out.
func (p *keplerPlan) build(w *ecs.World, q *ecs.Query2[Kepler, Body], g float64, ref ecs.Entity, all bool) {
	p.rel = p.rel[:0]
	q.Each(func(e ecs.Entity, k *Kepler, b *Body) {
		if !all && b.Mass <= 0 && e != ref {
			return
		}
		mu := k.Mu
		if mu == 0 {
			if pb, ok := w.Get[Body](k.Primary); ok {
				mu = g * pb.Mass
			}
		}
		if mu == 0 {
			return
		}
		p.rel = append(p.rel, relEntry{e: e, mass: b.Mass, el: k.Elements, mu: mu, elapsed: k.elapsed, prim: -1})
		p.primaries = append(p.primaries, k.Primary)
	})
	p.order = p.order[:0]
	for i := range p.rel {
		p.order = append(p.order, i)
	}
	slices.SortFunc(p.order, func(a, b int) int { return compareEntity(p.rel[a].e, p.rel[b].e) })
	for i := range p.rel {
		r := &p.rel[i]
		primary := p.primaries[i]
		if j, ok := slices.BinarySearchFunc(p.order, primary, func(k int, e ecs.Entity) int { return compareEntity(p.rel[k].e, e) }); ok {
			r.prim = p.order[j]
		}
		if pb, ok := w.Get[Body](primary); ok {
			r.base = State{Pos: pb.Pos, Vel: pb.Vel}
		}
	}
	p.primaries = p.primaries[:0]
	n := len(p.rel)
	p.local = slices.Grow(p.local[:0], n)[:n]
	p.abs = slices.Grow(p.abs[:0], n)[:n]
	p.solved = slices.Grow(p.solved[:0], n)[:n]
}

func compareEntity(a, b ecs.Entity) int {
	x, y := a.ID(), b.ID()
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// solve places every entry ahead seconds past where it is now, resolving
// primary chains so a moon follows its planet.
func (p *keplerPlan) solve(ahead float64) {
	// Each entry's own orbit is independent of the others, so a large
	// plan solves them across goroutines; only the chains need order.
	n := len(p.rel)
	if workers := min(runtime.GOMAXPROCS(0), n/(parallelBodies/4)); n >= parallelBodies && workers > 1 {
		var wg sync.WaitGroup
		per := (n + workers - 1) / workers
		for lo := 0; lo < n; lo += per {
			hi := min(lo+per, n)
			wg.Go(func() { p.solveLocal(ahead, lo, hi) })
		}
		wg.Wait()
	} else {
		p.solveLocal(ahead, 0, n)
	}
	clear(p.solved)
	for i := range p.rel {
		p.resolve(i, 0)
	}
}

// solveLocal places entries lo to hi relative to their primaries.
func (p *keplerPlan) solveLocal(ahead float64, lo, hi int) {
	for i := lo; i < hi; i++ {
		r := &p.rel[i]
		p.local[i] = r.el.AtTime(r.mu, r.elapsed+ahead).State(r.mu)
	}
}

// resolve is entry i's absolute state: its primary's plus its own. A
// chain deeper than sixteen links (or a cycle) falls back to the
// primary's Body.
func (p *keplerPlan) resolve(i int, depth int) State {
	if p.solved[i] {
		return p.abs[i]
	}
	r := &p.rel[i]
	base := r.base
	if r.prim >= 0 && depth < 16 {
		base = p.resolve(r.prim, depth+1)
	}
	st := State{Pos: base.Pos.Add(p.local[i].Pos), Vel: base.Vel.Add(p.local[i].Vel)}
	p.abs[i], p.solved[i] = st, true
	return st
}

// placeMassive appends the fixed bodies and then the plan's massive
// entries, as solved last, to dst.
func (p *keplerPlan) placeMassive(dst, fixed []Body) []Body {
	dst = append(dst[:0], fixed...)
	for i := range p.rel {
		if m := p.rel[i].mass; m > 0 {
			dst = append(dst, Body{Pos: p.abs[i].Pos, Vel: p.abs[i].Vel, Mass: m})
		}
	}
	return dst
}

func stateOf(w *ecs.World) *state {
	s := w.Resource[state]()
	if s == nil {
		w.SetResource(state{keplers: w.Query2[Kepler, Body](), ships: w.Query2[Ship, Body](),
			bodies: w.Query1[Body](), placed: w.Query2[Body, gfx.Transform]()})
		s = w.Resource[state]()
		s.pred.init()
	}
	return s
}

// fixedBodies appends every massive body that is neither a ship nor on a
// Kepler orbit to dst: the bodies that never move.
func (s *state) fixedBodies(w *ecs.World, dst []Body) []Body {
	dst = dst[:0]
	s.bodies.Each(func(e ecs.Entity, b *Body) {
		if b.Mass > 0 && !w.Has[Ship](e) && !w.Has[Kepler](e) {
			dst = append(dst, *b)
		}
	})
	return dst
}

// System advances orbits by dt real seconds (times Settings.TimeScale)
// and writes scaled positions into existing gfx.Transform components.
// It creates default Settings when absent. Use a nonnegative scaled
// timestep: the ship integrator does not integrate backwards. Kepler
// primary chains should be acyclic and no more than sixteen links deep.
func System(w *ecs.World, dt float64) {
	settings := w.Resource[Settings]()
	if settings == nil {
		w.SetResource(Settings{})
		settings = w.Resource[Settings]()
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
	// Free bodies first: integrated across this update under the field of
	// every massive body, with the Kepler bodies moved along their orbits
	// at each step, so a ship in a tight orbit stays accurate at high
	// time warp.
	substeps := settings.Substeps
	if substeps <= 0 {
		substeps = 8
	}
	s.sim.G, s.sim.Softening = g, settings.Softening
	s.advanceShips(w, g, sim, substeps)
	// Then the Kepler bodies: exact states relative to their primaries,
	// with absolute positions by walking each chain (star, planet, moon)
	// so every body sees its primary at the same instant.
	s.keplers.Each(func(e ecs.Entity, k *Kepler, _ *Body) { k.elapsed += sim })
	s.plan.build(w, s.keplers, g, ecs.None, true)
	s.plan.solve(0)
	for i := range s.plan.rel {
		if b, ok := w.Get[Body](s.plan.rel[i].e); ok {
			b.Pos, b.Vel = s.plan.abs[i].Pos, s.plan.abs[i].Vel
		}
	}
	// Place everything that has a transform.
	unit := settings.Scale
	if unit == 0 {
		unit = 1
	}
	s.placed.Each(func(e ecs.Entity, b *Body, t *gfx.Transform) {
		t.Position = b.Pos.Sub(settings.Origin).Mul(unit).Lin()
	})
}

// advanceShips integrates every ship across sim seconds, all of them
// together: each round every unfinished ship takes one step, and the
// massive bodies are placed once per distinct step midpoint and shared
// by the ships stepping there. Ships whose steps agree, which is every
// ship not in a tight orbit, therefore cost one placement per step
// between them rather than one each. Each ship takes exactly the steps
// integrate would give it alone, with the same arithmetic.
func (s *state) advanceShips(w *ecs.World, g, sim float64, substeps int) {
	runs := s.runs[:0]
	s.ships.Each(func(e ecs.Entity, _ *Ship, b *Body) {
		r := shipRun{pos: b.Pos, vel: b.Vel}
		if t, ok := w.Get[Thrust](e); ok {
			r.thrust = t.Accel
		}
		runs = append(runs, r)
	})
	s.runs = runs
	if len(runs) == 0 {
		return
	}
	s.fixed = s.fixedBodies(w, s.fixed)
	s.plan.build(w, s.keplers, g, ecs.None, false)
	var thrust Vec3
	accel := func(p, _ Vec3) Vec3 { return s.sim.FieldAt(p).Add(thrust) }

	// Everything starts from the placement at the start of the update.
	if len(s.placements) == 0 {
		s.placements = append(s.placements, placement{})
	}
	start := &s.placements[0]
	s.plan.solve(0)
	start.t, start.bodies = 0, s.plan.placeMassive(start.bodies, s.fixed)
	s.sim.Bodies = start.bodies
	for i := range runs {
		thrust = runs[i].thrust
		runs[i].a = accel(runs[i].pos, runs[i].vel).Len()
	}

	minSteps := max(substeps, 1)
	maxStep := sim / float64(minSteps)
	for {
		// Choose each unfinished ship's step, as integrate does, and
		// gather the distinct midpoints.
		used := 0
		for i := range runs {
			r := &runs[i]
			if sim-r.t <= 1e-9 {
				r.at = -1
				continue
			}
			step := min(sim-r.t, maxStep)
			if v := r.vel.Len(); r.a > 0 && v > 0 {
				step = min(step, max(0.01*v/r.a, maxStep/64))
			}
			r.step = step
			mid := r.t + step/2
			r.at = -1
			for k := range used {
				if s.placements[k].t == mid {
					r.at = k
					break
				}
			}
			if r.at < 0 {
				if used == len(s.placements) {
					s.placements = append(s.placements, placement{})
				}
				s.placements[used].t = mid
				s.placements[used].bodies = s.placements[used].bodies[:0]
				r.at = used
				used++
			}
		}
		if used == 0 {
			break
		}
		for k := range used {
			pl := &s.placements[k]
			s.plan.solve(pl.t)
			pl.bodies = s.plan.placeMassive(pl.bodies, s.fixed)
		}
		for i := range runs {
			r := &runs[i]
			if r.at < 0 {
				continue
			}
			s.sim.Bodies = s.placements[r.at].bodies
			thrust = r.thrust
			r.pos, r.vel = RK4(r.pos, r.vel, r.step, accel)
			r.t += r.step
			// The next step's size reads the acceleration under the
			// placement this step used, as integrate's does.
			if sim-r.t > 1e-9 {
				r.a = accel(r.pos, r.vel).Len()
			}
		}
	}
	i := 0
	s.ships.Each(func(_ ecs.Entity, _ *Ship, b *Body) {
		b.Pos, b.Vel = runs[i].pos, runs[i].vel
		i++
	})
	// Leave no placement behind for the next reader of s.sim.
	s.sim.Bodies = nil
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

// predictor holds what one prediction needs between its steps, kept on
// the world's orbit state so predicting every frame allocates nothing.
type predictor struct {
	plan   keplerPlan
	fixed  []Body
	sim    Simulation
	thrust Vec3
	ref    int // the reference body's plan entry, or -1
	refNow Vec3
	shift  Vec3
	// The method values, made once so passing them allocates nothing.
	placeFn func(t float64)
	accelFn func(p, v Vec3) Vec3
}

func (p *predictor) init() {
	p.placeFn = p.place
	p.accelFn = p.accel
}

func (p *predictor) accel(pos, _ Vec3) Vec3 { return p.sim.FieldAt(pos).Add(p.thrust) }

// place moves every massive Kepler body to where it will be at time t
// and notes how far the reference body has moved by then.
func (p *predictor) place(t float64) {
	p.plan.solve(t)
	p.sim.Bodies = p.plan.placeMassive(p.sim.Bodies, p.fixed)
	if p.ref >= 0 {
		p.shift = p.plan.abs[p.ref].Pos.Sub(p.refNow)
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
// To reuse a slice from frame to frame, call AppendPredict.
func Predict(w *ecs.World, ship ecs.Entity, seconds float64, samples int) []lin.Vec3 {
	return PredictRelative(w, ship, ecs.None, seconds, samples)
}

// AppendPredict is Predict appending the path to dst, for a caller that
// predicts every frame and keeps one slice for it: pass the last frame's
// path sliced to zero length and nothing is allocated once it has grown.
// When Predict would return nil, it returns dst unchanged.
func AppendPredict(dst []lin.Vec3, w *ecs.World, ship ecs.Entity, seconds float64, samples int) []lin.Vec3 {
	return AppendPredictRelative(dst, w, ship, ecs.None, seconds, samples)
}

// PredictRelative is Predict in the moving frame of another body: each
// point is shifted by how far that body will have moved by then, so the
// path of a ship orbiting a planet draws as a loop around the planet
// where it is now rather than a streak across the sky. With ref None it
// is the inertial path. Only a reference with Kepler is advanced; a
// free reference body supplies no moving-frame correction. Requirements,
// units and sample placement are the same as Predict. To reuse a slice
// from frame to frame, call AppendPredictRelative.
func PredictRelative(w *ecs.World, ship, ref ecs.Entity, seconds float64, samples int) []lin.Vec3 {
	if samples <= 0 {
		return nil
	}
	out := AppendPredictRelative(make([]lin.Vec3, 0, samples), w, ship, ref, seconds, samples)
	if len(out) == 0 {
		return nil
	}
	return out
}

// AppendPredictRelative is PredictRelative appending the path to dst, for
// a caller that predicts every frame and keeps one slice for it: pass the
// last frame's path sliced to zero length and nothing is allocated once
// it has grown. When PredictRelative would return nil, it returns dst
// unchanged.
func AppendPredictRelative(dst []lin.Vec3, w *ecs.World, ship, ref ecs.Entity, seconds float64, samples int) []lin.Vec3 {
	settings := w.Resource[Settings]()
	if settings == nil || samples <= 0 {
		return dst
	}
	refNow := Vec3{}
	if rb, ok := w.Get[Body](ref); ok {
		refNow = rb.Pos
	}
	s := stateOf(w)
	b, ok := w.Get[Body](ship)
	if !ok {
		return dst
	}
	g := settings.G
	if g == 0 {
		g = G
	}
	p := &s.pred
	p.thrust = Vec3{}
	if t, ok := w.Get[Thrust](ship); ok {
		p.thrust = t.Accel
	}
	unit := settings.Scale
	if unit == 0 {
		unit = 1
	}
	p.fixed = s.fixedBodies(w, p.fixed)
	p.plan.build(w, s.keplers, g, ref, false)
	p.ref = -1
	for i := range p.plan.rel {
		if p.plan.rel[i].e == ref {
			p.ref = i
			break
		}
	}
	p.sim.G, p.sim.Softening = g, settings.Softening
	p.refNow, p.shift = refNow, Vec3{}
	pos, vel := b.Pos, b.Vel
	h := seconds / float64(samples)
	dst = slices.Grow(dst, samples)
	p.place(0)
	for i := range samples {
		pos, vel = integrate(pos, vel, float64(i)*h, float64(i+1)*h, 1, p.placeFn, p.accelFn)
		p.place(float64(i+1) * h)
		dst = append(dst, pos.Sub(p.shift).Sub(settings.Origin).Mul(unit).Lin())
	}
	return dst
}

// Around describes a ship's orbit relative to the massive body whose
// gravity dominates it, in classical elements, for a readout.
func Around(w *ecs.World, ship ecs.Entity) (primary ecs.Entity, el Elements, mu float64, ok bool) {
	b, has := w.Get[Body](ship)
	if !has {
		return ecs.None, Elements{}, 0, false
	}
	settings := w.Resource[Settings]()
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
