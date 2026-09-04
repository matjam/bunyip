package input_test

import (
	"testing"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/hook"
)

// An edge fed before a frame that runs no update is seen by that frame's
// Draw once, by the next update once, and by no later frame.
func TestEdgesSeenOncePerFrameWithoutUpdate(t *testing.T) {
	d := hook.NewInput()
	s := d.Game().(*input.State)
	d.FeedKey(uint8(input.KeyF3), true, false, 0)
	d.FeedChar('a')
	d.FeedScroll(0, 1)
	see := func() (bool, string, float32) {
		d.SetDrawing(true)
		defer func() { d.SetDrawing(false); d.EndFrame() }()
		_, sy := s.Scroll()
		return s.KeyPressed(input.KeyF3), string(s.Chars()), sy
	}
	// Frame 1, zero updates: the edge shows.
	if p, c, sy := see(); !p || c != "a" || sy != 1 {
		t.Fatalf("frame 1: pressed=%v chars=%q scroll=%v", p, c, sy)
	}
	// Frame 2, still zero updates: it does not show again.
	if p, c, sy := see(); p || c != "" || sy != 0 {
		t.Errorf("frame 2 saw the edge again: pressed=%v chars=%q scroll=%v", p, c, sy)
	}
	// The update between frames 2 and 3 still sees it once.
	if !s.KeyPressed(input.KeyF3) || string(s.Chars()) != "a" {
		t.Error("update did not see the edge")
	}
	d.EndUpdate()
	if p, c, sy := see(); p || c != "" || sy != 0 {
		t.Errorf("frame 3 saw the edge a third time: pressed=%v chars=%q scroll=%v", p, c, sy)
	}
}

// A pair of sources for the two halves of an axis reads the whole travel
// in both directions.
func TestAxisPairReadsBothHalves(t *testing.T) {
	d := hook.NewInput()
	s := d.Game().(*input.State)
	a := input.NewActions()
	a.Bind("move_x", input.PadAxis(input.AxisLeftX), input.PadAxis(input.AxisLeftX).Neg())
	var axes [input.GamepadAxisCount]float32
	axes[input.AxisLeftX] = 1
	d.FeedGamepad(0, true, "pad", [input.GamepadButtonCount]bool{}, axes)
	if v := a.Value(s, "move_x"); v != 1 {
		t.Errorf("stick full right: move_x = %v, want 1", v)
	}
	axes[input.AxisLeftX] = -1
	d.FeedGamepad(0, true, "pad", [input.GamepadButtonCount]bool{}, axes)
	if v := a.Value(s, "move_x"); v != -1 {
		t.Errorf("stick full left: move_x = %v, want -1", v)
	}
}
