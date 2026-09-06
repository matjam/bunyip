// Package ui is an immediate-mode interface toolkit drawn with gfx. To
// build the interface, call widget methods inside Begin's closure every
// frame, nesting containers (Panel, Window, Row, Columns, ScrollArea,
// Tabs, Table, Tree, MenuBar, Modal) the same way. Widgets return what
// happened (a click, a changed value). Values live in the game's
// variables and are passed by pointer; Context retains interaction
// state such as focus, caret, selection, undo history and scrolling.
// Games using engine.Context.NewUI get a default theme and shared font
// plus clipboard and input-method placement wiring. Use New directly
// when supplying those dependencies yourself.
//
// The widgets are Label and RichLabel, Button and IconButton, Checkbox,
// Radio and RadioGroup, Slider, IntSlider and Spinner, Progress,
// Dropdown, ListBox and ReorderableList, TextField and TextArea with
// selection, clipboard and undo, ColorPicker, Image, Tooltip and
// Separator. Anchored, Stretched and Split place rectangles from the
// view's size. DragSource and DropTarget carry a payload from one widget
// to another and draw a ghost under the pointer.
//
// A widget's identity comes from its label and its enclosing containers.
// Widgets with the same label in one container are told apart by the
// order they are called in, so a list of identical buttons works as long
// as the order is stable. Focus moves with Tab and the d-pad and
// activates with Enter, Space or a gamepad's A. A list, a row of tabs, a
// table's rows, a tree, a radio group and an open dropdown are each one
// Tab stop, and the arrows, Home, End, PageUp and PageDown move between
// the items inside. Sliders and spinners step with the left and right
// arrows. WantsMouse reports panel hover and dragging; WantsKeyboard
// reports text focus. Neither consumes input or gates the game's own
// controls. Accessible lists the last frame's widgets with roles and
// values for screen readers and tests.
//
// Colours, spacing and the font live in a Theme. To restyle the toolkit,
// swap that one value. The built-in themes come from NamedTheme. A Skin
// of nine-slice textures inside the theme replaces the flat rectangles
// with drawn art.
package ui

import "github.com/matjam/bunyip/gfx"

// Theme is every colour and measure the widgets use. Measures are in
// view units. Start with a built-in theme or FromPalette; the zero
// Theme has no font and is not usable for text widgets through New;
// engine.Context.NewUI expands it into the built-in dark theme.
type Theme struct {
	Font *gfx.Font // required default font; this Theme does not manage its lifetime
	// BoldFont, ItalicFont and BoldItalicFont serve RichLabel; nil falls
	// back to Font.
	BoldFont, ItalicFont, BoldItalicFont *gfx.Font

	Text         gfx.Color // ordinary labels and widget values
	TextDim      gfx.Color // secondary labels and placeholders
	Panel        gfx.Color // panel background
	PanelBorder  gfx.Color // panel outline and separators
	Title        gfx.Color // panel titles
	Button       gfx.Color // idle button fill
	ButtonHover  gfx.Color // hovered button fill
	ButtonActive gfx.Color // pressed button fill
	Accent       gfx.Color // selections, focus rings and filled controls
	Field        gfx.Color // editor background
	FieldBorder  gfx.Color // unfocused editor outline
	Track        gfx.Color // slider, progress and scrollbar background

	Padding     float32 // inside panels and buttons
	Spacing     float32 // between stacked widgets
	BorderWidth float32 // outline thickness in view units
	RowHeight   float32 // minimum widget height
	FocusWidth  float32 // the keyboard focus ring; zero means 2

	// Skin, when set, draws widgets from textures; colours above still
	// tint text and fill in for any slice the skin leaves nil.
	Skin *Skin
}

// Palette is the handful of colours a theme is derived from. Build one
// with a game's art in mind and pass it to FromPalette.
type Palette struct {
	Background gfx.Color // panels
	Surface    gfx.Color // buttons and fields sit on this
	Border     gfx.Color // panel and control outlines
	Text       gfx.Color // ordinary text
	TextDim    gfx.Color // secondary text and placeholders
	Accent     gfx.Color // sliders, focus, checks
	Title      gfx.Color // panel titles
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
		t.FocusWidth = 4
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
