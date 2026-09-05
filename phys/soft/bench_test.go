package soft_test

import (
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
	"github.com/matjam/bunyip/phys/soft"
)

// BenchmarkCloth steps a sheet of a thousand particles hanging from its
// top edge over a sphere, at the default solver quality.
func BenchmarkCloth(b *testing.B) {
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0)})
	w.SpawnWith(gfx.Transform{Position: lin.V3(0, 0.5, 0)}, phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
	pins := make([]int, 0, 32)
	for x := range 32 {
		pins = append(pins, x)
	}
	w.SpawnWith(soft.NewCloth(soft.ClothSpec{
		Width: 32, Height: 32, Spacing: 0.1, Mass: 0.5,
		Origin: lin.V3(-1.5, 2, 0), Pinned: pins, Wind: lin.V3(3, 0, 1),
	}))
	w.AddSystem("soft", soft.System)
	run(w, 0.5)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}

// BenchmarkSoftBody steps a jelly ball of about two hundred particles
// resting on the ground.
func BenchmarkSoftBody(b *testing.B) {
	verts, idx := gfx.SphereMesh(12, 16)
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0), Ground: true})
	w.SpawnWith(soft.NewSoftBody3(soft.SoftBody3Spec{Vertices: verts, Indices: idx, Scale: 0.5, Position: lin.V3(0, 0.6, 0), Mass: 2}))
	w.AddSystem("soft", soft.System)
	run(w, 0.5)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}

// BenchmarkFluid steps two thousand fluid particles settled in a tank.
func BenchmarkFluid(b *testing.B) {
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity2: lin.V2(0, 900)})
	f := soft.NewFluid2(soft.Fluid2Spec{Bounds: lin.Rect{X: 0, Y: 0, W: 640, H: 480}, Spacing: 8})
	f.Fill(lin.Rect{X: 8, Y: 160, W: 424, H: 312})
	if f.Count() < 2000 {
		b.Fatalf("the tank holds only %d particles", f.Count())
	}
	w.SpawnWith(f)
	w.AddSystem("soft", soft.System)
	run(w, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}
