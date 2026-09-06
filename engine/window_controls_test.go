package engine

import (
	"errors"
	"math"
	"testing"

	"github.com/matjam/bunyip/internal/platform"
	"github.com/matjam/bunyip/lin"
)

type controlsWindow struct {
	window
	position                                 [2]float64
	size                                     [2]int
	shown, hidden, focused, refreshed, reset int
	err                                      error
}

func (w *controlsWindow) SetSize(x, y int) error { w.size = [2]int{x, y}; return w.err }
func (w *controlsWindow) SetPointerPosition(x, y float64) error {
	w.position = [2]float64{x, y}
	return w.err
}
func (w *controlsWindow) Show() error                    { w.shown++; return w.err }
func (w *controlsWindow) Hide() error                    { w.hidden++; return w.err }
func (w *controlsWindow) RequestFocus() error            { w.focused++; return w.err }
func (w *controlsWindow) RefreshCursor()                 { w.refreshed++ }
func (w *controlsWindow) SetCursor(platform.CursorShape) { w.reset++ }
func (w *controlsWindow) Capabilities() platform.WindowCapabilities {
	return platform.WindowCapabilities{Resize: true, PointerPosition: true}
}

func TestWindowControlsDispatchAndUnsupported(t *testing.T) {
	w := &controlsWindow{}
	c := &Context{win: w}
	if err := c.SetSize(640, 480); err != nil || w.size != [2]int{640, 480} {
		t.Fatalf("resize = %v, %v", w.size, err)
	}
	if err := c.SetSize(0, 480); err == nil {
		t.Fatal("zero size accepted")
	}
	for _, f := range []func() error{c.Show, c.Hide, c.RequestFocus} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	if w.shown != 1 || w.hidden != 1 || w.focused != 1 {
		t.Fatal("control requests not forwarded")
	}
	sentinel := errors.New("desktop declined")
	w.err = sentinel
	if !errors.Is(c.RequestFocus(), sentinel) {
		t.Fatal("backend error lost")
	}
	if !c.WindowCapabilities().Resize {
		t.Fatal("capabilities lost")
	}
	c = &Context{win: &headlessWindow{}}
	for _, f := range []func() error{c.Show, c.Hide, c.RequestFocus, func() error { return c.SetSize(640, 480) }, func() error { return c.SetPointerPosition(0, 0) }, func() error { return c.SetAlwaysOnTop(true) }} {
		if !errors.Is(f(), ErrUnsupported) {
			t.Fatal("headless request did not return unsupported")
		}
	}
	if _, err := c.Displays(); !errors.Is(err, ErrUnsupported) {
		t.Fatal("headless displays fabricated")
	}
}

func TestPointerPositionViewMapping(t *testing.T) {
	w := &controlsWindow{}
	c := &Context{win: w, Width: 640, Height: 360, viewport: lin.Rect{X: 120, Y: 60, W: 1280, H: 720}, pixelsPerPoint: 2}
	if err := c.SetPointerPosition(100, 200); err != nil {
		t.Fatal(err)
	}
	if w.position != [2]float64{160, 230} {
		t.Fatalf("letterboxed HiDPI point = %v", w.position)
	}
	if err := c.SetPointerPosition(float32(math.NaN()), 1); err == nil {
		t.Fatal("NaN accepted")
	}
	c.viewport.W = 0
	if err := c.SetPointerPosition(1, 1); err == nil {
		t.Fatal("empty viewport accepted")
	}
}

func TestPointerEnterRefreshesCustomCursor(t *testing.T) {
	w := &controlsWindow{}
	l := &loop{win: w, ctx: &Context{cursor: CursorHand}}
	l.handleEvents([]platform.Event{{Kind: platform.EventMouseEnter}})
	if w.refreshed != 1 || w.reset != 0 {
		t.Fatalf("enter refreshed %d times, replaced cursor %d times", w.refreshed, w.reset)
	}
}
