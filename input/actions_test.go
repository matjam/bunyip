package input

import (
	"encoding/json"
	"testing"
)

func TestActions(t *testing.T) {
	var s State
	a := NewActions()
	a.Bind("jump", KeySource(KeySpace), PadButton(ButtonA))
	a.Bind("move_x", KeySource(KeyD), KeySource(KeyA).Neg(), PadAxis(AxisLeftX), PadAxis(AxisLeftX).Neg())

	s.FeedKey(KeySpace, true, false, 0)
	if !a.Pressed(&s, "jump") || !a.Down(&s, "jump") {
		t.Error("space did not fire jump")
	}
	s.EndUpdate()
	if a.Pressed(&s, "jump") || !a.Down(&s, "jump") {
		t.Error("jump edge did not clear or hold did")
	}
	s.FeedKey(KeySpace, false, false, 0)
	if !a.Released(&s, "jump") {
		t.Error("jump did not release")
	}
	s.EndUpdate()

	s.FeedKey(KeyA, true, false, 0)
	if v := a.Value(&s, "move_x"); v != -1 {
		t.Errorf("A gives %v, want -1", v)
	}
	s.FeedKey(KeyD, true, false, 0)
	if v := a.Value(&s, "move_x"); v != 0 {
		t.Errorf("A and D give %v, want 0", v)
	}
	s.FeedKey(KeyA, false, false, 0)
	s.FeedKey(KeyD, false, false, 0)
	s.EndUpdate()

	// A stick past the dead zone, either way; a flick past half is an edge.
	var buttons [GamepadButtonCount]bool
	var axes [GamepadAxisCount]float32
	axes[AxisLeftX] = -0.6
	s.FeedGamepad(0, true, "pad", buttons, axes)
	if !s.Gamepad(0).JustConnected() {
		t.Error("the pad did not report connecting")
	}
	if v := a.Value(&s, "move_x"); v > -0.4 || v < -0.6 {
		t.Errorf("stick left gives %v, want about -0.5", v)
	}
	if !a.Pressed(&s, "move_x") {
		t.Error("a flick past half is not an edge")
	}
	s.EndUpdate()
	if s.Gamepad(0).JustConnected() || a.Pressed(&s, "move_x") {
		t.Error("edges did not clear")
	}
	axes[AxisLeftX] = 0.05
	s.FeedGamepad(0, true, "pad", buttons, axes)
	if v := a.Value(&s, "move_x"); v != 0 {
		t.Errorf("inside the dead zone gives %v", v)
	}
	buttons[ButtonA] = true
	s.FeedGamepad(0, true, "pad", buttons, axes)
	if !a.Pressed(&s, "jump") {
		t.Error("the pad's A did not fire jump")
	}
	s.EndUpdate()

	// Rebinding: Listen captures the next press, Rebind swaps it in, and
	// the bindings survive a round trip through JSON.
	s.FeedKey(KeyJ, true, false, 0)
	src, ok := a.Listen(&s)
	if !ok || src != KeySource(KeyJ) {
		t.Fatalf("Listen = %v %v", src, ok)
	}
	a.Rebind("jump", src)
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	b := NewActions()
	if err := json.Unmarshal(data, b); err != nil {
		t.Fatal(err)
	}
	if got := b.Bindings("jump"); len(got) != 1 || got[0] != KeySource(KeyJ) {
		t.Errorf("jump after round trip = %v (%s)", got, data)
	}
	if got := b.Bindings("move_x"); len(got) != 4 || got[1] != KeySource(KeyA).Neg() || got[3] != PadAxis(AxisLeftX).Neg() {
		t.Errorf("move_x after round trip = %v", got)
	}
	if names := b.Names(); len(names) != 2 || names[0] != "jump" {
		t.Errorf("names %v", names)
	}
	for _, text := range []string{"key:Space", "-axis:LeftX", "mouse:Right", "pad:DpadUp", "axis:RightTrigger*0.5"} {
		p, err := ParseSource(text)
		if err != nil || p.String() != text {
			t.Errorf("%q parses to %v (%v) and prints %q", text, p, err, p.String())
		}
	}
	if _, err := ParseSource("key:NoSuchKey"); err == nil {
		t.Error("an unknown key parsed")
	}
}

func TestHeldAndDoubleClick(t *testing.T) {
	var s State
	s.SetStep(0.1)
	s.FeedKey(KeyW, true, false, 0)
	for range 5 {
		s.EndUpdate()
	}
	if h := s.KeyHeld(KeyW); h < 0.49 || h > 0.51 {
		t.Errorf("held %v, want 0.5", h)
	}
	if keys := s.KeysDown(); len(keys) != 1 || keys[0] != KeyW {
		t.Errorf("keys down %v", keys)
	}
	s.FeedKey(KeyW, false, false, 0)
	s.EndUpdate()
	if s.KeyHeld(KeyW) != 0 || len(s.KeysDown()) != 0 {
		t.Error("held time did not clear")
	}

	click := func(x, y float32) {
		s.FeedMouseButton(MouseLeft, true, x, y)
		s.FeedMouseButton(MouseLeft, false, x, y)
	}
	click(10, 10)
	if s.MouseDoubleClicked(MouseLeft) {
		t.Error("one click is a double click")
	}
	s.EndUpdate()
	click(12, 11)
	if !s.MouseDoubleClicked(MouseLeft) {
		t.Error("two quick clicks are not a double click")
	}
	s.EndUpdate()
	click(12, 11)
	if s.MouseDoubleClicked(MouseLeft) {
		t.Error("a third click continued the double click")
	}
	s.EndUpdate()
	for range 6 { // 0.6 s later
		s.EndUpdate()
	}
	click(12, 11)
	if s.MouseDoubleClicked(MouseLeft) {
		t.Error("a slow second click counted")
	}
	s.EndUpdate()
	click(200, 200)
	if s.MouseDoubleClicked(MouseLeft) {
		t.Error("a second click far away counted")
	}
}
