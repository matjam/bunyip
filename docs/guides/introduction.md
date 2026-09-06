---
title: Introduction
group: Start
order: 1
summary: what Bunyip is, how it is put together, and what to read next
---

Bunyip is a complete game engine in Go. One package draws 2D sprites,
vector paths and shaped text and physically based 3D models with
shadows, sky and fog in the same frame. Around it sit an entity
component system, rigid-body physics in two and three dimensions,
skeletal animation with blend spaces and inverse kinematics, celestial
mechanics, an immediate-mode interface, an audio mixer with positional
sound and a tracker player, and the services a finished game needs:
asset packs, saves, translation, action maps, and networking with
prediction and reliable delivery. Every part is written for this engine
and documented in the same voice, so a game uses one API rather than a
dozen libraries with different conventions.

It is built for games that simulate as much as they render: roguelikes
with thousands of entities, 4X strategy on a hex map, a space game with
real orbits, an arcade game with a 3D hero on a 2D field. A turn-based
game blocks in the operating system between moves; a real-time game runs
a fixed timestep.

The engine is pure Go. It builds with `go build` and no cgo, calls
Vulkan through a generated binding and each operating system's own
windowing and audio APIs through purego, and cross-compiles by setting
`GOOS`. A game does not call Vulkan; it works with sprites, meshes,
cameras, lights and widgets. Rendering examples run to a screenshot
without a window; native window interaction still needs a desktop.

## Design

- **No cgo.** The engine builds with `CGO_ENABLED=0`. Native libraries
  (the Vulkan loader, AppKit, Core Audio, and their Windows and Linux
  counterparts) are opened at run time, so building is `go build` and
  cross-compiling is a matter of setting `GOOS`.
- **Native platform layers.** There is no SDL or GLFW. Each operating
  system has a small window, input and audio layer written against its
  own APIs.
- **Two loop modes.** Real-time games run a fixed timestep. Turn-based
  games set one flag, and the process then blocks in the operating
  system until the player does something.
- **Entity component system.** Game state lives in an entity component
  system with dense per-type storage. A query over a hundred thousand
  entities walks a few slices, and systems are plain functions over that
  data.
- **Immediate-mode interface.** The interface is rebuilt every frame from
  Go values, and closures scope every container, so there is no widget
  tree to keep in step with the game.
- **Self-verifying examples.** Rendering examples take `-seconds` and
  `-shot`, runs unattended to a screenshot, and the renderer's tests
  read pixels back from a headless surface. Each one also has a
  [walkthrough](../examples/index.html) that quotes the whole program and
  explains it section by section. The native `window` example is excluded
  from headless screenshot tests.

## The shape of a game

```go
type game struct{ x float32 }

func (g *game) Update(ctx *engine.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) {
		ctx.Quit()
	}
	g.x += 100 * float32(ctx.Delta)
	return nil
}

func (g *game) Draw(ctx *engine.Context) error {
	ctx.Gfx.FillRect(g.x, 100, 40, 40, gfx.RGB(255, 200, 40))
	return nil
}

func main() {
	engine.Run(engine.Config{Title: "Hello", Width: 960, Height: 600}, &game{})
}
```

`Update` advances the simulation and `Draw` queues drawing. The
[engine](../pkg/engine.html) package manages the window, the loop and
the frame pacing. The `Context` it passes in holds everything else a
game uses: graphics, input, audio, timing and the window.

## Package map

| Area | Packages |
|---|---|
| Engine | [engine](../pkg/engine.html) (loop, context, window), [input](../pkg/input.html) (keys, mouse, gamepads, action maps), [console](../pkg/console.html) (debugging commands and panels) |
| Graphics | [gfx](../pkg/gfx.html) (2D, 3D, text, post-processing), [ui](../pkg/ui.html), [anim](../pkg/anim.html), [particle](../pkg/particle.html), [tiled](../pkg/tiled.html), [gltf](../pkg/gltf.html), [lin](../pkg/lin.html) |
| Simulation | [ecs](../pkg/ecs.html), [phys](../pkg/phys.html) (2D and 3D rigid bodies), [phys/soft](../pkg/phys/soft.html) (cloth, soft bodies and 2D fluids), [orbit](../pkg/orbit.html) (celestial mechanics), [orbit/sol](../pkg/orbit/sol.html) |
| Audio | [audio](../pkg/audio.html) (mixer, music, effects), [audio/tracker](../pkg/audio/tracker.html) |
| Services | [asset](../pkg/asset.html), [save](../pkg/save.html), [locale](../pkg/locale.html), [rng](../pkg/rng.html), [timer](../pkg/timer.html), [tween](../pkg/tween.html), [grid](../pkg/grid.html), [network](../pkg/network.html) |
| Assets and map formats | [gfx/ktx2](../pkg/gfx/ktx2.html) (compressed textures), [grid/autotile](../pkg/grid/autotile.html) (terrain tile selection) |
| Tools | `bunyip-info`, `bunyip-play`, `bunyip-pack`, `bunyip-shader`, `bunyip-tex`, `bunyip-bundle`, `bunyip-docs` |

## Reading order

The guides come in five groups. Read Start first, then the group you
need before its API reference.

1. **Start**: [getting started](getting-started.html),
   [building Tetris](tetris.html).
2. **Engine**: [the window](window.html), [input](input.html),
   [entities and systems](ecs.html), [game services](services.html).
3. **Graphics**: [2D graphics](graphics-2d.html),
   [3D graphics](graphics-3d.html), [shaders](shaders.html),
   [animation](animation.html), [the interface](ui.html).
4. **Simulation**: [physics](physics.html), [orbits](space.html).
5. **Audio**: [audio](audio.html).

Alongside them are the [example programs](../examples/index.html), one
walkthrough per directory under `examples/`. A guide explains an area;
a walkthrough reads one complete program in that area line by line, so
it is the fastest way to see how the pieces fit together.
