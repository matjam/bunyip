package input

import "testing"

// TestFrameLatching checks that edges cleared by EndUpdate stay visible
// to Draw until EndFrame, and that Update never sees an edge twice.
func TestFrameLatching(t *testing.T) {
	var s State
	s.FeedMouseButton(MouseLeft, true, 1, 2)
	s.FeedKey(KeySpace, true, false, 0)
	s.FeedChar('a')
	s.FeedScroll(0, 1)
	if !s.MousePressed(MouseLeft) || !s.KeyPressed(KeySpace) {
		t.Fatal("update should see the edges")
	}
	s.EndUpdate()
	if s.MousePressed(MouseLeft) || s.KeyPressed(KeySpace) || len(s.Chars()) != 0 {
		t.Fatal("a second update must not see the same edges")
	}
	s.SetDrawing(true)
	if !s.MousePressed(MouseLeft) || !s.KeyPressed(KeySpace) || string(s.Chars()) != "a" {
		t.Fatal("draw should see the frame's edges")
	}
	if _, dy := s.Scroll(); dy != 1 {
		t.Fatalf("draw scroll %v", dy)
	}
	s.SetDrawing(false)
	s.EndFrame()
	s.SetDrawing(true)
	if s.MousePressed(MouseLeft) || s.KeyPressed(KeySpace) || len(s.Chars()) != 0 {
		t.Fatal("edges survived EndFrame")
	}
	s.SetDrawing(false)
	// Edges fed after the last update of a frame are visible to Draw too.
	s.FeedMouseButton(MouseLeft, false, 1, 2)
	s.SetDrawing(true)
	if !s.MouseReleased(MouseLeft) {
		t.Fatal("draw should see edges not yet consumed by an update")
	}
	s.SetDrawing(false)
	if !s.MouseReleased(MouseLeft) {
		t.Fatal("the next update should still see them")
	}
	var buttons [GamepadButtonCount]bool
	buttons[ButtonA] = true
	s.FeedGamepad(0, true, "pad", buttons, [GamepadAxisCount]float32{})
	s.EndUpdate()
	s.SetDrawing(true)
	if !s.Gamepad(0).Pressed(ButtonA) {
		t.Fatal("gamepad edge not latched for draw")
	}
}
