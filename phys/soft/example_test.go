package soft_test

import (
	"fmt"
	"math"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys/soft"
)

func Example() {
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0), Ground: true})
	w.AddSystem("soft", soft.System)

	// A ball of jelly dropped onto the ground plane.
	verts, indices := gfx.SphereMesh(12, 16)
	ball := w.SpawnWith(soft.NewSoftBody3(soft.SoftBody3Spec{
		Vertices: verts, Indices: indices, Scale: 0.5, Position: lin.V3(0, 3, 0), Mass: 2,
	}))
	for range 180 {
		w.Update(1.0 / 60)
	}
	b, _ := w.Get[soft.SoftBody3](ball)
	lowest := float32(math.Inf(1))
	for _, p := range b.Particles() {
		lowest = min(lowest, p.Y)
	}
	fmt.Printf("the jelly rests on the plane, lowest particle at y = %.2f, with %.0f%% of its volume\n",
		lowest, 100*b.Volume()/b.RestVolume())
	// Output:
	// the jelly rests on the plane, lowest particle at y = 0.06, with 100% of its volume
}

func ExampleNewCloth() {
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0)})
	w.AddSystem("soft", soft.System)

	// A sheet hung from its two top corners.
	const cols, rows = 12, 10
	sheet := w.SpawnWith(soft.NewCloth(soft.ClothSpec{
		Width: cols, Height: rows, Spacing: 0.1, Mass: 0.5,
		Origin: lin.V3(0, 2, 0), Pinned: []int{0, cols - 1},
	}))
	for range 240 {
		w.Update(1.0 / 60)
	}
	c, _ := w.Get[soft.Cloth](sheet)
	pos := c.Positions()
	fmt.Printf("the bottom corner hangs %.1f below the pin\n", pos[0].Y-pos[c.Index(0, rows-1)].Y)
	// Output:
	// the bottom corner hangs 0.9 below the pin
}

func ExampleNewFluid2() {
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity2: lin.V2(0, 900)})
	w.AddSystem("soft", soft.System)

	// A column of liquid released in a tank, in view units.
	f := soft.NewFluid2(soft.Fluid2Spec{Bounds: lin.Rect{W: 400, H: 300}, Spacing: 8})
	f.Fill(lin.Rect{X: 20, Y: 20, W: 120, H: 160})
	tank := w.SpawnWith(f)
	for range 240 {
		w.Update(1.0 / 60)
	}
	got, _ := w.Get[soft.Fluid2](tank)
	spread := float32(0)
	for _, p := range got.Positions() {
		spread = max(spread, p.X)
	}
	fmt.Printf("%d particles, spread to x = %.0f\n", got.Count(), spread)
	// Output:
	// 300 particles, spread to x = 396
}
