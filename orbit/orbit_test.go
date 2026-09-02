package orbit

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/orbit/sol"
)

func within(a, b, rel float64) bool {
	return math.Abs(a-b) <= rel*math.Max(math.Abs(a), math.Abs(b))+1e-12
}

func TestElementsRoundTrip(t *testing.T) {
	el := Elements{SemiMajorAxis: 26600e3, Eccentricity: 0.74, Inclination: 1.1, Node: 0.7, ArgPeriapsis: 4.7, TrueAnomaly: 2.3}
	back := ElementsOf(el.State(sol.MuEarth), sol.MuEarth)
	for name, pair := range map[string][2]float64{
		"a": {el.SemiMajorAxis, back.SemiMajorAxis}, "e": {el.Eccentricity, back.Eccentricity}, "i": {el.Inclination, back.Inclination},
		"node": {el.Node, back.Node}, "argp": {el.ArgPeriapsis, back.ArgPeriapsis}, "nu": {el.TrueAnomaly, back.TrueAnomaly},
	} {
		if !within(pair[0], pair[1], 1e-9) {
			t.Errorf("%s: %v != %v", name, pair[0], pair[1])
		}
	}
	// A circular equatorial orbit has degenerate angles; the anomaly is
	// the position angle.
	c := ElementsOf(Elements{SemiMajorAxis: 7000e3, TrueAnomaly: 1}.State(sol.MuEarth), sol.MuEarth)
	if !within(c.TrueAnomaly, 1, 1e-9) || c.Eccentricity > 1e-9 {
		t.Errorf("circular: %+v", c)
	}
}

func TestPropagate(t *testing.T) {
	mu := sol.MuEarth
	// A circular orbit returns to its start after one period.
	el := Circular(7000e3)
	s0 := el.State(mu)
	period := el.Period(mu)
	s1 := Propagate(s0, mu, period)
	if s1.Pos.Sub(s0.Pos).Len() > 1 || s1.Vel.Sub(s0.Vel).Len() > 1e-3 {
		t.Fatalf("after one period: %+v vs %+v", s1, s0)
	}
	q := Propagate(s0, mu, period/4)
	if !within(q.Pos.Y, 7000e3, 1e-6) || math.Abs(q.Pos.X) > 1 {
		t.Fatalf("quarter period: %+v", q.Pos)
	}
	// Elliptical: energy is conserved.
	e := Elements{SemiMajorAxis: 20000e3, Eccentricity: 0.6, Inclination: 0.5}
	s := e.State(mu)
	for range 20 {
		s = Propagate(s, mu, 977)
	}
	if !within(Energy(s, mu), Energy(e.State(mu), mu), 1e-9) {
		t.Fatal("energy drifted")
	}
	// Hyperbolic flyby: leaves and conserves energy; backwards undoes it.
	hyp := State{Pos: V3(10000e3, 0, 0), Vel: V3(0, 12000, 0)}
	e0 := Energy(hyp, mu)
	if e0 <= 0 {
		t.Fatal("expected an open orbit")
	}
	far := Propagate(hyp, mu, 6*3600)
	if far.Pos.Len() < 1e8 || !within(Energy(far, mu), e0, 1e-9) {
		t.Fatalf("hyperbolic propagation: r=%v energy %v vs %v", far.Pos.Len(), Energy(far, mu), e0)
	}
	back := Propagate(far, mu, -6*3600)
	if back.Pos.Sub(hyp.Pos).Len() > 1 {
		t.Fatalf("reverse propagation off by %v m", back.Pos.Sub(hyp.Pos).Len())
	}
	// AtTime agrees with Propagate.
	a1 := e.AtTime(mu, 5000).State(mu)
	a2 := Propagate(e.State(mu), mu, 5000)
	if a1.Pos.Sub(a2.Pos).Len() > 1 {
		t.Fatalf("AtTime vs Propagate differ by %v m", a1.Pos.Sub(a2.Pos).Len())
	}
	// Game units: G = 1, a star of mass 1000, an orbit of radius 10.
	game := Circular(10)
	gs := game.State(1000)
	if got := Propagate(gs, 1000, game.Period(1000)/2).Pos; !within(got.X, -10, 1e-6) {
		t.Fatalf("game-unit half orbit %v", got)
	}
}

func TestKeplerAndHelpers(t *testing.T) {
	if E := SolveKepler(1, 0.5); !within(E, 1.4987011335, 1e-9) {
		t.Fatalf("Kepler E=%v", E)
	}
	for _, e := range []float64{0, 0.3, 0.9, 0.99} {
		for _, nu := range []float64{0.1, 1, 3, 5} {
			if got := TrueAnomalyFromMean(MeanAnomalyFromTrue(nu, e), e); !within(got, nu, 1e-8) {
				t.Errorf("anomaly round trip e=%v nu=%v got %v", e, nu, got)
			}
		}
	}
	// LEO to GEO Hohmann: about 2.42 and 1.47 km/s, 5.27 hours.
	dv1, dv2, coast := Hohmann(sol.MuEarth, 6678e3, 42164e3)
	if !within(dv1, 2425, 0.01) || !within(dv2, 1467, 0.01) || !within(coast, 5.27*3600, 0.01) {
		t.Fatalf("Hohmann %v %v %v", dv1, dv2, coast)
	}
	if !within(CircularVelocity(sol.MuEarth, 6771e3), 7672, 0.01) || !within(EscapeVelocity(sol.MuEarth, sol.EarthRadius), 11180, 0.01) {
		t.Fatal("velocity helpers")
	}
	if soi := SphereOfInfluence(sol.AU, sol.EarthMass, sol.SunMass); !within(soi, 9.24e8, 0.02) {
		t.Fatalf("Earth SOI %v", soi)
	}
	if p := Circular(42164e3).Period(sol.MuEarth); !within(p, 86164, 0.01) {
		t.Fatalf("GEO period %v", p)
	}
}

func TestSimulation(t *testing.T) {
	// Earth and Moon on a mutual circular orbit: leapfrog holds energy
	// over a month and the Moon comes back around in 27.3 days.
	d := sol.MoonDistance
	mu := G * (sol.EarthMass + sol.MoonMass)
	vrel := math.Sqrt(mu / d)
	sim := Simulation{Bodies: []Body{
		{Vel: V3(0, -vrel*sol.MoonMass/(sol.EarthMass+sol.MoonMass), 0), Mass: sol.EarthMass},
		{Pos: V3(d, 0, 0), Vel: V3(0, vrel*sol.EarthMass/(sol.EarthMass+sol.MoonMass), 0), Mass: sol.MoonMass},
	}}
	e0 := sim.Energy()
	start := sim.Bodies[1].Pos.Sub(sim.Bodies[0].Pos)
	closest, closestT := math.Inf(1), 0.0
	for sim.Time < 30*sol.Day {
		sim.Step(60)
		if sim.Time > 20*sol.Day {
			rel := sim.Bodies[1].Pos.Sub(sim.Bodies[0].Pos)
			if dd := rel.Sub(start).Len(); dd < closest {
				closest, closestT = dd, sim.Time
			}
		}
	}
	if !within(sim.Energy(), e0, 1e-6) {
		t.Fatalf("energy drift %v vs %v", sim.Energy(), e0)
	}
	if math.Abs(closestT-27.3*sol.Day) > 0.2*sol.Day || closest > d*0.01 {
		t.Fatalf("Moon returned at %.2f days, %v m off", closestT/sol.Day, closest)
	}
	// A test particle in circular orbit under RK4 stays circular.
	pos, vel := V3(7000e3, 0, 0), V3(0, CircularVelocity(sol.MuEarth, 7000e3), 0)
	earth := Simulation{Bodies: []Body{{Mass: sol.EarthMass}}}
	for range 1000 {
		pos, vel = RK4(pos, vel, 10, func(p, _ Vec3) Vec3 { return earth.FieldAt(p) })
	}
	if !within(pos.Len(), 7000e3, 1e-4) { // 10 s steps over 1.7 orbits
		t.Fatalf("RK4 radius %v", pos.Len())
	}
	// Three fictional bodies in game units also conserve energy.
	toy := Simulation{G: 1, Bodies: []Body{
		{Mass: 100}, {Pos: V3(10, 0, 0), Vel: V3(0, 3.2, 0), Mass: 1}, {Pos: V3(-25, 0, 0), Vel: V3(0, -2, 0.3), Mass: 0.5},
	}}
	te := toy.Energy()
	for range 20000 {
		toy.Step(0.01)
	}
	if !within(toy.Energy(), te, 1e-4) {
		t.Fatalf("toy system energy %v vs %v", toy.Energy(), te)
	}
}

func TestSystem(t *testing.T) {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings{TimeScale: 3600, Scale: 1e-6, Substeps: 16})
	w.AddSystem("orbit", System)
	sun := w.SpawnWith(Body{Mass: sol.SunMass}, gfx.Transform{})
	earth := w.SpawnWith(Body{Mass: sol.EarthMass}, Kepler{Primary: sun, Elements: Circular(sol.AU)}, gfx.Transform{})
	moon := w.SpawnWith(Body{Mass: sol.MoonMass}, Kepler{Primary: earth, Elements: Circular(sol.MoonDistance)}, gfx.Transform{})
	// A ship in low Earth orbit, a free body under everyone's gravity.
	leo := Circular(7000e3).State(sol.MuEarth)
	w.Update(0) // place the Kepler bodies so the ship can start relative to Earth
	eb, _ := ecs.Get[Body](w, earth)
	ship := w.SpawnWith(Ship{}, Body{Pos: eb.Pos.Add(leo.Pos), Vel: eb.Vel.Add(leo.Vel)}, gfx.Transform{})
	period := Circular(7000e3).Period(sol.MuEarth)
	steps := 200
	for range steps {
		w.Update(period / float64(steps) / 3600) // TimeScale turns hours into seconds
	}
	eb, _ = ecs.Get[Body](w, earth)
	sb, _ := ecs.Get[Body](w, ship)
	rel := sb.Pos.Sub(eb.Pos)
	if !within(rel.Len(), 7000e3, 0.01) {
		t.Fatalf("ship radius after one orbit %v", rel.Len())
	}
	mb, _ := ecs.Get[Body](w, moon)
	if !within(mb.Pos.Sub(eb.Pos).Len(), sol.MoonDistance, 1e-6) {
		t.Fatalf("moon did not follow Earth: %v", mb.Pos.Sub(eb.Pos).Len())
	}
	et, _ := ecs.Get[gfx.Transform](w, earth)
	if !within(float64(et.Position.Len()), sol.AU*1e-6, 1e-3) {
		t.Fatalf("scaled transform %v", et.Position)
	}
	primary, el, _, ok := Around(w, ship)
	if !ok || primary != earth || !within(el.SemiMajorAxis, 7000e3, 0.01) {
		t.Fatalf("Around: %v %+v %v", primary, el, ok)
	}
	if path := Predict(w, ship, period, 32); len(path) != 32 {
		t.Fatal("prediction length")
	}
}
