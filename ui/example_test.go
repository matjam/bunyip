package ui_test

import (
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/ui"
)

// In a game these come from bunyip.Context: ctx.Gfx and ctx.Input. The
// font is created once in Init.
var (
	g    *gfx.Graphics
	in   *input.State
	font *gfx.Font
)

// Options a menu edits.
var (
	volume     float32 = 0.8
	fullscreen bool
	name       string
	quality    int
)

func ExampleContext_Begin() {
	u := ui.New(g, ui.DarkTheme(font))
	// Every frame, inside Draw:
	u.Begin(in, func() {
		u.Panel("Options", ui.Rect{X: 20, Y: 20, W: 300, H: 260}, func() {
			u.Slider("Volume", &volume, 0, 1)
			u.Checkbox("Fullscreen", &fullscreen)
			u.Dropdown("Quality", &quality, []string{"Low", "High"})
			u.TextField("Player name", &name)
			u.Row(2, func() {
				if u.Button("Apply") {
					// ...
				}
				u.Button("Cancel")
			})
		})
	})
}

func ExampleNamedTheme() {
	theme, _ := ui.NamedTheme("nord", font)
	theme.RowHeight = 32
	u := ui.New(g, theme)
	_ = u
}
