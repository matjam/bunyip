package input

// GamepadButton indexes the buttons of a standard extended gamepad.
type GamepadButton uint8

const (
	ButtonA GamepadButton = iota // bottom face button (Cross on PlayStation)
	ButtonB                      // right face button (Circle)
	ButtonX                      // left face button (Square)
	ButtonY                      // top face button (Triangle)
	ButtonLeftShoulder
	ButtonRightShoulder
	ButtonLeftStick
	ButtonRightStick
	ButtonMenu    // Start / Options
	ButtonOptions // Back / Share
	ButtonHome
	ButtonDpadUp
	ButtonDpadDown
	ButtonDpadLeft
	ButtonDpadRight
	GamepadButtonCount
)

// GamepadAxis indexes the analogue inputs, each in -1..1 (triggers 0..1).
type GamepadAxis uint8

const (
	AxisLeftX GamepadAxis = iota
	AxisLeftY             // +1 is up, as the hardware reports it
	AxisRightX
	AxisRightY
	AxisLeftTrigger
	AxisRightTrigger
	GamepadAxisCount
)

// MaxGamepads is how many controllers the engine tracks.
const MaxGamepads = 4

// Gamepad is one controller's current state.
type Gamepad struct {
	Connected bool
	Name      string
	Buttons   [GamepadButtonCount]bool
	Axes      [GamepadAxisCount]float32
	pressed   [GamepadButtonCount]bool
	released  [GamepadButtonCount]bool

	framePressed, frameReleased [GamepadButtonCount]bool // latched for Draw
}

// Pressed reports whether the button went down since the last update.
func (g *Gamepad) Pressed(b GamepadButton) bool { return g.pressed[b] }

// Released reports whether the button went up since the last update.
func (g *Gamepad) Released(b GamepadButton) bool { return g.released[b] }

// Down reports whether the button is held.
func (g *Gamepad) Down(b GamepadButton) bool { return g.Buttons[b] }

// Axis returns an analogue value with a small dead zone applied.
func (g *Gamepad) Axis(a GamepadAxis) float32 {
	v := g.Axes[a]
	if v > -0.08 && v < 0.08 {
		return 0
	}
	return v
}

// Gamepad returns a snapshot of controller i (0..MaxGamepads-1); a
// disconnected one reads as idle. During Draw the edges cover the whole
// frame, as for keys and mouse buttons.
func (s *State) Gamepad(i int) *Gamepad {
	if i < 0 || i >= MaxGamepads {
		return &Gamepad{}
	}
	g := s.gamepads[i]
	if s.drawing {
		for b := range g.pressed {
			g.pressed[b] = g.pressed[b] || g.framePressed[b]
			g.released[b] = g.released[b] || g.frameReleased[b]
		}
	}
	return &g
}

// FeedGamepad replaces controller i's state, deriving press and release edges.
func (s *State) FeedGamepad(i int, connected bool, name string, buttons [GamepadButtonCount]bool, axes [GamepadAxisCount]float32) {
	if i < 0 || i >= MaxGamepads {
		return
	}
	g := &s.gamepads[i]
	for b := range buttons {
		if buttons[b] && !g.Buttons[b] {
			g.pressed[b] = true
		}
		if !buttons[b] && g.Buttons[b] {
			g.released[b] = true
		}
	}
	g.Connected, g.Name, g.Buttons, g.Axes = connected, name, buttons, axes
}

// MouseDelta returns pointer movement since the last update, in view units;
// it keeps reporting while the cursor is captured.
func (s *State) MouseDelta() (dx, dy float32) {
	if s.drawing {
		return s.mouseDX + s.frame.mouseDX, s.mouseDY + s.frame.mouseDY
	}
	return s.mouseDX, s.mouseDY
}

// FeedMouseDelta accumulates relative pointer movement.
func (s *State) FeedMouseDelta(dx, dy float32) { s.mouseDX += dx; s.mouseDY += dy }
