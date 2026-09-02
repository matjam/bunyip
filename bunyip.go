// Package bunyip is the engine's front door. A game implements Game, fills
// in a Config and calls Run; the engine owns the window, the event loop,
// the renderer and the frame pacing.
//
// Two loop modes serve two kinds of game. Real-time games get a fixed
// timestep: Update runs at Config.FixedStep regardless of frame rate and
// Draw runs once per frame. Turn-based games set TurnBased, and the loop
// then sleeps in the operating system until input arrives, running one
// Update and one Draw per batch of events; the process uses no CPU while
// the player thinks.
package bunyip

import (
	"log/slog"
	"time"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
)

// Config describes the window and the loop.
type Config struct {
	Title     string
	Width     int // window content size in points
	Height    int
	Resizable bool
	NoVSync   bool // present without waiting for the display; vsync is on by default

	TurnBased bool          // wait for input instead of running a clock
	FixedStep time.Duration // real-time update interval; default 1/60 s

	// ViewWidth and ViewHeight fix the game's view in view units: the 2D
	// coordinate space and the 3D viewport. The window scales that view by
	// Scaling and centres it, so a pixel-art game designs for 320 by 180
	// once. Zero means the view is the window's size in points and follows
	// it as the window resizes.
	ViewWidth, ViewHeight int
	Scaling               Scaling

	// Headless runs without a window or input: frames render offscreen at
	// Width by Height and Context.Screenshot still works, for tests and
	// screenshot runs on a build machine.
	Headless bool

	Validation bool // enable Vulkan validation when installed
	NoAudio    bool // skip opening the audio device
	Log        *slog.Logger

	// Debug shows the frame-timing overlay at start; F3 toggles it either
	// way. Pprof, when set to an address such as "localhost:6060", serves
	// Go's profiler there.
	Debug bool
	Pprof string

	recovering bool // set by Run when rebuilding after a device loss
}

// Scaling is how a fixed view fills the window.
type Scaling uint8

const (
	// ScaleFit is the largest size that fits with the aspect ratio kept;
	// black bars fill the rest.
	ScaleFit Scaling = iota
	// ScaleInteger scales by whole numbers only, so pixel art stays crisp,
	// falling back to ScaleFit when even one times does not fit.
	ScaleInteger
	// ScaleStretch fills the window, distorting the aspect ratio.
	ScaleStretch
)

// Game is what Run drives. Update advances the simulation and Draw queues
// drawing through ctx.Gfx. Optional interfaces Initer and Shutdowner add
// setup and teardown with a live context.
type Game interface {
	Update(ctx *Context) error
	Draw(ctx *Context) error
}

// Initer is implemented by games that need the graphics context to load
// resources before the first update.
type Initer interface {
	Init(ctx *Context) error
}

// Shutdowner is implemented by games that free resources on exit.
type Shutdowner interface {
	Shutdown(ctx *Context)
}

// Recoverer is implemented by games that can survive a lost GPU device.
// When the driver reports a device loss, Run rebuilds the graphics stack
// and calls Recover with the new context; every texture, mesh, font and
// render texture the game created is gone and must be created again.
// Games without Recover get the error from Run instead.
type Recoverer interface {
	Recover(ctx *Context) error
}

// Context is everything a game touches during a callback.
type Context struct {
	Gfx   *gfx.Graphics
	Input *input.State
	Log   *slog.Logger

	// Audio always exists; without an output device it mixes into silence.
	Audio *audio.Mixer

	// Clear is the frame's background colour; set it whenever you like.
	Clear gfx.Color

	// Width and Height are the view size in points; Scale is pixels per point.
	Width, Height float32
	Scale         float32

	// Delta is the seconds this Update covers (the fixed step in real-time
	// mode, wall-clock time since the previous update in turn-based mode).
	// Time is seconds since Run started. Frame counts drawn frames.
	Delta float64
	Time  float64
	Frame uint64
	// Alpha, during Draw, is how far the clock has run past the last
	// Update as a fraction of a fixed step, 0 to 1. Drawing a body at
	// previous + (current - previous) * Alpha moves it smoothly when the
	// display runs faster than the update rate. It is 1 in turn-based mode.
	Alpha float32

	// Stats holds the previous frame's timings.
	Stats Stats

	scopes []Scope
	app    waker
	quit   bool
	redraw bool
	shot   string
	win    windowControl
}

// waker is what the context needs from the platform app.
type waker interface {
	Wake()
}

// Wake makes a turn-based game run an Update and Draw even though no
// input arrived. It is safe to call from any goroutine, so a timer, a
// network reply or a finished asset load can prod the game while it
// sleeps waiting for the player.
func (c *Context) Wake() {
	if c.app != nil {
		c.app.Wake()
	}
}

// windowControl is what the context needs from the platform window.
type windowControl interface {
	Fullscreen() bool
	SetFullscreen(bool)
	SetCursorCaptured(bool)
	CursorCaptured() bool
	SetTextInputRect(x, y, w, h float64)
}

// SetTextInputRect tells the operating system's input method where text
// is being entered, in view units from the top-left, so that candidate
// windows for languages such as Japanese open beside the field. Text
// fields in the ui package call it for you.
func (c *Context) SetTextInputRect(x, y, w, h float32) {
	c.win.SetTextInputRect(float64(x), float64(y), float64(w), float64(h))
}

// SetFullscreen enters or leaves full-screen mode.
func (c *Context) SetFullscreen(on bool) { c.win.SetFullscreen(on) }

// Fullscreen reports whether the window is full screen.
func (c *Context) Fullscreen() bool { return c.win.Fullscreen() }

// SetCursorCaptured hides the cursor and delivers relative motion only,
// through Input.MouseDelta.
func (c *Context) SetCursorCaptured(on bool) { c.win.SetCursorCaptured(on) }

// CursorCaptured reports the capture state.
func (c *Context) CursorCaptured() bool { return c.win.CursorCaptured() }

// Quit ends the loop after the current callback.
func (c *Context) Quit() { c.quit = true }

// RequestRedraw asks a turn-based loop to draw again without waiting for
// input, for animations that span turns. Real-time loops always redraw.
func (c *Context) RequestRedraw() { c.redraw = true }

// Screenshot writes the next drawn frame to a PNG at path.
func (c *Context) Screenshot(path string) { c.shot = path }
