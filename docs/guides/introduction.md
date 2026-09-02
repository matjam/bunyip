---
title: Introduction
order: 1
summary: what Bunyip is, how it is put together, and what to read next
---

Bunyip is a game engine written in Go. It is aimed at games that
simulate as much as they render: roguelikes, 4X strategy, space games,
arcade games, and anything that puts 2D sprites and 3D models on the
same screen. It draws with Vulkan, but a game never sees Vulkan; it
sees sprites, meshes, cameras, lights and widgets.

## Design

- **Pure Go.** The engine builds with `CGO_ENABLED=0`. Native libraries
  (the Vulkan loader, AppKit, Core Audio, and their Windows and Linux
  counterparts) are opened at run time, so building is `go build` and
  cross-compiling is a matter of setting `GOOS`.
- **Native platform layers.** There is no SDL or GLFW. Each operating
  system has a small window, input and audio layer written against its
  own APIs.
- **Two loop modes.** Real-time games run a fixed timestep. Turn-based
  games set one flag, and the process then sleeps in the operating
  system until the player does something.
- **Data first.** Game state lives in an entity component system with
  dense per-type storage. A query over a hundred thousand entities
  walks a few slices, and systems are plain functions over that data.
- **Immediate-mode interface.** The interface is rebuilt every frame from
  Go values, and closures scope every container, so there is no widget
  tree to keep in step with the game.
- **Self-verifying examples.** Every example takes `-seconds` and
  `-shot`, runs to a screenshot without anyone watching, and the
  renderer's tests read pixels back from a headless surface.

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
frame pacing. The `Context` it passes in holds everything else a game
touches: graphics, input, audio, timing and the window.

## Package map

| Area | Packages |
|---|---|
| Engine | [bunyip](../pkg/bunyip.html) (loop, context, window), [input](../pkg/input.html) (keys, mouse, gamepads, action maps) |
| Graphics | [gfx](../pkg/gfx.html) (2D, 3D, text, post-processing), [ui](../pkg/ui.html), [anim](../pkg/anim.html), [particle](../pkg/particle.html), [tiled](../pkg/tiled.html), [gltf](../pkg/gltf.html), [lin](../pkg/lin.html) |
| Simulation | [ecs](../pkg/ecs.html), [phys](../pkg/phys.html) (2D and 3D rigid bodies), [orbit](../pkg/orbit.html) (celestial mechanics), [orbit/sol](../pkg/orbit/sol.html) |
| Audio | [audio](../pkg/audio.html) (mixer, music, effects), [audio/tracker](../pkg/audio/tracker.html) |
| Services | [asset](../pkg/asset.html), [save](../pkg/save.html), [locale](../pkg/locale.html), [rng](../pkg/rng.html), [timer](../pkg/timer.html), [tween](../pkg/tween.html), [grid](../pkg/grid.html), [network](../pkg/network.html) |
| Tools | `bunyip-info`, `bunyip-play`, `bunyip-pack`, `bunyip-shader`, `bunyip-bundle`, `bunyip-docs` |

## Reading order

1. [Getting started](getting-started.html) installs the Vulkan driver,
   opens a window and explains the loop, the view and the window
   controls.
2. [Building Tetris](tetris.html) writes a complete game on the entity
   component system, using input, timers, drawing, the interface and
   sound along the way.
3. [Entities and systems](ecs.html) explains how the ECS stores data and
   how to structure a game around queries, systems, resources and
   events.
4. The remaining guides each cover one area: [rendering](rendering.html),
   [shaders](shaders.html), [animation](animation.html),
   [physics](physics.html), [orbits](space.html), the
   [interface](ui.html), [audio](audio.html), [input](input.html) and
   the [game services](services.html). Read the one you need before its
   API reference.
