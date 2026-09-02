package input

import "testing"

// benchActions binds a movement action the way a game would: two keys, a
// gamepad stick in both directions.
func benchActions() (*Actions, *State) {
	a := NewActions()
	a.Bind("move_x", KeySource(KeyD), KeySource(KeyA).Neg(), PadAxis(AxisLeftX), PadAxis(AxisLeftX).Neg())
	a.Bind("jump", KeySource(KeySpace), PadButton(ButtonA))
	s := &State{}
	s.feedKey(KeyD, true, false, 0)
	return a, s
}

// BenchmarkActionsValueByName is a query per action per frame through the
// bindings map.
func BenchmarkActionsValueByName(b *testing.B) {
	a, s := benchActions()
	b.ReportAllocs()
	b.ResetTimer()
	var v float32
	for b.Loop() {
		v += a.Value(s, "move_x")
	}
	_ = v
}

// BenchmarkActionsValueByHandle is the same query through a handle
// resolved once at startup.
func BenchmarkActionsValueByHandle(b *testing.B) {
	a, s := benchActions()
	move := a.Action("move_x")
	b.ReportAllocs()
	b.ResetTimer()
	var v float32
	for b.Loop() {
		v += move.Value(s)
	}
	_ = v
}

// BenchmarkActionsPressedByName and ByHandle cover the edge query, which
// a game makes as often as Value.
func BenchmarkActionsPressedByName(b *testing.B) {
	a, s := benchActions()
	b.ReportAllocs()
	b.ResetTimer()
	n := 0
	for b.Loop() {
		if a.Pressed(s, "jump") {
			n++
		}
	}
	_ = n
}

func BenchmarkActionsPressedByHandle(b *testing.B) {
	a, s := benchActions()
	jump := a.Action("jump")
	b.ReportAllocs()
	b.ResetTimer()
	n := 0
	for b.Loop() {
		if jump.Pressed(s) {
			n++
		}
	}
	_ = n
}

// BenchmarkKeysDown is the allocating form, kept to show what the append
// form saves.
func BenchmarkKeysDown(b *testing.B) {
	s := &State{}
	s.feedKey(KeyW, true, false, 0)
	s.feedKey(KeyLeftShift, true, false, 0)
	b.ReportAllocs()
	b.ResetTimer()
	n := 0
	for b.Loop() {
		n += len(s.KeysDown())
	}
	_ = n
}

// BenchmarkAppendKeysDown is the same scan into a slice kept between
// frames.
func BenchmarkAppendKeysDown(b *testing.B) {
	s := &State{}
	s.feedKey(KeyW, true, false, 0)
	s.feedKey(KeyLeftShift, true, false, 0)
	keys := make([]Key, 0, 16)
	b.ReportAllocs()
	b.ResetTimer()
	n := 0
	for b.Loop() {
		keys = s.AppendKeysDown(keys[:0])
		n += len(keys)
	}
	_ = n
}
