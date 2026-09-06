package input

// GamepadButton indexes the buttons of a standard extended gamepad.
type GamepadButton uint8

// Standard controller buttons. GamepadButtonCount is the array bound,
// not a button; face-button names describe positions across brands.
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

// Standard axes: stick X is positive right, stick Y is positive up,
// and triggers run from released (0) to pressed (1).
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
	Info                            GamepadInfo               // native metadata and mapped capabilities; empty when disconnected
	Connected                       bool                      // controller is currently present
	Name                            string                    // device-provided display name
	Buttons                         [GamepadButtonCount]bool  // held buttons
	Axes                            [GamepadAxisCount]float32 // raw axes before Axis applies its dead zone
	pressed                         [GamepadButtonCount]bool
	released                        [GamepadButtonCount]bool
	prevAxes                        [GamepadAxisCount]float32 // the previous update's axes, for edges on sticks
	justConnected, justDisconnected bool

	framePressed, frameReleased [GamepadButtonCount]bool // latched for Draw
}

// Pressed reports whether the button went down since the last update.
func (g *Gamepad) Pressed(b GamepadButton) bool { return g.pressed[b] }

// Released reports whether the button went up since the last update.
func (g *Gamepad) Released(b GamepadButton) bool { return g.released[b] }

// Down reports whether the button is held.
func (g *Gamepad) Down(b GamepadButton) bool { return g.Buttons[b] }

// Axis returns an analogue value, replacing values strictly between
// -0.08 and 0.08 with zero; values outside that range are not rescaled.
func (g *Gamepad) Axis(a GamepadAxis) float32 {
	v := g.Axes[a]
	if v > -0.08 && v < 0.08 {
		return 0
	}
	return v
}

// Gamepad returns a snapshot of controller i (0..MaxGamepads-1); a
// disconnected one reads as idle. Out-of-range indices return an idle
// snapshot. Changing the snapshot does not modify State. During Draw
// button edges cover the whole frame, as for keys and mouse buttons;
// connection flags and axis transitions still cover the update only.
func (s *State) Gamepad(i int) *Gamepad {
	if i < 0 || i >= MaxGamepads {
		return &Gamepad{}
	}
	g := s.gamepads[i]
	if s.drawing {
		g.pressed = g.framePressed
		g.released = g.frameReleased
	}
	return &g
}

// feedGamepad replaces controller i's state, deriving press and release edges.
func (s *State) feedGamepad(i int, connected bool, name string, buttons [GamepadButtonCount]bool, axes [GamepadAxisCount]float32) {
	if i < 0 || i >= MaxGamepads {
		return
	}
	g := &s.gamepads[i]
	for b := range buttons {
		if buttons[b] && !g.Buttons[b] {
			g.pressed[b], g.framePressed[b] = true, true
		}
		if !buttons[b] && g.Buttons[b] {
			g.released[b], g.frameReleased[b] = true, true
		}
	}
	if connected && !g.Connected {
		g.justConnected = true
	}
	if !connected && g.Connected {
		g.justDisconnected = true
	}
	g.Connected, g.Name, g.Buttons, g.Axes = connected, name, buttons, axes
}

// JustConnected reports whether the controller appeared since the last
// update, for a "player 2 joined" prompt.
func (g *Gamepad) JustConnected() bool { return g.justConnected }

// JustDisconnected reports whether the controller went away since the
// last update, so a game can pause.
func (g *Gamepad) JustDisconnected() bool { return g.justDisconnected }

// MouseDelta returns pointer movement since the last update, in view units;
// it keeps reporting while the cursor is captured.
func (s *State) MouseDelta() (dx, dy float32) {
	if s.drawing {
		return s.frame.mouseDX, s.frame.mouseDY
	}
	return s.mouseDX, s.mouseDY
}

// feedMouseDelta accumulates relative pointer movement.
func (s *State) feedMouseDelta(dx, dy float32) {
	s.mouseDX += dx
	s.mouseDY += dy
	s.frame.mouseDX += dx
	s.frame.mouseDY += dy
}
