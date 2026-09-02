---
title: The interface
order: 10
summary: immediate-mode widgets, layout, keyboard and gamepad navigation, drag and drop, themes and skins
---

The [ui](../pkg/ui.html) package is an immediate-mode toolkit. The game
rebuilds the interface every frame by calling widget methods, and each
widget returns what happened. There is no widget tree to create,
update or free.

## Frames and containers

Everything happens inside `Begin`, which finishes the frame when its
body returns. Containers take their contents as closures too:

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

`Anchored`, `Stretched` and `Split` compute rectangles from the view: a
panel pinned to a corner, one that grows with the window, a sidebar
beside a main area. `Tabs` switches between sets of widgets, `Table`
lays out rows of `Cell` values (or any widget) under a header, `Tree`
and `TreeOpen` nest collapsible sections, and `Window` is a panel the
user drags by its title and resizes by its corner. `MenuBar`, `Menu` and
`MenuItem` make a menu bar with drop-down lists, and `Modal` dims
everything else and takes all input until its flag is cleared.

## Widgets

Besides the labels, buttons, checkboxes, sliders, progress bars and
dropdowns above there are `Radio` and `RadioGroup`; `IntSlider` and
`Spinner` for whole numbers; `ListBox` for a scrolling selection;
`ColorPicker` with a hue bar and a saturation-value square; `Image`,
`ImageRegion` and `IconButton` for pictures; `Tooltip` and `Separator`;
and `RichLabel` for markup with bold, italic, colour and links, which
returns the link that was clicked (set `Theme.BoldFont` and
`ItalicFont` for the faces).

`TextField` edits one line and `TextArea` several wrapped lines. Both
have a caret, a selection made with Shift and the arrows or by dragging,
Home and End, word jumps with Ctrl or Cmd and the arrows, select all,
cut, copy, paste, and undo and redo (Ctrl or Cmd with Z, Shift+Z or Y).
Set the context's `Clipboard` to the engine's `Context` and the
clipboard is the system's. Text fields show the input method's
composition underlined and report their rectangle through
`OnTextInputRect` so the platform can place candidate windows; wire
that to `ctx.SetTextInputRect` once.

## State and identity

Widgets keep no state of their own. Values live in the game's variables
and are passed by pointer. A widget's identity comes from its label and
the enclosing containers, and widgets with the same label in one
container are told apart by the order they are called in, so a list of
identical buttons works as long as the order is stable. A widget
reports `true` when something happened: a click, a changed value, a
toggled box.

`WantsMouse` and `WantsKeyboard` tell the game whether the interface
consumed the pointer or has a text field focused, so a click on a button
does not also fire the game's own click handler.

## Keyboard and gamepad

Tab and Shift-Tab move a focus ring between widgets; Enter, Space or a
gamepad's A button activate the focused one, and the d-pad's up and
down move focus.

A `ListBox`, a `ReorderableList`, a row of `Tabs`, a `Table`'s rows, a
`Tree`, a `RadioGroup` and an open `Dropdown` are each one Tab stop.
Inside one, the arrows move between the items (up and down through rows
and tree nodes, left and right along tabs and radios), Home and End go
to the ends, PageUp and PageDown move a page of rows, and Enter or A
activates the item. A row that focus moves to inside a `ScrollArea` or
a list scrolls into view. Tab leaves for the next widget, and Shift-Tab
comes back to the item it left. On a gamepad, up and down on the d-pad
step through everything in order, so a list is walked row by row and
left out the other end; left and right move along tabs and radios.
Right opens a focused tree node and Left closes it. A focused `Slider`,
`IntSlider` or `Spinner` steps with the left and right arrows, the
minus and plus keys, or the d-pad. `Table` returns the row clicked or
activated, or -1.

## Drag and drop

`DragSource` wraps widgets in a region that can be picked up. Pressing
on it and moving the pointer a few units starts a drag carrying a
payload, and a ghost of the label follows the pointer; set `DragGhost`
on the context to draw something else, such as the item's icon.
`DropTarget` makes the previous widget a place the drag can end, and
`DropTargetRect` does the same for a rectangle drawn without widgets,
such as a cell of an inventory grid. Both take an accept function (nil
accepts anything), outline the target in the accent colour while an
accepted payload hovers over it, and report the payload on the frame
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

`ReorderableList` shows rows that can be dragged to a new place, with a
marker where the row will land. It reports the row and its new index,
and `Move` applies the change to the slice. With a row focused, Ctrl or
Cmd with Up or Down moves it one step, and focus follows the row.

## Themes

A `Theme` holds every colour and measure. Build one from a `Palette` of
seven colours with `FromPalette`, or pick a built-in one:

```go
theme, _ := ui.NamedTheme("gruvbox", font) // dark, light, nord, gruvbox, solarized-dark, solarized-light, sepia, high-contrast
theme.RowHeight = 32
u.Theme = theme
```

The theme can change at any time; the next frame draws with it. The
high-contrast theme has thicker borders and a wider focus ring, and
`Theme.FocusWidth` sets the ring for any theme.

## Skins

`ui.Rect` is `lin.Rect` and `ui.Slice` is `gfx.NineSlice`, so the values
the interface uses draw outside it too.

A theme may carry a `Skin` of nine-slice textures for panels, buttons
in three states, fields, checkboxes, tracks, fills, knobs and scroll
thumbs. Any slice left nil falls back to the theme's flat colours, so a
skin can start with just a button image. The gallery example draws a
complete skin procedurally; a game would load PNGs the same way.

## Accessibility

`Accessible` returns the last frame's widgets in reading order with a
role, label, value, rectangle and state, so a game or a platform layer
can hand them to a screen reader or drive the interface from them in a
test. Table rows, lists and their rows, drag sources and drop targets
have roles of their own, and the state shows a row being dragged or a
target ready for a drop. The engine does not yet connect this tree to
the operating system's screen reader.
