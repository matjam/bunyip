package ecs_test

import (
	"fmt"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

type Position struct{ X, Y float32 }
type Velocity struct{ X, Y float32 }
type Health struct{ HP int }

func Example() {
	w := ecs.NewWorld()
	w.SpawnWith(Position{0, 0}, Velocity{1, 0}, Health{10})
	w.SpawnWith(Position{5, 5}, Velocity{0, 1})
	w.SpawnWith(Position{9, 9}) // no velocity: never moves

	// A query walks every entity with both components; keep it and reuse it.
	movers := ecs.NewQuery2[Position, Velocity](w)
	movers.Each(func(e ecs.Entity, p *Position, v *Velocity) {
		p.X += v.X
		p.Y += v.Y
	})
	// Iteration order is by table and then by row, not by spawn order.
	ecs.Each(w, func(e ecs.Entity, p *Position) { fmt.Println(p.X, p.Y) })
	fmt.Println(movers.Count(), "movers of", w.Count())
	// Output:
	// 1 0
	// 5 6
	// 9 9
	// 2 movers of 3
}

func ExampleWorld_AddSystem() {
	type Score struct{ Points int }
	type Killed struct{ Entity ecs.Entity }

	w := ecs.NewWorld()
	ecs.SetResource(w, Score{})
	w.SpawnWith(Health{1})
	w.SpawnWith(Health{5})

	// Systems run in order; producers before consumers.
	damage := ecs.NewQuery1[Health](w)
	w.AddSystem("damage", func(w *ecs.World, dt float64) {
		damage.Each(func(e ecs.Entity, h *Health) {
			h.HP--
			if h.HP <= 0 {
				w.Despawn(e) // safe: the visited entity may be despawned
				ecs.Emit(w, Killed{e})
			}
		})
	})
	w.AddSystem("score", func(w *ecs.World, dt float64) {
		ecs.Resource[Score](w).Points += 10 * len(ecs.Events[Killed](w))
	})
	w.Update(1.0 / 60)
	fmt.Println(ecs.Resource[Score](w).Points, w.Count())
	// Output:
	// 10 1
}

func ExampleSetParent() {
	// A turret on a tank: the turret's transform is relative to the tank.
	w := ecs.NewWorld()
	tank := w.SpawnWith(gfx.At(10, 0, 0))
	turret := w.SpawnWith(gfx.At(0, 1, 0))
	ecs.SetParent(w, turret, tank)
	fmt.Println(ecs.WorldMatrix(w, turret).MulPoint(lin.Vec3{}))
	w.Despawn(tank) // children go with their parent
	fmt.Println(w.Alive(turret))
	// Output:
	// {10 1 0}
	// false
}

func ExampleCommands() {
	w := ecs.NewWorld()
	w.SpawnWith(Health{3})
	w.SpawnWith(Health{0})

	// Changing other entities while a query walks them is not allowed;
	// record the changes and apply them after.
	var cmd ecs.Commands
	ecs.Each(w, func(e ecs.Entity, h *Health) {
		if h.HP == 0 {
			cmd.Spawn(Position{1, 1}) // a corpse
			cmd.Despawn(e)
		}
	})
	cmd.Apply(w)
	fmt.Println(ecs.Count[Health](w), ecs.Count[Position](w))
	// Output:
	// 1 1
}
