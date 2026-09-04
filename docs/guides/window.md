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
| `Headless`, `NoVSync`, `HandleClose`, `PauseUnfocused` | `SetPosition`, `Position`, `SetAlwaysOnTop` |

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

Underneath, the swapchain and the render targets are sized in pixels from
the window's framebuffer size, so a fixed view still renders at the
display's full resolution and text stays sharp.

## What the game can observe

The loop turns window events into a few things a game reads.

- `ctx.Focused` reports keyboard focus. `Config.PauseUnfocused` stops
  updates and silences the mixer while another window has focus; frames
  still draw.
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
by `Config.Height`, `ctx.Scale` is 1, the size never changes, and no
events arrive, so no key is ever pressed and the window never asks to
close. The window controls still exist and do nothing. `ctx.Screenshot`
works. Every example takes `-seconds N` and `-shot file.png`, and the
test that runs them all runs headless on a machine with no display.

```
BUNYIP_HEADLESS=1 go run ./examples/tetris -seconds 2 -shot /tmp/t.png
```

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
| `SetIcon` | yes | yes | no-op | yes |
| `SetSizeLimits` | yes | yes | yes | yes |
| `SetCursor`, `SetCursorVisible` | yes | yes | yes | yes |
| `SetCursorCaptured` | yes | yes | yes | yes |
| `SetFullscreen`, `Fullscreen` | yes | yes | yes | yes |
| `SetTextInputRect` | yes | recorded, no IME yet | recorded, no IME yet | recorded, no IME yet |
| `SetPosition`, `Position` | yes | no-op | no-op | no-op |
| `SetAlwaysOnTop` | yes | no-op | no-op | no-op |
| `SetCursorImage` | yes | no-op | no-op | no-op |
| `Clipboard`, `SetClipboard` | yes | yes | error | error |
| DPI scale (`ctx.Scale`) | the display's factor | the window's DPI over 96 | the output's integer scale | always 1 |

Where a control is a no-op, the call returns and nothing happens.
`Position` returns (0, 0) where it is not implemented.

Three things behave differently under Wayland. The title bar is the
compositor's: the layer asks for server-side decorations through
`zxdg_decoration_manager_v1`, and a compositor that does not offer that
protocol shows the window with no frame, because drawing one is the
client's job and this layer does not draw. `SetIcon` does nothing,
because the protocol has no request for it; the icon comes from the
desktop entry whose name matches the app id, which the layer derives
from `Config.Title`. `SetCursorCaptured` uses
`zwp_pointer_constraints_v1` and `zwp_relative_pointer_v1` where the
compositor has them, and where it does not the pointer is only hidden
and the deltas come from absolute motion, so it can leave the window.

Scale under Wayland is an integer buffer scale. The layer follows
`wl_surface.preferred_buffer_scale` where the compositor sends it and
falls back to the largest scale among the outputs the surface is on. A
change of scale arrives as a resize event, the same as a change of size.

## Lifecycle

`runOnce` builds the stack in one order and tears it down in the reverse:

1. The window opens (or the headless stand-in is made).
2. The renderer is created on the window's surface, at its pixel size.
3. Graphics, input and the mixer are built, and the audio device is
   opened unless `Config.NoAudio` or `Headless`.
4. `Config.Icon` is applied and the view is placed in the window.
5. With `Config.Console`, the console is built and the log is teed
   through it, so records from `Init` onwards reach it.
6. `Init` runs, if the game has one, then the loop.

On the way out the game's `Shutdown` runs first, while the context is
still live and GPU resources can still be destroyed, then the debug
overlay and console, the audio device, the graphics context, the
renderer, and last the window.

`Initer`, `Shutdowner` and `Recoverer` are all optional interfaces on the
game. When the graphics driver reports a lost device, `Run` tears the
whole stack down and builds it again, reusing the one connection to the
window system but opening a fresh window and renderer. A game that
implements `Recoverer` gets `Recover` with the new context instead of
`Init`; every texture, mesh, font and render texture it created is gone
and must be created again. A game without `Recover` gets the error back
from `Run` instead.

Full detail on every field and method named here is in the
[package documentation](../pkg/bunyip.html).
