package bunyip

import "github.com/matjam/bunyip/input"

// KeyboardLayout reads the current native keyboard layout for binding labels
// and physical/logical lookup. It does not modify text-input or dead-key state.
// Refresh when displaying binding UI; a snapshot is not intended for per-frame polling.
// Call on the game goroutine. Headless mode or an unavailable native keymap
// returns ErrUnsupported; Key.String remains a stable physical-name fallback.
func (c *Context) KeyboardLayout() (input.KeyboardLayout, error) {
	if a, ok := c.app.(interface {
		KeyboardLayout() (input.KeyboardLayout, error)
	}); ok {
		return a.KeyboardLayout()
	}
	return input.KeyboardLayout{}, ErrUnsupported
}
