package orbit

import (
	"fmt"
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
)

// benchNBody is n bodies on a disc around a heavy centre, in game units.
func benchNBody(n int) *Simulation {
	s := &Simulation{G: 1, Softening: 0.01}
	s.Bodies = append(s.Bodies, Body{Mass: 1e6})
	for i := 1; i < n; i++ {
		a := float64(i) * 2.399963
		r := 10 + float64(i%100)
		v := math.Sqrt(1e6 / r)
		s.Bodies = append(s.Bodies, Body{Pos: V3(r*math.Cos(a), r*math.Sin(a), 0), Vel: V3(-v*math.Sin(a), v*math.Cos(a), 0), Mass: 1})
	}
	return s
}

// BenchmarkNBodyStep steps an n-body simulation once.
func BenchmarkNBodyStep(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s := benchNBody(n)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.Step(1e-4)
			}
		})
	}
}

// benchSystem is a star with planets, each with moons, as Kepler bodies
// (1000 in all, every one massive and carrying a transform), plus the
// given number of ships.
func benchSystem(ships int) *ecs.World {
	w := ecs.NewWorld()
	w.SetResource(Settings{G: 1})
	w.AddSystem("orbit", System)
	star := w.SpawnWith(Body{Mass: 1e6}, gfx.Transform{})
	count := 1
	for p := 0; count < 1000; p++ {
		planet := w.SpawnWith(Body{Mass: 100}, gfx.Transform{},
			Kepler{Primary: star, Elements: Elements{SemiMajorAxis: 100 + float64(p)*20, Eccentricity: 0.05, TrueAnomaly: float64(p)}})
		count++
		for m := 0; m < 19 && count < 1000; m++ {
			w.SpawnWith(Body{Mass: 0.01}, gfx.Transform{},
				Kepler{Primary: planet, Elements: Elements{SemiMajorAxis: 2 + float64(m)*0.5, Eccentricity: 0.01, Inclination: 0.1, TrueAnomaly: float64(m)}})
			count++
		}
	}
	for i := range ships {
		r := 150.0 + float64(i)
		v := math.Sqrt(1e6 / r)
		w.SpawnWith(Body{Pos: V3(r, 0, 0), Vel: V3(0, v, 0), Mass: 1}, Ship{}, gfx.Transform{})
	}
	w.Update(1.0 / 60)
	return w
}

// BenchmarkOrbitSystem runs one update of the orbit system over 1000
// Kepler bodies, with no ships and with ten.
func BenchmarkOrbitSystem(b *testing.B) {
	for _, ships := range []int{0, 10} {
		b.Run(fmt.Sprintf("ships=%d", ships), func(b *testing.B) {
			w := benchSystem(ships)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.Update(1.0 / 60)
			}
		})
	}
}
