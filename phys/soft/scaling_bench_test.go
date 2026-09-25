package soft_test

import (
	"fmt"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
	"github.com/matjam/bunyip/phys/soft"
)

// clothScene hangs a side x side sheet from its top edge over a sphere,
// with extra static boxes scattered well away from it, so the cost of
// the solids scan shows separately from the constraint solve.
func clothScene(side, extraSolids int) *ecs.World {
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0)})
	w.SpawnWith(gfx.Transform{Position: lin.V3(0, 0.5, 0)}, phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
	for i := range extraSolids {
		x := float32(i%50)*4 + 100
		z := float32(i/50)*4 + 100
		w.SpawnWith(gfx.Transform{Position: lin.V3(x, 0, z)}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(1, 1, 1)}})
	}
	pins := make([]int, 0, side)
	for x := range side {
		pins = append(pins, x)
	}
	spacing := float32(3.2) / float32(side)
	w.SpawnWith(soft.NewCloth(soft.ClothSpec{
		Width: side, Height: side, Spacing: spacing, Mass: 0.5,
		Origin: lin.V3(-1.5, 2, 0), Pinned: pins, Wind: lin.V3(3, 0, 1),
	}))
	w.AddSystem("soft", soft.System)
	for range 10 {
		w.Update(step)
	}
	return w
}

// BenchmarkClothScaling steps cloths of 1k, 10k and 50k particles, and
// the smaller two among 200 distant boxes.
func BenchmarkClothScaling(b *testing.B) {
	for _, c := range []struct{ side, solids int }{{32, 0}, {100, 0}, {224, 0}, {32, 200}, {100, 200}} {
		b.Run(fmt.Sprintf("particles=%d/solids=%d", c.side*c.side, c.solids+1), func(b *testing.B) {
			w := clothScene(c.side, c.solids)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.Update(step)
			}
		})
	}
}

// fluidScene fills a tank sized for about n particles, with extra static
// boxes placed outside the tank so they never touch the fluid.
func fluidScene(n, extraSolids int) *ecs.World {
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity2: lin.V2(0, 900)})
	const spacing = 8
	// Fill covers about 60% of the tank height.
	cols := 200
	rows := n / cols
	width := float32(cols) * spacing
	f := soft.NewFluid2(soft.Fluid2Spec{Bounds: lin.Rect{X: 0, Y: 0, W: width + 16, H: float32(rows)*spacing*1.6 + 16}, Spacing: spacing})
	f.Fill(lin.Rect{X: 8, Y: float32(rows) * spacing * 0.6, W: width, H: float32(rows) * spacing})
	w.SpawnWith(f)
	for i := range extraSolids {
		w.SpawnWith(gfx.At2(-1000-float32(i)*40, -1000), phys.Collider2{Shape: phys.Box2{HalfW: 10, HalfH: 10}})
	}
	w.AddSystem("soft", soft.System)
	for range 10 {
		w.Update(step)
	}
	return w
}

// BenchmarkFluidScaling steps tanks of 2k, 10k and 50k particles, and
// the 10k tank with 20 boxes outside it.
func BenchmarkFluidScaling(b *testing.B) {
	for _, c := range []struct{ n, solids int }{{2000, 0}, {10000, 0}, {50000, 0}, {10000, 20}} {
		b.Run(fmt.Sprintf("particles=%d/solids=%d", c.n, c.solids), func(b *testing.B) {
			w := fluidScene(c.n, c.solids)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.Update(step)
			}
		})
	}
}

// BenchmarkSoftBodies50 steps 50 jelly balls of about two hundred
// particles each on the ground plane.
func BenchmarkSoftBodies50(b *testing.B) {
	verts, idx := gfx.SphereMesh(12, 16)
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0), Ground: true})
	for i := range 50 {
		w.SpawnWith(soft.NewSoftBody3(soft.SoftBody3Spec{Vertices: verts, Indices: idx, Scale: 0.5,
			Position: lin.V3(float32(i%10)*2, 0.6, float32(i/10)*2), Mass: 2}))
	}
	w.AddSystem("soft", soft.System)
	run(w, 0.2)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}
