package phys

import (
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// benchRand is a deterministic generator so every run of a benchmark
// builds the same world.
type benchRand uint32

func (r *benchRand) next() float32 {
	*r = *r*1664525 + 1013904223
	return float32(uint32(*r)>>8) / float32(1<<24)
}

// falling3 scatters n dynamic spheres in the air with nothing to hit, so
// the cost is the broadphase and the per-body bookkeeping.
func falling3(n int) *ecs.World {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	r := benchRand(12345)
	for range n {
		x := (r.next() - 0.5) * 400
		y := 20 + r.next()*400
		z := (r.next() - 0.5) * 400
		w.SpawnWith(gfx.At(x, y, z), Dynamic3(1), Collider3{Shape: Sphere{Radius: 0.5}})
	}
	return w
}

// stacked3 builds columns of boxes on a floor, so the cost is contact
// generation and the solver.
func stacked3(n int) *ecs.World {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.At(0, -0.5, 0), Collider3{Shape: Box3{Half: lin.V3(60, 0.5, 60)}})
	const height = 10
	for i := range n {
		col := i / height
		level := i % height
		x := float32(col%10) * 3
		z := float32(col/10) * 3
		w.SpawnWith(gfx.At(x, 0.5+float32(level)*1.01, z), Dynamic3(1),
			Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	}
	return w
}

// statics3 scatters n static boxes for the query benchmarks.
func statics3(n int) *ecs.World {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	r := benchRand(999)
	for range n {
		x := (r.next() - 0.5) * 400
		y := (r.next() - 0.5) * 10
		z := (r.next() - 0.5) * 10
		w.SpawnWith(gfx.At(x, y, z), Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	}
	w.Update(step) // build the collider query
	return w
}

func BenchmarkStep3D_4000Dynamic(b *testing.B) {
	w := falling3(4000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}

func BenchmarkBoxes3D_500Stacked(b *testing.B) {
	w := stacked3(500)
	for range 30 { // let the stacks settle before timing
		w.Update(step)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}

func BenchmarkRaycast3_4000(b *testing.B) {
	w := statics3(4000)
	ray := Ray3{Origin: lin.V3(-300, 0, 0), Dir: lin.V3(600, 0, 0)}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if hits := RaycastAll3(w, ray, 0); len(hits) < 0 {
			b.Fatal("unreachable")
		}
	}
}

func BenchmarkRaycast3_4000Into(b *testing.B) {
	w := statics3(4000)
	ray := Ray3{Origin: lin.V3(-300, 0, 0), Dir: lin.V3(600, 0, 0)}
	var hits []Hit3
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hits = RaycastAll3Into(hits[:0], w, ray, 0)
	}
	_ = hits
}

// falling2 is the 2D equivalent of falling3.
func falling2(n int) *ecs.World {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	r := benchRand(12345)
	for range n {
		x := (r.next() - 0.5) * 4000
		y := 20 + r.next()*4000
		w.SpawnWith(gfx.At2(x, y), Dynamic2(1), Collider2{Shape: Circle{Radius: 0.5}})
	}
	return w
}

// stacked2 is the 2D equivalent of stacked3.
func stacked2(n int) *ecs.World {
	w := ecs.NewWorld()
	ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	w.SpawnWith(gfx.At2(0, -0.5), Collider2{Shape: Box2{HalfW: 200, HalfH: 0.5}})
	const height = 10
	for i := range n {
		col := i / height
		level := i % height
		w.SpawnWith(gfx.At2(float32(col)*3, 0.5+float32(level)*1.01), Dynamic2(1),
			Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}})
	}
	return w
}

func BenchmarkStep2D_4000Dynamic(b *testing.B) {
	w := falling2(4000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}

func BenchmarkBoxes2D_500Stacked(b *testing.B) {
	w := stacked2(500)
	for range 30 {
		w.Update(step)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}
