package phys

import (
	"testing"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// TestQueriesAllocNothing holds the queries to no allocations once their
// buffers have grown. A shape placed for a query used to be built afresh
// every time, which cost hundreds of allocations a step with a hundred
// fast bodies over a large static set.
func TestQueriesAllocNothing(t *testing.T) {
	cases := []struct {
		name string
		run  func() func()
	}{
		{"ShapeCast3", func() func() {
			w := statics3(2000)
			return func() {
				ShapeCast3(w, Sphere{Radius: 0.4}, lin.V3(-300, 0, 0), lin.Quat{}, lin.V3(600, 0, 0), 0)
			}
		}},
		{"ShapeCast2", func() func() {
			w := statics2(2000)
			return func() {
				ShapeCast2(w, Circle{Radius: 0.4}, lin.V2(-300, 0), 0, lin.V2(600, 0), 0)
			}
		}},
		{"RaycastAll3Into", func() func() {
			w := statics3(2000)
			var hits []Hit3
			return func() {
				hits = RaycastAll3Into(hits[:0], w, Ray3{Origin: lin.V3(-300, 0, 0), Dir: lin.V3(600, 0, 0)}, 0)
			}
		}},
		{"OverlapShape3Into", func() func() {
			w := statics3(2000)
			var hits []Hit3
			return func() {
				hits = OverlapShape3Into(hits[:0], w, Sphere{Radius: 2}, lin.V3(0, 0, 0), lin.Quat{}, 0)
			}
		}},
		{"CharacterMove3", func() func() {
			w := ecs.NewWorld()
			w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
			w.AddSystem("phys", System3)
			w.SpawnWith(gfx.At(0, -0.5, 0), Collider3{Shape: Box3{Half: lin.V3(60, 0.5, 60)}})
			for i := range 16 {
				y := 0.15 * float32(i)
				w.SpawnWith(gfx.At(4+float32(i)*0.5, y, 0), Collider3{Shape: Box3{Half: lin.V3(0.25, y+0.15, 3)}})
			}
			e := w.SpawnWith(gfx.At(0, 1, 0))
			w.Update(step)
			c := CharacterController3{Radius: 0.35, HalfHeight: 0.45, StepHeight: 0.35}
			tr, _ := w.Get[gfx.Transform](e)
			return func() {
				tr.Position = lin.V3(3.5, 1, 0)
				c.Move(w, e, lin.V3(2, -6, 0), step)
			}
		}},
		{"CharacterMove2", func() func() {
			w := ecs.NewWorld()
			w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
			w.AddSystem("phys", System2)
			w.SpawnWith(gfx.At2(0, -0.5), Collider2{Shape: Box2{HalfW: 60, HalfH: 0.5}})
			for i := range 16 {
				y := 0.15 * float32(i)
				w.SpawnWith(gfx.At2(4+float32(i)*0.5, y), Collider2{Shape: Box2{HalfW: 0.25, HalfH: y + 0.15}})
			}
			e := w.SpawnWith(gfx.At2(0, 1))
			w.Update(step)
			c := CharacterController2{Radius: 0.35, HalfHeight: 0.45, StepHeight: 0.35}
			tr, _ := w.Get[gfx.Transform2](e)
			return func() {
				tr.Position = lin.V2(3.5, 1)
				c.Move(w, e, lin.V2(2, -6), step)
			}
		}},
		{"CCDStep", func() func() {
			w := ccd3(100)
			return func() { w.Update(step) }
		}},
		{"RaycastAll3IntoCapsulesAndHulls", func() func() {
			w := ecs.NewWorld()
			w.SetResource(Settings3{})
			w.AddSystem("phys", System3)
			for i := range 50 {
				var s Shape3 = Capsule{Radius: 0.3, HalfHeight: 0.5}
				if i%2 == 1 {
					s = octahedron(0.5)
				}
				w.SpawnWith(gfx.At(float32(i)*2, 0, 0), Collider3{Shape: s})
			}
			w.Update(step)
			var hits []Hit3
			return func() {
				hits = RaycastAll3Into(hits[:0], w, Ray3{Origin: lin.V3(-5, 0, 0), Dir: lin.V3(110, 0, 0)}, 0)
			}
		}},
		{"ShapeCast3Mesh", func() func() {
			w := ecs.NewWorld()
			w.SetResource(Settings3{})
			w.AddSystem("phys", System3)
			w.SpawnWith(gfx.Transform{}, Collider3{Shape: flatMesh(20, 0, 20)})
			w.Update(step)
			return func() {
				ShapeCast3(w, Capsule{Radius: 0.35, HalfHeight: 0.45}, lin.V3(3, 1.2, 2), lin.Quat{}, lin.V3(0, -0.5, 0), 0)
			}
		}},
		{"CharacterMove3Mesh", func() func() {
			w := ecs.NewWorld()
			w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
			w.AddSystem("phys", System3)
			w.SpawnWith(gfx.Transform{}, Collider3{Shape: flatMesh(20, 0, 20)})
			e := w.SpawnWith(gfx.At(0, 0.9, 0), Collider3{Shape: Capsule{Radius: 0.35, HalfHeight: 0.45}})
			w.Update(step)
			c := CharacterController3{Radius: 0.35, HalfHeight: 0.45, StepHeight: 0.35}
			tr, _ := w.Get[gfx.Transform](e)
			return func() {
				tr.Position = lin.V3(0, 0.9, 0)
				c.Move(w, e, lin.V3(2, -6, 1), step)
			}
		}},
		{"CCDStepMesh", func() func() {
			w := ecs.NewWorld()
			w.SetResource(Settings3{Gravity: lin.V3(0, -10, 0)})
			w.AddSystem("phys", System3)
			w.SpawnWith(gfx.Transform{}, Collider3{Shape: flatMesh(20, 0, 20)})
			body := Dynamic3(1)
			body.CCD = true
			e := w.SpawnWith(gfx.At(0, 5, 0), body, Collider3{Shape: Capsule{Radius: 0.2, HalfHeight: 0.3}})
			w.Update(step)
			tr, _ := w.Get[gfx.Transform](e)
			b, _ := w.Get[Body3](e)
			return func() {
				tr.Position, b.Vel = lin.V3(0, 5, 0), lin.V3(0, -300, 0)
				w.Update(step)
			}
		}},
		{"Raycast2ChainAndPolygon", func() func() {
			w := ecs.NewWorld()
			w.SetResource(Settings2{})
			w.AddSystem("phys", System2)
			w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Chain2{Points: []lin.Vec2{{X: -20, Y: 0}, {X: 0, Y: -1}, {X: 20, Y: 0}}}})
			hexagon := Polygon2{Points: []lin.Vec2{{X: 1, Y: 0}, {X: 0.5, Y: 0.8}, {X: -0.5, Y: 0.8}, {X: -1, Y: 0}, {X: -0.5, Y: -0.8}, {X: 0.5, Y: -0.8}}}
			for i := range 10 {
				w.SpawnWith(gfx.At2(float32(i)*3-15, 3), Collider2{Shape: hexagon})
			}
			w.Update(step)
			var hits []Hit2
			return func() {
				hits = RaycastAll2Into(hits[:0], w, Ray2{Origin: lin.V2(-18, 3), Dir: lin.V2(36, 0)}, 0)
				Raycast2(w, Ray2{Origin: lin.V2(1, 5), Dir: lin.V2(0, -10)}, 0)
				Nearest2(w, lin.V2(1, 1), 3, 0)
			}
		}},
		{"Step2Chain", func() func() {
			w := ecs.NewWorld()
			w.SetResource(Settings2{Gravity: lin.V2(0, -10)})
			w.AddSystem("phys", System2)
			w.SpawnWith(gfx.At2(0, 0), Collider2{Shape: Chain2{Points: []lin.Vec2{{X: -20, Y: 0}, {X: 0, Y: -1}, {X: 20, Y: 0}}}})
			e := w.SpawnWith(gfx.At2(0, 2), Dynamic2(1), Collider2{Shape: Circle{Radius: 0.5}})
			w.Update(step)
			tr, _ := w.Get[gfx.Transform2](e)
			return func() {
				tr.Position = lin.V2(0, -0.2)
				w.Update(step)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := c.run()
			for range 4 {
				run() // grow the buffers and fill the shape cache
			}
			if n := testing.AllocsPerRun(50, run); n != 0 {
				t.Errorf("%s allocates %v times per call, want none", c.name, n)
			}
		})
	}
}
