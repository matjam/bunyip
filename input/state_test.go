package input

import "testing"

// TestFrameLatching checks that edges cleared by endUpdate stay visible
// to Draw until endFrame, and that Update never sees an edge twice.
func TestFrameLatching(t *testing.T) {
	var s State
	s.feedMouseButton(MouseLeft, true, 1, 2)
	s.feedKey(KeySpace, true, false, 0)
	s.feedChar('a')
	s.feedScroll(0, 1)
	if !s.MousePressed(MouseLeft) || !s.KeyPressed(KeySpace) {
		t.Fatal("update should see the edges")
	}
	s.endUpdate()
	if s.MousePressed(MouseLeft) || s.KeyPressed(KeySpace) || len(s.Chars()) != 0 {
		t.Fatal("a second update must not see the same edges")
	}
	s.setDrawing(true)
	if !s.MousePressed(MouseLeft) || !s.KeyPressed(KeySpace) || string(s.Chars()) != "a" {
		t.Fatal("draw should see the frame's edges")
	}
	if _, dy := s.Scroll(); dy != 1 {
		t.Fatalf("draw scroll %v", dy)
	}
	s.setDrawing(false)
	s.endFrame()
	s.setDrawing(true)
	if s.MousePressed(MouseLeft) || s.KeyPressed(KeySpace) || len(s.Chars()) != 0 {
		t.Fatal("edges survived endFrame")
	}
	s.setDrawing(false)
	// Edges fed after the last update of a frame are visible to Draw too.
	s.feedMouseButton(MouseLeft, false, 1, 2)
	s.setDrawing(true)
	if !s.MouseReleased(MouseLeft) {
		t.Fatal("draw should see edges not yet consumed by an update")
	}
	s.setDrawing(false)
	if !s.MouseReleased(MouseLeft) {
		t.Fatal("the next update should still see them")
	}
	var buttons [GamepadButtonCount]bool
	buttons[ButtonA] = true
	s.feedGamepad(0, true, "pad", buttons, [GamepadAxisCount]float32{})
	s.endUpdate()
	s.setDrawing(true)
	if !s.Gamepad(0).Pressed(ButtonA) {
		t.Fatal("gamepad edge not latched for draw")
	}
}
