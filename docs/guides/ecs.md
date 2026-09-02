---
title: Entities and systems
group: Engine
order: 3
summary: the entity component system, how it stores data, and how to structure a game around it
---

The [ecs](../pkg/ecs.html) package is how a Bunyip game keeps its world.
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

Entities are generational: a handle to a despawned entity stays dead
even after the slot is reused, so a stale reference can never read
someone else's data.

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

## How it is stored, and why it is fast

Every distinct set of component types gets a table (an archetype) with
one dense column per type. An entity lives in exactly one table. A query
finds the tables containing its components and then walks their columns
side by side: no hashing, no pointer chasing, no allocation. Iterating
two components over a hundred thousand entities takes a fraction of a
millisecond.

Adding or removing a component moves the entity to another table by
copying its row, so structural changes cost more than reads. Do them
when something happens, not every frame.

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
`Update`. `w.Stats()` reports each system's time for the debug overlay.

Resources are singletons stored on the world by type: the score, the
rules, the input for this frame, a random number generator. Systems
fetch them with `ecs.Resource[Score](w)`.

Events let systems talk without coupling. A producer calls
`ecs.Emit(w, Cleared{Rows: 2})`; consumers later in the same Update read
`ecs.Events[Cleared](w)`. Events are cleared at the start of the next
Update, and `Draw` still sees what the last Update emitted.

## Hierarchy

`SetParent` links entities; `WorldMatrix` composes their
`gfx.Transform` components from the root down, and despawning a parent
despawns its children. The solar example's moons orbit their planets
this way: each moon has a small orbit component relative to its parent
and the hierarchy does the rest.

## Saving and loading

A world saves as JSON: every live entity with its parent links and each
component encoded by `encoding/json`, then the resources. Components
and resources are written under names you register, so a save made by
one build loads in the next even after a type moves or is renamed.
Register at start-up; an unregistered type fails the save with an
`UnregisteredError` naming every offender, or is left out with
`SaveOptions{SkipUnregistered: true}`.

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
pointers. A component that keeps entities somewhere reflection cannot
reach can implement `Remap(func(Entity) Entity)` and rewrite them
itself. A reference to an entity the file does not hold becomes `None`.
`gfx.Transform` and `gfx.Transform2` are registered by default.

Only exported fields are saved, as with any `encoding/json` value, and
a type with its own `MarshalJSON` is written that way.

## Prefabs

A prefab is a template: a set of component values, and child prefabs
for a hierarchy, that spawns as many independent copies as the game
asks for. Build one in code, or read it from JSON in the same form a
save uses for one entity's components:

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

Copies are deep through exported fields: slices, maps, pointers and
interface values get their own storage, so an inventory slice in the
clone is not the original's. Unexported fields are copied as values,
so a slice or pointer kept in one is still shared. A component that
`encoding/json` can save is always copied fully, which is the rule of
thumb for both cloning and prefabs.

## Modelling advice

- Components are data, systems are behaviour. A component with methods
  is fine; a component that reaches for other entities is a system in
  disguise.
- Prefer small components. A query reads only what it names, and an
  entity with many components still costs one table row.
- Tag components (empty structs) mark categories cheaply: `Frozen{}`,
  `Enemy{}`, `Selected{}`.
- Anything there is exactly one of is a resource, not an entity.
