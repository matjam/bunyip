---
title: API migration
group: Start
order: 3
summary: adopt Go 1.27 methods, scoped operations and engine-managed resource lifetimes
---

Bunyip requires Go 1.27 or later. This pre-1.0 API pass replaces generic
functions that operated on an existing object with methods on that
object. The old functions are removed.

## Generic methods

| Previous call | Current call |
|---|---|
| `ecs.Get[Position](world, entity)` | `world.Get[Position](entity)` |
| `ecs.Add(world, entity, velocity)` | `world.Add(entity, velocity)` |
| `ecs.Remove[Velocity](world, entity)` | `world.Remove[Velocity](entity)` |
| `ecs.Has[Velocity](world, entity)` | `world.Has[Velocity](entity)` |
| `ecs.NewQuery2[Position, Velocity](world)` | `world.Query2[Position, Velocity]()` |
| `ecs.Each[Position](world, visit)` | `world.Each[Position](visit)` |
| `ecs.SetResource(world, settings)` | `world.SetResource(settings)` |
| `ecs.Resource[Settings](world)` | `world.Resource[Settings]()` |
| `ecs.Emit(world, event)` | `world.Emit(event)` |
| `ecs.Events[Hit](world)` | `world.Events[Hit]()` |
| `ecs.Count[Position](world)` | `world.Count[Position]()` |
| `world.Count()` | `world.Len()` |
| `ecs.RemoveLater[Velocity](&commands, entity)` | `commands.Remove[Velocity](entity)` |
| `anim.Property(curve, get, set)` | `curve.Property(get, set)` |
| `asset.Load(loader, name, decode)` | `loader.Load(name, decode)` |
| `save.Load(store, name, defaults)` | `store.Load(name, defaults)` |
| `rng.Pick(random, choices)` | `random.Pick(choices)` |
| `rng.Shuffle(random, choices)` | `random.Shuffle(choices)` |

The same receiver-first change applies to `MustResource`, `Each2` through
`Each4`, and `Query1` through `Query4`. Constructors and operations without
an existing receiver, such as `ecs.NewWorld`, `ecs.With`, `ecs.Without`
and `ecs.Register`, remain package functions. Generic methods operate on
concrete receivers; Go interface methods cannot declare type parameters.

## Resource ownership

Graphics now releases its remaining textures, fonts, meshes, models,
shaders, environments and render textures before the engine shuts down.
That also happens when setup or drawing fails and before device recovery.
A `Shutdown` method containing only GPU resource destruction can be removed.
Keep calling `Destroy` when a resource should be released earlier, such as
when unloading a level. GPU handles from the old context remain invalid
after device recovery and must be recreated.

Use `ctx.Cleanup(func() { ... })` for other resources whose lifetime follows
the context. Callbacks run in reverse registration order, including when
`Init` or `Recover` fails. They run after the game's successful-lifecycle
`Shutdown` callback and before the engine closes its services.

## Files and settings

Asset loaders and `asset.NewLoader` accept `fs.FS`, including `embed.FS`,
`os.DirFS`, `fstest.MapFS` and Bunyip's overlay filesystem. A single source
no longer needs an `asset.OpenFS(asset.FSSource(source))` wrapper. Keep
the overlay filesystem when combining packs, development files and mods.
`asset.FS.Open` and `ReadFile` use standard slash-separated `io/fs` paths;
absolute paths and `..` components are invalid. Hot reload still uses
`*asset.FS` because it needs the locations of loose files.

`Loader.Close` now waits for accepted reads and decodes to finish, so the
underlying filesystem can safely close afterward. `Store.Load` copies
settings defaults even when the saved file does not exist, keeping maps
and slices independent on the first launch as well as later launches.

For streamed audio from a disk file, `Mixer.OpenMusicFile` owns the file
and releases it when `Music.Close` finishes. `OpenMusic` still borrows
its reader. `Close` now waits for decoding to stop, so a borrowed reader
can safely be closed afterward; a custom reader must unblock any pending
read or seek before that wait can finish.

Network activity hooks now notice events queued before registration,
and server hooks also cover already connected clients. Registering a hook
can call it immediately, outside internal locks. Keep the callback short,
as with `ctx.Wake`.

## Scoped simulation work

Wrap deferred structural changes in `world.Defer(func(commands *ecs.Commands)
{ ... })`. The world applies the commands when the closure returns normally.
A panic discards those queued commands and propagates. Direct changes to
the world are not rolled back. Put the entire query traversal inside the
closure so the queued changes take effect after traversal finishes.

For a track that targets a component field, use
`curve.Field(func(component *Health) *float32 { return &component.Value })`.
Keep `curve.Property(get, set)` when reading or writing requires conversion
or normalization. Generic tweens now retain their value type after
`OnDone` and reverse their interpolation correctly with `YoYo`.

## Console drawing

With `Config.Console` enabled, the engine draws the console after the
game and the debug overlay. Remove `ctx.Console.Draw(ctx)` from the
game's `Draw` method. A separately constructed console remains explicitly
drawn by its owner. Gameplay input routing still uses `Console.Open`;
enabling the console does not automatically pause the simulation.

## UI and drawing closures

`ctx.NewUI(ui.Theme{})` creates a dark interface with a shared default
font and connects clipboard access and input-method positioning. Pass a
custom theme to keep its styling; a missing font still gets the default.
Create the interface once during setup and continue using its existing
`Begin` and container closures each frame.

`Graphics.Layered` and `WithCamera2D` temporarily change drawing state
inside a closure. They and the existing blend, transform, shader, colour
matrix, clipping and render-target closures restore state on normal
return or panic. Queued drawing is not rolled back.

Use `Graphics.ConfigurePost` to edit the current post-processing settings
without restating their defaults:

```go
ctx.Gfx.ConfigurePost(func(settings *gfx.PostSettings) {
	settings.Bloom = 0.3
	settings.Saturation = 0 // intentional grayscale
})
```

For small applications, `bunyip.GameFuncs` accepts optional setup,
update, drawing and shutdown closures. Larger games can continue
implementing the small lifecycle interfaces directly.
