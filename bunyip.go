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
	VSync     bool // default true; set NoVSync to disable
	NoVSync   bool

	TurnBased bool          // wait for input instead of running a clock
	FixedStep time.Duration // real-time update interval; default 1/60 s

	Validation bool // enable Vulkan validation when installed
	NoAudio    bool // skip opening the audio device
	Log        *slog.Logger

	recovering bool // set by Run when rebuilding after a device loss
}

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

	quit   bool
	redraw bool
	shot   string
	win    windowControl
}

// windowControl is what the context needs from the platform window.
type windowControl interface {
	Fullscreen() bool
	SetFullscreen(bool)
	SetCursorCaptured(bool)
	CursorCaptured() bool
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
