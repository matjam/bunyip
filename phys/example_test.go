package phys_test

import (
	"fmt"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
)

func Example() {
	w := ecs.NewWorld()
	w.SetResource(phys.Settings3{Gravity: lin.V3(0, -9.8, 0)})
	w.AddSystem("physics", phys.System3)

	// A static floor (collider, no body) and a ball dropped onto it.
	w.SpawnWith(gfx.Transform{}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(10, 0.5, 10)}})
	ball := w.SpawnWith(gfx.At(0, 5, 0), phys.Dynamic3(1), phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
	for range 180 {
		w.Update(1.0 / 60)
	}
	t, _ := w.Get[gfx.Transform](ball)
	fmt.Printf("ball rests at y = %.1f\n", t.Position.Y)
	// Output:
	// ball rests at y = 1.0
}

func ExampleRaycast2() {
	w := ecs.NewWorld()
	w.AddSystem("physics", phys.System2)
	w.SpawnWith(gfx.At2(100, 0), phys.Collider2{Shape: phys.Box2{HalfW: 10, HalfH: 10}})
	w.Update(1.0 / 60)
	hit, ok := phys.Raycast2(w, phys.Ray2{Origin: lin.V2(0, 0), Dir: lin.V2(200, 0)}, 0)
	fmt.Printf("%v at (%.0f, %.0f), normal x %.0f\n", ok, hit.Point.X, hit.Point.Y, hit.Normal.X)
	// Output:
	// true at (90, 0), normal x -1
}
