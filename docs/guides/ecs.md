---
title: Entities and systems
group: Engine
order: 3
summary: the entity component system, how it stores data, and how to structure a game around it
---

The [ecs](../pkg/ecs.html) package stores a Bunyip game's world.
An entity is a handle, components are plain Go structs attached to it,
and systems are functions that run over every entity carrying a given
set of components. The Tetris guide uses it for a small game; the solar
example drives four hundred asteroids with it.

## Entities and components

```go
type Position struct{ X, Y float32 }
type Velocity struct{ X, Y float32 }

w := ecs.NewWorld()
e := w.SpawnWith(Position{0, 0}, Velocity{1, 0})
ecs.Add(w, e, Health{10})
if p, ok := ecs.Get[Position](w, e); ok {
	p.X += 5 // pointers into storage; write through them right away
}
ecs.Remove[Velocity](w, e)
w.Despawn(e)
```

Entity handles are generational. A handle to a despawned entity stays
invalid even after its slot is reused, so a stale handle never reads
another entity's data.

Where the type is known only at run time, `w.Components(e)` lists the
types an entity carries, `w.ComponentValues(e)` returns copies of them
in the same order, and `w.SetComponent(e, v)` writes one back from an
`any`. That is what an editor or the debug console's entity panel uses;
game code that knows the type calls `ecs.Get` and `ecs.Add`.

## Queries

A query names the components it reads and walks every entity that has
all of them. Make it once and keep it; it caches which tables match and
only rescans when a new combination of components appears.

```go
movers := ecs.NewQuery2[Position, Velocity](w, ecs.Without[Frozen]())

movers.Each(func(e ecs.Entity, p *Position, v *Velocity) {
	p.X += v.X * dt
	p.Y += v.Y * dt
})
```

`With` and `Without` filters narrow a query without reading the extra
component. `Each`, `Each2` and `Count` are one-off shortcuts for code
that does not keep a query.

## Storage

Every distinct set of component types gets a table (an archetype) with
one dense column per type. An entity lives in exactly one table. A query
finds the tables containing its components and then walks their columns
side by side, with no hashing, no pointer chasing and no allocation.
Iterating two components over a hundred thousand entities takes a
fraction of a millisecond.

Adding or removing a component moves the entity to another table by
copying its row, so structural changes cost more than reads. Make them
in response to game events rather than every frame.

## Changing the world while iterating

Rows are visited last to first, so the entity a query is currently
visiting may be despawned or given a new component inside the callback.
Changes to *other* entities go through a `Commands` buffer and are
applied after the walk:

```go
var cmd ecs.Commands
enemies.Each(func(e ecs.Entity, h *Health) {
	if h.HP <= 0 {
		cmd.Spawn(Corpse{}, Position{...})
		cmd.Despawn(e)
	}
})
cmd.Apply(w)
```

## Systems, resources and events

A system is a function `func(w *ecs.World, dt float64)`. Register them
in the order they should run and call `w.Update(dt)` from the game's
`Update`. `w.Stats()` reports each system's time for the debug overlay,
`w.SetSystemEnabled(name, false)` turns one off without unregistering
it, so a debugger can pause part of the simulation, and `w.Updates()`
counts the updates the world has run.

Resources are singletons stored on the world by type, such as the
score, the rules, the input for this frame or a random number
generator. Systems fetch them with `ecs.Resource[Score](w)`.

Events pass data between systems without coupling them. A producer calls
`ecs.Emit(w, Cleared{Rows: 2})`; consumers later in the same Update read
`ecs.Events[Cleared](w)`. Events are cleared at the start of the next
Update, and `Draw` still sees what the last Update emitted.

```go
ecs.SetResource(w, Score{})
w.AddSystem("rules", func(w *ecs.World, dt float64) {
	ecs.Emit(w, Cleared{Rows: 2})
})
w.AddSystem("score", func(w *ecs.World, dt float64) {
	for _, ev := range ecs.Events[Cleared](w) {
		ecs.Resource[Score](w).Points += 100 * ev.Rows
	}
})
w.Update(ctx.Delta) // runs both, in registration order
```

## Hierarchy

`SetParent` links entities; `WorldMatrix` composes their
`gfx.Transform` components from the root down, and despawning a parent
despawns its children. The solar example's moons orbit their planets
this way. Each moon has a small orbit component relative to its parent,
and the hierarchy composes that with the planet's own position.

```go
planet := w.SpawnWith(gfx.At(80, 0, 0))
moon := w.SpawnWith(gfx.At(4, 0, 0)) // relative to the planet
ecs.SetParent(w, moon, planet)

// Drawing composes the chain from the root down.
gr.DrawMesh(mesh, mat, ecs.WorldMatrix(w, moon).Mul(lin.Scale(size)))
for _, child := range ecs.ChildrenOf(w, planet) {
	highlight(child)
}
w.Despawn(planet) // the moon goes with it
```

`WorldMatrix` climbs the parent chain each time it is called. To pay for
the chain once a frame instead of once an entity, call
`UpdateWorldMatrices` from a system after the ones that move transforms;
it walks every hierarchy from its roots down and caches a matrix per
entity, and `WorldMatrix` reads the cache instead of climbing. Reading
every entity's matrix over a ten thousand entity hierarchy four deep
takes about half as long with the pass as without it.

```go
w.AddSystem("transforms", func(w *ecs.World, dt float64) {
	ecs.UpdateWorldMatrices(w)
})
```

The cache lasts until the next `World.Update`, so `Draw` reads what the
last `Update` left. `SetParent` and `Despawn` drop it, and a transform
written after the pass is not seen until the pass runs again.
`WorldMatrix` falls back to the walk whenever the cache is not fresh, so
code that never calls the pass behaves as it always has.

## Saving and loading

A world saves as JSON. The file holds every live entity with its parent
links, each component encoded by `encoding/json`, and then the
resources. Components and resources are written under names you
register, so a save made by one build loads in the next even after a
type moves or is renamed. Register at start-up. An unregistered type
fails the save with an `UnregisteredError` naming every unregistered
type, or is left out with `SaveOptions{SkipUnregistered: true}`.

```go
ecs.Register[Position]("Position")
ecs.Register[Health]("Health")
ecs.Register[Score]("Score") // resources too

var buf bytes.Buffer
if err := w.Save(&buf); err != nil { ... }

fresh := ecs.NewWorld()
if err := fresh.Load(&buf); err != nil { ... }
```

`Load` gives every entity a new handle and rewrites the references to
it: parent links, `Children` lists, and any `Entity` field it can see
inside a component or resource, including inside slices, maps and
pointers. A component that stores entities where reflection cannot
reach them can implement `Remap(func(Entity) Entity)` and rewrite them
itself. A reference to an entity the file does not hold becomes `None`.
`gfx.Transform` and `gfx.Transform2` are registered by default.

Only exported fields are saved, as with any `encoding/json` value, and
a type with its own `MarshalJSON` is written that way.

## Prefabs

A prefab is a template. It holds a set of component values, plus child
prefabs for a hierarchy, and spawns as many independent copies as the
game asks for. Build one in code, or read it from JSON in the same form
a save uses for one entity's components:

```go
tank := ecs.NewPrefab(Position{}, Health{10}, Team{Red}).
	Child(ecs.NewPrefab(gfx.At(0, 1, 0), Turret{}))
e := tank.Spawn(w)
ecs.Add(w, e, Position{x, y}) // place it after
```

```json
{"components": {"Health": {"HP": 10}, "Team": {"Side": 1}},
 "children": [{"components": {"gfx.Transform": {"Position": {"Y": 1}}}}]}
```

`ParsePrefab` reads that form and `json.Marshal` writes it, so prefabs
can live in asset files beside the sprites they use. `PrefabOf` takes a
prefab from a live entity and its descendants, which is how an editor
would save what it built.

## Cloning

`Clone(w, e)` makes a new entity carrying copies of `e`'s components,
under the same parent; `CloneTree` copies the descendants as well and
keeps the hierarchy between the copies. A field that referred to
something in the copied tree refers to its copy afterwards.

```go
tank := w.SpawnWith(Position{0, 0}, Health{10})
turret := w.SpawnWith(Position{0, 1})
ecs.SetParent(w, turret, tank)

second := ecs.CloneTree(w, tank) // the tank and its turret
if h, ok := ecs.Get[Health](w, second); ok {
	h.HP = 3 // the original still has 10
}
spark := ecs.Clone(w, muzzleFlash) // one entity, no descendants
```

Copies are deep through exported fields: slices, maps, pointers and
interface values get their own storage, so an inventory slice in the
clone is not the original's. Unexported fields are copied as values,
so a slice or pointer kept in one is still shared. A component that
`encoding/json` can save is always copied fully. Use that as the test
for both cloning and prefabs.

## Modelling advice

- Components are data, systems are behaviour. A component may have
  methods, but a component that reads or writes other entities belongs
  in a system instead.
- Prefer small components. A query reads only what it names, and an
  entity with many components still costs one table row.
- Tag components (empty structs) mark categories cheaply: `Frozen{}`,
  `Enemy{}`, `Selected{}`.
- Anything there is exactly one of is a resource, not an entity.
