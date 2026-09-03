---
title: Getting started
group: Start
order: 2
summary: install the Vulkan driver, open a window, learn the loop and the window controls
---

## Requirements

Bunyip needs Go 1.26 or later and a Vulkan driver. There is nothing to
compile against. The engine opens the driver at run time.

| Platform | Install |
|---|---|
| macOS | `brew install vulkan-loader molten-vk`. For validation messages during development, also `brew install vulkan-validationlayers`. |
| Linux | The GPU vendor's Vulkan driver, plus `libxcb` and `libasound`, which every desktop has. `libxkbcommon` and `libxkbcommon-x11` enable text input. |
| Windows | A current GPU driver; `vulkan-1.dll` ships with it. |

The engine is developed and tested on macOS. The Linux window layer has
run on real hardware, both Wayland and X11. The Windows layer and the
Linux audio layer cross-compile and pass their unit tests but have not.

## A window

Create a module with a `main.go`:

```go
package main

import (
	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
)

type game struct{}

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) {
		ctx.Quit()
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	ctx.Gfx.FillRect(100, 100, 200, 120, gfx.RGB(120, 190, 255))
	return nil
}

func main() {
	bunyip.Run(bunyip.Config{Title: "First window", Width: 960, Height: 600, Resizable: true}, &game{})
}
```

```
go mod init example.com/first
go get github.com/matjam/bunyip
go run .
```

`Run` opens the window, creates the renderer and the audio device,
calls the game's `Init` method if it has one, and runs the loop until
`Quit`. Textures, fonts, meshes and sounds are created from the context
in `Init` and freed in an optional `Shutdown`. If the graphics device is
lost, `Init` runs again on a fresh device, so it must be safe to call
twice.

## The loop

Real-time is the default mode. `Update` runs at a fixed step, 60 Hz
unless `Config.FixedStep` sets another value, and `Draw` runs once per
displayed frame. `ctx.Delta` is the step, so the simulation is the same
whatever the frame rate. When frames come faster than updates,
`ctx.Alpha` during `Draw` holds how far the clock has run past the last
update, as a fraction of a step. Draw a body at
`previous + (current - previous) * Alpha` to keep motion smooth.

After a stall (a long load, a window drag) the loop catches up with
extra updates. `Config.MaxCatchUp` caps how much lost time it makes up
and `Config.MaxSteps` caps the updates in one frame; the rest of the
time is dropped rather than simulated. `Config.PauseUnfocused` stops
updates and silences the mixer while another window has focus.

Turn-based games set `Config.TurnBased`. The loop then blocks in the
operating system until input arrives and runs one `Update` and one
`Draw` per batch of events, so the process uses no CPU between events.
A timer, a network message or a finished asset load can wake it with
`ctx.Wake`. Call `ctx.RequestRedraw` to ask for another frame while an
animation is playing.

## The window

`Config` sizes the window at the start. `Context` controls it while the
game runs: the title, the icon, size limits, the pointer's shape and
visibility, full screen, the window's place on the screen, and a fixed
view that the engine scales and letterboxes for you. The
[window guide](window.html) covers all of it, along with what a resize
does to coordinates, which controls each platform supports, and what a
headless run gives you instead.

## Input

`ctx.Input` reports what is held (`KeyDown`), what changed this update
(`KeyPressed`, `KeyReleased`, `MousePressed`), the pointer, the wheel,
typed text and gamepads. During `Draw` the "changed" accessors cover the
whole frame rather than the last update, so an interface built in
`Draw` sees a click even when several updates ran before it. The
[input guide](input.html) covers the rest, including action maps for
rebinding.

## Debugging

F3 toggles an overlay with the frame time, the update and draw times,
draw-call counts and any profile scopes the game recorded; `Config.Debug`
shows it from the start.

`ctx.Profile` times a section of game code. It returns a scope, and
`End` closes it and records how long it took, into `ctx.Stats.Scopes`
and the overlay:

```go
pathing := ctx.Profile("pathfinding")
g.findPaths()
pathing.End()

// Or for a whole function:
defer ctx.Profile("simulate").End()
```

The scope is a small value rather than a closure, so it allocates
nothing and a section that runs many times a frame can be timed.
Timing runs whether or not the overlay is shown.

`Config.DrawBudget` turns the draw-call count
into a warning when a frame exceeds it. `Config.Pprof` serves Go's
profiler on an address. `Config.LogFile` writes the log to a file and
appends a stack trace if the game panics. That file is the one to ask a
player for. `bunyip.FlyCamera` is a free-flying camera for looking round
a 3D scene while it is being built.

## The examples

Every example accepts `-seconds N` to exit after N seconds and
`-shot file.png` to save a screenshot partway through, so each one can
verify itself in a script. `Config.Headless` (or the environment
variable `BUNYIP_HEADLESS=1`) runs a game with no window, rendering
offscreen, so the same screenshots come out of a build machine. A few to
try:

```
go run ./examples/gallery -skin -theme nord
go run ./examples/tiles
go run ./examples/lighting
go run ./examples/terrain
go run ./examples/tetris
go run ./cmd/bunyip-info
```

## Shipping

`bunyip-pack` bundles an asset directory into one file that the
[asset](../pkg/asset.html) package reads alongside loose files.
`bunyip-bundle` produces a macOS `.app` carrying the Vulkan loader and
MoltenVK, or a plain folder on other systems:

```
go build -o mygame .
go run github.com/matjam/bunyip/cmd/bunyip-bundle -name "My Game" -exe ./mygame -assets ./assets -o dist
```
