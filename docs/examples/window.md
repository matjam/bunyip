---
title: Window
example: window
summary: the platform layer's smoke test, opening a native window and presenting cleared frames without the engine
---

Like [clear](clear.html), this example bypasses the engine loop. It calls the
internal platform and render packages directly to open a native window,
create a Vulkan surface on it, build a swapchain, present a cleared
frame every iteration, and print every event the window layer produces.
It is the smoke test for the platform layer: when a new operating
system backend is written, or an existing one is changed, this program
says whether the whole path from event loop to present still works.

Presenting is part of the test rather than decoration. A Wayland window
is not mapped until a buffer is committed to its surface, so a program
that only polls for events shows nothing and proves nothing. The frames
clear to one dark colour, so the moment the window appears on screen the
instance, device, surface, swapchain, command buffers and present queue
have all worked.

The rest of the engine is absent. There is no `bunyip.Run`, no
`Graphics` and no game type; the loop is written by hand. Key codes come
from [input](../pkg/input.html), because the platform layer reports keys
by physical position using the same codes a game sees. The window guide,
[The window](../guides/window.html), describes the layer this program
exercises from a game's point of view.

Because there is no engine, there is no `-shot` flag and no screenshot,
and `examples_test.go` skips this example for that reason. Run it with:

```bash
go run ./examples/window -seconds 3
```

`-seconds N` closes the window after N seconds; the default of 0 runs
until the window is closed or Escape is pressed. `-wait` blocks in the
operating system for events instead of polling, which is what the
engine's turn-based mode does. Blocking is only used when no deadline is
set, since a blocked poll cannot notice that the time is up.

## The package comment and imports

The doc comment says what the program is for, which matters more here
than in the other examples: a reader who finds this file is usually
debugging a platform backend. The imports are the two internal packages
that sit under the engine, `platform` for the window and its events and
`render` for the Vulkan device and swapchain, plus `vk` for the raw
Vulkan types and `input` for key codes.

```go
// Command window opens a native window, presents cleared frames through a
// Vulkan swapchain and prints every event until the window is closed or
// -seconds elapse. It is the smoke test for the platform layer. Presenting
// is part of the test: a Wayland window only appears once a buffer is
// committed to its surface, so a window that opens on every platform must
// draw. The frames clear to one dark colour; the moment the window shows
// it, the whole stack from event loop to present has worked.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/platform"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)
```

## main and the flags

`main` parses the two flags and hands them to `run`, which returns an
error rather than exiting, so every failure below prints one line and
sets a non-zero status.

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds (0: run until closed)")
	wait := flag.Bool("wait", false, "block for events instead of polling (turn-based mode)")
	flag.Parse()
	if err := run(*seconds, *wait); err != nil {
		fmt.Fprintln(os.Stderr, "window:", err)
		os.Exit(1)
	}
}
```

## Opening the window and the renderer

`platform.NewApp` connects to the windowing system. On Linux it chooses
Wayland or X11 here, so a failure at this line is a display connection
problem rather than a Vulkan one. `app.NewWindow` takes a
`platform.Config` whose zero values are defaults in the same way the
engine's `Config` is.

Two sizes come back from the window and both are printed. `Size` is in
points, the units the operating system lays out windows in, and
`PixelSize` is the framebuffer size in pixels. On a display with a scale
factor of 2 they differ by a factor of two, and the swapchain must be
built at the pixel size.

`win.Visible()` is printed beside them. Visibility is whether the window
can be seen at all, which is not the same question as focus: a window
loses focus the moment another window is clicked, but stays visible.
It is false while the window is minimised, and on the platforms that
report it, while the window is wholly covered by other windows. Each
backend answers from what its system tells it: miniaturised or occluded
on macOS, minimised on Windows, suspended through xdg_toplevel version 6
under Wayland, and unmapped, minimised or obscured on X11. A game does
not usually call this method, because the engine turns it into
`ctx.Visible` and, with `Config.PauseHidden`, into a pause; printing it
here is how a new backend is checked.

`render.NewRenderer` takes the instance extensions the platform needs,
a callback that creates the surface once the instance exists, and the
initial extent. The callback shape exists because a Vulkan surface can
only be created after the instance, while the window must exist before
the instance is asked which extensions it needs. The final `true`
requests validation layers. The surface handle is captured in the
closure so the report below can query it. `defer r.Destroy()` releases
the device, swapchain and instance on every return path.

```go
func run(seconds float64, wait bool) error {
	app, err := platform.NewApp()
	if err != nil {
		return err
	}
	win, err := app.NewWindow(platform.Config{Title: "Bunyip window", Width: 800, Height: 600, Resizable: true})
	if err != nil {
		return err
	}
	w, h := win.Size()
	pw, ph := win.PixelSize()
	fmt.Printf("window: %dx%d points, %dx%d pixels, scale %.2f, visible %v\n", w, h, pw, ph, win.Scale(), win.Visible())

	var surface vk.VkSurfaceKHR
	r, err := render.NewRenderer(render.Config{AppName: "window"},
		platform.RequiredInstanceExtensions(),
		func(instance vk.VkInstance) (vk.VkSurfaceKHR, error) {
			s, err := win.CreateSurface(instance)
			surface = s
			return s, err
		},
		vk.VkExtent2D{Width: uint32(pw), Height: uint32(ph)}, true)
	if err != nil {
		return err
	}
	defer r.Destroy()
	fmt.Printf("surface: 0x%x on instance 0x%x\n", surface, r.Instance.Handle)
	if err := reportSurface(r.Device.Physical(), surface); err != nil {
		return err
	}
	fmt.Printf("swapchain: %dx%d, format %d, %d images\n",
		r.Swapchain.Extent.Width, r.Swapchain.Extent.Height, r.Swapchain.Format, len(r.Swapchain.Images))
```

## The loop

The frame is presented before the events are polled, and the comment
says why: on Wayland the window is not on screen until something has
been committed to its surface, and a first poll that blocks would then
block on a window nobody can see or click. In polling mode the present
call also paces the loop, because the swapchain waits for vsync.

`app.Poll` returns the events that have arrived. It blocks only when
asked to and only when no deadline is set. Resize events rebuild the
swapchain at the new pixel size. A close event, or Escape, closes the
window, which ends the loop; the deadline does the same when `-seconds`
is given.

```go
	deadline := time.Time{}
	if seconds > 0 {
		deadline = time.Now().Add(time.Duration(seconds * float64(time.Second)))
	}
	for !win.Closed() {
		// The frame comes first so the window is mapped before the first
		// blocking poll; vsync paces the loop in polling mode.
		if err := present(r); err != nil {
			return err
		}
		for _, e := range app.Poll(wait && deadline.IsZero()) {
			printEvent(e)
			if e.Kind == platform.EventResize {
				r.Resize(e.PixelW, e.PixelH)
			}
			if e.Kind == platform.EventClose || (e.Kind == platform.EventKeyDown && e.Key == input.KeyEscape) {
				win.Close()
			}
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			win.Close()
		}
	}
	fmt.Println("closed")
	return nil
}
```

## Presenting one frame

`BeginFrame` acquires a swapchain image and returns a frame plus an `ok`
flag. The flag is false, with no error, while the swapchain is being
rebuilt after a resize, and the frame is skipped. `BeginSwapchainPass`
starts a render pass that clears to the given colour, and `EndFrame`
submits and presents. The colour is four floats in linear space, the
same convention the engine uses for `Config.Clear`.

```go
// present records one frame that clears the swapchain image and presents
// it. A frame is dropped without error while the swapchain rebuilds after
// a resize.
func present(r *render.Renderer) error {
	fr, ok, err := r.BeginFrame()
	if err != nil || !ok {
		return err
	}
	r.BeginSwapchainPass(fr, [4]float32{0.10, 0.12, 0.16, 1})
	_, err = r.EndFrame(fr, false)
	return err
}
```

## Printing events

Every event kind gets a line with the fields that kind carries. Mouse
movement is dropped because it would bury everything else. The point of
printing the fields rather than the event is that a backend is checked
against them: modifier flags on key events, whether a key repeat is
reported, precise scroll deltas from a trackpad against coarse deltas
from a wheel, and both sizes plus the scale factor on a resize.

`EventVisible` is the running counterpart of the `Visible` call above:
the backend sends one whenever the answer changes, so minimising the
window prints `visible=false` and restoring it prints `visible=true`.
It is separate from `EventFocus` on purpose, because the two go
different ways: clicking another window on the same screen changes focus
and not visibility, while minimising changes both. A backend that never
sends this event is one where nothing pauses under
`Config.PauseHidden`, which is what makes the line worth printing. The
`default` case prints the kind alone for the events that carry no
fields, such as `EventClose`.

```go
func printEvent(e platform.Event) {
	switch e.Kind {
	case platform.EventMouseMove:
		return // too chatty for a log
	case platform.EventKeyDown, platform.EventKeyUp:
		fmt.Printf("%s key=%s mods=%s repeat=%v\n", e.Kind, e.Key, e.Mods, e.Repeat)
	case platform.EventChar:
		fmt.Printf("%s %q\n", e.Kind, e.Rune)
	case platform.EventMouseDown, platform.EventMouseUp:
		fmt.Printf("%s button=%d at %.0f,%.0f\n", e.Kind, e.Button, e.X, e.Y)
	case platform.EventScroll:
		fmt.Printf("%s dx=%.2f dy=%.2f precise=%v\n", e.Kind, e.DX, e.DY, e.Precise)
	case platform.EventResize:
		fmt.Printf("%s %dx%d points, %dx%d pixels, scale %.2f\n", e.Kind, e.Width, e.Height, e.PixelW, e.PixelH, e.Scale)
	case platform.EventFocus:
		fmt.Printf("%s focused=%v\n", e.Kind, e.Focused)
	case platform.EventVisible:
		fmt.Printf("%s visible=%v\n", e.Kind, e.Visible)
	default:
		fmt.Println(e.Kind)
	}
}
```

## Reporting the surface

The last function asks the physical device what it can do with this
surface: the current extent, the range of image counts a swapchain may
request, the supported transforms and usage flags, whether queue family
0 can present, and the available formats. These are the queries a
swapchain makes, so running them by hand and printing the answers turns
a silent surface problem into a readable line. `vk.Check` turns a Vulkan
result code into an error naming the call that produced it.

```go
// reportSurface prints what the chosen physical device can present to the
// surface, which exercises the whole path a swapchain needs.
func reportSurface(dev vk.VkPhysicalDevice, surface vk.VkSurfaceKHR) error {
	var caps vk.VkSurfaceCapabilitiesKHR
	if err := vk.Check("vkGetPhysicalDeviceSurfaceCapabilitiesKHR", vk.VkGetPhysicalDeviceSurfaceCapabilitiesKHR(dev, surface, &caps)); err != nil {
		return err
	}
	fmt.Printf("surface: current extent %dx%d, images %d..%d, transforms 0x%x, usage 0x%x\n",
		caps.CurrentExtent.Width, caps.CurrentExtent.Height, caps.MinImageCount, caps.MaxImageCount,
		caps.SupportedTransforms, caps.SupportedUsageFlags)
	var supported vk.VkBool32
	if err := vk.Check("vkGetPhysicalDeviceSurfaceSupportKHR", vk.VkGetPhysicalDeviceSurfaceSupportKHR(dev, 0, surface, &supported)); err != nil {
		return err
	}
	var nfmt uint32
	vk.VkGetPhysicalDeviceSurfaceFormatsKHR(dev, surface, &nfmt, nil)
	formats := make([]vk.VkSurfaceFormatKHR, nfmt)
	if nfmt > 0 {
		vk.VkGetPhysicalDeviceSurfaceFormatsKHR(dev, surface, &nfmt, &formats[0])
	}
	fmt.Printf("surface: queue family 0 present support %d, %d formats (first: format %d colorspace %d)\n",
		supported, nfmt, formats[0].Format, formats[0].ColorSpace)
	return nil
}
```

## What to try

- Change the clear colour in `present` and confirm the window shows it;
  this is the fastest check that presenting works at all.
- Run with `-wait` and no `-seconds` to see turn-based behaviour: the
  loop blocks until events arrive, then prints them and presents another
  frame. One poll can return several events.
- Remove the `EventMouseMove` case from `printEvent` to watch the
  pointer coordinates the backend reports, including whether they are in
  points or pixels.
- Resize the window and watch the resize lines from `printEvent`; drag
  it between displays with different scale factors to see `Scale`
  change and `run` rebuild the swapchain.
- Minimise the window and restore it, then cover it completely with
  another window, and watch which of those the backend reports as
  `visible=false`. The answer differs by platform, which is the point.
- Call `win.SetClipboard("hello")` after a key press and
  `win.Clipboard()` on the next one, and paste into another program.
  Neither Wayland nor X11 keeps clipboard text anywhere: the program
  that copied owns the selection and hands the text over on request, so
  the text lives only as long as this process, unless a clipboard
  manager takes it over. A read waits about a second for the owner to
  answer and comes back empty if nobody does. Under Wayland a write also
  fails until the window has had some input, because the compositor
  changes the selection only in answer to a key, a button or the pointer
  arriving.
- Set `BUNYIP_X11=1` on Linux to force the X11 backend, and compare the
  event stream with the Wayland one.
