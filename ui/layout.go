package ui

import (
	"strings"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

// Anchor names a point of an area a rectangle hangs from.
type Anchor uint8

const (
	TopLeft Anchor = iota
	Top
	TopRight
	Left
	Center
	Right
	BottomLeft
	Bottom
	BottomRight
)

// Anchored places a w by h rectangle at an anchor of area, margin units
// in from the edges: a panel that stays in the corner whatever the
// window's size.
func Anchored(area Rect, a Anchor, w, h, margin float32) Rect {
	x, y := area.X+margin, area.Y+margin
	switch a {
	case Top, Center, Bottom:
		x = area.X + (area.W-w)/2
	case TopRight, Right, BottomRight:
		x = area.X + area.W - w - margin
	}
	switch a {
	case Left, Center, Right:
		y = area.Y + (area.H-h)/2
	case BottomLeft, Bottom, BottomRight:
		y = area.Y + area.H - h - margin
	}
	return Rect{X: x, Y: y, W: w, H: h}
}

// Stretched fills area with margins on every side, for a panel that
// grows with the window.
func Stretched(area Rect, left, top, right, bottom float32) Rect {
	return Rect{X: area.X + left, Y: area.Y + top, W: area.W - left - right, H: area.H - top - bottom}
}

// Split cuts area into two along its longer side at fraction t, for a
// sidebar and a main view.
func Split(area Rect, t float32, gap float32) (first, second Rect) {
	if area.W >= area.H {
		w := (area.W - gap) * t
		return Rect{X: area.X, Y: area.Y, W: w, H: area.H}, Rect{X: area.X + w + gap, Y: area.Y, W: area.W - w - gap, H: area.H}
	}
	h := (area.H - gap) * t
	return Rect{X: area.X, Y: area.Y, W: area.W, H: h}, Rect{X: area.X, Y: area.Y + h + gap, W: area.W, H: area.H - h - gap}
}

// Tabs draws a row of tabs and keeps *selected on the clicked one,
// reporting a change; the widgets that follow belong to that tab. The
// row is one Tab stop: the left and right arrows move along it and
// Enter selects.
func (c *Context) Tabs(labels []string, selected *int) bool {
	changed := false
	saved := c.beginGroup(c.id("tabs:"+strings.Join(labels, "|")), navLeftRight, len(labels))
	defer c.endGroup(saved)
	c.Row(len(labels), func() {
		for i, l := range labels {
			id := c.id("tab:" + l)
			r := c.next(c.Theme.RowHeight)
			hover, _, clicked := c.interact(id, r)
			if clicked && *selected != i {
				*selected = i
				changed = true
			}
			col := c.Theme.Button
			if hover {
				col = c.Theme.ButtonHover
			}
			if *selected == i {
				col = c.Theme.Panel
			}
			c.fill(r, col)
			c.border(r, c.Theme.PanelBorder)
			if *selected == i {
				c.fill(Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, c.Theme.Accent)
			}
			c.textCentred(l, r, c.Theme.Text)
			c.note("tab", l, "", *selected == i)
		}
	})
	return changed
}

// Table lays out a header row and rows of cells in columns; weights give
// the columns' relative widths (nil for equal) and cell draws each cell
// with the usual widgets. Rows alternate in shade. The rows are one Tab
// stop that the arrows move through; it returns the row clicked or
// activated with Enter this frame, or -1. Widgets inside cells are
// their own Tab stops.
func (c *Context) Table(columns []string, weights []float32, rows int, cell func(row, col int)) (clicked int) {
	clicked = -1
	if weights == nil {
		weights = make([]float32, len(columns))
		for i := range weights {
			weights[i] = 1
		}
	}
	c.Columns(weights, func() {
		for _, col := range columns {
			r := c.next(c.Theme.RowHeight)
			c.fill(r, c.Theme.Track)
			_, h := c.Theme.Font.Measure(col, gfx.TextOptions{})
			c.text(col, r.X+c.Theme.Padding/2, r.Y+(r.H-h)/2, c.Theme.Title)
		}
	})
	id := c.id("table:" + strings.Join(columns, "|"))
	for row := range rows {
		saved := c.beginGroup(id, navUpDown, 10)
		if p := c.currentPanel(); p != nil {
			r := Rect{X: p.rect.X + c.Theme.Padding, Y: p.cursor - c.Theme.Spacing/2, W: p.rect.W - 2*c.Theme.Padding, H: c.Theme.RowHeight + c.Theme.Spacing}
			hover, _, click := c.interact(id+widgetID(row+1), r)
			if click {
				clicked = row
			}
			switch {
			case hover:
				c.fill(r, c.Theme.ButtonHover.WithAlpha(0.5))
			case row%2 == 1:
				c.fill(r, c.Theme.Field.WithAlpha(0.35))
			}
			c.noteAt("row", c.formatInt(row), "", false, r, id+widgetID(row+1))
		}
		c.endGroup(saved)
		c.Columns(weights, func() {
			for col := range columns {
				cell(row, col)
			}
		})
	}
	return clicked
}

// Cell draws a plain text cell, for tables of values.
func (c *Context) Cell(text string) {
	r := c.next(c.Theme.RowHeight)
	_, h := c.Theme.Font.Measure(text, gfx.TextOptions{})
	c.text(text, r.X+c.Theme.Padding/2, r.Y+(r.H-h)/2, c.Theme.Text)
}

// Tree draws a collapsible node: a triangle and a label that toggle on
// click, with body's widgets indented beneath while open. Nodes start
// closed; TreeOpen starts them open.
func (c *Context) Tree(label string, body func()) { c.tree(label, false, body) }

// TreeOpen is Tree starting open.
func (c *Context) TreeOpen(label string, body func()) { c.tree(label, true, body) }

// tree draws one node. The outermost node of a tree makes the tree one
// Tab stop whose nodes (and the widgets inside them) the up and down
// arrows move through; Right opens the focused node and Left closes it.
func (c *Context) tree(label string, openAtFirst bool, body func()) {
	id := c.id("tree:" + label)
	open, seen := c.expanded[id]
	if !seen {
		open = openAtFirst
		c.expanded[id] = open
	}
	saved := c.group
	if saved.id == 0 {
		c.group = navGroup{id: id, keys: navUpDown, page: 10}
		defer c.endGroup(saved)
	}
	r := c.next(c.Theme.RowHeight)
	hover, _, clicked := c.interact(id, r)
	if clicked {
		open = !open
		c.expanded[id] = open
	}
	if c.navFocus == id && c.keyNav() {
		if c.in.KeyPressed(input.KeyRight) && !open || c.in.KeyPressed(input.KeyLeft) && open {
			open = !open
			c.expanded[id] = open
		}
	}
	if hover {
		c.fill(r, c.Theme.ButtonHover.WithAlpha(0.5))
	}
	c.note("tree", label, "", open)
	mark := ">"
	if open {
		mark = "v"
	}
	_, h := c.Theme.Font.Measure(label, gfx.TextOptions{})
	c.text(mark, r.X+2, r.Y+(r.H-h)/2, c.Theme.TextDim)
	c.text(label, r.X+16, r.Y+(r.H-h)/2, c.Theme.Text)
	if !open {
		return
	}
	// Body widgets sit in a narrower panel indented under the node.
	p := c.currentPanel()
	if p == nil {
		body()
		return
	}
	const indent = 16
	sub := &panel{id: id, rect: Rect{X: p.rect.X + indent, Y: p.cursor, W: p.rect.W - indent, H: p.rect.H}, cursor: p.cursor}
	c.panels = append(c.panels, sub)
	body()
	c.endRow(sub)
	c.panels = c.panels[:len(c.panels)-1]
	p.cursor = sub.cursor
}

// MenuBar lays a row of menus across r; body calls Menu for each.
func (c *Context) MenuBar(r Rect, body func()) {
	p := &panel{id: c.id("menubar"), rect: r, cursor: r.Y}
	c.panels = append(c.panels, p)
	c.frameRects = append(c.frameRects, r)
	c.fill(r, c.Theme.Panel)
	c.fill(Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, c.Theme.PanelBorder)
	c.menuX = r.X + c.Theme.Padding
	c.menuBar = r
	body()
	c.panels = c.panels[:len(c.panels)-1]
}

// Menu is one heading in a MenuBar; clicking it opens a list beneath in
// which body's MenuItem calls appear. The list closes when an item is
// chosen or the pointer clicks elsewhere.
func (c *Context) Menu(label string, body func()) {
	id := c.id("menu:" + label)
	w, h := c.Theme.Font.Measure(label, gfx.TextOptions{})
	r := Rect{X: c.menuX, Y: c.menuBar.Y, W: w + 2*c.Theme.Padding, H: c.menuBar.H}
	c.menuX += r.W
	hover, _, clicked := c.interact(id, r)
	if clicked {
		if c.openMenu == id {
			c.openMenu = 0
		} else {
			c.openMenu = id
		}
	} else if hover && c.openMenu != 0 && c.openMenu != id {
		c.openMenu = id // sliding along an open bar switches menus
	}
	if hover || c.openMenu == id {
		c.fill(r, c.Theme.ButtonHover)
	}
	c.text(label, r.X+c.Theme.Padding, r.Y+(r.H-h)/2, c.Theme.Text)
	c.note("menu", label, "", c.openMenu == id)
	if c.openMenu != id {
		return
	}
	c.deferred = append(c.deferred, func() {
		list := Rect{X: r.X, Y: r.Y + r.H, W: max(r.W, 160), H: 0}
		// Lay the items out once to find the height, then draw.
		p := &panel{id: id, rect: Rect{X: list.X, Y: list.Y, W: list.W, H: 1000}, cursor: list.Y + c.Theme.Spacing}
		c.menuItems = c.menuItems[:0]
		c.panels = append(c.panels, p)
		c.menuMeasure = true
		body()
		c.menuMeasure = false
		c.panels = c.panels[:len(c.panels)-1]
		list.H = p.cursor - list.Y + c.Theme.Spacing
		c.frameRects = append(c.frameRects, list)
		c.box(c.skin().Panel, list, c.Theme.Panel, c.Theme.PanelBorder)
		p = &panel{id: id, rect: Rect{X: list.X, Y: list.Y, W: list.W, H: list.H}, cursor: list.Y + c.Theme.Spacing}
		c.panels = append(c.panels, p)
		body()
		c.panels = c.panels[:len(c.panels)-1]
		if c.pressed && !list.Contains(lin.V2(c.mouseX, c.mouseY)) && !c.menuBar.Contains(lin.V2(c.mouseX, c.mouseY)) {
			c.openMenu = 0
		}
	})
}

// MenuItem is a choice in an open Menu; it reports a click and closes
// the menu.
func (c *Context) MenuItem(label string) bool {
	id := c.id("item:" + label)
	r := c.next(c.Theme.RowHeight)
	if c.menuMeasure {
		return false
	}
	hover, _, clicked := c.interact(id, r)
	if hover {
		c.fill(r, c.Theme.ButtonHover)
	}
	_, h := c.Theme.Font.Measure(label, gfx.TextOptions{})
	c.text(label, r.X+c.Theme.Padding, r.Y+(r.H-h)/2, c.Theme.Text)
	c.note("menuitem", label, "", false)
	if clicked {
		c.openMenu = 0
	}
	return clicked
}

// Modal dims everything and draws a panel above it that alone takes
// input while open; body builds its contents. Pass the same open flag
// each frame and clear it to close.
func (c *Context) Modal(title string, r Rect, open *bool, body func()) {
	if open == nil || !*open {
		return
	}
	id := c.id("modal:" + title)
	c.modal = id
	c.deferred = append(c.deferred, func() {
		vw, vh := c.g.View()
		c.fill(Rect{X: 0, Y: 0, W: vw, H: vh}, gfx.Color{A: 0.45})
		c.frameRects = append(c.frameRects, Rect{X: 0, Y: 0, W: vw, H: vh})
		c.inModal = true
		c.Panel(title, r, body)
		c.inModal = false
	})
}

// Window is a panel the user can move by its title bar and resize by
// its bottom-right corner; the rectangle it updates is the caller's.
func (c *Context) Window(title string, r *Rect, body func()) {
	id := c.id("window:" + title)
	bar := Rect{X: r.X, Y: r.Y, W: r.W, H: c.Theme.RowHeight}
	grip := Rect{X: r.X + r.W - 14, Y: r.Y + r.H - 14, W: 14, H: 14}
	_, held, _ := c.interact(id, bar)
	drag := c.drags[id]
	if held {
		if drag == nil {
			drag = &dragState{dx: c.mouseX - r.X, dy: c.mouseY - r.Y}
			c.drags[id] = drag
		}
		r.X, r.Y = c.mouseX-drag.dx, c.mouseY-drag.dy
	} else {
		delete(c.drags, id)
	}
	gid := id + 1
	_, gheld, _ := c.interact(gid, grip)
	if gheld {
		r.W = max(c.mouseX-r.X+7, 3*c.Theme.RowHeight)
		r.H = max(c.mouseY-r.Y+7, 2*c.Theme.RowHeight)
	}
	c.Panel(title, *r, body)
	c.fill(Rect{X: r.X, Y: r.Y + bar.H, W: r.W, H: 1}, c.Theme.PanelBorder)
	c.text("//", grip.X+2, grip.Y-2, c.Theme.TextDim)
	c.note("window", title, "", false)
}

type dragState struct{ dx, dy float32 }
