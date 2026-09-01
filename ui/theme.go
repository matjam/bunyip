// Package ui is an immediate-mode interface toolkit drawn with gfx. Every
// frame the game rebuilds the interface by calling widget methods between
// Begin and End; widgets return what happened (a click, a changed value)
// and keep no state of their own beyond what the Theme and the caller
// provide. Colours, spacing and the font live in a Theme, so a game skins
// the toolkit by swapping one value.
package ui

import "github.com/matjam/bunyip/gfx"

// Theme is every colour and measure the widgets use.
type Theme struct {
	Font *gfx.Font

	Text         gfx.Color
	TextDim      gfx.Color
	Panel        gfx.Color
	PanelBorder  gfx.Color
	Title        gfx.Color
	Button       gfx.Color
	ButtonHover  gfx.Color
	ButtonActive gfx.Color
	Accent       gfx.Color
	Field        gfx.Color
	FieldBorder  gfx.Color
	Track        gfx.Color

	Padding     float32 // inside panels and buttons
	Spacing     float32 // between stacked widgets
	BorderWidth float32
	RowHeight   float32 // minimum widget height
}

// DarkTheme is a dark default; pass the font the interface should use.
func DarkTheme(font *gfx.Font) Theme {
	return Theme{
		Font:         font,
		Text:         gfx.RGB(230, 230, 235),
		TextDim:      gfx.RGB(150, 150, 165),
		Panel:        gfx.RGBA(30, 32, 40, 235),
		PanelBorder:  gfx.RGB(70, 74, 90),
		Title:        gfx.RGB(255, 210, 120),
		Button:       gfx.RGB(58, 62, 78),
		ButtonHover:  gfx.RGB(78, 84, 104),
		ButtonActive: gfx.RGB(120, 140, 200),
		Accent:       gfx.RGB(120, 190, 255),
		Field:        gfx.RGB(20, 22, 28),
		FieldBorder:  gfx.RGB(90, 94, 110),
		Track:        gfx.RGB(50, 52, 64),
		Padding:      8,
		Spacing:      6,
		BorderWidth:  1,
		RowHeight:    28,
	}
}

// LightTheme is a light default.
func LightTheme(font *gfx.Font) Theme {
	t := DarkTheme(font)
	t.Text = gfx.RGB(30, 30, 35)
	t.TextDim = gfx.RGB(110, 110, 120)
	t.Panel = gfx.RGBA(245, 245, 248, 240)
	t.PanelBorder = gfx.RGB(190, 190, 200)
	t.Title = gfx.RGB(120, 70, 0)
	t.Button = gfx.RGB(220, 222, 230)
	t.ButtonHover = gfx.RGB(200, 205, 220)
	t.ButtonActive = gfx.RGB(120, 150, 220)
	t.Accent = gfx.RGB(40, 110, 200)
	t.Field = gfx.RGB(255, 255, 255)
	t.FieldBorder = gfx.RGB(170, 170, 180)
	t.Track = gfx.RGB(205, 208, 218)
	return t
}
