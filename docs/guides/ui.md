---
title: The interface
order: 10
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

## Navigating inside widgets

A `ListBox`, `ReorderableList`, a row of `Tabs`, a `Table`'s rows, a
`Tree`, a `RadioGroup` and an open `Dropdown` are each one Tab stop.
Inside one, the arrows move between the items (up and down through rows
and tree nodes, left and right along tabs), Home and End go to the ends,
PageUp and PageDown move a page of rows, and Enter or A activates the
item: selecting a row, switching a tab, choosing a radio or an option. A
row moved to inside a `ScrollArea` or list scrolls into view. Tab leaves
for the next widget, and Shift-Tab comes back to the item it left. The
d-pad's up and down step through everything in order, so a gamepad walks
a list row by row and out the other end; its left and right move along
tabs and radios. Right opens a focused tree node and Left closes it. A
focused `Slider`, `IntSlider` or `Spinner` steps with the left and right
arrows, the minus and plus keys, or the d-pad. `Table` returns the row
clicked or activated, or -1.

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

`ui.Rect` is `lin.Rect` and `ui.Slice` is `gfx.NineSlice`, so the values
the interface uses draw outside it too.

A theme may carry a `Skin` of nine-slice textures for panels, buttons in
three states, fields, checkboxes, tracks, fills, knobs and scroll thumbs.
Any slice left nil falls back to the theme's flat colours, so a skin can
start with just a button image. The gallery example draws a complete skin
procedurally; a game would load PNGs the same way.

## Text editing

`TextField` edits one line and `TextArea` several wrapped lines in a box.
Both have a caret, a selection made with Shift and the arrows or by
dragging, Home and End, word jumps with Ctrl or Cmd and the arrows,
select all, cut, copy and paste, and undo and redo (Ctrl or Cmd with Z,
Shift+Z or Y). Set `ui.Clipboard` to the engine's `Context` and the
clipboard is the system's.

## Layout and containers

`Anchored`, `Stretched` and `Split` compute rectangles from the view:
a panel pinned to a corner, one that grows with the window, a sidebar
beside a main area. `Tabs` switches between sets of widgets, `Table`
lays out rows of `Cell` values or any widget under a header, `Tree` and
`TreeOpen` nest collapsible sections, and `Window` is a panel the user
drags by its title and resizes by its corner. `MenuBar`, `Menu` and
`MenuItem` make a menu bar with drop-down lists, and `Modal` dims
everything and takes all input until its flag is cleared.

## More widgets

`Radio` and `RadioGroup`, `IntSlider` and `Spinner` for whole numbers,
`ListBox` for a scrolling selection, `ColorPicker` with a hue bar and a
saturation-value square, `Image` and `ImageRegion` for pictures,
`IconButton` for a button with a picture, and `RichLabel` for markup
with bold, italic, colour and links (set `Theme.BoldFont` and
`ItalicFont` for the faces), which returns the link clicked.

## Drag and drop

`DragSource` wraps any widgets in a region that can be picked up:
pressing on it and moving the pointer a few units starts a drag carrying
a payload, and a ghost of the label follows the pointer (set
`DragGhost` on the context to draw something else, such as the item's
icon). `DropTarget` makes the previous widget a place the drag can end,
and `DropTargetRect` does the same for a rectangle drawn without
widgets, such as a cell of an inventory grid. Both take an accept
function (nil takes anything), outline the target in the accent colour
while an accepted payload hovers, and report the payload on the frame
the pointer is released there. `Dragging` returns the payload in flight
so a target can show itself ready. Escape cancels a drag and nothing is
dropped.

```go
for _, item := range inventory {
	u.DragSource(item.Name, item, func() { u.Button(item.Name) })
}
u.Label("Equip")
if p, ok := u.DropTarget("equip", func(p any) bool { return p.(Item).Wearable }); ok {
	equip(p.(Item))
}
```

`ReorderableList` shows rows that drag to a new place, with a marker
where the row will land; it reports the row and its new index, and
`Move` applies the change to the slice. With a row focused, Ctrl or Cmd
with Up or Down moves it a step, and focus follows the row.

## Accessibility

`Accessible` returns the last frame's widgets in reading order with a
role, label, value, rectangle and state, so a game or a platform layer
can hand them to a screen reader or drive the interface from them.
Table rows, lists and their rows, drag sources and drop targets have
roles of their own, with the state showing a row being dragged or a
target ready for a drop. The high-contrast theme has thicker borders and
a wider focus ring, and `Theme.FocusWidth` sets the ring for any theme.
