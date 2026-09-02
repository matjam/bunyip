// Package input reports the state of the keyboard, mouse and gamepads:
// what is held, what changed this update, where the pointer is and what
// text was typed, in view units.
package input

import "github.com/matjam/bunyip/lin"

// State is the keyboard and mouse as a game sees them each update: what is
// held, what changed since the last update, where the pointer is and what
// text was typed. The engine feeds it from platform events.
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
	// several updates (or none) before Draw. So each update's transients
	// are also latched into frame, which Draw reads and which clears at
	// the end of the frame; drawing selects which set the accessors show.
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
// typed text), first latching them for Draw. The engine calls it after
// each game update.
func (s *State) endUpdate() {
	f := &s.frame
	for k := range s.pressed {
		f.pressed[k] = f.pressed[k] || s.pressed[k]
		f.released[k] = f.released[k] || s.released[k]
		f.repeated[k] = f.repeated[k] || s.repeated[k]
	}
	s.repeated = [KeyCount]bool{}
	for b := range s.buttonsPressed {
		f.buttonsPressed[b] = f.buttonsPressed[b] || s.buttonsPressed[b]
		f.buttonsReleased[b] = f.buttonsReleased[b] || s.buttonsReleased[b]
		f.doubleClicked[b] = f.doubleClicked[b] || s.doubleClicked[b]
	}
	f.scrollX += s.scrollX
	f.scrollY += s.scrollY
	f.mouseDX += s.mouseDX
	f.mouseDY += s.mouseDY
	f.chars = append(f.chars, s.chars...)
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
		for b := range g.pressed {
			g.framePressed[b] = g.framePressed[b] || g.pressed[b]
			g.frameReleased[b] = g.frameReleased[b] || g.released[b]
		}
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
func (s *State) KeysDown() []Key {
	var keys []Key
	for k, down := range s.down {
		if down {
			keys = append(keys, Key(k))
		}
	}
	return keys
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
	return s.doubleClicked[b] || s.drawing && s.frame.doubleClicked[b]
}

// KeyPressed reports whether the key went down since the last update
// (or, during Draw, since the last frame). Key repeats count as presses
// so held keys scroll and step.
func (s *State) KeyPressed(k Key) bool { return s.pressed[k] || s.drawing && s.frame.pressed[k] }

// KeyReleased reports whether the key went up since the last update.
func (s *State) KeyReleased(k Key) bool { return s.released[k] || s.drawing && s.frame.released[k] }

// Mods returns the modifier keys currently held.
func (s *State) Mods() Mods { return s.mods }

// Mouse returns the pointer position in view units.
func (s *State) Mouse() (x, y float32) { return s.mouseX, s.mouseY }

// MousePos returns the pointer position as a vector.
func (s *State) MousePos() lin.Vec2 { return lin.V2(s.mouseX, s.mouseY) }

// KeyRepeated reports whether the key produced an operating-system
// repeat since the last update: a held key in a menu or a text field.
// KeyPressed already counts repeats; use KeyPressed and not KeyRepeated
// for the first press alone.
func (s *State) KeyRepeated(k Key) bool { return s.repeated[k] || s.drawing && s.frame.repeated[k] }

// MouseDown reports whether the button is held.
func (s *State) MouseDown(b MouseButton) bool { return s.buttons[b] }

// MousePressed reports whether the button went down since the last
// update (or, during Draw, since the last frame).
func (s *State) MousePressed(b MouseButton) bool {
	return s.buttonsPressed[b] || s.drawing && s.frame.buttonsPressed[b]
}

// MouseReleased reports whether the button went up since the last update.
func (s *State) MouseReleased(b MouseButton) bool {
	return s.buttonsReleased[b] || s.drawing && s.frame.buttonsReleased[b]
}

// Scroll returns wheel movement since the last update, in lines.
func (s *State) Scroll() (dx, dy float32) {
	if s.drawing {
		return s.scrollX + s.frame.scrollX, s.scrollY + s.frame.scrollY
	}
	return s.scrollX, s.scrollY
}

// Chars returns the text typed since the last update, in order.
func (s *State) Chars() []rune {
	if s.drawing && len(s.frame.chars) > 0 {
		return append(append([]rune{}, s.frame.chars...), s.chars...)
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
		s.pressed[k] = true
		if repeat {
			s.repeated[k] = true
		} else {
			s.down[k] = true
		}
		return
	}
	s.down[k] = false
	s.released[k] = true
}

// feedChar records a typed character.
func (s *State) feedChar(r rune) { s.chars = append(s.chars, r) }

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
		s.buttonsPressed[b] = true
		dx, dy := x-s.lastClickX[b], y-s.lastClickY[b]
		if s.lastClick[b] > 0 && s.clock-s.lastClick[b] <= DoubleClickTime && dx*dx+dy*dy <= DoubleClickDistance*DoubleClickDistance {
			s.doubleClicked[b] = true
			s.lastClick[b] = 0 // a third click starts over
		} else {
			s.lastClick[b] = s.clock + 1e-6 // never exactly zero
		}
		s.lastClickX[b], s.lastClickY[b] = x, y
	} else {
		s.buttonsReleased[b] = true
	}
}

// feedScroll accumulates wheel movement in lines.
func (s *State) feedScroll(dx, dy float32) { s.scrollX += dx; s.scrollY += dy }

// feedFocusLost releases everything, since key-up events stop arriving.
func (s *State) feedFocusLost() {
	for k := range s.down {
		if s.down[k] {
			s.down[k] = false
			s.released[k] = true
		}
	}
	for b := range s.buttons {
		s.buttons[b] = false
	}
	s.mods = 0
}
