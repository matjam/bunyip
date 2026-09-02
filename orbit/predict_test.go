package orbit_test

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/orbit"
)

// exampleSystem builds the star, planet and moon of the space example in
// game units, with a ship in a low orbit around the planet.
func exampleSystem() (*ecs.World, ecs.Entity, ecs.Entity) {
	w := ecs.NewWorld()
	ecs.SetResource(w, orbit.Settings{G: 1, Substeps: 8})
	star := w.SpawnWith(orbit.Body{Mass: 5000}, gfx.Transform{})
	planet := w.SpawnWith(orbit.Body{Mass: 12}, gfx.Transform{},
		orbit.Kepler{Primary: star, Elements: orbit.Elements{SemiMajorAxis: 55, Eccentricity: 0.1, Inclination: 0.1, ArgPeriapsis: 1, TrueAnomaly: 2}})
	w.SpawnWith(orbit.Body{Mass: 0.12}, gfx.Transform{}, orbit.Kepler{Primary: planet, Elements: orbit.Elements{SemiMajorAxis: 3}})
	w.AddSystem("orbits", orbit.System)
	w.Update(0)
	pb, _ := ecs.Get[orbit.Body](w, planet)
	rel := orbit.Elements{SemiMajorAxis: 1.8, TrueAnomaly: math.Pi}.State(pb.Mass)
	ship := w.SpawnWith(orbit.Ship{}, orbit.Thrust{}, orbit.Body{Pos: pb.Pos.Add(rel.Pos), Vel: pb.Vel.Add(rel.Vel)}, gfx.Transform{})
	return w, ship, planet
}

// TestPredictRelativeStaysInOrbit predicts thirty seconds (seven orbits)
// of a ship circling a planet that itself moves at nine units per second,
// and checks every predicted point stays near the planet: prediction
// must move the planet along with the ship, in small enough steps.
func TestPredictRelativeStaysInOrbit(t *testing.T) {
	w, ship, planet := exampleSystem()
	pb, _ := ecs.Get[orbit.Body](w, planet)
	path := orbit.PredictRelative(w, ship, planet, 30, 120)
	if len(path) != 120 {
		t.Fatalf("got %d points", len(path))
	}
	planetNow := pb.Pos.Lin()
	for i, p := range path {
		r := p.Sub(planetNow).Len()
		if r < 1.4 || r > 2.2 {
			t.Fatalf("point %d is %.2f from the planet; the orbit is between 1.6 and 1.9", i, r)
		}
	}
}

func BenchmarkPredictRelative(b *testing.B) {
	w, ship, planet := exampleSystem()
	for b.Loop() {
		orbit.PredictRelative(w, ship, planet, 30, 120)
	}
}
