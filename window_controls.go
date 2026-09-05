package bunyip

import (
	"errors"
	"image"
	"math"

	"github.com/matjam/bunyip/internal/platform"
)

// ErrUnsupported identifies an operation unavailable in the active backend,
// including native window operations during a headless run.
var ErrUnsupported = platform.ErrUnsupported

// WindowCapabilities describes native requests the active backend can issue.
// Desktop policy can still decline supported requests.
type WindowCapabilities struct {
	Resize, Show, Hide, Focus, AlwaysOnTop, CursorImage, PointerPosition bool
}

// Display is a snapshot of an attached display and its advertised modes.
type Display struct {
	Name        string          // system-provided description; not a persistent identifier
	Bounds      image.Rectangle // macOS: logical points; X11/Windows: desktop pixels
	BoundsKnown bool            // false where the backend cannot determine desktop bounds
	Scale       float64         // physical pixels per logical point, or zero when unknown
	Current     VideoMode
	Modes       []VideoMode // advertised modes; Wayland may expose only the current mode
}

// VideoMode describes physical pixel dimensions and a reported refresh rate.
type VideoMode struct {
	Width, Height int     // physical pixels
	RefreshHz     float64 // zero means the operating system did not report a rate
}

// WindowCapabilities reports available native window operations.
func (c *Context) WindowCapabilities() WindowCapabilities {
	if w, ok := c.win.(interface {
		Capabilities() platform.WindowCapabilities
	}); ok {
		return WindowCapabilities(w.Capabilities())
	}
	return WindowCapabilities{}
}

// SetSize requests a positive content size in logical window points. Resize
// events update the view later; the desktop can constrain the requested size.
// Call this and the other native controls on the game goroutine.
func (c *Context) SetSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return errors.New("bunyip: window size must be positive")
	}
	if w, ok := c.win.(interface{ SetSize(int, int) error }); ok {
		return w.SetSize(width, height)
	}
	return ErrUnsupported
}

// Show requests that the window be shown, without requesting keyboard focus.
// Native visibility changes arrive through the regular event loop.
// The current Wayland backend does not implement hide/remap coordination and
// returns ErrUnsupported, as does headless mode.
func (c *Context) Show() error {
	if w, ok := c.win.(interface{ Show() error }); ok {
		return w.Show()
	}
	return ErrUnsupported
}

// Hide requests that the window be hidden. Wake does not show a hidden window.
// Wayland and headless mode return ErrUnsupported.
func (c *Context) Hide() error {
	if w, ok := c.win.(interface{ Hide() error }); ok {
		return w.Hide()
	}
	return ErrUnsupported
}

// RequestFocus asks the desktop to focus the window. A successful request does
// not guarantee focus: use Focused to observe the desktop's decision.
// Wayland activation is not implemented and returns ErrUnsupported.
func (c *Context) RequestFocus() error {
	if w, ok := c.win.(interface{ RequestFocus() error }); ok {
		return w.RequestFocus()
	}
	return ErrUnsupported
}

// SetPointerPosition requests a pointer position in view coordinates. It uses
// the current viewport and window scale; input changes arrive on the next poll.
// Wayland forbids arbitrary pointer warping and returns ErrUnsupported.
func (c *Context) SetPointerPosition(x, y float32) error {
	if math.IsNaN(float64(x)) || math.IsNaN(float64(y)) || math.IsInf(float64(x), 0) || math.IsInf(float64(y), 0) {
		return errors.New("bunyip: pointer position must be finite")
	}
	if w, ok := c.win.(interface{ SetPointerPosition(float64, float64) error }); ok {
		if c.Width <= 0 || c.Height <= 0 || c.viewport.W <= 0 || c.viewport.H <= 0 || c.pixelsPerPoint <= 0 {
			return errors.New("bunyip: window has no drawable viewport")
		}
		pp := float64(c.pixelsPerPoint)
		return w.SetPointerPosition((float64(c.viewport.X)+float64(x)*float64(c.viewport.W)/float64(c.Width))/pp,
			(float64(c.viewport.Y)+float64(y)*float64(c.viewport.H)/float64(c.Height))/pp)
	}
	return ErrUnsupported
}

// Displays enumerates displays known to the active window system. It performs
// no display mode switch. Call on the game goroutine, as for window controls.
func (c *Context) Displays() ([]Display, error) {
	if a, ok := c.app.(interface {
		Displays() ([]platform.Display, error)
	}); ok {
		displays, err := a.Displays()
		if err != nil {
			return nil, err
		}
		out := make([]Display, len(displays))
		for i, d := range displays {
			out[i] = Display{Name: d.Name, Bounds: d.Bounds, BoundsKnown: d.BoundsKnown, Scale: d.Scale, Current: VideoMode(d.Current)}
			for _, m := range d.Modes {
				out[i].Modes = append(out[i].Modes, VideoMode(m))
			}
		}
		return out, nil
	}
	return nil, ErrUnsupported
}
