---
title: Getting started
group: Start
order: 3
summary: install the Vulkan driver, open a window, learn the loop and the window controls
---

## Requirements

Bunyip needs Go 1.27 or later and a Vulkan driver. There is nothing to
compile against. The engine opens the driver at run time.

Set `CGO_ENABLED=0` for all Go commands in this guide. On macOS or Linux,
run `export CGO_ENABLED=0`; in PowerShell, use `$env:CGO_ENABLED = "0"`.

| Platform | Install |
|---|---|
| macOS | `brew install vulkan-loader molten-vk`. For validation messages during development, also `brew install vulkan-validationlayers`. |
| Linux | The GPU vendor's Vulkan driver and Vulkan loader. Native Wayland needs `libwayland-client` and `libxkbcommon`; X11 needs `libxcb`, with `libxkbcommon` and `libxkbcommon-x11` for text input. Audio uses ALSA (`libasound`). |
| Windows | A current GPU driver; `vulkan-1.dll` ships with it. |

The engine has been tested on macOS and Linux. Linux windowing has run
on both native Wayland and X11, and Linux audio output and capture have
hardware verification. Windows, Linux gamepads and macOS capture have
build and test coverage but still need hardware verification.

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
	if err := bunyip.Run(bunyip.Config{Title: "First window", Width: 960, Height: 600, Resizable: true}, &game{}); err != nil {
		panic(err)
	}
}
```

```
go mod init example.com/first
go get github.com/matjam/bunyip
go run .
```

`Run` opens the window, creates the renderer and the audio device,
calls the game's `Init` method if it has one, and runs the loop until
`Quit`. Graphics owns its textures, fonts, meshes and other GPU resources
and releases them at shutdown, including after setup or drawing fails.
Use `Destroy` to release one earlier, for example when unloading a level.
For other cleanup, register a closure with `ctx.Cleanup` after acquiring
the resource. The callbacks run in reverse order, after optional
`Shutdown` and before the engine closes its devices; they also run if
`Init` or `Recover` fails. To recover from a lost
graphics device, implement `Recover(ctx *bunyip.Context) error` and
recreate resources there. The engine calls `Recover` with a fresh
context instead of `Init`; without that method, `Run` returns the
device-loss error. The mixer and console are also rebuilt, so restore
audio configuration, playback and console registrations in `Recover`.
`Shutdown` is called only after successful `Init` or `Recover`.

Small programs can provide closures with `GameFuncs` instead of declaring
a game type. Unset callbacks do nothing:

```go
err := bunyip.Run(bunyip.Config{Title: "Hello"}, bunyip.GameFuncs{
	DrawFunc: func(ctx *bunyip.Context) error {
		ctx.Gfx.DebugText(20, 20, "Hello, Bunyip")
		return nil
	},
})
if err != nil {
	panic(err)
}
```

Window width and height default independently to 1280 and 720 points, so
`Config{Width: 960}` keeps that width and uses the default height.

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

`ctx.SetTimeScale` changes how fast game time runs without changing the
update rate: `ctx.SetTimeScale(0.25)` scales `ctx.Delta` to a quarter,
so the simulation crawls and can be watched, and `0` freezes it.
`ctx.Time` stays real time. The console's `timescale` command sets it.

Turn-based games set `Config.TurnBased`. The loop then blocks in the
operating system until input arrives and runs one `Update` and one
`Draw` per batch of events while active. The main loop blocks between
events; audio and game-owned goroutines can still run.
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

`Config.Console` turns on the in-game console: a command line on the
backquote key and panels on F4 that show the frame timings, the GPU
resources, a world's entities, the physics simulation, the mixer and the
input devices, and that let a game expose its own commands and tunable
variables. The engine draws it after the game and the debug overlay. The
[console guide](console.html) covers it.

`Config.DrawBudget` turns the draw-call count
into a warning when a frame exceeds it. `Config.Pprof` serves Go's
profiler on an address. `Config.LogFile` writes the log to a file and
appends a stack trace if the game panics. That file is the one to ask a
player for. `bunyip.FlyCamera` is a free-flying camera for looking round
a 3D scene while it is being built.

## The examples

Most examples accept `-seconds N` to exit after N seconds and
`-shot file.png` to save a screenshot partway through. The headless
harness excludes `window`, `network` and `clear`; `assets` runs with a
nonblank check but no golden comparison. `Config.Headless` (or the environment
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
