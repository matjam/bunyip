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
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
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
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
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
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
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

// ccd3 flies n small fast bodies with CCD through a field of static
// boxes, so the cost is the sweep each of them runs every substep.
func ccd3(n int) *ecs.World {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	r := benchRand(4242)
	for range 2000 {
		w.SpawnWith(gfx.At((r.next()-0.5)*200, (r.next()-0.5)*40, (r.next()-0.5)*200),
			Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	}
	for range n {
		body := Dynamic3(0.05)
		body.CCD = true
		body.Vel = lin.V3((r.next()-0.5)*300, (r.next()-0.5)*40, (r.next()-0.5)*300)
		w.SpawnWith(gfx.At((r.next()-0.5)*100, (r.next()-0.5)*20, (r.next()-0.5)*100), body,
			Collider3{Shape: Sphere{Radius: 0.05}})
	}
	w.Update(step)
	return w
}

// stairs3 is a floor with a flight of steps for the character benchmark.
func stairs3(w *ecs.World) {
	w.SpawnWith(gfx.At(0, -0.5, 0), Collider3{Shape: Box3{Half: lin.V3(60, 0.5, 60)}})
	for i := range 16 {
		y := 0.15 * float32(i)
		w.SpawnWith(gfx.At(4+float32(i)*0.5, y, 0),
			Collider3{Shape: Box3{Half: lin.V3(0.25, y+0.15, 3)}})
	}
	// Enough scenery around the character that a query which scans every
	// collider pays for it.
	r := benchRand(31337)
	for range 2000 {
		w.SpawnWith(gfx.At((r.next()-0.5)*200, (r.next()-0.5)*40, (r.next()-0.5)*200),
			Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
	}
}

func BenchmarkCCD3_100Fast(b *testing.B) {
	w := ccd3(100)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}

func BenchmarkShapeCast3_4000(b *testing.B) {
	w := statics3(4000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ShapeCast3(w, Sphere{Radius: 0.4}, lin.V3(-300, 0, 0), lin.Quat{}, lin.V3(600, 0, 0), 0)
	}
}

func BenchmarkCharacter3Move(b *testing.B) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	stairs3(w)
	e := w.SpawnWith(gfx.At(0, 1, 0))
	w.Update(step)
	c := CharacterController3{Radius: 0.35, HalfHeight: 0.45, StepHeight: 0.35}
	tr, _ := w.Get[gfx.Transform](e)
	c.Move(w, e, lin.V3(2, -6, 0), step) // warm the caches
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		// The same move from the same pose every time, so the benchmark
		// measures a steady state rather than a walk across the level.
		tr.Position = lin.V3(3.5, 1, 0)
		c.Move(w, e, lin.V3(2, -6, 0), step)
	}
}

func BenchmarkShapeCast2_4000(b *testing.B) {
	w := statics2(4000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ShapeCast2(w, Circle{Radius: 0.4}, lin.V2(-300, 0), 0, lin.V2(600, 0), 0)
	}
}

func BenchmarkCharacter2Move(b *testing.B) {
	w := ecs.NewWorld()
	w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	w.SpawnWith(gfx.At2(0, -0.5), Collider2{Shape: Box2{HalfW: 60, HalfH: 0.5}})
	r := benchRand(31337)
	for range 2000 {
		w.SpawnWith(gfx.At2((r.next()-0.5)*400, (r.next()-0.5)*40),
			Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}})
	}
	e := w.SpawnWith(gfx.At2(0, 1))
	w.Update(step)
	c := CharacterController2{Radius: 0.35, HalfHeight: 0.45, StepHeight: 0.35}
	tr, _ := w.Get[gfx.Transform2](e)
	c.Move(w, e, lin.V2(2, -6), step) // warm the caches
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		// The same move from the same pose every time, so the benchmark
		// measures a steady state rather than a walk across the level.
		tr.Position = lin.V2(3.5, 1)
		c.Move(w, e, lin.V2(2, -6), step)
	}
}

// statics2 scatters n static boxes for the 2D query benchmarks.
func statics2(n int) *ecs.World {
	w := ecs.NewWorld()
	w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
	w.AddSystem("phys", System2)
	r := benchRand(999)
	for range n {
		w.SpawnWith(gfx.At2((r.next()-0.5)*400, (r.next()-0.5)*10),
			Collider2{Shape: Box2{HalfW: 0.5, HalfH: 0.5}})
	}
	w.Update(step)
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
	w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
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
	w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
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
