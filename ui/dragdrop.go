package ui

import (
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

// dragThreshold is how far the pointer moves while pressed before a drag
// starts, in view units.
const dragThreshold = 4

// dragging is a drag in progress.
type dragging struct {
	source  widgetID
	label   string
	payload any
}

// reorderState is a ReorderableList row being dragged to a new place.
type reorderState struct {
	list  widgetID
	from  int
	label string
}

// bounds accumulates the rectangles widgets take while a DragSource body
// runs.
type bounds struct {
	r   Rect
	set bool
}

func (b *bounds) add(r Rect) {
	if !b.set {
		b.r, b.set = r, true
		return
	}
	x0, y0 := min(b.r.X, r.X), min(b.r.Y, r.Y)
	x1, y1 := max(b.r.X+b.r.W, r.X+r.W), max(b.r.Y+b.r.H, r.Y+r.H)
	b.r = Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// dragID is the active widget while a drag is in progress, so no widget
// under the pointer counts as held.
const dragID widgetID = 1

// DragSource makes the widgets body creates draggable: pressing on them
// and moving the pointer a few units starts a drag carrying payload,
// with a ghost of label (or what DragGhost draws) following the pointer
// until it is released on a DropTarget or Escape cancels it. It reports
// whether its payload is being dragged this frame.
func (c *Context) DragSource(label string, payload any, body func()) bool {
	id := c.id("drag:" + label)
	c.bounds = append(c.bounds, bounds{})
	body()
	b := c.bounds[len(c.bounds)-1]
	c.bounds = c.bounds[:len(c.bounds)-1]
	if len(c.bounds) > 0 && b.set {
		c.bounds[len(c.bounds)-1].add(b.r)
	}
	r := b.r
	over := b.set && r.Contains(lin.V2(c.mouseX, c.mouseY))
	if c.modal == 0 || c.inModal {
		if c.pressed && over && c.drag == nil && c.reorder == nil {
			c.pressSource = id
		}
		if c.down && c.pressSource == id && c.drag == nil && c.moved() {
			c.drag = &dragging{source: id, label: label, payload: payload}
			c.active = dragID
		}
	}
	dragged := c.drag != nil && c.drag.source == id
	if dragged {
		c.fill(r, c.Theme.Panel.WithAlpha(0.5))
		c.border(r, c.Theme.Accent)
	}
	c.noteAt("draggable", label, "", dragged, r, id)
	return dragged
}

// moved reports whether the pointer has travelled past the drag threshold
// since the button went down.
func (c *Context) moved() bool {
	dx, dy := c.mouseX-c.pressX, c.mouseY-c.pressY
	return dx*dx+dy*dy >= dragThreshold*dragThreshold
}

// Dragging returns the payload of the drag in progress, so a target can
// show itself ready while something it accepts is over it.
func (c *Context) Dragging() (payload any, ok bool) {
	if c.drag == nil {
		return nil, false
	}
	return c.drag.payload, true
}

// DropTarget makes the previous widget a place a drag can end. While an
// accepted payload hovers it is outlined in the accent colour; on the
// frame the pointer is released there it reports the payload dropped.
// accept may be nil to take anything.
func (c *Context) DropTarget(label string, accept func(payload any) bool) (payload any, dropped bool) {
	return c.dropTarget(label, c.lastRect, accept)
}

// DropTargetRect is DropTarget for an explicit rectangle, such as a cell
// of an inventory grid drawn without widgets.
func (c *Context) DropTargetRect(label string, r Rect, accept func(payload any) bool) (payload any, dropped bool) {
	return c.dropTarget(label, r, accept)
}

func (c *Context) dropTarget(label string, r Rect, accept func(payload any) bool) (payload any, dropped bool) {
	id := c.id("drop:" + label)
	ready := false
	if d := c.drag; d != nil && (c.modal == 0 || c.inModal) && r.Contains(lin.V2(c.mouseX, c.mouseY)) && (accept == nil || accept(d.payload)) {
		ready = true
		c.dropHover = true
		c.border(Rect{X: r.X - 1, Y: r.Y - 1, W: r.W + 2, H: r.H + 2}, c.Theme.Accent)
		if c.released {
			payload, dropped = d.payload, true
		}
	}
	c.noteAt("droptarget", label, "", ready, r, id)
	return payload, dropped
}

// drawGhost draws what follows the pointer during a drag, above every
// overlay.
func (c *Context) drawGhost() {
	label := ""
	var payload any
	switch {
	case c.drag != nil:
		label, payload = c.drag.label, c.drag.payload
	case c.reorder != nil:
		label = c.reorder.label
	default:
		return
	}
	if c.DragGhost != nil && c.drag != nil {
		c.DragGhost(label, payload, c.mouseX, c.mouseY)
		return
	}
	w, h := c.Theme.Font.Measure(label, gfx.TextOptions{})
	box := Rect{X: c.mouseX + 10, Y: c.mouseY + 10, W: w + 2*c.Theme.Padding, H: h + 2*c.Theme.Padding}
	c.fill(box, c.Theme.Panel.WithAlpha(0.85))
	c.border(box, c.Theme.Accent)
	c.text(label, box.X+c.Theme.Padding, box.Y+c.Theme.Padding, c.Theme.Text)
}

// ReorderableList shows items in a scrolling box of the given height
// where a row can be dragged to a new place, with a marker showing where
// it will land. It reports that the item at from should move to index
// to; the caller applies the move, for instance with Move. With a row
// focused, Ctrl or Cmd with Up or Down moves it a step.
func (c *Context) ReorderableList(label string, items []string, height float32) (from, to int, moved bool) {
	id := c.id("reorder:" + label)
	r := c.next(height)
	c.box(c.skin().Field, r, c.Theme.Field, c.Theme.FieldBorder)
	inner := Rect{X: r.X + 2, Y: r.Y + 2, W: r.W - 4, H: r.H - 4}
	pitch := c.Theme.RowHeight + c.Theme.Spacing
	from, to = -1, -1
	rs := c.reorder
	if rs != nil && rs.list != id {
		rs = nil
	}
	n := len(items)
	saved := c.beginGroup(id, navUpDown, max(int(inner.H/pitch), 1))
	c.ScrollArea("list:"+label, inner, float32(n)*pitch, func() {
		ids := make([]widgetID, n)
		focused := -1
		for i, item := range items {
			ids[i] = c.id("item:" + item)
			if ids[i] == c.navFocus {
				focused = i
			}
		}
		// Ctrl or Cmd with an arrow moves the focused row; focus follows it
		// since the row keeps its identity.
		if focused >= 0 && c.keyNav() && c.in.Mods()&(input.ModControl|input.ModSuper) != 0 {
			switch {
			case c.in.KeyPressed(input.KeyUp) && focused > 0:
				from, to, moved = focused, focused-1, true
			case c.in.KeyPressed(input.KeyDown) && focused < n-1:
				from, to, moved = focused, focused+1, true
			}
			if moved {
				c.navMovedNext = true
			}
		}
		first := c.currentPanel().cursor
		for i, item := range items {
			row := c.next(c.Theme.RowHeight)
			hover, _, _ := c.interact(ids[i], row)
			over := row.Contains(lin.V2(c.mouseX, c.mouseY))
			if rs == nil && c.pressed && over && c.drag == nil {
				c.pressSource = ids[i]
			}
			if rs == nil && c.down && c.pressSource == ids[i] && c.drag == nil && c.moved() {
				rs = &reorderState{list: id, from: i, label: item}
				c.reorder = rs
				c.active = dragID
				c.setFocus(ids[i])
			}
			switch {
			case rs != nil && rs.from == i:
				c.fill(row, c.Theme.Accent.WithAlpha(0.25))
			case hover:
				c.fill(row, c.Theme.ButtonHover)
			}
			_, h := c.Theme.Font.Measure(item, gfx.TextOptions{})
			c.text(item, row.X+c.Theme.Padding/2, row.Y+(row.H-h)/2, c.Theme.Text)
			c.noteAt("listitem", item, "", rs != nil && rs.from == i, row, ids[i])
		}
		if rs != nil {
			slot := int(math.Floor(float64((c.mouseY-first)/pitch + 0.5)))
			slot = max(0, min(slot, n))
			y := first + float32(slot)*pitch - c.Theme.Spacing/2
			c.fill(Rect{X: inner.X, Y: y - 1, W: inner.W - 10 - c.Theme.Spacing, H: 3}, c.Theme.Accent)
			if c.released {
				dest := slot
				if dest > rs.from {
					dest--
				}
				if dest != rs.from {
					from, to, moved = rs.from, dest, true
				}
				c.reorder = nil
			}
		}
	})
	c.endGroup(saved)
	c.noteAt("list", label, "", c.reorder != nil && c.reorder.list == id, r, id)
	return from, to, moved
}

// Move shifts the element at from so it ends up at index to, keeping the
// order of the rest: what a caller does with ReorderableList's result.
func Move[T any](s []T, from, to int) {
	if from < 0 || from >= len(s) || to < 0 || to >= len(s) || from == to {
		return
	}
	v := s[from]
	if from < to {
		copy(s[from:to], s[from+1:to+1])
	} else {
		copy(s[to+1:from+1], s[to:from])
	}
	s[to] = v
}
