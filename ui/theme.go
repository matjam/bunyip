// Package ui is an immediate-mode interface toolkit drawn with gfx. Every
// frame the game rebuilds the interface by calling widget methods inside
// Begin's closure, nesting containers (Panel, Row, Columns, ScrollArea)
// the same way; widgets return what happened (a click, a changed value)
// and keep no state of their own beyond what the Theme and the caller
// provide. Colours, spacing and the font live in a Theme, so a game
// restyles the toolkit by swapping one value; a Skin of textures inside
// the theme replaces the flat rectangles with drawn art.
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

	// Skin, when set, draws widgets from textures; colours above still
	// tint text and fill in for any slice the skin leaves nil.
	Skin *Skin
}

// Palette is the handful of colours a theme is derived from. Build one
// with a game's art in mind and pass it to FromPalette.
type Palette struct {
	Background gfx.Color // panels
	Surface    gfx.Color // buttons and fields sit on this
	Border     gfx.Color
	Text       gfx.Color
	TextDim    gfx.Color
	Accent     gfx.Color // sliders, focus, checks
	Title      gfx.Color
}

// FromPalette derives a full theme: buttons brighten as they are hovered
// and pressed, fields sit slightly below the background, tracks between.
func FromPalette(font *gfx.Font, p Palette) Theme {
	dark := luminance(p.Background) < 0.5
	lift := func(c gfx.Color, amount float32) gfx.Color {
		if dark {
			return shade(c, 1+amount)
		}
		return shade(c, 1-amount)
	}
	bg := p.Background
	if bg.A == 1 {
		bg.A = 0.93
	}
	return Theme{
		Font:         font,
		Text:         p.Text,
		TextDim:      p.TextDim,
		Panel:        bg,
		PanelBorder:  p.Border,
		Title:        p.Title,
		Button:       p.Surface,
		ButtonHover:  lift(p.Surface, 0.18),
		ButtonActive: p.Accent,
		Accent:       p.Accent,
		Field:        lift(p.Background, -0.15),
		FieldBorder:  p.Border,
		Track:        lift(p.Surface, -0.1),
		Padding:      8,
		Spacing:      6,
		BorderWidth:  1,
		RowHeight:    28,
	}
}

func shade(c gfx.Color, f float32) gfx.Color {
	return gfx.Color{R: min(1, c.R*f), G: min(1, c.G*f), B: min(1, c.B*f), A: c.A}
}

func luminance(c gfx.Color) float32 { return 0.2126*c.R + 0.7152*c.G + 0.0722*c.B }

// hex makes a colour from 0xRRGGBB.
func hex(v uint32) gfx.Color { return gfx.RGB(uint8(v>>16), uint8(v>>8), uint8(v)) }

// Palettes are the built-in colour schemes by name; see ThemeNames.
var Palettes = map[string]Palette{
	"dark": {Background: hex(0x1E2028), Surface: hex(0x3A3E4E), Border: hex(0x464A5A), Text: hex(0xE6E6EB),
		TextDim: hex(0x9696A5), Accent: hex(0x78BEFF), Title: hex(0xFFD278)},
	"light": {Background: hex(0xF5F5F8), Surface: hex(0xDCDEE6), Border: hex(0xBEBEC8), Text: hex(0x1E1E23),
		TextDim: hex(0x6E6E78), Accent: hex(0x286EC8), Title: hex(0x784600)},
	"nord": {Background: hex(0x2E3440), Surface: hex(0x3B4252), Border: hex(0x4C566A), Text: hex(0xECEFF4),
		TextDim: hex(0xD8DEE9), Accent: hex(0x88C0D0), Title: hex(0xEBCB8B)},
	"gruvbox": {Background: hex(0x282828), Surface: hex(0x3C3836), Border: hex(0x504945), Text: hex(0xEBDBB2),
		TextDim: hex(0xA89984), Accent: hex(0x83A598), Title: hex(0xFABD2F)},
	"solarized-dark": {Background: hex(0x002B36), Surface: hex(0x073642), Border: hex(0x586E75), Text: hex(0xEEE8D5),
		TextDim: hex(0x93A1A1), Accent: hex(0x268BD2), Title: hex(0xB58900)},
	"solarized-light": {Background: hex(0xFDF6E3), Surface: hex(0xEEE8D5), Border: hex(0x93A1A1), Text: hex(0x073642),
		TextDim: hex(0x586E75), Accent: hex(0x268BD2), Title: hex(0xB58900)},
	"sepia": {Background: hex(0xF4ECD8), Surface: hex(0xEADBC0), Border: hex(0xB9A582), Text: hex(0x4B3B2A),
		TextDim: hex(0x7A6650), Accent: hex(0xB05A2A), Title: hex(0x8B4513)},
	"high-contrast": {Background: hex(0x000000), Surface: hex(0x000000), Border: hex(0xFFFFFF), Text: hex(0xFFFFFF),
		TextDim: hex(0xC0C0C0), Accent: hex(0xFFFF00), Title: hex(0x00FFFF)},
}

// themeOrder lists the palettes in a sensible menu order.
var themeOrder = []string{"dark", "light", "nord", "gruvbox", "solarized-dark", "solarized-light", "sepia", "high-contrast"}

// ThemeNames returns the built-in theme names in menu order.
func ThemeNames() []string { return append([]string(nil), themeOrder...) }

// NamedTheme builds a built-in theme by name; ok is false for an unknown
// name.
func NamedTheme(name string, font *gfx.Font) (theme Theme, ok bool) {
	p, ok := Palettes[name]
	if !ok {
		return Theme{}, false
	}
	t := FromPalette(font, p)
	if name == "high-contrast" {
		t.BorderWidth = 2
	}
	return t, true
}

// DarkTheme is the dark default; pass the font the interface should use.
func DarkTheme(font *gfx.Font) Theme {
	t, _ := NamedTheme("dark", font)
	return t
}

// LightTheme is the light default.
func LightTheme(font *gfx.Font) Theme {
	t, _ := NamedTheme("light", font)
	return t
}
