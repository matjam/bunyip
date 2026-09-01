package platform

import "github.com/matjam/bunyip/input"

// The input vocabulary is re-exported so that this package's events can be
// read without importing input as well.
type (
	Key         = input.Key
	Mods        = input.Mods
	MouseButton = input.MouseButton
)
