package input

// State is the keyboard and mouse as a game sees them each update: what is
// held, what changed since the last update, where the pointer is and what
// text was typed. The engine feeds it from platform events.
type State struct {
	down     [KeyCount]bool
	pressed  [KeyCount]bool
	released [KeyCount]bool
	mods     Mods

	mouseX, mouseY   float64
	buttons          [MouseButtonCount]bool
	buttonsPressed   [MouseButtonCount]bool
	buttonsReleased  [MouseButtonCount]bool
	scrollX, scrollY float64
	mouseDX, mouseDY float64
	chars            []rune
	composition      string
	gamepads         [MaxGamepads]Gamepad

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
	scrollX, scrollY                float64
	mouseDX, mouseDY                float64
	chars                           []rune
}

// EndUpdate clears the per-update transients (pressed, released, scroll,
// typed text), first latching them for Draw. The engine calls it after
// each game update.
func (s *State) EndUpdate() {
	f := &s.frame
	for k := range s.pressed {
		f.pressed[k] = f.pressed[k] || s.pressed[k]
		f.released[k] = f.released[k] || s.released[k]
	}
	for b := range s.buttonsPressed {
		f.buttonsPressed[b] = f.buttonsPressed[b] || s.buttonsPressed[b]
		f.buttonsReleased[b] = f.buttonsReleased[b] || s.buttonsReleased[b]
	}
	f.scrollX += s.scrollX
	f.scrollY += s.scrollY
	f.mouseDX += s.mouseDX
	f.mouseDY += s.mouseDY
	f.chars = append(f.chars, s.chars...)
	s.pressed = [KeyCount]bool{}
	s.released = [KeyCount]bool{}
	s.buttonsPressed = [MouseButtonCount]bool{}
	s.buttonsReleased = [MouseButtonCount]bool{}
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
	}
}

// EndFrame clears the transients latched for Draw. The engine calls it
// after each drawn frame.
func (s *State) EndFrame() {
	s.frame = frameTransients{chars: s.frame.chars[:0]}
	for i := range s.gamepads {
		s.gamepads[i].framePressed = [GamepadButtonCount]bool{}
		s.gamepads[i].frameReleased = [GamepadButtonCount]bool{}
	}
}

// SetDrawing switches the accessors between what happened since the
// last update (for Update) and what happened since the last drawn frame
// (for Draw, where immediate-mode interfaces run). The engine sets it
// around Draw.
func (s *State) SetDrawing(drawing bool) { s.drawing = drawing }

// KeyDown reports whether the key is held.
func (s *State) KeyDown(k Key) bool { return s.down[k] }

// KeyPressed reports whether the key went down since the last update
// (or, during Draw, since the last frame). Key repeats count as presses
// so held keys scroll and step.
func (s *State) KeyPressed(k Key) bool { return s.pressed[k] || s.drawing && s.frame.pressed[k] }

// KeyReleased reports whether the key went up since the last update.
func (s *State) KeyReleased(k Key) bool { return s.released[k] || s.drawing && s.frame.released[k] }

// Mods returns the modifier keys currently held.
func (s *State) Mods() Mods { return s.mods }

// Mouse returns the pointer position in view units.
func (s *State) Mouse() (x, y float64) { return s.mouseX, s.mouseY }

func (s *State) MouseDown(b MouseButton) bool { return s.buttons[b] }
func (s *State) MousePressed(b MouseButton) bool {
	return s.buttonsPressed[b] || s.drawing && s.frame.buttonsPressed[b]
}
func (s *State) MouseReleased(b MouseButton) bool {
	return s.buttonsReleased[b] || s.drawing && s.frame.buttonsReleased[b]
}

// Scroll returns wheel movement since the last update, in lines.
func (s *State) Scroll() (dx, dy float64) {
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

// Feed methods, called by the engine as events arrive.

func (s *State) FeedKey(k Key, down, repeat bool, mods Mods) {
	s.mods = mods
	if k == KeyUnknown {
		return
	}
	if down {
		s.pressed[k] = true
		if !repeat {
			s.down[k] = true
		}
		return
	}
	s.down[k] = false
	s.released[k] = true
}

func (s *State) FeedChar(r rune) { s.chars = append(s.chars, r) }

func (s *State) FeedComposition(text string) { s.composition = text }

func (s *State) FeedMouseMove(x, y float64) { s.mouseX, s.mouseY = x, y }

func (s *State) FeedMouseButton(b MouseButton, down bool, x, y float64) {
	s.mouseX, s.mouseY = x, y
	if int(b) >= len(s.buttons) {
		return
	}
	s.buttons[b] = down
	if down {
		s.buttonsPressed[b] = true
	} else {
		s.buttonsReleased[b] = true
	}
}

func (s *State) FeedScroll(dx, dy float64) { s.scrollX += dx; s.scrollY += dy }

// FeedFocusLost releases everything, since key-up events stop arriving.
func (s *State) FeedFocusLost() {
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
