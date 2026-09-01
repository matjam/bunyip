package platform

import (
	"fmt"

	"github.com/ebitengine/purego/objc"
)

// Window is one NSWindow with a CAMetalLayer-backed content view.
type Window struct {
	app      *App
	nsWindow objc.ID
	view     objc.ID
	layer    objc.ID
	delegate objc.ID
	width    int
	height   int
	pixelW   int
	pixelH   int
	scale    float64
	closed   bool
	captured bool

	marked    string   // the input method's uncommitted text
	inputRect textRect // where the game is taking text
}

// NewWindow opens a window and shows it.
func (a *App) NewWindow(cfg Config) (*Window, error) {
	c := a.c
	style := uint64(nsWindowStyleMaskTitled | nsWindowStyleMaskClosable | nsWindowStyleMaskMiniaturizable)
	if cfg.Resizable {
		style |= nsWindowStyleMaskResizable
	}
	rect := nsRect{Size: nsSize{Width: float64(cfg.Width), Height: float64(cfg.Height)}}
	win := objc.ID(c.NSWindow).Send(c.sel.alloc).Send(c.sel.initWithContentRect, rect, style, nsBackingStoreBuffered, false)
	if win == 0 {
		return nil, fmt.Errorf("platform: NSWindow init failed")
	}
	w := &Window{app: a, nsWindow: win}
	win.Send(c.sel.setTitle, c.nsString(cfg.Title))
	win.Send(c.sel.setRestorable, false)
	win.Send(c.sel.setAcceptsMouseMovedEvents, true)
	win.Send(c.sel.setCollectionBehavior, uint64(nsWindowCollectionBehaviorFullScreenPrimary))

	w.view = objc.ID(a.view).Send(c.sel.alloc).Send(c.sel.initWithFrame, rect)
	w.layer = objc.ID(c.CAMetalLayer).Send(c.sel.layer)
	w.view.Send(c.sel.setLayer, w.layer)
	w.view.Send(c.sel.setWantsLayer, true)
	win.Send(c.sel.setContentView, w.view)
	win.Send(a.tsel.makeFirstResponder, w.view) // key presses go to the view's text-input methods

	w.delegate = objc.ID(a.delegate).Send(c.sel.new)
	win.Send(c.sel.setDelegate, w.delegate)
	a.windows[win] = w

	win.Send(c.sel.center)
	win.Send(c.sel.makeKeyAndOrderFront, objc.ID(0))
	w.updateGeometry()
	return w, nil
}

// updateGeometry reads the content size and backing scale, pushes them into
// the Metal layer so MoltenVK sees the new drawable size, and reports them.
func (w *Window) updateGeometry() {
	c := w.app.c
	bounds := objc.Send[nsRect](w.view, c.sel.bounds)
	scale := objc.Send[float64](w.nsWindow, c.sel.backingScaleFactor)
	w.width, w.height = int(bounds.Size.Width), int(bounds.Size.Height)
	w.scale = scale
	w.pixelW = int(bounds.Size.Width*scale + 0.5)
	w.pixelH = int(bounds.Size.Height*scale + 0.5)
	w.layer.Send(c.sel.setContentsScale, scale)
	w.layer.Send(c.sel.setDrawableSize, nsSize{Width: float64(w.pixelW), Height: float64(w.pixelH)})
	w.app.push(Event{Kind: EventResize, Window: w, Width: w.width, Height: w.height,
		PixelW: w.pixelW, PixelH: w.pixelH, Scale: scale})
}

// Size is the content area in points.
func (w *Window) Size() (int, int) { return w.width, w.height }

// PixelSize is the framebuffer size in pixels.
func (w *Window) PixelSize() (int, int) { return w.pixelW, w.pixelH }

// Scale is pixels per point.
func (w *Window) Scale() float64 { return w.scale }

// Closed reports whether Close has run.
func (w *Window) Closed() bool { return w.closed }

// Close destroys the window. Events for it stop after this.
func (w *Window) Close() {
	if w.closed {
		return
	}
	w.closed = true
	w.SetCursorCaptured(false)
	c := w.app.c
	delete(w.app.windows, w.nsWindow)
	w.nsWindow.Send(c.sel.setDelegate, objc.ID(0))
	w.nsWindow.Send(c.sel.close)
	w.delegate.Send(c.sel.release)
}

// Fullscreen reports whether the window occupies a full-screen space.
func (w *Window) Fullscreen() bool {
	return objc.Send[uint64](w.nsWindow, w.app.c.sel.styleMask)&nsWindowStyleMaskFullScreen != 0
}

// SetFullscreen enters or leaves macOS full-screen mode. The change is
// animated by the system; a resize event follows when it completes.
func (w *Window) SetFullscreen(on bool) {
	if w.Fullscreen() != on {
		w.nsWindow.Send(w.app.c.sel.toggleFullScreen, objc.ID(0))
	}
}

// SetCursorCaptured hides the cursor and stops it moving, so that mouse
// events carry only relative motion; games use it for first-person looks.
func (w *Window) SetCursorCaptured(on bool) {
	if w.captured == on {
		return
	}
	w.captured = on
	c := w.app.c
	if on {
		objc.ID(c.NSCursor).Send(c.sel.hide)
	} else {
		objc.ID(c.NSCursor).Send(c.sel.unhide)
	}
	if c.associateCursor != nil {
		c.associateCursor(!on)
	}
}

// CursorCaptured reports the capture state.
func (w *Window) CursorCaptured() bool { return w.captured }

// MetalLayer is the CAMetalLayer the Vulkan surface is created over.
func (w *Window) MetalLayer() objc.ID { return w.layer }
