package bunyip

import (
	"image"
	"time"

	"github.com/matjam/bunyip/internal/platform"
)

// window is what the loop needs from a platform window. A headless run
// supplies a stand-in of a fixed size that never closes on its own.
type window interface {
	windowControl
	Size() (int, int)
	PixelSize() (int, int)
	Scale() float64
	Closed() bool
	Close()
}

// eventSource is what the loop needs from the platform app.
type eventSource interface {
	waker
	Poll(wait bool) []platform.Event
	Gamepads() []platform.GamepadState
	Clipboard() (string, error)
	SetClipboard(string) error
}

// headlessWindow is a window without a window: a size and a few flags.
type headlessWindow struct {
	w, h     int
	closed   bool
	full     bool
	captured bool
}

func (w *headlessWindow) Size() (int, int)                     { return w.w, w.h }
func (w *headlessWindow) PixelSize() (int, int)                { return w.w, w.h }
func (w *headlessWindow) Scale() float64                       { return 1 }
func (w *headlessWindow) Closed() bool                         { return w.closed }
func (w *headlessWindow) Close()                               { w.closed = true }
func (w *headlessWindow) Fullscreen() bool                     { return w.full }
func (w *headlessWindow) SetFullscreen(on bool)                { w.full = on }
func (w *headlessWindow) CursorCaptured() bool                 { return w.captured }
func (w *headlessWindow) SetCursorCaptured(on bool)            { w.captured = on }
func (w *headlessWindow) SetTextInputRect(_, _, _, _ float64)  {}
func (w *headlessWindow) SetTitle(string)                      {}
func (w *headlessWindow) SetSizeLimits(_, _, _, _ int)         {}
func (w *headlessWindow) SetCursorVisible(bool)                {}
func (w *headlessWindow) SetCursor(platform.CursorShape)       {}
func (w *headlessWindow) SetIcon(image.Image)                  {}
func (w *headlessWindow) SetPosition(x, y int)                 {}
func (w *headlessWindow) Position() (int, int)                 { return 0, 0 }
func (w *headlessWindow) SetAlwaysOnTop(bool)                  {}
func (w *headlessWindow) SetCursorImage(image.Image, int, int) {}

// headlessApp delivers no events; a waiting poll sleeps one step so a
// turn-based headless game still ticks.
type headlessApp struct {
	step time.Duration
	clip string
}

func (a *headlessApp) Poll(wait bool) []platform.Event {
	if wait {
		time.Sleep(a.step)
	}
	return nil
}
func (a *headlessApp) Gamepads() []platform.GamepadState { return nil }
func (a *headlessApp) Wake()                             {}
func (a *headlessApp) Clipboard() (string, error)        { return a.clip, nil }
func (a *headlessApp) SetClipboard(s string) error       { a.clip = s; return nil }
