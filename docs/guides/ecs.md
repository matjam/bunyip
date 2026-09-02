---
title: Entities and systems
order: 4
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

## Modelling advice

- Components are data, systems are behaviour. A component with methods
  is fine; a component that reaches for other entities is a system in
  disguise.
- Prefer small components. A query reads only what it names, and an
  entity with many components still costs one table row.
- Tag components (empty structs) mark categories cheaply: `Frozen{}`,
  `Enemy{}`, `Selected{}`.
- Anything there is exactly one of is a resource, not an entity.
