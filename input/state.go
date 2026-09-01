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
	chars            []rune
}

// EndUpdate clears the per-update transients (pressed, released, scroll,
// typed text). The engine calls it after each game update.
func (s *State) EndUpdate() {
	s.pressed = [KeyCount]bool{}
	s.released = [KeyCount]bool{}
	s.buttonsPressed = [MouseButtonCount]bool{}
	s.buttonsReleased = [MouseButtonCount]bool{}
	s.scrollX, s.scrollY = 0, 0
	s.chars = s.chars[:0]
}

// KeyDown reports whether the key is held.
func (s *State) KeyDown(k Key) bool { return s.down[k] }

// KeyPressed reports whether the key went down since the last update.
// Key repeats count as presses so held keys scroll and step.
func (s *State) KeyPressed(k Key) bool { return s.pressed[k] }

// KeyReleased reports whether the key went up since the last update.
func (s *State) KeyReleased(k Key) bool { return s.released[k] }

// Mods returns the modifier keys currently held.
func (s *State) Mods() Mods { return s.mods }

// Mouse returns the pointer position in view units.
func (s *State) Mouse() (x, y float64) { return s.mouseX, s.mouseY }

func (s *State) MouseDown(b MouseButton) bool     { return s.buttons[b] }
func (s *State) MousePressed(b MouseButton) bool  { return s.buttonsPressed[b] }
func (s *State) MouseReleased(b MouseButton) bool { return s.buttonsReleased[b] }

// Scroll returns wheel movement since the last update, in lines.
func (s *State) Scroll() (dx, dy float64) { return s.scrollX, s.scrollY }

// Chars returns the text typed since the last update, in order.
func (s *State) Chars() []rune { return s.chars }

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
