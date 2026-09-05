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
			ecs.SetResource(w, Settings3{Gravity: lin.V3(0, -10, 0)})
			w.AddSystem("phys", System3)
			w.SpawnWith(gfx.At(0, -0.5, 0), Collider3{Shape: Box3{Half: lin.V3(60, 0.5, 60)}})
			for i := range 16 {
				y := 0.15 * float32(i)
				w.SpawnWith(gfx.At(4+float32(i)*0.5, y, 0), Collider3{Shape: Box3{Half: lin.V3(0.25, y+0.15, 3)}})
			}
			e := w.SpawnWith(gfx.At(0, 1, 0))
			w.Update(step)
			c := CharacterController3{Radius: 0.35, HalfHeight: 0.45, StepHeight: 0.35}
			tr, _ := ecs.Get[gfx.Transform](w, e)
			return func() {
				tr.Position = lin.V3(3.5, 1, 0)
				c.Move(w, e, lin.V3(2, -6, 0), step)
			}
		}},
		{"CharacterMove2", func() func() {
			w := ecs.NewWorld()
			ecs.SetResource(w, Settings2{Gravity: lin.V2(0, -10)})
			w.AddSystem("phys", System2)
			w.SpawnWith(gfx.At2(0, -0.5), Collider2{Shape: Box2{HalfW: 60, HalfH: 0.5}})
			for i := range 16 {
				y := 0.15 * float32(i)
				w.SpawnWith(gfx.At2(4+float32(i)*0.5, y), Collider2{Shape: Box2{HalfW: 0.25, HalfH: y + 0.15}})
			}
			e := w.SpawnWith(gfx.At2(0, 1))
			w.Update(step)
			c := CharacterController2{Radius: 0.35, HalfHeight: 0.45, StepHeight: 0.35}
			tr, _ := ecs.Get[gfx.Transform2](w, e)
			return func() {
				tr.Position = lin.V2(3.5, 1)
				c.Move(w, e, lin.V2(2, -6), step)
			}
		}},
		{"CCDStep", func() func() {
			w := ccd3(100)
			return func() { w.Update(step) }
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
