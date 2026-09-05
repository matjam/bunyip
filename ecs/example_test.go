package ecs_test

import (
	"bytes"
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
	movers := w.Query2[Position, Velocity]()
	movers.Each(func(e ecs.Entity, p *Position, v *Velocity) {
		p.X += v.X
		p.Y += v.Y
	})
	// Iteration order is by table and then by row, not by spawn order.
	w.Each(func(e ecs.Entity, p *Position) { fmt.Println(p.X, p.Y) })
	fmt.Println(movers.Count(), "movers of", w.Len())
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
	w.SetResource(Score{})
	w.SpawnWith(Health{1})
	w.SpawnWith(Health{5})

	// Systems run in order; producers before consumers.
	damage := w.Query1[Health]()
	w.AddSystem("damage", func(w *ecs.World, dt float64) {
		damage.Each(func(e ecs.Entity, h *Health) {
			h.HP--
			if h.HP <= 0 {
				w.Despawn(e) // safe: the visited entity may be despawned
				w.Emit(Killed{e})
			}
		})
	})
	w.AddSystem("score", func(w *ecs.World, dt float64) {
		w.Resource[Score]().Points += 10 * len(w.Events[Killed]())
	})
	w.Update(1.0 / 60)
	fmt.Println(w.Resource[Score]().Points, w.Len())
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

func ExamplePrefab() {
	// A template spawns as many independent copies as the game needs.
	tank := ecs.NewPrefab(Position{0, 0}, Health{10}).
		Child(ecs.NewPrefab(gfx.At(0, 1, 0))) // the turret
	w := ecs.NewWorld()
	a := tank.Spawn(w)
	b := tank.Spawn(w)
	if h, ok := w.Get[Health](a); ok {
		h.HP = 3
	}
	hb, _ := w.Get[Health](b)
	fmt.Println(hb.HP, len(ecs.ChildrenOf(w, a)), w.Len())
	// Output:
	// 10 1 4
}

func ExampleWorld_Save() {
	// Register names once, at start-up, so files outlive renames.
	ecs.Register[Position]("Position")
	ecs.Register[Health]("Health")

	w := ecs.NewWorld()
	w.SpawnWith(Position{1, 2}, Health{5})
	var save bytes.Buffer
	if err := w.Save(&save); err != nil {
		fmt.Println(err)
	}

	loaded := ecs.NewWorld()
	if err := loaded.Load(&save); err != nil {
		fmt.Println(err)
	}
	loaded.Each(func(e ecs.Entity, p *Position) { fmt.Println(p.X, p.Y) })
	// Output:
	// 1 2
}

func ExampleWorld_Instantiate() {
	// Register names once, at start-up; a scene file holds these.
	ecs.Register[Position]("Position")
	ecs.Register[Health]("Health")

	// A camp of two: the guard's Leader field points at the chief, which
	// the document writes as the chief's number in the entity list.
	doc := []byte(`{
	  "version": 1,
	  "name": "west camp",
	  "properties": {"music": "wind.ogg"},
	  "entities": [
	    {"name": "camp", "components": {"gfx.Transform": {"Position": {"X": 40}}}},
	    {"name": "chief", "parent": 1, "prefab": "orc", "components": {"Health": {"HP": 40}}},
	    {"parent": 1, "prefab": "orc", "components": {"gfx.Transform": {"Position": {"X": 2}}}}
	  ]
	}`)
	scene, err := ecs.ParseScene(doc)
	if err != nil {
		fmt.Println(err)
		return
	}

	w := ecs.NewWorld()
	w.SetResource(ecs.PrefabLibrary{"orc": ecs.NewPrefab(Health{8}, gfx.Transform{})})

	// Two copies of the camp, the second a hundred units east.
	west, err := w.Instantiate(scene)
	if err != nil {
		fmt.Println(err)
		return
	}
	if _, err := w.Instantiate(scene, ecs.InstantiateOptions{Offset: lin.V3(100, 0, 0)}); err != nil {
		fmt.Println(err)
		return
	}
	chief, _ := west.Entity("chief")
	h, _ := w.Get[Health](chief)
	fmt.Println(scene.Properties["music"], h.HP, w.Len())

	// Removing one copy takes its entities and leaves the other whole.
	west.Despawn(w)
	fmt.Println(w.Len())
	// Output:
	// wind.ogg 40 6
	// 3
}

func ExampleWorld_Defer() {
	w := ecs.NewWorld()
	w.SpawnWith(Health{3})
	w.SpawnWith(Health{0})

	// Changing other entities while a query walks them is not allowed;
	// the scope applies the recorded changes after the walk finishes.
	w.Defer(func(cmd *ecs.Commands) {
		w.Each(func(e ecs.Entity, h *Health) {
			if h.HP == 0 {
				cmd.Spawn(Position{1, 1}) // a corpse
				cmd.Despawn(e)
			}
		})
	})
	fmt.Println(w.Count[Health](), w.Count[Position]())
	// Output:
	// 1 1
}
