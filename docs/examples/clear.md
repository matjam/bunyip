---
title: Clear
example: clear
summary: the renderer's smoke test: a window cleared to a cycling colour, with one frame written to a PNG
---

This is the smallest program in the repository that puts pixels on a
screen. It opens a window, creates a renderer on it, and clears the
swapchain image to a colour that cycles with time. With `-shot` it reads
one frame back and writes it to a PNG, which is how the renderer's output
is checked on a build machine with nobody watching.

Like [window](window.html), it bypasses the engine loop. There is
no [bunyip.Run](../pkg/bunyip.html#Run), no `Game`, and no
[gfx](../pkg/gfx.html): the program drives the platform layer and the
Vulkan backend directly, through the module's internal packages. Read it
to see what [bunyip.Run](../pkg/bunyip.html#Run) does on a game's behalf,
and read [the window guide](../guides/window.html) for the loop that
replaces this one in a real game.

Because the frame is a single clear, every pixel of the result is the
same colour, so the examples test skips it and the site shows no
screenshot for it. Run it yourself:

```bash
go run ./examples/clear -seconds 3 -shot out.png
```

The flags are `-seconds N` to exit after that many seconds, `-shot
file.png` to write the frame at half that time, and `-validate` to enable
the Vulkan validation layers when they are installed, which is on by
default.

## The imports

The package comment says what the command does, in the form `go doc`
prints. The imports are what marks this program out: `internal/platform`
is the per-operating-system window and event layer, `internal/render` is
the Vulkan backend, and `internal/vk` is the generated binding. A game
never imports these. It imports [input](../pkg/input.html) for the key
codes, which this program does too.

```go
// Command clear opens a window and clears it to a colour that cycles over
// time. With -shot it also writes one frame to a PNG, which is how renderer
// output is checked without a person looking at the screen.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"math"
	"os"
	"time"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/platform"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)
```

## main: the flags

Every example takes `-seconds` and `-shot` so that a run verifies itself.
`main` parses them, calls `run`, and turns an error into a message on
standard error and a non-zero exit status.

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds (0: until closed)")
	shot := flag.String("shot", "", "write the frame at -seconds/2 (or the first frame) to this PNG")
	validate := flag.Bool("validate", true, "enable validation layers when installed")
	flag.Parse()
	if err := run(*seconds, *shot, *validate); err != nil {
		fmt.Fprintln(os.Stderr, "clear:", err)
		os.Exit(1)
	}
}
```

## Opening a window and a renderer

`platform.NewApp` connects to the window system: AppKit on macOS, Wayland
or X11 on Linux, Win32 on Windows. `app.NewWindow` opens one window from
a `platform.Config`, whose zero values are sensible defaults in the same
way [bunyip.Config](../pkg/bunyip.html#Config)'s are.

The renderer wants the window's size in pixels rather than in logical
units, which is what `win.PixelSize` returns and what a high-density
display makes different. `render.NewRenderer` takes the instance
extensions the platform needs, the window's own surface constructor, the
first size, and a flag asking for a swapchain. Passing
`win.CreateSurface` rather than a finished surface keeps the Vulkan calls
in the render package and the operating system calls in the platform
package. `defer r.Destroy()` releases the device objects on the way out,
which is the same duty a game discharges in `Shutdown`.

```go
func run(seconds float64, shot string, validate bool) error {
	app, err := platform.NewApp()
	if err != nil {
		return err
	}
	win, err := app.NewWindow(platform.Config{Title: "Bunyip clear", Width: 640, Height: 480, Resizable: true})
	if err != nil {
		return err
	}
	pw, ph := win.PixelSize()
	cfg := render.Config{AppName: "clear", Validation: validate, Log: slog.Default()}
	r, err := render.NewRenderer(cfg, platform.RequiredInstanceExtensions(), win.CreateSurface,
		vk.VkExtent2D{Width: uint32(pw), Height: uint32(ph)}, true)
	if err != nil {
		return err
	}
	defer r.Destroy()
```

## When to take the screenshot

The screenshot is taken halfway through the run, so it lands after the
first frames rather than on them. `shotAt` starts at positive infinity,
which means never, and drops to the halfway mark only when `-shot` is
given. A run with `-shot` and no `-seconds` captures the first frame.

```go
	start := time.Now()
	shotAt := time.Duration(math.Inf(1))
	if shot != "" {
		shotAt = 0
		if seconds > 0 {
			shotAt = time.Duration(seconds / 2 * float64(time.Second))
		}
	}
	frames := 0
```

## The frame loop

`app.Poll(false)` returns the events that have arrived without blocking.
The `false` is what makes this a real-time loop: passing `true` would
block until an event arrives, which is what a turn-based game does and
what [bunyip.Config](../pkg/bunyip.html#Config)'s `TurnBased` field
selects. The loop closes the window on a close event or on Escape, and
hands a resize to the renderer, which rebuilds the swapchain.

```go
	for !win.Closed() {
		for _, e := range app.Poll(false) {
			switch {
			case e.Kind == platform.EventClose, e.Kind == platform.EventKeyDown && e.Key == input.KeyEscape:
				win.Close()
			case e.Kind == platform.EventResize:
				r.Resize(e.PixelW, e.PixelH)
			}
		}
		if win.Closed() {
			break
		}
```

The clear colour is three sines of the elapsed time at different phases,
each mapped from the range minus one to one into zero to one. These are
linear values written straight into the clear value, not sRGB bytes; a
game that wants a colour from bytes calls [gfx.RGB](../pkg/gfx.html#RGB)
and lets the engine convert.

`r.BeginFrame` waits for a frame slot and acquires a swapchain image. The
`ok` it returns is false when the swapchain is out of date, in which case
the loop simply tries again. `r.BeginSwapchainPass` starts the render
pass with the clear value, and `r.EndFrame` submits and presents it. The
`capture` argument makes `EndFrame` read the image back into an
`image.Image`, which costs a copy through host-visible memory and is why
it is a parameter rather than something the renderer always does.

```go
		t := time.Since(start)
		if seconds > 0 && t.Seconds() >= seconds {
			win.Close()
			break
		}
		s := t.Seconds()
		clear := [4]float32{float32(0.5 + 0.5*math.Sin(s)), float32(0.5 + 0.5*math.Sin(s+2)), float32(0.5 + 0.5*math.Sin(s+4)), 1}
		fr, ok, err := r.BeginFrame()
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		r.BeginSwapchainPass(fr, clear)
		capture := shot != "" && t >= shotAt
		img, err := r.EndFrame(fr, capture)
		if err != nil {
			return err
		}
		frames++
		if capture {
			if err := writePNG(shot, img); err != nil {
				return err
			}
			fmt.Printf("wrote %s (%dx%d) at frame %d\n", shot, img.Bounds().Dx(), img.Bounds().Dy(), frames)
			shot = ""
		}
	}
```

Setting `shot` to the empty string after writing the file is what stops
the program writing a PNG every frame from then on. The last line reports
the frame count, which is a rough frame rate for a run of a known length.

```go
	fmt.Printf("%d frames in %.1fs\n", frames, time.Since(start).Seconds())
	return nil
}
```

## Writing the PNG

`EndFrame` returns a plain `image.Image`, so writing it out is the
standard library and nothing else.

```go
func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
```

## What to try

- Change the clear colour in `run` to a constant and check the PNG with
  an image viewer; this is how a renderer change is bisected.
- Pass `true` to `app.Poll` in `run` and watch the frame count collapse:
  the loop then runs once per event rather than as fast as it can.
- Print `e.Kind` for every event in `run` to see what the platform layer
  reports as the window is moved, focused and resized.
- Remove the `-validate` default in `main` and compare the startup log
  with the validation layers off.
