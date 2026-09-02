---
title: Getting started
order: 2
summary: install the Vulkan driver, open a window, run the examples
---

## Requirements

Bunyip needs Go 1.23 or later and a Vulkan driver. There is nothing to
compile against: the engine opens the driver at run time.

| Platform | Install |
|---|---|
| macOS | `brew install vulkan-loader molten-vk`. Optional: `brew install vulkan-validationlayers` for validation during development. |
| Linux | Your GPU vendor's Vulkan driver, plus `libxcb` and `libasound` (present on every desktop). `libxkbcommon` and `libxkbcommon-x11` enable text input. |
| Windows | A current GPU driver; `vulkan-1.dll` ships with it. |

> The Windows and Linux layers cross-compile and pass their tests, but the
> engine is developed and verified on macOS first. Report what you find.

## A window

Create a module and a `main.go`:

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

`Run` opens the window, creates the renderer and audio device, calls your
`Init` if you have one, and runs the loop until `Quit`. Resources such as
textures and fonts are created from `ctx.Gfx` in `Init` and freed in an
optional `Shutdown`.

## The two loop modes

Real-time is the default: `Update` runs at a fixed step (60 Hz unless
`Config.FixedStep` says otherwise) and `Draw` runs once per displayed
frame. `ctx.Delta` is the step, so physics stays deterministic whatever
the frame rate.

Turn-based games set `Config.TurnBased`. The loop then blocks in the
operating system until input arrives and runs one `Update` and one `Draw`
per batch of events. A timer, a network message or a finished asset load
can wake it with `ctx.Wake()`, and `ctx.RequestRedraw()` asks for another
frame while an animation is running.

When the display runs faster than the update rate, `ctx.Alpha` during
`Draw` says how far the clock has run past the last update as a fraction
of a step. Drawing a body at `previous + (current - previous) * Alpha`
keeps motion smooth at any frame rate.

## A fixed view

By default the view is the window's size in points and follows it. A
game that designs for one resolution sets `Config.ViewWidth` and
`ViewHeight`; the engine scales that view into the window by
`Config.Scaling` and centres it. `ScaleFit` keeps the aspect ratio with
black bars, `ScaleInteger` uses whole multiples so pixel art stays crisp,
and `ScaleStretch` fills the window. `ctx.Width` and `ctx.Height` are
then the view's size, pointer positions arrive in view units, and the 3D
scene renders at the viewport's size.

## The window

`ctx.SetTitle`, `ctx.SetIcon` (or `Config.Icon`), `ctx.SetSizeLimits`,
`ctx.SetFullscreen`, `ctx.SetCursorVisible`, `ctx.SetCursor` and
`ctx.SetCursorCaptured` control the window; `ctx.Focused` says whether it
has focus. `ctx.Clipboard` and `ctx.SetClipboard` reach the system
clipboard on macOS and Windows. With `Config.HandleClose` the close
button no longer quits: `ctx.CloseRequested` becomes true for an update
and the game saves, asks, and calls `ctx.Quit` itself.

## Input and the frame

`ctx.Input` reports what is held (`KeyDown`), what changed (`KeyPressed`,
`KeyReleased`, `MousePressed`), the pointer, the wheel, typed text and
gamepads. Edges are per update, and during `Draw` they cover the whole
frame, so an interface built in `Draw` reacts to clicks that `Update`
already consumed.

## The examples

Every example accepts `-seconds N` to exit after N seconds and
`-shot file.png` to save a screenshot halfway through, so each one can
verify itself in a script. `Config.Headless` runs a game with no window
at all, rendering offscreen, so the same screenshots come out of a
build machine or a test. Try a few:

```
go run ./examples/gallery -skin -theme nord
go run ./examples/tiles
go run ./examples/lighting
go run ./examples/tetris
go run ./cmd/bunyip-info
```

## Shipping

`bunyip-pack` bundles an asset directory into one file that
[asset](../pkg/asset.html) reads alongside loose files. `bunyip-bundle`
produces a macOS `.app` carrying the Vulkan loader and MoltenVK, or a plain
folder on other systems:

```
go build -o mygame .
go run github.com/matjam/bunyip/cmd/bunyip-bundle -name "My Game" -exe ./mygame -assets ./assets -o dist
```
