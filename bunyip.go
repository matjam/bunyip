// Package bunyip runs a game's main loop. To start a game, implement
// Game, fill in a Config and call Run. Run owns the window, the event
// loop, the renderer, the audio device and the frame pacing, and passes
// a Context to each call with the input state, the Graphics to draw
// with, the audio Mixer, the clock and the window controls.
//
// # The loop
//
// There are two loop modes. Real-time games use a fixed timestep:
// Update runs at Config.FixedStep regardless of frame rate, Draw runs
// once per frame, and Context.Alpha reports how far the next update is
// so drawing can interpolate. Turn-based games set TurnBased. The loop
// then sleeps in the operating system until input arrives, or until
// Context.Wake is called from another goroutine, and runs one Update
// and one Draw per batch of events. The process uses no CPU between
// batches.
//
// # The view
//
// Config.ViewWidth and ViewHeight fix the game's coordinate space, and
// the window scales it by Config.Scaling (fit with letterboxing, whole
// multiples for pixel art, or stretch). Without them the view follows
// the window's size in points. Config.Headless runs the same loop
// without a window, for tests and screenshot runs. Context.Screenshot
// saves any frame.
//
// # Errors and exit
//
// Returning an error from Init, Update or Draw stops the loop, and Run
// returns it. Context.Quit stops it cleanly. With Config.HandleClose the
// window's close button sets Context.CloseRequested instead of quitting,
// so a game can save or prompt first. A lost graphics device is rebuilt
// and Init runs again, so Init must be safe to run twice.
package bunyip

import (
	"image"
	"log/slog"
	"time"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/platform"
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
	// MaxCatchUp caps how much lost time the loop makes up with extra
	// updates after a stall (a window drag, a long load), so a game does
	// not spiral into ever more updates per frame; the rest is dropped.
	// Zero means a quarter of a second. MaxSteps caps the updates in one
	// frame the same way; zero means no cap beyond MaxCatchUp.
	MaxCatchUp time.Duration
	MaxSteps   int
	// PauseUnfocused stops updates and silences the mixer while the
	// window does not have focus, so a game does not play on behind
	// another window; frames still draw. Off by default: a server, a
	// music player or a game with real-time multiplayer keeps running.
	PauseUnfocused bool

	// DrawBudget is the number of draw calls (2D and 3D together) a
	// frame should stay under; the debug overlay warns when a frame goes
	// over, so a batching regression is noticed. Zero means no warning.
	DrawBudget int
	// LogFile appends the engine's log (and a panic's stack trace) to a
	// file when Log is nil, for crash reports from players' machines;
	// empty logs to the terminal.
	LogFile string

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

	// Icon is the window's or application's icon; nil keeps the default.
	// HandleClose leaves the window open when the user asks to close it
	// and sets Context.CloseRequested instead, so the game can save or
	// ask first and then Quit.
	Icon        image.Image
	HandleClose bool

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

	// Width and Height are the view's size and Scale is pixels per point.
	// With Config.ViewWidth and ViewHeight set, Width and Height are that
	// fixed view in view units and stay put as the window resizes, and
	// Scale is the pixels a view unit covers; without them the view is the
	// window's content size in points and follows it.
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

	scopes   []Scope
	app      clipboardWaker
	quit     bool
	redraw   bool
	shot     string
	win      windowControl
	focused  bool
	closeReq bool
	cursor   Cursor
}

// waker is what the context needs from the platform app.
type waker interface {
	Wake()
}

// clipboardWaker adds the clipboard to what the context needs.
type clipboardWaker interface {
	waker
	Clipboard() (string, error)
	SetClipboard(string) error
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
	SetTitle(string)
	SetSizeLimits(minW, minH, maxW, maxH int)
	SetCursorVisible(bool)
	SetCursor(platform.CursorShape)
	SetIcon(image.Image)
	SetPosition(x, y int)
	Position() (x, y int)
	SetAlwaysOnTop(bool)
	SetCursorImage(img image.Image, hotX, hotY int)
}

// Window controls. Every method from here to Screenshot must be called
// from Update or Draw, on the goroutine that called Run; Wake above is
// the exception and is safe from any goroutine.

// SetPosition moves the window so its content's top-left corner sits at
// a point on the screen, in points from the screen's top-left: a
// remembered position from a settings file, a tool window beside the
// main one. macOS today; the other platforms ignore it.
func (c *Context) SetPosition(x, y int) { c.win.SetPosition(x, y) }

// Position returns the window content's top-left corner on the screen,
// in points from the screen's top-left, for saving with the settings.
func (c *Context) Position() (x, y int) { return c.win.Position() }

// SetAlwaysOnTop keeps the window above other applications' windows, for
// an overlay or a companion tool. macOS today; the other platforms
// ignore it.
func (c *Context) SetAlwaysOnTop(on bool) { c.win.SetAlwaysOnTop(on) }

// SetCursorImage replaces the pointer with an image, its hot spot at
// (hotX, hotY) pixels from the image's top-left: a crosshair, a hand, a
// sword. SetCursor with a shape puts the system pointer back. macOS
// today; the other platforms keep the system pointer.
func (c *Context) SetCursorImage(img image.Image, hotX, hotY int) {
	c.win.SetCursorImage(img, hotX, hotY)
}

// Cursor is a pointer shape for SetCursor.
type Cursor uint8

const (
	CursorArrow      Cursor = Cursor(platform.CursorArrow)
	CursorHand       Cursor = Cursor(platform.CursorHand)
	CursorIBeam      Cursor = Cursor(platform.CursorIBeam)
	CursorCrosshair  Cursor = Cursor(platform.CursorCrosshair)
	CursorResizeH    Cursor = Cursor(platform.CursorResizeH)
	CursorResizeV    Cursor = Cursor(platform.CursorResizeV)
	CursorGrab       Cursor = Cursor(platform.CursorGrab)
	CursorGrabbing   Cursor = Cursor(platform.CursorGrabbing)
	CursorNotAllowed Cursor = Cursor(platform.CursorNotAllowed)
)

// SetTitle changes the window's title.
func (c *Context) SetTitle(title string) { c.win.SetTitle(title) }

// SetIcon sets the window's or application's icon from an image; 256
// pixels square is a good size.
func (c *Context) SetIcon(img image.Image) { c.win.SetIcon(img) }

// SetSizeLimits bounds the window's content size in points; zero lifts a
// bound.
func (c *Context) SetSizeLimits(minW, minH, maxW, maxH int) {
	c.win.SetSizeLimits(minW, minH, maxW, maxH)
}

// SetCursorVisible shows or hides the pointer over the window.
func (c *Context) SetCursorVisible(on bool) { c.win.SetCursorVisible(on) }

// SetCursor picks the pointer's shape over the window.
func (c *Context) SetCursor(shape Cursor) {
	c.cursor = shape
	c.win.SetCursor(platform.CursorShape(shape))
}

// Focused reports whether the window has keyboard focus.
func (c *Context) Focused() bool { return c.focused }

// CloseRequested reports that the user asked to close the window since
// the last Update. With Config.HandleClose the loop keeps running and the
// game decides: save, ask, then Quit. Without it the loop quits on its
// own and this is never true.
func (c *Context) CloseRequested() bool { return c.closeReq }

// Clipboard returns the system clipboard's text, empty when it holds
// none. Linux under X11 has no clipboard yet and returns an error.
func (c *Context) Clipboard() (string, error) { return c.app.Clipboard() }

// SetClipboard puts text on the system clipboard.
func (c *Context) SetClipboard(text string) error { return c.app.SetClipboard(text) }

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
