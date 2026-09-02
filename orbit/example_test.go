package orbit_test

import (
	"fmt"
	"math"

	"github.com/matjam/bunyip/orbit"
	"github.com/matjam/bunyip/orbit/sol"
)

func ExampleHohmann() {
	// Low Earth orbit to geostationary: two burns and a coast.
	dv1, dv2, coast := orbit.Hohmann(sol.MuEarth, 6678e3, 42164e3)
	fmt.Printf("burn %.2f km/s, coast %.1f h, burn %.2f km/s\n", dv1/1000, coast/3600, dv2/1000)
	// Output:
	// burn 2.43 km/s, coast 5.3 h, burn 1.47 km/s
}

func ExamplePropagate() {
	// A fictional world in game units: G is 1 and the star has mass 1000.
	const mu = 1000
	el := orbit.Elements{SemiMajorAxis: 20, Eccentricity: 0.3}
	s := el.State(mu)
	half := orbit.Propagate(s, mu, el.Period(mu)/2)
	fmt.Printf("periapsis %.0f, apoapsis %.0f, at half a period r = %.1f\n", el.Periapsis(), el.Apoapsis(), half.Pos.Len())
	// Output:
	// periapsis 14, apoapsis 26, at half a period r = 26.0
}

func ExampleSimulation() {
	// Two stars of equal mass circling each other, in game units.
	sim := orbit.Simulation{G: 1, Bodies: []orbit.Body{
		{Pos: orbit.V3(-5, 0, 0), Vel: orbit.V3(0, -5, 0), Mass: 500},
		{Pos: orbit.V3(5, 0, 0), Vel: orbit.V3(0, 5, 0), Mass: 500},
	}}
	e0 := sim.Energy()
	for range 10000 {
		sim.Step(0.001)
	}
	drift := math.Abs((sim.Energy() - e0) / e0)
	fmt.Printf("energy conserved to 1e-9: %v, separation %.2f\n", drift < 1e-9, sim.Bodies[1].Pos.Sub(sim.Bodies[0].Pos).Len())
	// Output:
	// energy conserved to 1e-9: true, separation 10.00
}
