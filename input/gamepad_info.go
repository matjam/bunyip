package input

// GamepadInfo describes the controls mapped into Gamepad, not every hardware
// input. Zero numeric IDs and false masks mean unknown or unavailable. Names
// and backend identifiers are descriptive, not persistent device identity.
type GamepadInfo struct {
	Name, Backend       string
	VendorID, ProductID uint16
	Buttons             [GamepadButtonCount]bool
	Axes                [GamepadAxisCount]bool
}

// HasButton reports whether this backend maps the requested button.
func (i GamepadInfo) HasButton(button GamepadButton) bool {
	return button < GamepadButtonCount && i.Buttons[button]
}

// HasAxis reports whether this backend maps the requested axis.
func (i GamepadInfo) HasAxis(axis GamepadAxis) bool { return axis < GamepadAxisCount && i.Axes[axis] }
