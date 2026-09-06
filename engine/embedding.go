package engine

import (
	"errors"
	"github.com/matjam/bunyip/internal/platform"
)

// NativeBackend identifies the native type of a borrowed embedding parent.
type NativeBackend uint8

const (
	NativeWin32 NativeBackend = iota + 1 // HWND in the current process and UI thread
	NativeCocoa                          // NSView attached to an NSWindow on the main thread
	NativeX11                            // Window XID on the engine's X server and screen
)

// NativeParent is borrowed native content into which Bunyip inserts an owned
// rendering child. Keep it alive until Run returns or the additional Window
// reports Closed. Bunyip never destroys the parent or replaces its delegate.
// Run owns scheduling and native event dispatch on the host UI thread; this
// is not an adapter for toolkits that require their own event loop.
type NativeParent struct {
	Backend NativeBackend
	Handle  uintptr
}

func (p *NativeParent) platform() (*platform.NativeParent, error) {
	if p == nil {
		return nil, nil
	}
	if p.Handle == 0 || p.Backend < NativeWin32 || p.Backend > NativeX11 {
		return nil, errors.New("bunyip: invalid native parent")
	}
	return &platform.NativeParent{Backend: uint8(p.Backend), Handle: p.Handle}, nil
}

// SetBounds places embedded content in its parent's logical points, with a
// top-left origin (X11 uses pixels). It disables automatic parent fitting.
// Width and height must be positive. Top-level and headless outputs return
// ErrUnsupported. Call on the game goroutine.
func (c *Context) SetBounds(x, y, width, height int) error {
	if width <= 0 || height <= 0 {
		return errors.New("bunyip: embedded bounds must have positive size")
	}
	if w, ok := c.win.(interface {
		SetBounds(int, int, int, int) error
	}); ok {
		if err := w.SetBounds(x, y, width, height); err != nil {
			return err
		}
		// AppKit and Win32 can resize synchronously, before the next poll clears
		// its event buffer. X11's later ConfigureNotify completes its request.
		if c.owner != nil {
			c.owner.loop.applySize()
		}
		return nil
	}
	return ErrUnsupported
}
