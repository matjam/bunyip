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
w.Add(e, Health{10})
if p, ok := w.Get[Position](e); ok {
	p.X += 5 // pointers into storage; write through them right away
}
w.Remove[Velocity](e)
w.Despawn(e)
```

Entity handles are generational. A handle to a despawned entity stays
invalid even after its slot is reused, so a stale handle never reads
another entity's data.
Use `w.Alive(e)` to test whether it exists; `e.Valid()` only tests for a
nonzero handle. Handles belong to their world and are not portable to
another world. Construct worlds with `NewWorld`; their zero value is
not initialized, and world access must be serialized.

Where the type is known only at run time, `w.Components(e)` lists the
types an entity carries, `w.ComponentValues(e)` returns shallow copies
in the same order, and `w.SetComponent(e, v)` writes one back from an
`any`. That is what an editor or the debug console's entity panel uses;
game code that knows the type calls `ecs.World.Get` and `ecs.World.Add`.
Slices, maps and pointers inside those copies still refer to the
entity's storage. Component pointers from `Get` and queries are only
valid until a structural change touches their table.

## Queries

A query names the components it reads and walks every entity that has
all of them. Make it once and keep it; it caches which tables match and
only rescans when a new combination of components appears.

```go
movers := w.Query2[Position, Velocity](ecs.Without[Frozen]())

movers.Each(func(e ecs.Entity, p *Position, v *Velocity) {
	p.X += v.X * dt
	p.Y += v.Y * dt
})
```

`With` and `Without` filters narrow a query without reading the extra
component. `Each`, `Each2`, `Each3`, `Each4` and `Count` are shortcuts
that reuse a cached query per world and ordered component set.

## Storage

Every distinct set of component types gets a table (an archetype) with
one dense column per type. An entity lives in exactly one table. A query
finds the tables containing its components and then walks their columns
side by side. Repeated walks reuse their scratch storage after the
matching table list and snapshots have grown. A world supports up to
256 distinct component types.

Adding or removing a component moves the entity to another table by
copying its row, so structural changes cost more than reads. Make them
in response to game events rather than every frame.

## Changing the world while iterating

Rows are visited last to first, so the entity a query is currently
visiting may be despawned or given a new component inside the callback.
Changes to *other* entities go through `World.Defer`, whose `Commands`
buffer is applied after the enclosing closure returns.

The walk snapshots all matched table lengths, so moving the current
entity into another matched table does not visit it twice. Despawning
an entity with children changes other entities and must be deferred.
Do not start another walk of the same query within its callback; the
same restriction applies to nested `Each` helpers with the same
ordered component set.

Wrap the whole query walk so commands apply after it completes:

```go
w.Defer(func(cmd *ecs.Commands) {
	enemies.Each(func(e ecs.Entity, h *Health) {
		if h.HP <= 0 {
			cmd.Spawn(Corpse{}, Position{...})
			cmd.Despawn(e)
		}
	})
})
```

The scope applies commands in order on a normal return, including an
early return. A panic discards pending commands and propagates. Each
nested scope finishes independently. This is not a transaction: direct
world writes and completed nested scopes are not rolled back. Do not
retain the scoped buffer or call its `Apply` method. Keep an explicit
`Commands` value and call `Apply(w)` when work must span several scopes.

## Systems, resources and events

A system is a function `func(w *ecs.World, dt float64)`. Register them
in the order they should run and call `w.Update(dt)` from the game's
`Update`. `w.Stats()` reports each system's time for the debug overlay,
`w.SetSystemEnabled(name, false)` turns one off without unregistering
it, so a debugger can pause part of the simulation, and `w.Updates()`
counts the updates the world has run.

Resources are singletons stored on the world by type, such as the
score, the rules, the input for this frame or a random number
generator. Systems fetch them with `w.Resource[Score]()`.

Events pass data between systems without coupling them. A producer calls
`w.Emit(Cleared{Rows: 2})`; consumers later in the same Update read
`w.Events[Cleared]()`. Events are cleared at the start of the next
Update, and `Draw` still sees what the last Update emitted.

```go
w.SetResource(Score{})
w.AddSystem("rules", func(w *ecs.World, dt float64) {
	w.Emit(Cleared{Rows: 2})
})
w.AddSystem("score", func(w *ecs.World, dt float64) {
	for _, ev := range w.Events[Cleared]() {
		w.Resource[Score]().Points += 100 * ev.Rows
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
last `Update` left. Spawning, `SetParent` and `Despawn` drop it, and a
transform written after the pass is not seen until the pass runs again.
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
Loading adds to the current world and replaces resources of matching
types; it does not clear existing entities. Unknown type names fail
before adding anything unless `LoadOptions.SkipUnknown` is set. A
component decode error may leave a partially loaded world, so load
into a fresh world when failure must leave the running one untouched.
`gfx.Transform`, `gfx.Transform2` and `ecs.Name` are registered by
default.

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
w.Add(e, Position{x, y}) // place it after
```

```json
{"components": {"Health": {"HP": 10}, "Team": {"Side": 1}},
 "children": [{"components": {"gfx.Transform": {"Position": {"Y": 1}}}}]}
```

`ParsePrefab` reads that form and `json.Marshal` writes it, so prefabs
can live in asset files beside the sprites they use. `PrefabOf` takes a
prefab from a live entity and its descendants, which is how an editor
would save what it built.

## Scenes

A scene is a JSON document of entities to spawn as a unit: a level, a
room, an ambush, the contents of a shop. Where a prefab is one template
stamped over and over, a scene is a whole arrangement, with its parent
links, its cross-references and its own properties. `ParseScene` reads
one, `asset.Scene` reads one through the asset system, `Encode` writes
it, `World.Instantiate` spawns a copy, and `World.ExportScene` captures
live entities back into a document, so a game can round-trip what it
built.

```json
{
  "version": 1,
  "name": "west camp",
  "properties": {"music": "wind.ogg", "wave": 2},
  "entities": [
    {"name": "camp", "components": {"gfx.Transform": {"Position": {"X": 40}}}},
    {"name": "chief", "parent": 1, "prefab": "orc",
     "components": {"Health": {"HP": 40}}},
    {"parent": 1, "prefab": "orc",
     "components": {"gfx.Transform": {"Position": {"X": 2}}, "Follows": {"Leader": 2}}}
  ]
}
```

The document is versioned; version 1 is the only one there is.
`properties` is free-form and for the game's own use, decoded by
`encoding/json`, so a number arrives as a `float64`.

Entities are numbered from one in list order, and that number is the
only way the document links entities together. `parent` holds it, and so
does an `Entity` field inside a component, which is why zero means both
"no parent" and `None`. In the example above the third entity follows
the second. Building a scene in code, `SceneRef` turns a number into the
value to store in such a field.

`name` is a label, not a link. `SceneInstance.Entity` finds a spawned
entity by it, and `Instantiate` puts it on the entity as a `Name`
component so `ExportScene` can write it out again. Names are unique
within a scene.

Components use the same registered names and the same encoding as
`World.Save`, so the two formats stay in step. A name this build has not
registered fails `Instantiate` with an `UnregisteredError`, before
anything is spawned, unless `InstantiateOptions{SkipUnknown: true}` is
set.

### Spawning and removing

```go
scene, err := asset.Scene(fs, "levels/camp.json")
if err != nil { ... }

camp, err := w.Instantiate(scene, ecs.InstantiateOptions{Offset: lin.V3(120, 0, 0)})
if err != nil { ... }

chief, ok := camp.Entity("chief")
...
camp.Despawn(w) // every entity this copy spawned, and their children
```

Each call spawns fresh entities, so several copies of one scene coexist
and one copy's `Despawn` leaves the others alone. `Roots` are the
entities the scene left unparented and `Spawned` is every entity the
copy made, in scene order with each prefab's children after its root.
Name lookup does not check whether the entity is still alive; call
`w.Alive` when entities may have despawned. `InstantiateOptions.Parent`
hangs the roots under an entity you already have, and `Offset` moves each root by adding
to its `gfx.Transform` or `gfx.Transform2` position; a root with neither
component is not moved.

### Prefab references

An entity may name a prefab instead of listing every component. The
prefab comes from a `PrefabLibrary`, a map of names to prefabs, passed
in `InstantiateOptions.Prefabs` or stored on the world as a resource.
The entity's own components are written over the prefab's, so a document
holds only the components that differ from the template. An override
replaces a whole component rather than merging field by field, so the
parts a template varies belong in components of their own.

```go
lib := ecs.PrefabLibrary{"orc": orcPrefab, "hut": hutPrefab}
w.SetResource(lib)
```

A reference the library cannot resolve is a `MissingPrefabError` naming
the prefab. The prefab format has unnamed children, so overrides apply
to the prefab's root; anything else an instance needs is an ordinary
scene entity parented to it.

### Exporting

```go
scene, err := w.ExportScene(camp.Roots...) // or no roots for the whole world
data, err := scene.Encode()
```

`ExportScene` walks the given roots and their descendants, or every
parentless entity when given none. Entity fields pointing inside the
captured set become the right numbers, and references out of it become
`None`. It writes components rather than prefab references, because a
live entity does not remember which prefab made it, so a scene exported
after instantiating one is flat but spawns the same thing. `Encode`
writes indented JSON, which reads and merges well in version control.

Building a scene in code takes the same shape:

```go
s := ecs.NewScene("west camp")
s.SetProperty("wave", 2)
camp, _ := s.AddEntity("camp", gfx.At(40, 0, 0))
chief, _ := s.AddPrefab("chief", "orc", Health{HP: 40})
guard, _ := s.AddEntity("", gfx.At(2, 0, 0), Follows{Leader: ecs.SceneRef(chief)})
s.SetParent(chief, camp)
s.SetParent(guard, camp)
```

The solar example is the working reference: its sun, planets and moons
live in `examples/solar/system.json`, the moons reference the prefab in
`examples/solar/moon.json`, both are embedded with `go:embed` and read
through `asset.Scene` and `asset.Prefab`, and the asteroid belt is
spawned in code under the entity the scene named `sun`.

There is no scene editor yet. The format and the API are what an editor
would write and read.

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
if h, ok := w.Get[Health](second); ok {
	h.HP = 3 // the original still has 10
}
spark := ecs.Clone(w, muzzleFlash) // one entity, no descendants
```

Copies are deep through exported fields: slices, maps, pointers and
interface values get their own storage, so an inventory slice in the
clone is not the original's. Unexported fields are copied as values,
so a slice or pointer kept in one is still shared. Cloning does not
invoke custom JSON methods, and `Remapper` rewrites entity references
rather than arbitrary private storage. Components such as cloth and
soft bodies keep private particle slices; construct those separately
for independent instances rather than cloning or spawning shared
templates of them.

## Modelling advice

- Components are data, systems are behaviour. A component may have
  methods, but a component that reads or writes other entities belongs
  in a system instead.
- Prefer small components. A query reads only what it names, and an
  entity with many components still costs one table row.
- Tag components (empty structs) mark categories cheaply: `Frozen{}`,
  `Enemy{}`, `Selected{}`.
- Anything there is exactly one of is a resource, not an entity.
