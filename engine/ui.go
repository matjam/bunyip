package engine

import (
	"errors"

	"github.com/matjam/bunyip/ui"
)

// NewUI creates an interface with clipboard and input-method placement
// connected to this context. Create it once during Init and reuse it
// while this graphics context is alive.
//
// A zero theme selects DarkTheme with the engine's shared Go Regular font
// at 14 view units. A custom theme keeps every setting; only a nil Font is
// filled with that default. The engine owns the default font, so do not
// destroy it separately. Device recovery requires creating a new interface.
func (c *Context) NewUI(theme ui.Theme) (*ui.Context, error) {
	if c.Gfx == nil {
		return nil, errors.New("bunyip: create UI: graphics context is unavailable")
	}
	useDefaultTheme := theme == (ui.Theme{})
	if theme.Font == nil {
		theme.Font = c.Gfx.DebugFont()
		if theme.Font == nil {
			return nil, errors.New("bunyip: create UI: default font is unavailable")
		}
	}
	if useDefaultTheme {
		theme = ui.DarkTheme(theme.Font)
	}
	u := ui.New(c.Gfx, theme)
	u.Clipboard = c
	u.OnTextInputRect = c.SetTextInputRect
	return u, nil
}
