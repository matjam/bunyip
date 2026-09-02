package particle_test

import (
	"fmt"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/particle"
)

// A campfire: a preset tweaked before New, updated each step and drawn
// each frame. Drawing needs a Graphics, so it is only shown here.
func Example() {
	fire := particle.Fire()
	fire.Position = lin.V2(480, 400)
	fire.Rate = 120
	fire.Prewarm = 1
	sys := particle.New(fire)

	// In Update:
	sys.Update(1.0 / 60)
	// In Draw:
	//	sys.Draw(ctx.Gfx)

	fmt.Println(sys.Emitting(), sys.Alive() > 0)
	// Output:
	// true true
}

// An emitter from scratch: a ring of green motes drifting outward and
// fading, with a curve for size and keyframes for colour.
func ExampleEmitter() {
	e := particle.Emitter{
		Rate:         30,
		Lifetime:     particle.Range{Min: 1, Max: 2},
		Shape:        particle.Ring(20),
		Spread:       6.3, // every direction
		Speed:        particle.Range{Min: 10, Max: 30},
		RadialAccel:  40,
		Size:         particle.Range{Min: 4, Max: 8},
		SizeOverLife: particle.Keys(0, 0, 0.2, 1, 1, 0),
		ColorOverLife: []particle.ColorKey{
			{T: 0, Color: gfx.RGB(120, 255, 160)},
			{T: 1, Color: gfx.RGB(20, 80, 40)},
		},
		AlphaOverLife: particle.Linear(1, 0),
		Blend:         gfx.BlendAdd,
	}
	sys := particle.New(e)
	sys.Update(0.5)
	fmt.Println(sys.Alive())
	// Output:
	// 15
}

// A one-shot effect: confetti bursts on Start and the system is
// Finished once the pieces have fallen and faded.
func ExampleSystem_Finished() {
	e := particle.Confetti()
	e.Position = lin.V2(200, 100)
	sys := particle.New(e)
	fmt.Println(sys.Alive(), sys.Finished())
	for range 300 {
		sys.Update(1.0 / 60)
	}
	fmt.Println(sys.Alive(), sys.Finished())
	// Output:
	// 150 false
	// 0 true
}
