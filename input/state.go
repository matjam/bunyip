// Package input reports the state of the keyboard, mouse and gamepads:
// what is held, what changed this update, where the pointer is and what
// text was typed, in view units.
// Read Context.Input on the game loop goroutine. During Update, edge
// accessors cover events since the preceding update; during Draw,
// keyboard and mouse edges, text and motion cover the drawn frame.
// These views are cleared independently, so drawing does not consume
// input pending for Update. Repeated OS key events count as presses.
package input

import "github.com/matjam/bunyip/lin"

// State is the keyboard and mouse as a game sees them each update: what is
// held, what changed since the last update, where the pointer is and what
// text was typed. The engine feeds it from platform events.
// The zero value is idle. State is not safe for concurrent access.
// Key and mouse accessors require valid enum values below their Count.
type State struct {
	down     [KeyCount]bool
	pressed  [KeyCount]bool
	released [KeyCount]bool
	mods     Mods

	mouseX, mouseY   float32
	buttons          [MouseButtonCount]bool
	buttonsPressed   [MouseButtonCount]bool
	buttonsReleased  [MouseButtonCount]bool
	scrollX, scrollY float32
	mouseDX, mouseDY float32
	repeated         [KeyCount]bool
	chars            []rune
	composition      string
	gamepads         [MaxGamepads]Gamepad
	held             [KeyCount]float32 // seconds each key has been down
	step             float32           // seconds per update, for held times and double clicks
	clock            float32           // seconds of updates so far
	lastClick        [MouseButtonCount]float32
	lastClickX       [MouseButtonCount]float32
	lastClickY       [MouseButtonCount]float32
	doubleClicked    [MouseButtonCount]bool

	// Transients are cleared after every Update, but a frame may run
	// several updates (or none) before Draw. So every edge is fed into
	// both the per-update set and frame, which Draw reads and which
	// clears at the end of the frame; drawing selects which set the
	// accessors show. Feeding both directly, rather than latching one
	// into the other at the end of an update, keeps an edge from a frame
	// that ran no update from being reported again by the next frames.
	frame   frameTransients
	drawing bool
}

// frameTransients accumulate the per-update edges over one drawn frame.
type frameTransients struct {
	pressed, released               [KeyCount]bool
	buttonsPressed, buttonsReleased [MouseButtonCount]bool
	doubleClicked                   [MouseButtonCount]bool
	scrollX, scrollY                float32
	mouseDX, mouseDY                float32
	chars                           []rune
	repeated                        [KeyCount]bool
}

// endUpdate clears the per-update transients (pressed, released, scroll,
// typed text) and advances the held times. The engine calls it after
// each game update. Draw's copy of the edges lives in frame and is
// cleared by endFrame.
func (s *State) endUpdate() {
	s.repeated = [KeyCount]bool{}
	step := s.step
	if step <= 0 {
		step = 1.0 / 60
	}
	s.clock += step
	for k := range s.down {
		if s.down[k] {
			s.held[k] += step
		} else {
			s.held[k] = 0
		}
	}
	s.pressed = [KeyCount]bool{}
	s.released = [KeyCount]bool{}
	s.buttonsPressed = [MouseButtonCount]bool{}
	s.buttonsReleased = [MouseButtonCount]bool{}
	s.doubleClicked = [MouseButtonCount]bool{}
	s.scrollX, s.scrollY = 0, 0
	s.mouseDX, s.mouseDY = 0, 0
	s.chars = s.chars[:0]
	for i := range s.gamepads {
		g := &s.gamepads[i]
		g.pressed = [GamepadButtonCount]bool{}
		g.released = [GamepadButtonCount]bool{}
		g.justConnected, g.justDisconnected = false, false
		g.prevAxes = g.Axes // stick edges last one update, like presses
	}
}

// endFrame clears the transients latched for Draw. The engine calls it
// after each drawn frame.
func (s *State) endFrame() {
	s.frame = frameTransients{chars: s.frame.chars[:0]}
	for i := range s.gamepads {
		s.gamepads[i].framePressed = [GamepadButtonCount]bool{}
		s.gamepads[i].frameReleased = [GamepadButtonCount]bool{}
	}
}

// setDrawing switches the accessors between what happened since the
// last update (for Update) and what happened since the last drawn frame
// (for Draw, where immediate-mode interfaces run). The engine sets it
// around Draw.
func (s *State) setDrawing(drawing bool) { s.drawing = drawing }

// KeyDown reports whether the key is held.
func (s *State) KeyDown(k Key) bool { return s.down[k] }

// KeyHeld reports how long the key has been held, in seconds of updates,
// and zero when it is up: charge-up attacks, hold-to-confirm, cheat
// codes that want a long press.
func (s *State) KeyHeld(k Key) float32 { return s.held[k] }

// KeysDown returns every key currently held, in key order: for
// rebinding screens and combos.
//
// It allocates the slice it returns, so it is for a settings screen or a
// combo check that runs on demand, not for every frame of play. To ask
// every frame, keep a slice and use AppendKeysDown, or ask about the keys
// the game cares about with KeyDown.
func (s *State) KeysDown() []Key { return s.AppendKeysDown(nil) }

// AppendKeysDown appends every key currently held to dst, in key order,
// and returns the extended slice. Pass a slice kept between frames,
// truncated to zero length, and the scan allocates nothing:
//
//	g.held = in.AppendKeysDown(g.held[:0])
func (s *State) AppendKeysDown(dst []Key) []Key {
	for k, down := range s.down {
		if down {
			dst = append(dst, Key(k))
		}
	}
	return dst
}

// SetStep tells the state how many seconds each update covers, for
// held times and double-click timing. The engine sets it from the fixed
// step; zero means a sixtieth of a second.
func (s *State) SetStep(seconds float32) { s.step = seconds }

// DoubleClickTime is how close two presses must be to count as a
// double click, in seconds, and DoubleClickDistance how close in view
// units.
const (
	DoubleClickTime     = 0.4
	DoubleClickDistance = 6
)

// MouseDoubleClicked reports whether the button was pressed twice within
// DoubleClickTime and DoubleClickDistance, on the second press.
func (s *State) MouseDoubleClicked(b MouseButton) bool {
	if s.drawing {
		return s.frame.doubleClicked[b]
	}
	return s.doubleClicked[b]
}

// KeyPressed reports whether the key went down since the last update
// (or, during Draw, since the last frame). Key repeats count as presses
// so held keys scroll and step.
func (s *State) KeyPressed(k Key) bool {
	if s.drawing {
		return s.frame.pressed[k]
	}
	return s.pressed[k]
}

// KeyReleased reports whether the key went up since the last update
// (or, during Draw, since the last drawn frame).
func (s *State) KeyReleased(k Key) bool {
	if s.drawing {
		return s.frame.released[k]
	}
	return s.released[k]
}

// Mods returns the active modifiers, including Caps Lock and Num Lock.
// Modifier changes are reported even when no key is pressed or released.
func (s *State) Mods() Mods { return s.mods }

// Mouse returns the pointer position in view units.
func (s *State) Mouse() (x, y float32) { return s.mouseX, s.mouseY }

// MousePos returns the pointer position as a vector.
func (s *State) MousePos() lin.Vec2 { return lin.V2(s.mouseX, s.mouseY) }

// KeyRepeated reports whether the key produced an operating-system
// repeat since the last update (or, during Draw, since the last frame).
// KeyPressed already counts repeats. KeyPressed(k) && !KeyRepeated(k)
// filters repeats, but also filters an initial press if both kinds of
// event arrived in the same update or frame.
func (s *State) KeyRepeated(k Key) bool {
	if s.drawing {
		return s.frame.repeated[k]
	}
	return s.repeated[k]
}

// MouseDown reports whether the button is held.
func (s *State) MouseDown(b MouseButton) bool { return s.buttons[b] }

// MousePressed reports whether the button went down since the last
// update (or, during Draw, since the last frame).
func (s *State) MousePressed(b MouseButton) bool {
	if s.drawing {
		return s.frame.buttonsPressed[b]
	}
	return s.buttonsPressed[b]
}

// MouseReleased reports whether the button went up since the last update
// (or, during Draw, since the last drawn frame).
func (s *State) MouseReleased(b MouseButton) bool {
	if s.drawing {
		return s.frame.buttonsReleased[b]
	}
	return s.buttonsReleased[b]
}

// Scroll returns wheel movement since the last update, in lines. A
// trackpad's smooth scrolling is scaled to lines by the engine.
// During Draw it covers movement since the last drawn frame.
func (s *State) Scroll() (dx, dy float32) {
	if s.drawing {
		return s.frame.scrollX, s.frame.scrollY
	}
	return s.scrollX, s.scrollY
}

// Chars returns committed text typed since the last update, in order
// (or, during Draw, since the last drawn frame). The returned slice is
// borrowed from State; copy it to retain text beyond the current call
// to the game's Update or Draw, and do not modify it.
func (s *State) Chars() []rune {
	if s.drawing {
		return s.frame.chars
	}
	return s.chars
}

// Composition returns the input method's uncommitted text, such as the
// syllables of a Japanese word still being converted. Text fields show it
// after the committed text; it is empty when nothing is being composed.
func (s *State) Composition() string { return s.composition }

// feedKey records a key going down or up; the engine calls the feed
// methods as platform events arrive.
func (s *State) feedKey(k Key, down, repeat bool, mods Mods) {
	s.mods = mods
	if k == KeyUnknown {
		return
	}
	if down {
		s.pressed[k], s.frame.pressed[k] = true, true
		if repeat {
			s.repeated[k], s.frame.repeated[k] = true, true
		} else {
			s.down[k] = true
		}
		return
	}
	s.down[k] = false
	s.released[k], s.frame.released[k] = true, true
}

// feedModifiers replaces modifier state without changing keys or text.
func (s *State) feedModifiers(mods Mods) { s.mods = mods }

// feedChar records a typed character.
func (s *State) feedChar(r rune) {
	s.chars = append(s.chars, r)
	s.frame.chars = append(s.frame.chars, r)
}

// feedComposition records the input method's uncommitted text.
func (s *State) feedComposition(text string) { s.composition = text }

// feedMouseMove records the pointer position in view units.
func (s *State) feedMouseMove(x, y float32) { s.mouseX, s.mouseY = x, y }

// feedMouseButton records a button going down or up at a position.
func (s *State) feedMouseButton(b MouseButton, down bool, x, y float32) {
	s.mouseX, s.mouseY = x, y
	if int(b) >= len(s.buttons) {
		return
	}
	s.buttons[b] = down
	if down {
		s.buttonsPressed[b], s.frame.buttonsPressed[b] = true, true
		dx, dy := x-s.lastClickX[b], y-s.lastClickY[b]
		if s.lastClick[b] > 0 && s.clock-s.lastClick[b] <= DoubleClickTime && dx*dx+dy*dy <= DoubleClickDistance*DoubleClickDistance {
			s.doubleClicked[b], s.frame.doubleClicked[b] = true, true
			s.lastClick[b] = 0 // a third click starts over
		} else {
			s.lastClick[b] = s.clock + 1e-6 // never exactly zero
		}
		s.lastClickX[b], s.lastClickY[b] = x, y
	} else {
		s.buttonsReleased[b], s.frame.buttonsReleased[b] = true, true
	}
}

// feedScroll accumulates wheel movement in lines.
func (s *State) feedScroll(dx, dy float32) {
	s.scrollX += dx
	s.scrollY += dy
	s.frame.scrollX += dx
	s.frame.scrollY += dy
}

// feedFocusLost releases everything, since key-up events stop arriving.
func (s *State) feedFocusLost() {
	for k := range s.down {
		if s.down[k] {
			s.down[k] = false
			s.released[k], s.frame.released[k] = true, true
		}
	}
	for b := range s.buttons {
		s.buttons[b] = false
	}
	s.mods = 0
}
