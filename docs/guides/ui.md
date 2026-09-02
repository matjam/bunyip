---
title: The interface
order: 5
summary: immediate-mode widgets, themes and skins
---

The [ui](../pkg/ui.html) package is an immediate-mode toolkit: the game
rebuilds the interface every frame by calling widget methods, and each
widget returns what happened. There is no widget tree to create, update
or free.

## Frames and containers

Everything happens inside `Begin`, which finishes the frame when its body
returns. Containers take their contents as closures too:

```go
u.Begin(ctx.Input, func() {
	u.Panel("Options", ui.Rect{X: 20, Y: 20, W: 300, H: 260}, func() {
		u.Slider("Volume", &volume, 0, 1)
		u.Checkbox("Fullscreen", &fullscreen)
		u.Columns([]float32{2, 1}, func() {
			u.Dropdown("Quality", &quality, []string{"Low", "High"})
			u.Button("Apply")
		})
		u.ScrollArea("log", ui.Rect{X: 30, Y: 160, W: 280, H: 100}, 40*28, func() {
			for _, line := range log {
				u.Label(line)
			}
		})
	})
})
```

Widgets stack top to bottom inside a panel; `Row` and `Columns` lay a
few side by side. `Label` wraps to the width it is given.

## State and identity

Widgets keep no state of their own. Values live in your variables and are
passed by pointer; identity comes from the label and the enclosing panel,
so two identical labels in one panel are still distinct. A widget reports
`true` when something happened: a click, a changed value, a toggled
box.

`WantsMouse` and `WantsKeyboard` tell the game whether the interface
consumed the pointer or has a text field focused, so a click on a button
does not also fire the game's own click handler.

## Keyboard, gamepad and text

Tab and Shift-Tab move a focus ring between widgets; Enter, Space or a
gamepad's A button activate the focused one, and the d-pad moves focus.
Text fields take typed text, show the input method's composition
underlined, and report their rectangle through `OnTextInputRect` so the
platform can place candidate windows. Wire that to
`ctx.SetTextInputRect` once.

## Themes

A `Theme` holds every colour and measure. Build one from a `Palette` of
seven colours with `FromPalette`, or pick a built-in one:

```go
theme, _ := ui.NamedTheme("gruvbox", font) // dark, light, nord, gruvbox, solarized-dark, solarized-light, sepia, high-contrast
theme.RowHeight = 32
u.Theme = theme
```

Change the theme at any time; the next frame draws with it.

## Skins

A theme may carry a `Skin` of nine-slice textures for panels, buttons in
three states, fields, checkboxes, tracks, fills, knobs and scroll thumbs.
Any slice left nil falls back to the theme's flat colours, so a skin can
start with just a button image. The gallery example draws a complete skin
procedurally; a game would load PNGs the same way.
