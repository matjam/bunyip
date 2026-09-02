package scene_test

import (
	"fmt"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/scene"
)

type Health struct{ HP int }
type Name string

func Example() {
	w := scene.NewWorld()
	hero := w.Spawn()
	scene.Set(w, hero, Name("hero"))
	scene.Set(w, hero, Health{10})
	rat := w.Spawn()
	scene.Set(w, rat, Health{2})

	// Iterate by component type; modify through the pointer.
	scene.Each(w, func(e scene.Entity, h *Health) { h.HP-- })
	scene.Each2(w, func(e scene.Entity, n *Name, h *Health) {
		fmt.Println(*n, h.HP)
	})

	w.Despawn(rat)
	fmt.Println(w.Count(), w.Alive(rat))
	// Output:
	// hero 9
	// 1 false
}

func ExampleWorld_SetParent() {
	// A turret on a tank: the turret's transform is relative to the tank.
	w := scene.NewWorld()
	tank, turret := w.Spawn(), w.Spawn()
	scene.Set(w, tank, gfx.At(10, 0, 0))
	scene.Set(w, turret, gfx.At(0, 1, 0))
	w.SetParent(turret, tank)
	fmt.Println(w.WorldMatrix(turret).MulPoint(lin.Vec3{}))
	// Output:
	// {10 1 0}
}
