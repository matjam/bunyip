package platform

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Linux has two window systems in use, so the layer has two backends and
// picks one at startup. Wayland is tried first, through libwayland-client in
// wayland_linux.go, and X11 second, through libxcb in x11_linux.go. Set
// BUNYIP_X11=1 to force X11, which is how an XWayland run is compared
// against a native one.
//
// App and Window carry the state of both backends. Every method below is a
// two-line switch on which one is live, so that the rest of the engine sees
// one type.

func init() {
	// The connection is used from one thread; Wake goes through a pipe under
	// Wayland and through xcb, which is thread-safe, under X11.
	runtime.LockOSThread()
}

// ErrUnsupported is returned when neither window system can be reached.
var ErrUnsupported = errors.New("platform: cannot reach a Wayland compositor or an X server (is WAYLAND_DISPLAY or DISPLAY set?)")

// App is the process's connection to the window system. Create one per
// process.
type App struct {
	wl *wlApp // nil under X11

	pending []Event
	mu      sync.Mutex

	// X11.
	x       *xlib
	conn    unsafe.Pointer
	screen  *xcbScreen
	windows map[uint32]*Window
	wakeWin atomic.Uint32 // the window Wake targets; read off the main goroutine
	mods    Mods

	atomWMProtocols, atomWMDelete, atomNetWMName, atomUTF8, atomNetWMState, atomNetWMFullscreen, atomWake uint32
	atomNetWMHidden                                                                                      uint32

	xkbCtx, xkbKeymap, xkbState unsafe.Pointer
	xkbDevice                   int32

	// Key repeat. keysDown is indexed by X11 key code, which is one byte,
	// and says whether a press is the first or a repeat. detectableRepeat
	// says the server agreed to send repeats as presses alone; peeked
	// holds the event the fallback took off the queue to look at.
	keysDown         [256]bool
	detectableRepeat bool
	peeked           *xcbGenericEvent
}

// Window is one window. Only the fields of the live backend are set.
type Window struct {
	app *App
	wl  *wlWindow // nil under X11

	// X11.
	id        uint32
	width     int
	height    int
	closed    bool
	captured  bool
	fullscr   bool
	mapped    bool
	obscured  bool
	wmHidden  bool // _NET_WM_STATE_HIDDEN: the window manager minimised it
	visible   bool
	cursor    uint32 // the invisible cursor while captured
	inputRect textRect
	mouseX    float64
	mouseY    float64

	minW, minH, maxW, maxH int // content size limits; zero is none
	cursorHidden           bool
	shape                  CursorShape
	shapeCursor            uint32 // the glyph cursor in force, or 0
	blankCursor            uint32 // the invisible cursor while hidden, or 0
}

type textRect struct{ X, Y, W, H float64 }

// NewApp connects to the window system, Wayland first and X11 second.
func NewApp() (*App, error) {
	a := &App{}
	var wlErr error
	if os.Getenv("BUNYIP_X11") == "" && waylandAvailable() {
		wl, err := newWaylandApp(a)
		if err == nil {
			a.wl = wl
			backendName = "wayland"
			return a, nil
		}
		wlErr = err
	}
	if err := a.connectX11(); err != nil {
		if wlErr != nil {
			return nil, fmt.Errorf("%w (wayland: %v; x11: %v)", ErrUnsupported, wlErr, err)
		}
		return nil, err
	}
	backendName = "x11"
	return a, nil
}

// NewWindow opens a window and shows it.
func (a *App) NewWindow(cfg Config) (*Window, error) {
	if a.wl != nil {
		w := &Window{app: a}
		wl, err := a.wl.newWindow(w, cfg)
		if err != nil {
			return nil, err
		}
		w.wl = wl
		return w, nil
	}
	return a.newX11Window(cfg)
}

// Poll drains pending events into the returned slice, reused by the next
// call. With wait set it blocks until at least one event arrives.
func (a *App) Poll(wait bool) []Event {
	if a.wl != nil {
		return a.wl.poll(wait)
	}
	return a.x11Poll(wait)
}

// Wake makes a blocked Poll return, delivering an EventWake. It is safe to
// call from any goroutine.
func (a *App) Wake() {
	if a.wl != nil {
		a.wl.wake()
		return
	}
	a.x11Wake()
}

// Size is the content size in points.
func (w *Window) Size() (int, int) {
	if w.wl != nil {
		return w.wl.width, w.wl.height
	}
	return w.width, w.height
}

// PixelSize is the framebuffer size. X11 reports no scale, so it matches
// Size there; Wayland multiplies by the buffer scale.
func (w *Window) PixelSize() (int, int) {
	if w.wl != nil {
		return w.wl.width * w.wl.scale, w.wl.height * w.wl.scale
	}
	return w.width, w.height
}

// Scale is pixels per point.
func (w *Window) Scale() float64 {
	if w.wl != nil {
		return float64(w.wl.scale)
	}
	return 1
}

// Visible reports whether the window can be seen. It is false while the
// window is minimised, unmapped or wholly covered by other windows. X11
// reads MapNotify, VisibilityNotify and _NET_WM_STATE_HIDDEN; Wayland
// reads the suspended state of xdg_toplevel version six, so a compositor
// that offers an earlier version always reports true.
func (w *Window) Visible() bool {
	if w.wl != nil {
		return w.wl.visible
	}
	return w.visible
}

// Closed reports whether the window was destroyed.
func (w *Window) Closed() bool {
	if w.wl != nil {
		return w.wl.closed
	}
	return w.closed
}

// Close destroys the window.
func (w *Window) Close() {
	if w.wl != nil {
		if !w.wl.closed {
			w.wl.destroy()
		}
		return
	}
	w.closeX11()
}

// Fullscreen reports whether the window is full screen.
func (w *Window) Fullscreen() bool {
	if w.wl != nil {
		return w.wl.fullscreen
	}
	return w.fullscr
}

// SetFullscreen asks the compositor or the window manager for full screen.
// Under Wayland the answer arrives as a configure, so Fullscreen only
// changes once the compositor agrees.
func (w *Window) SetFullscreen(on bool) {
	if w.wl != nil {
		w.wl.setFullscreen(on)
		return
	}
	w.setFullscreenX11(on)
}

// SetCursorCaptured hides the cursor, holds it in the window and reports
// motion as deltas.
func (w *Window) SetCursorCaptured(on bool) {
	if w.wl != nil {
		w.wl.setCaptured(on)
		return
	}
	w.setCursorCapturedX11(on)
}

// CursorCaptured reports the capture state.
func (w *Window) CursorCaptured() bool {
	if w.wl != nil {
		return w.wl.captured
	}
	return w.captured
}

// SetTextInputRect records where text is entered. Neither backend wires an
// input method yet, so it only stores the rectangle.
func (w *Window) SetTextInputRect(x, y, width, height float64) {
	if w.wl != nil {
		w.wl.inputRect = textRect{X: x, Y: y, W: width, H: height}
		return
	}
	w.inputRect = textRect{X: x, Y: y, W: width, H: height}
}
