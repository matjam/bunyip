---
title: Introduction
order: 1
summary: what Bunyip is, how it is put together, and what to read next
---

Bunyip is a game engine written in Go for games that think as much as they
render: roguelikes, 4X strategy, arcade games, and anything that wants 2D
sprites and 3D models on the same screen. It draws with Vulkan, but a game
never sees Vulkan; it sees sprites, meshes, cameras, lights and a UI.

## Design in five points

- **Pure Go.** `CGO_ENABLED=0` everywhere. Native libraries (the Vulkan
  loader, AppKit, Core Audio, and their Windows and Linux counterparts) are
  opened at run time, so the toolchain is just `go build`, and
  cross-compiling is one environment variable away.
- **Native platform layers.** No SDL or GLFW. Each operating system gets a
  small window, input and audio layer written against its own APIs.
- **Two loop modes.** Real-time games get a fixed timestep. Turn-based
  games set one flag and the process sleeps in the operating system until
  the player does something, using no CPU while they think.
- **Data first.** Game state lives in an entity component system with
  dense per-type storage, so a query over a hundred thousand entities is
  a walk down a few slices, and systems are plain functions over that
  data.
- **Immediate mode.** The interface is rebuilt every frame from plain Go
  values, and closures scope every container, so there is no retained
  widget tree to keep in sync with the game.
- **Self-verifying examples.** Every example takes `-seconds` and `-shot`,
  runs to a screenshot without anyone watching, and the renderer's tests
  read pixels back from a headless surface.

## The shape of a game

```go
type game struct{ x float32 }

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) {
		ctx.Quit()
	}
	g.x += 100 * float32(ctx.Delta)
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	ctx.Gfx.FillRect(g.x, 100, 40, 40, gfx.RGB(255, 200, 40))
	return nil
}

func main() {
	bunyip.Run(bunyip.Config{Title: "Hello", Width: 960, Height: 600}, &game{})
}
```

`Update` advances the simulation and `Draw` queues drawing. The
[bunyip](../pkg/bunyip.html) package owns the window, the loop and the
frame pacing; the `Context` it passes in holds everything else a game
touches: graphics, input, audio, timing and the window.

## Package map

| Area | Packages |
|---|---|
| Engine | [bunyip](../pkg/bunyip.html) (loop, context), [input](../pkg/input.html) |
| Graphics | [gfx](../pkg/gfx.html) (2D, 3D, text, post-processing), [anim](../pkg/anim.html), [ui](../pkg/ui.html), [gltf](../pkg/gltf.html), [lin](../pkg/lin.html) |
| Audio | [audio](../pkg/audio.html) (mixer, music, effects), [audio/tracker](../pkg/audio/tracker.html) |
| Services | [ecs](../pkg/ecs.html), [asset](../pkg/asset.html), [save](../pkg/save.html), [rng](../pkg/rng.html), [timer](../pkg/timer.html), [tween](../pkg/tween.html), [grid](../pkg/grid.html), [network](../pkg/network.html) |
| Tools | `bunyip-info`, `bunyip-play`, `bunyip-pack`, `bunyip-bundle`, `bunyip-docs` |

## Where to go next

- [Getting started](getting-started.html) installs what the engine needs
  and opens a window.
- [Building Tetris](tetris.html) writes a complete game from an empty file
  on the entity component system, touching input, timing, drawing, the UI
  and sound on the way.
- [Entities and systems](ecs.html) explains how the ECS stores data and
  how to structure a game around queries, systems, resources and events.
- The concept guides on [rendering](rendering.html), the [interface](ui.html),
  [audio](audio.html) and [game services](services.html) explain each area
  before you dive into its API reference.
