---
title: Why Go?
group: Start
order: 2
summary: why Go is a practical foundation for a game engine, and the tradeoffs to understand
---

## Why use Go for games programming?

A game engine needs more than a renderer. It needs simulation, asset
processing, audio, networking, tools and a way to diagnose what went wrong
on a player's machine. Go gives these parts a common language and toolchain.
For Bunyip, its appeal is the combination of native compilation, straightforward
code and tools that work across the whole project.

That makes Go a useful choice for developers who want to build their game
in code, understand the engine underneath it and spend less time maintaining
build infrastructure. Here is how that choice shapes Bunyip.

## One language for the engine and the game

Gameplay code uses the same structs, functions, interfaces and packages as
the renderer and asset tools. You can follow a drawing call into the engine
with your editor, change it, and rebuild with the same toolchain. There is
no separate gameplay scripting language to learn or binding layer to maintain
between game logic and the engine's Go API.

Small interfaces let a subsystem accept the capabilities it needs. For
example, Bunyip's asset helpers accept `fs.FS`: the same loading code can
read embedded files in a shipped game and an in-memory filesystem in a
test. Closures keep short operations together, such as drawing through a
camera or building a UI panel. Generics keep component queries typed without
requiring a parallel hierarchy of game objects.

This is useful for an engine's users as well as its maintainers: ordinary
Go techniques remain useful as a prototype grows. The [API design
guide](api-design.html) shows the ownership and scope conventions in detail.

## Native code with control over the data

Go compiles game logic to native machine code. Structs, arrays and slices
let you represent a simulation directly and process related values together.
Bunyip's ECS stores components in archetypes and exposes typed iteration:

```go
type Position struct{ X, Y float64 }
type Velocity struct{ X, Y float64 }

// In Update, with a world containing these components:
world.Each2(func(e ecs.Entity, p *Position, v *Velocity) {
	p.X += v.X * ctx.Delta
	p.Y += v.Y * ctx.Delta
})
```

The update expresses the work directly, without type assertions in the
game's loop. You can measure this work, change its data layout or algorithm,
and keep the rest of the game intact. Go does not guarantee that every
abstraction is free, or that a particular simulation will meet its frame
budget. Its value here is that clear code and measurable performance can
live in the same implementation.

## Tools for the whole development cycle

Formatting, tests, benchmarks and static checks are part of the standard
toolchain. A combat rule, map generator or save conversion can be a normal
Go package with tests that do not open a window. Bunyip also supports
offscreen rendering for visual checks; those checks still need a Vulkan
implementation.

Go's CPU and heap profiles help identify expensive functions and allocation
sources, while execution traces help investigate scheduling and blocking.
Bunyip adds frame timings, an in-game console and profile scopes so you can
relate engine work to what appears on screen. See the [Go diagnostics
guide](https://go.dev/doc/diagnostics) for the language's profiling tools.

The same packages can support an asset converter, game server and desktop
client. For example, a server and client can import a shared package of
combat rules, while a command-line tool validates the same level format
that the game loads. That reuse reduces duplicated logic; deterministic
simulation still requires deliberate choices about ordering, randomness
and numeric behavior.

## Concurrency for work around the frame

Goroutines and channels make it practical to organize independent work:
reading assets, decoding images, receiving network messages or preparing
a save. Bunyip's asset loader already manages background workers, so game
code can collect finished results instead of managing a worker pool itself.

Keep the boundary clear: workers prepare CPU data, and the game's callbacks
create GPU resources and update the live game state. Sharing mutable state
still needs synchronization. Starting a goroutine for every entity is not
automatically faster, and concurrent execution does not establish a stable
simulation order. Concurrency is most useful when the work and its result
have a clear owner.

## Managed memory with an explicit frame budget

Go manages the lifetime of ordinary Go memory. Component structs, decoded
data and temporary application values do not each need a destructor. This
reduces lifetime bookkeeping while building and changing a game.

Garbage collection still costs CPU time and memory, and it can affect frame
timing. Allocation rate and the live object graph both matter. Reuse buffers
in frequently executed code, avoid rebuilding large temporary structures
every frame, and profile before adding pools or tuning the collector. Go's
[garbage collector guide](https://go.dev/doc/gc-guide) explains these costs
and the memory-versus-CPU tradeoff. Go provides no hard real-time guarantee.

GPU resources and open files have separate lifetimes. Bunyip's Graphics
owns its GPU resources, and `Context.Cleanup` handles registered cleanup
at the end of a context. Managed memory and explicit resource ownership
work together: the collector is not the mechanism for unloading a texture
or closing an asset pack.

## A simpler build and distribution path

Bunyip builds with `CGO_ENABLED=0`. Its Go code calls native graphics,
windowing and audio APIs at run time, so building the engine does not
require a C compiler or platform SDK headers. The project cross-builds for
Linux, Windows and macOS on amd64 and arm64 using the Go toolchain.

The resulting game does still need the appropriate native libraries and
graphics driver on the target machine. Cross-compilation also does not
replace testing input, audio and graphics on that platform. The [getting
started guide](getting-started.html) lists runtime requirements and current
hardware verification, and describes Bunyip's bundling tool.

## Choosing Go means choosing its tradeoffs

Go suits Bunyip's approach: a code-driven engine with reusable packages,
typed game state, built-in development tools and desktop native rendering.
It is especially appealing when you want the game, its tools and its server
to share a language and want to be able to modify the engine yourself.

Your required platforms and workflow still decide whether that is a good
fit. Bunyip currently targets desktop systems; Go's support for other targets
does not supply Bunyip with browser, mobile or console backends. If your
project depends on a visual scene-authoring workflow, a particular middleware
integration or strict control of every allocation and scheduling decision,
evaluate those requirements before committing to an engine.

To see the approach in practice, [open your first window](getting-started.html)
or read how the [API manages ownership and cleanup](api-design.html).
