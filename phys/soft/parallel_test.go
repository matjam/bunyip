package soft_test

import (
	"math"
	"runtime"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
	"github.com/matjam/bunyip/phys/soft"
)

// bigScene is a world large enough that the solver splits its passes
// across goroutines: a cloth and a fluid of several thousand particles
// each and twenty soft bodies, with colliders among them.
func bigScene() *ecs.World {
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0), Gravity2: lin.V2(0, 900), Ground: true, GroundY: -1})
	w.SpawnWith(gfx.Transform{Position: lin.V3(0, 0.5, 0)}, phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
	w.SpawnWith(gfx.Transform{Position: lin.V3(0.8, 0.1, 0.3), Rotation: lin.AxisAngle(lin.V3(0, 1, 0), 0.6)},
		phys.Collider3{Shape: phys.Box3{Half: lin.V3(0.4, 0.3, 0.5)}})
	w.SpawnWith(soft.NewCloth(soft.ClothSpec{Width: 150, Height: 150, Spacing: 0.02, Mass: 0.5,
		Origin: lin.V3(-1.5, 1.5, -1.5), Right: lin.V3(1, 0, 0), Down: lin.V3(0, 0, 1), Pinned: []int{0, 149}, Wind: lin.V3(1, 0, 0.5)}))
	verts, idx := gfx.SphereMesh(10, 12)
	for i := range 20 {
		w.SpawnWith(soft.NewSoftBody3(soft.SoftBody3Spec{Vertices: verts, Indices: idx, Scale: 0.3,
			Position: lin.V3(float32(i%5)*0.7-1.5, 1.2+float32(i/5)*0.3, float32(i%3)*0.5), Mass: 2}))
	}
	f := soft.NewFluid2(soft.Fluid2Spec{Bounds: lin.Rect{W: 800, H: 600}, Spacing: 8})
	f.Fill(lin.Rect{X: 8, Y: 200, W: 784, H: 392})
	w.SpawnWith(f)
	w.SpawnWith(gfx.Transform2{Position: lin.V2(400, 400), Rotation: 0.4}, phys.Collider2{Shape: phys.Box2{HalfW: 60, HalfH: 20}})
	w.AddSystem("soft", soft.System)
	return w
}

// TestSmallScenesAllocateNothing holds a cloth, a soft body and a fluid
// below the sizes that split across goroutines to no allocations a
// step: a closure handed to the goroutines must not move its captured
// variables to the heap on the path that never starts one.
func TestSmallScenesAllocateNothing(t *testing.T) {
	w := ecs.NewWorld()
	w.SetResource(soft.Settings{Gravity3: lin.V3(0, -9.8, 0), Gravity2: lin.V2(0, 900), Ground: true})
	w.SpawnWith(gfx.Transform{Position: lin.V3(0, 0.5, 0)}, phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
	w.SpawnWith(soft.NewCloth(soft.ClothSpec{Width: 24, Height: 24, Pinned: []int{0, 23}}))
	verts, idx := gfx.SphereMesh(8, 10)
	for i := range 2 {
		w.SpawnWith(soft.NewSoftBody3(soft.SoftBody3Spec{Vertices: verts, Indices: idx, Scale: 0.3, Position: lin.V3(float32(i), 1, 0)}))
	}
	f := soft.NewFluid2(soft.Fluid2Spec{Bounds: lin.Rect{W: 320, H: 240}, Spacing: 8})
	f.Fill(lin.Rect{X: 8, Y: 80, W: 300, H: 150})
	w.SpawnWith(f)
	w.AddSystem("soft", soft.System)
	for range 10 {
		w.Update(step)
	}
	if n := testing.AllocsPerRun(20, func() { w.Update(step) }); n != 0 {
		t.Errorf("a small scene allocates %v times a step", n)
	}
}

// snapshot is every particle position in a world, as raw bits.
func snapshot(w *ecs.World) []uint32 {
	var out []uint32
	add3 := func(ps []lin.Vec3) {
		for _, p := range ps {
			out = append(out, math.Float32bits(p.X), math.Float32bits(p.Y), math.Float32bits(p.Z))
		}
	}
	w.Each(func(_ ecs.Entity, c *soft.Cloth) { add3(c.Positions()) })
	w.Each(func(_ ecs.Entity, b *soft.SoftBody3) { add3(b.Particles()) })
	w.Each(func(_ ecs.Entity, f *soft.Fluid2) {
		for _, p := range f.Positions() {
			out = append(out, math.Float32bits(p.X), math.Float32bits(p.Y))
		}
	})
	return out
}

// TestParallelMatchesOneThread steps the same large scene on one
// goroutine and on several and wants every particle in the same place,
// bit for bit: the split passes write only their own particles and the
// cloth's link batches share no particle, so how the work is shared out
// must not show in the result.
func TestParallelMatchesOneThread(t *testing.T) {
	steps := 20
	if testing.Short() {
		steps = 5
	}
	results := map[int][]uint32{}
	for _, procs := range []int{1, 4, 8} {
		old := runtime.GOMAXPROCS(procs)
		w := bigScene()
		for range steps {
			w.Update(step)
		}
		runtime.GOMAXPROCS(old)
		results[procs] = snapshot(w)
	}
	for _, procs := range []int{4, 8} {
		a, b := results[1], results[procs]
		if len(a) != len(b) {
			t.Fatalf("%d goroutines: %d numbers, want %d", procs, len(b), len(a))
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("%d goroutines: number %d is %08x, one goroutine gives %08x", procs, i, b[i], a[i])
			}
		}
	}
}
