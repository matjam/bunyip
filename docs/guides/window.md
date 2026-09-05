---
title: The window
group: Engine
order: 1
summary: how the window is opened, sized and controlled, what is fixed at start and what changes at run time
---

## Opening the window

`bunyip.Run` opens the window from the `Config` it is given, and closes
it when the loop ends. There is no window object in the API and there is
only one window. A game reaches it through methods on the `Context` it
gets in `Init`, `Update` and `Draw`.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyF11) {
		ctx.SetFullscreen(!ctx.Fullscreen())
	}
	return nil
}
```

`Config` sets what the window is at the moment it opens. `Context`
methods change it afterwards.

| Fixed at start | Changed at run time |
|---|---|
| `Title` (the first one), `Width`, `Height`, `Resizable` | `SetTitle`, `SetIcon`, `SetSizeLimits` |
| `Icon` | `SetCursor`, `SetCursorVisible`, `SetCursorImage`, `SetCursorCaptured` |
| `ViewWidth`, `ViewHeight`, `Scaling` | `SetFullscreen`, `Fullscreen` |
| `Headless`, `FixedClock`, `NoVSync`, `HandleClose`, `PauseUnfocused`, `PauseHidden` | `SetPosition`, `Position`, `SetAlwaysOnTop` |

Two things are missing on purpose. There is no programmatic resize.
Nothing in the API sets the window's size after it opens, so the player
and the window manager control it. There is also no `Config` field to
start full screen. To start full screen, call `ctx.SetFullscreen(true)`
from `Init`. Full-screen state is read back from the operating system
rather than remembered by the engine, so `ctx.Fullscreen` is still right
after the player uses the system's own full-screen button.

## Points, pixels and view units

Three units meet at the window. Mixing them is the usual cause of
misplaced sprites.

| Unit | What it is | Where it appears |
|---|---|---|
| Point | The system's logical pixel, the unit a window is sized in | `Config.Width` and `Height`, `SetSizeLimits`, `SetPosition`, `Position` |
| Pixel | A real pixel in the framebuffer | The swapchain and every render target, `SetCursorImage`'s hot spot |
| View unit | The game's own coordinate space | Everything drawn, every pointer position the game reads |

`ctx.Width` and `ctx.Height` are the view's size: view units when
`Config.ViewWidth` and `ViewHeight` are set, window points when they are
not. `ctx.Scale` is how many pixels one of those units covers, which is
the display's scale factor on a window that has no fixed view.

## Resizing

Without a fixed view the view follows the window. Every resize sets
`ctx.Width` and `ctx.Height` to the new content size in points, and the
2D coordinate space grows with the window: a sprite at (10, 10) stays in
the top-left corner and more of the world becomes visible.

With `Config.ViewWidth` and `ViewHeight` set, the view is fixed.
`Config.Scaling` decides how it fills the window: `ScaleFit` takes the
largest size that keeps the aspect ratio, `ScaleInteger` scales by whole
numbers so pixel art stays crisp, and `ScaleStretch` fills the window and
distorts. `ScaleFit` and `ScaleInteger` centre the view and leave black
bars around it. `ctx.Width` and `ctx.Height` never change, so layout
written once is still right; only `ctx.Scale` changes.

```go
bunyip.Run(bunyip.Config{
	Title: "Pixels", Width: 1280, Height: 720, Resizable: true,
	ViewWidth: 320, ViewHeight: 180, Scaling: bunyip.ScaleInteger,
}, &game{})
```

Pointer positions and pointer movement are mapped out of window points
and into view units before the game sees them, so a click inside the
letterboxed image lands on the sprite that is drawn there. A click on a
black bar maps outside the view's rectangle.
`Context.SetTextInputRect` applies the inverse mapping to its view-unit
rectangle, including letterbox offsets, stretching and display density,
so input-method candidate windows line up with the drawn field. Updates
are skipped while a minimized window has no drawable area.

Underneath, the swapchain and the render targets are sized in pixels from
the window's framebuffer size, so a fixed view still renders at the
display's full resolution and text stays sharp.

## What the game can observe

The loop turns window events into a few things a game reads.

The pause settings apply to both fixed-step and turn-based games.
Paused wall time does not accumulate into catch-up updates or the next
turn-based delta. The turn-based update that resumes the game has zero
`Delta`; later active intervals use the current time scale. `Time`
continues to measure elapsed wall time during a pause.

- `ctx.Focused` reports keyboard focus. `Config.PauseUnfocused` stops
  updates and silences the mixer while another window has focus; frames
  still draw.
- `ctx.Visible` reports whether the window can be seen. It is false while
  the window is minimised, and while it is wholly covered by other
  windows on the platforms that report that. `Config.PauseHidden` stops
  updates and silences the mixer while it is false, the same way
  `PauseUnfocused` does for focus; setting both pauses while either is
  true. The loop touches the mixer only when that combined state
  changes, so a game that paused its own mixer keeps it paused. A
  headless run is always visible.
- `ctx.CloseRequested` is true for one update after the player asks to
  close, but only with `Config.HandleClose`. Without it the loop quits on
  its own and the game never sees the request.
- `ctx.Quit` ends the loop after the current callback.
- `ctx.RequestRedraw` asks a turn-based loop for another frame without
  waiting for input, which is how an animation that spans turns plays.
  `ctx.Wake` ends the loop's wait early, from a timer, a network reply
  or a finished asset load.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.CloseRequested() {
		g.save()
		ctx.Quit()
	}
	return nil
}
```

A resize is not delivered to the game as an event. The loop handles it
itself and the new sizes are in `ctx.Width`, `ctx.Height` and `ctx.Scale`
at the next callback.

## Threading

`Run` must be called from the main goroutine: window systems deliver
events only to the thread that created the connection, and the platform
layer pins itself to that thread. Every window method on `Context` must
be called from `Init`, `Update`, `Draw` or `Shutdown`, on that same
goroutine. `ctx.Wake` is the exception. It is safe from any goroutine,
which is what makes it useful for waking a sleeping turn-based game.

## Headless runs

`Config.Headless`, or the environment variable `BUNYIP_HEADLESS=1`, runs
the same loop with no window. Frames render offscreen at `Config.Width`
by `Config.Height`. Without a fixed view, `ctx.Scale` is 1; a fixed view
still uses the configured scaling. The size never changes, and no
events arrive, so no key is ever pressed and the window never asks to
close. Most window controls do nothing; fullscreen and cursor-capture
setters remember their flags without affecting a desktop. Clipboard text
is kept in memory. `ctx.Screenshot` works. Screenshot-capable examples
take `-seconds N` and `-shot file.png`. The headless test harness skips
`window` (native platform smoke test), `network` (requires a peer), and
`clear` (intentionally uniform output). It checks `assets` for nonblank
output but excludes it from golden comparisons because it changes its
own files and run counter.

```
BUNYIP_HEADLESS=1 go run ./examples/tetris -seconds 2 -shot /tmp/t.png
```

## A reproducible run

A screenshot is only worth comparing against a stored one if the same
run draws the same frame every time, and the loop reads the wall clock,
so it does not: how many updates ran before a given frame depends on how
long the machine took.

`Config.FixedClock`, or `BUNYIP_FIXED_CLOCK=1`, advances a real-time
game's clock by one `FixedStep` each frame and runs one `Update` per
frame unless paused. It has no effect in turn-based mode.
`ctx.Time` is then the frame number times the step, `ctx.Delta`
is the step multiplied by `TimeScale`, and `ctx.Alpha` is zero, so frame N is the
same picture on any machine as long as the game seeds its own random
numbers. Nothing paces the loop either, so a headless run goes as fast
as it can. It is for tests and for recording, not for playing: a game
run this way speeds up and slows down with the frame rate.

```
BUNYIP_HEADLESS=1 BUNYIP_FIXED_CLOCK=1 go run ./examples/tetris -seconds 2 -shot /tmp/t.png
```

The examples test uses both, which is how it compares each example
against a stored image in `examples/testdata`.

## What each platform supports

macOS is the tested platform. The Wayland and X11 layers have run on
real Linux hardware. The Windows layer cross-compiles and passes its
unit tests, but it has not. `docs/design/gaps.md` in the repository
keeps the current list of what is missing.

Linux has two window systems, so the layer picks one when the process
starts. It uses Wayland when `WAYLAND_DISPLAY` is set (or the default
socket exists under `XDG_RUNTIME_DIR`), `libwayland-client.so.0` loads
and the connection succeeds, and X11 otherwise. Set `BUNYIP_X11=1` to
force X11, which is how a run under XWayland is compared against a
native one. The choice is logged at debug level as `bunyip: window
backend`.

| Control | macOS | Windows | Wayland | X11 |
|---|---|---|---|---|
| `SetTitle` | yes | yes | yes | yes |
| `SetIcon` | yes | yes | with `xdg-toplevel-icon-v1` | yes |
| `SetSizeLimits` | yes | yes | yes | yes |
| `SetCursor`, `SetCursorVisible` | yes | yes | yes | yes |
| `SetCursorCaptured` | yes | yes | yes | yes |
| `SetFullscreen`, `Fullscreen` | yes | yes | yes | yes |
| `SetTextInputRect` | yes | recorded, no IME yet | recorded, no IME yet | recorded, no IME yet |
| `SetPosition`, `Position` | yes | no-op | no-op | no-op |
| `SetAlwaysOnTop` | yes | no-op | no-op | no-op |
| `SetCursorImage` | yes | no-op | no-op | no-op |
| `Clipboard`, `SetClipboard` | yes | yes | yes | yes |
| `ctx.Visible` | miniaturised and occluded | minimised | suspended (xdg_toplevel 6) | unmapped, minimised and obscured |
| DPI scale (`ctx.Scale`) | the display's factor | the window's DPI over 96 | the compositor's fractional scale, or the output's integer scale | always 1 |

Where a control is a no-op, the call returns and nothing happens.
`Position` returns (0, 0) where it is not implemented.

The clipboard is not a place the system keeps text on X11. One program
owns the CLIPBOARD selection and hands the text over when another asks,
so `SetClipboard` makes the game the owner and the text is available only
while the game runs; a clipboard manager is what keeps it afterwards.
`Clipboard` asks the current owner and waits about a second for the
answer, returning empty text when nobody owns the selection or nobody
replies. It asks for `UTF8_STRING` first and falls back to `STRING` for
an owner too old to offer the first, decoding that as Latin-1 only when
its bytes are not valid UTF-8. Text longer than one X request is
transferred in chunks through INCR, in both directions. Both calls need
an open window, because a selection belongs to one.

Wayland works the same way underneath, through `wl_data_device`. The
compositor tells the window what types the selection holds and the text
comes over a pipe the owner writes into, so `Clipboard` waits about a
second for it. A compositor tells only the focused window what the
selection holds, so a read while another window has focus finds nothing.
A compositor with no `wl_data_device_manager` returns an error from both
calls.

`SetClipboard` under Wayland also returns an error until the window has
had some input. The compositor changes the selection only in answer to a
key, a button, or the pointer or keyboard arriving, and quotes that
event; a game that copies to the clipboard before the player has touched
anything has nothing to quote. In practice a copy always follows a key
press or a click, so this shows up only in a game that writes the
clipboard from `Init`.

Three things behave differently under Wayland. The title bar is the
compositor's: the layer asks for server-side decorations through
`zxdg_decoration_manager_v1`, and a compositor that does not offer that
protocol shows the window with no frame, because drawing one is the
client's job and this layer does not draw. `SetIcon` sends the pixels
through `xdg-toplevel-icon-v1` where the compositor has it and does
nothing where it does not, in which case the icon comes from the desktop
entry whose name matches the app id, which the layer derives from
`Config.Title`. `SetCursorCaptured` uses
`zwp_pointer_constraints_v1` and `zwp_relative_pointer_v1` where the
compositor has them, and where it does not the pointer is only hidden
and the deltas come from absolute motion, so it can leave the window.

Scale under Wayland is fractional where the compositor offers
`wp_fractional_scale_v1` and `wp_viewporter`. The compositor sends the
scale it prefers in 120ths, the buffer is sized to the logical size times
that scale, and `wp_viewport.set_destination` maps the buffer back onto
the logical size, so text rasterises at the display's real density
instead of at a whole multiple. Without those protocols the layer falls
back to an integer buffer scale, from
`wl_surface.preferred_buffer_scale` where the compositor sends it and
otherwise from the largest scale among the outputs the surface is on. A
change of scale arrives as a resize event, the same as a change of size.

## Lifecycle

`runOnce` builds the stack in one order and tears it down in the reverse:

1. The window opens (or the headless stand-in is made).
2. The renderer is created on the window's surface, at its pixel size.
3. Graphics, input and the mixer are built, and the audio device is
   opened unless `Config.NoAudio` or `Headless`.
4. With `Config.Console`, the console is built and the log is teed
   through it, so records from `Init` onwards reach it.
5. `Config.Icon` is applied and the view is placed in the window.
6. `Init` runs, if the game has one, then the loop.

After successful setup, the game's `Shutdown` runs first on exit, while the context is
still live and GPU resources can still be destroyed, then the debug
overlay and console, the audio device, the graphics context, the
renderer, and last the window.

If `Init` or `Recover` returns an error, `Shutdown` is not called. Clean
up partially created game resources before returning that error.

`Initer`, `Shutdowner` and `Recoverer` are all optional interfaces on the
game. When the graphics driver reports a lost device, `Run` tears the
whole stack down and builds it again, reusing the one connection to the
window system but opening a fresh window and renderer. A game that
implements `Recoverer` gets `Recover` with the new context instead of
`Init`; every texture, mesh, font and render texture it created is gone
and must be created again. The input state, mixer and console are also
new: restore bus settings, restart desired audio playback, and register
console commands again. Old voice handles belong to the discarded mixer.
The loop clock and frame count restart. A game without `Recover` gets the error back
from `Run` instead.

Full detail on every field and method named here is in the
[package documentation](../pkg/bunyip.html).
