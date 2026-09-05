---
title: The interface
group: Graphics
order: 5
summary: immediate-mode widgets, layout, keyboard and gamepad navigation, drag and drop, themes and skins
---

The [ui](../pkg/ui.html) package is an immediate-mode toolkit. The game
rebuilds the interface every frame by calling widget methods, and each
widget returns what happened. There is no widget tree to create,
update or free.

## Frames and containers

Build the interface inside `Begin`, which finishes the frame when its
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

`Anchored`, `Stretched` and `Split` compute rectangles from the view.
Use `Anchored` for a panel pinned to a corner, `Stretched` for one that
grows with the window, and `Split` for a sidebar beside a main area.
`Tabs` switches between sets of widgets, `Table`
lays out rows of `Cell` values (or any widget) under a header, `Tree`
and `TreeOpen` nest collapsible sections, and `Window` is a panel the
user drags by its title and resizes by its corner. `MenuBar`, `Menu` and
`MenuItem` make a menu bar with drop-down lists, and `Modal` dims
everything else and takes all input until its flag is cleared.

```go
view := ui.Rect{W: ctx.Width, H: ctx.Height}
tools, scene := ui.Split(view, 0.25, 8)
u.Begin(ctx.Input, func() {
	u.MenuBar(ui.Rect{X: 0, Y: 0, W: ctx.Width, H: 22}, func() {
		u.Menu("File", func() {
			if u.MenuItem("Quit") {
				ctx.Quit()
			}
		})
	})
	u.Panel("Tools", tools, func() { u.Button("Brush") })
	u.Panel("Scene", ui.Stretched(scene, 8, 8, 8, 8), func() {
		u.Tabs([]string{"World", "Entities"}, &g.tab)
	})
	// g.inspector is a ui.Rect the game keeps; the user moves it.
	u.Window("Inspector", &g.inspector, func() { u.Label("Drag my title") })
	u.Panel("HUD", ui.Anchored(view, ui.TopRight, 160, 48, 12), func() {
		u.Label("Score 0")
	})
})
```

## Widgets

Besides the labels, buttons, checkboxes, sliders, progress bars and
dropdowns above there are `Radio` and `RadioGroup`; `IntSlider` and
`Spinner` for whole numbers; `ListBox` for a scrolling selection;
`ColorPicker` with a hue bar and a saturation-value square;
`CurveEditor` for a value that changes over a span; `Image`,
`ImageRegion` and `IconButton` for pictures; `Tooltip` and `Separator`;
and `RichLabel` for markup with bold, italic, colour and links, which
returns the link that was clicked (set `Theme.BoldFont` and
`ItalicFont` for the faces).

`CurveEditor(label, &points, lo, hi, height)` draws a curve as a graph
and edits it in place: drag a point to move it, click an empty part of
the graph to add one, right-click a point to remove it. The points are
`lin.Vec2` pairs kept in increasing x, x running 0 to 1 across the graph
and y from `lo` to `hi` up it; the first and last keep their x so the
curve always spans the range. `particle.Curve` converts both ways with
`Points` and `CurveOf`, which is how the gallery's particle editor tunes
size and alpha over a particle's life.

```go
u.Panel("Settings", ui.Rect{X: 20, Y: 20, W: 320, H: 300}, func() {
	u.Label("Long labels wrap to the width of the panel.")
	u.Slider("Volume", &g.volume, 0, 1)
	u.IntSlider("Lives", &g.lives, 1, 9)
	u.TextField("Player name", &g.name)
	u.Dropdown("Quality", &g.quality, []string{"Low", "Medium", "High"})
	u.ListBox("Maps", 56, []string{"Archipelago", "Pangaea"}, &g.mapIdx)
	u.Separator()
	if u.Button("Apply") {
		g.save()
	}
	u.Tooltip("Writes the settings file.") // describes the widget above
	if link := u.RichLabel("[b]Bold[/b] and a [link=docs]link[/link]."); link != "" {
		g.open(link)
	}
})
```

`TextField` edits one line and `TextArea` several wrapped lines. Both
have a caret, a selection made with Shift and the arrows or by dragging,
Home and End, word jumps with Ctrl or Cmd and the arrows, select all,
cut, copy, paste, and undo and redo (Ctrl or Cmd with Z, Shift+Z or Y).
To use the system clipboard, set the context's `Clipboard` to the
engine's `Context`. Text fields show the input method's
composition underlined and report their rectangle through
`OnTextInputRect` so the platform can place candidate windows; wire
that to `ctx.SetTextInputRect` once.

## State and identity

Widgets keep no state of their own. Values live in the game's variables
and are passed by pointer. A widget's identity comes from its label and
the enclosing containers, and widgets with the same label in one
container are told apart by the order they are called in, so a list of
identical buttons works as long as the order is stable. A widget
returns `true` when something happened to it, such as a click, a
changed value or a toggled box.

```go
u.Begin(ctx.Input, func() {
	u.Panel("Audio", ui.Rect{X: 20, Y: 20, W: 260, H: 120}, func() {
		// The value lives in the game; the widget reports the change.
		if u.Slider("Music", &g.volume, 0, 1) {
			ctx.Audio.SetMasterVolume(g.volume)
		}
		if u.Checkbox("Fullscreen", &g.fullscreen) {
			ctx.SetFullscreen(g.fullscreen)
		}
	})
})
```

`WantsMouse` and `WantsKeyboard` tell the game whether the interface
consumed the pointer or has a text field focused, so a click on a button
does not also fire the game's own click handler.

```go
if !u.WantsMouse() && ctx.Input.MousePressed(input.MouseLeft) {
	p := ctx.Input.MousePos()
	g.shootAt(p.X, p.Y)
}
if !u.WantsKeyboard() && ctx.Input.KeyPressed(input.KeyEscape) {
	g.pause()
}
```

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
focus then continues to the next widget. Left and right move along tabs
and radios.
Right opens a focused tree node and Left closes it. A focused `Slider`,
`IntSlider` or `Spinner` steps with the left and right arrows, the
minus and plus keys, or the d-pad. `Table` returns the row clicked or
activated, or -1.

```go
u.Theme.FocusWidth = 3 // the ring Tab draws; zero means 2
u.Panel("Party", ui.Rect{X: 20, Y: 20, W: 300, H: 200}, func() {
	// The table is one Tab stop; the arrows walk its rows.
	if row := u.Table([]string{"Name", "HP"}, []float32{2, 1}, len(g.party),
		func(row, col int) {
			if col == 0 {
				u.Cell(g.party[row].Name)
			} else {
				u.Cell(strconv.Itoa(g.party[row].HP))
			}
		}); row >= 0 {
		g.selected = row // clicked, or Enter or A on the focused row
	}
	u.RadioGroup(&g.difficulty, []string{"Easy", "Hard"})
})
```

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
the pointer is released there. `Dragging` returns the payload of the
drag in progress, so the game can draw a target differently while a
drag is under way. Escape cancels a drag and nothing is dropped.

```go
u.DragGhost = func(label string, payload any, x, y float32) {
	ctx.Gfx.DrawText(g.font, label, x, y, gfx.RGB(255, 255, 255))
}
for _, item := range inventory {
	u.DragSource(item.Name, item, func() { u.Button(item.Name) })
}
u.Label("Equip")
if _, ok := u.Dragging(); ok {
	u.Label("Drop it here")
}
if p, ok := u.DropTarget("equip", func(p any) bool { return p.(Item).Wearable }); ok {
	equip(p.(Item))
}
```

`ReorderableList` shows rows that can be dragged to a new place, with a
marker where the row will land. It reports the row and its new index,
and `Move` applies the change to the slice. With a row focused, Ctrl or
Cmd with Up or Down moves it one step, and focus follows the row.

```go
if from, to, ok := u.ReorderableList("Turn order", g.order, 100); ok {
	ui.Move(g.order, from, to)
}
```

## Themes

A `Theme` holds every colour and measure. Build one from a `Palette` of
seven colours with `FromPalette`, or pick a built-in one:

```go
theme, _ := ui.NamedTheme("gruvbox", font) // dark, light, nord, gruvbox, solarized-dark, solarized-light, sepia, high-contrast
theme.RowHeight = 32
u.Theme = theme
```

To match a game's own art, pass its seven colours to `FromPalette` and
leave the other measures at their defaults:

```go
u.Theme = ui.FromPalette(font, ui.Palette{
	Background: gfx.Hex(0x2b1d2e), // panels
	Surface:    gfx.Hex(0x3d2b42), // buttons and fields
	Border:     gfx.Hex(0x6b4f72),
	Text:       gfx.Hex(0xf2e8f4),
	TextDim:    gfx.Hex(0xa892ad),
	Accent:     gfx.Hex(0xffb454), // sliders, focus, checks
	Title:      gfx.Hex(0xffd9a0),
})
```

The theme can change at any time; the next frame draws with it. The
high-contrast theme has thicker borders and a wider focus ring, and
`Theme.FocusWidth` sets the ring for any theme. `ThemeNames` lists the
built-in ones, for a dropdown in a settings panel.

## Skins

`ui.Rect` is `lin.Rect` and `ui.Slice` is `gfx.NineSlice`, so the game
can draw with the same values outside the interface.

A theme may carry a `Skin` of nine-slice textures for panels, buttons
in three states, fields, checkboxes, tracks, fills, knobs and scroll
thumbs. Any slice left nil falls back to the theme's flat colours, so a
skin can start with one button image and grow from there. The borders
of each slice are in texture pixels.

```go
btn, err := ctx.Gfx.NewTexture(buttonPNG, gfx.TextureOptions{Linear: true, NoMipmaps: true})
if err != nil {
	return err
}
panel, err := ctx.Gfx.NewTexture(panelPNG, gfx.TextureOptions{Linear: true, NoMipmaps: true})
if err != nil {
	return err
}
u.Theme.Skin = &ui.Skin{
	Button: &ui.Slice{Tex: btn, Left: 11, Top: 11, Right: 11, Bottom: 11},
	Panel:  &ui.Slice{Tex: panel, Left: 14, Top: 14, Right: 14, Bottom: 14},
}
```

The textures are the game's to destroy in `Shutdown`. The gallery
example draws a complete skin procedurally; a game would load PNGs the
same way.

## Accessibility

`Accessible` returns the last frame's widgets in reading order with a
role, label, value, rectangle and state, so a game or a platform layer
can hand them to a screen reader or drive the interface from them in a
test. Table rows, lists and their rows, drag sources and drop targets
have roles of their own, and the state shows a row being dragged or a
target ready for a drop. The engine does not yet connect this tree to
the operating system's screen reader.

```go
u.Begin(ctx.Input, func() { g.buildInterface() })
for _, n := range u.Accessible() {
	if n.Focused { // "slider Volume 0.80"
		g.speak(n.Role + " " + n.Label + " " + n.Value)
	}
	if n.Role == "listitem" && n.State {
		g.speak(n.Label + " picked up")
	}
}
```

The list covers the frame that was built most recently, so read it
after `Begin` returns. A
test finds a widget by role and label and clicks the centre of its
`Rect`, which is how the package's own navigation tests work.
