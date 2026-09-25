package phys

import (
	"fmt"
	"math"
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// benchTerrain builds a gently rolling height grid of side x side quads,
// two triangles each, spaced one unit apart and centred on the origin.
func benchTerrain(side int) MeshShape {
	verts := make([]lin.Vec3, 0, (side+1)*(side+1))
	for z := 0; z <= side; z++ {
		for x := 0; x <= side; x++ {
			fx, fz := float32(x)-float32(side)/2, float32(z)-float32(side)/2
			y := 0.3 * float32(math.Sin(float64(fx)*0.3)*math.Cos(float64(fz)*0.2))
			verts = append(verts, lin.V3(fx, y, fz))
		}
	}
	idx := make([]uint32, 0, side*side*6)
	for z := range side {
		for x := range side {
			a := uint32(z*(side+1) + x)
			b, c, d := a+1, a+uint32(side+1), a+uint32(side+1)+1
			idx = append(idx, a, c, b, b, c, d)
		}
	}
	return NewMeshShape(verts, idx)
}

// BenchmarkCharacters200Mesh moves 200 character controllers over a
// terrain of about 100k triangles, each carrying its own capsule
// collider, then steps the world once: one frame of a crowd.
func BenchmarkCharacters200Mesh(b *testing.B) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	terrain := benchTerrain(224)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: terrain})
	c := CharacterController3{Radius: 0.35, HalfHeight: 0.45, StepHeight: 0.35}
	cap := c.capsule()
	var ents []ecs.Entity
	var ctrls []CharacterController3
	for i := range 200 {
		x, z := float32(i%20)*8-80, float32(i/20)*8-40
		ents = append(ents, w.SpawnWith(gfx.At(x, 1.5, z), Collider3{Shape: cap}))
		ctrls = append(ctrls, c)
	}
	w.Update(step)
	vel := lin.V3(2, -6, 1)
	// Let every character fall onto the terrain first, so the timed moves
	// walk on the ground rather than float above it.
	for range 60 {
		for i, e := range ents {
			ctrls[i].Move(w, e, lin.V3(0, -6, 0), step)
		}
	}
	start := make([]lin.Vec3, len(ents))
	grounded := 0
	for i, e := range ents {
		ctrls[i].Move(w, e, vel, step)
		t, _ := w.Get[gfx.Transform](e)
		start[i] = t.Position
		if ctrls[i].Grounded {
			grounded++
		}
	}
	if grounded < len(ents) {
		b.Fatalf("only %d of %d characters are grounded", grounded, len(ents))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i, e := range ents {
			t, _ := w.Get[gfx.Transform](e)
			t.Position = start[i]
			ctrls[i].Move(w, e, vel, step)
		}
		w.Update(step)
	}
}

// BenchmarkCharacterMoveScaling is the stairs scene with a single
// controller, split by whether the scenery is 0, 2000 or 8000 boxes, to
// show the per-query cost growing with the collider count.
func BenchmarkCharacterMoveScaling(b *testing.B) {
	for _, n := range []int{0, 2000, 8000} {
		b.Run(fmt.Sprintf("scenery=%d", n), func(b *testing.B) {
			w := ecs.NewWorld()
			w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
			w.AddSystem("phys", System3)
			w.SpawnWith(gfx.At(0, -0.5, 0), Collider3{Shape: Box3{Half: lin.V3(60, 0.5, 60)}})
			r := benchRand(31337)
			for range n {
				w.SpawnWith(gfx.At((r.next()-0.5)*400, (r.next()-0.5)*40, (r.next()-0.5)*400),
					Collider3{Shape: Box3{Half: lin.V3(0.5, 0.5, 0.5)}})
			}
			e := w.SpawnWith(gfx.At(0, 1, 0))
			w.Update(step)
			c := CharacterController3{Radius: 0.35, HalfHeight: 0.45, StepHeight: 0.35}
			tr, _ := w.Get[gfx.Transform](e)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				tr.Position = lin.V3(3.5, 1, 0)
				c.Move(w, e, lin.V3(2, -6, 0), step)
			}
		})
	}
}

// BenchmarkRaycasts10k casts ten thousand short rays (ten units, a
// line-of-sight check) into 4000 static boxes: one frame's worth.
func BenchmarkRaycasts10k(b *testing.B) {
	w := statics3(4000)
	r := benchRand(7)
	rays := make([]Ray3, 10000)
	for i := range rays {
		o := lin.V3((r.next()-0.5)*400, (r.next()-0.5)*10, (r.next()-0.5)*10)
		rays[i] = Ray3{Origin: o, Dir: lin.V3((r.next()-0.5)*10, 0, (r.next()-0.5)*10)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, ray := range rays {
			Raycast3(w, ray, 0)
		}
	}
}

// BenchmarkStaticHeavy steps 100 falling spheres in a level of 10000
// static boxes that none of them touch, so the cost is what the step
// spends on colliders that cannot move.
func BenchmarkStaticHeavy(b *testing.B) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	r := benchRand(99)
	for range 10000 {
		w.SpawnWith(gfx.At((r.next()-0.5)*1000, (r.next()-0.5)*20, (r.next()-0.5)*1000),
			Collider3{Shape: Box3{Half: lin.V3(1, 1, 1)}})
	}
	for range 100 {
		w.SpawnWith(gfx.At((r.next()-0.5)*1000, 100+r.next()*100, (r.next()-0.5)*1000), Dynamic3(1), Collider3{Shape: Sphere{Radius: 0.5}})
	}
	w.Update(step)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}

// BenchmarkRagdolls50 steps fifty ragdolls dropped on a floor, reporting
// the share of bodies that are slow and asleep after ten seconds.
func BenchmarkRagdolls50(b *testing.B) {
	for _, sleep := range []float32{0, 0.5} {
		b.Run(fmt.Sprintf("sleep=%v", sleep), func(b *testing.B) {
			w := ecs.NewWorld()
			w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0), SleepTime: sleep})
			w.AddSystem("phys", System3)
			w.SpawnWith(gfx.Transform{}, Collider3{Shape: Box3{Half: lin.V3(100, 0.5, 100)}})
			for i := range 50 {
				NewRagdoll3(w, RagdollSpec{Position: lin.V3(float32(i%10)*3-15, 1, float32(i/10)*3-7),
					Rotation: lin.AxisAngle(lin.V3(1, 0, 0), 1.2)})
			}
			run(w, 10)
			asleep, total, slow := 0, 0, 0
			w.Each(func(_ ecs.Entity, body *Body3) {
				total++
				if body.Asleep() {
					asleep++
				}
				if body.Vel.Len() < 0.05 && body.AngVel.Len() < 0.05 {
					slow++
				}
			})
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w.Update(step)
			}
			b.ReportMetric(float64(slow)/float64(total), "slow")
			b.ReportMetric(float64(asleep)/float64(total), "asleep")
		})
	}
}

// BenchmarkTriggers steps 1000 dynamic spheres falling through ten
// large trigger volumes, so every body overlaps a trigger every substep.
func BenchmarkTriggers(b *testing.B) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	for i := range 10 {
		w.SpawnWith(gfx.At(float32(i)*40-200, 0, 0), Collider3{Shape: Box3{Half: lin.V3(20, 1000, 20)}, Trigger: true})
	}
	r := benchRand(5)
	for range 1000 {
		body := Dynamic3(1)
		body.GravityScale = 0.0001
		w.SpawnWith(gfx.At((r.next()-0.5)*400, r.next()*100, (r.next()-0.5)*40), body, Collider3{Shape: Sphere{Radius: 0.5}})
	}
	w.Update(step)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}

// BenchmarkHingeChain steps a hanging chain of 500 links joined by
// hinges, the joint solver's cost with no contacts.
func BenchmarkHingeChain(b *testing.B) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	prev := ecs.None
	for i := range 500 {
		e := w.SpawnWith(gfx.At(float32(i)*0.5+0.25, 50, 0), Dynamic3(1),
			Collider3{Shape: Box3{Half: lin.V3(0.25, 0.05, 0.05)}, Layers: Layers{Layer: 2, Mask: 1}})
		anchorA := lin.V3(0, 50, 0)
		if prev != ecs.None {
			anchorA = lin.V3(0.25, 0, 0)
		}
		w.SpawnWith(HingeJoint3{A: prev, B: e, AnchorA: anchorA, AnchorB: lin.V3(-0.25, 0, 0),
			AxisA: lin.V3(0, 0, 1), AxisB: lin.V3(0, 0, 1)})
		prev = e
	}
	run(w, 0.5)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.Update(step)
	}
}

// BenchmarkRaycastCapsules casts one ray through a row of 500 capsule
// colliders, the shape of every ragdoll part and character.
func BenchmarkRaycastCapsules(b *testing.B) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{})
	w.AddSystem("phys", System3)
	for i := range 500 {
		w.SpawnWith(gfx.At(float32(i)*2, 0, 0), Collider3{Shape: Capsule{Radius: 0.3, HalfHeight: 0.5}})
	}
	w.Update(step)
	ray := Ray3{Origin: lin.V3(-5, 0, 0), Dir: lin.V3(1010, 0, 0)}
	var hits []Hit3
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		hits = RaycastAll3Into(hits[:0], w, ray, 0)
	}
}

// BenchmarkShapeCastMesh sweeps a capsule a short way across the
// 100k-triangle terrain, the query a character's ground probe makes.
func BenchmarkShapeCastMesh(b *testing.B) {
	w := ecs.NewWorld()
	w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
	w.AddSystem("phys", System3)
	w.SpawnWith(gfx.Transform{}, Collider3{Shape: benchTerrain(224)})
	w.Update(step)
	cap := Capsule{Radius: 0.35, HalfHeight: 0.45}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ShapeCast3(w, cap, lin.V3(3, 1.2, 2), lin.Quat{}, lin.V3(0, -0.5, 0), 0)
	}
}
