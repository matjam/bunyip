package hook

// GamepadInfo carries fixed comparable metadata across the input boundary.
type GamepadInfo struct {
	Name, Backend       string
	VendorID, ProductID uint16
	Buttons             [GamepadButtonCount]bool
	Axes                [GamepadAxisCount]bool
}
