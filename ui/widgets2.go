package ui

import (
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

// ScrollArea lays out its contents inside r with a vertical scrollbar,
// clipping what does not fit; contentHeight is the total height the
// contents need. The scroll position is kept per label across frames.
func (c *Context) ScrollArea(label string, r Rect, contentHeight float32, contents func()) {
	id := c.id("scroll:" + label)
	st := c.scroll[id]
	if st == nil {
		st = &scrollState{}
		c.scroll[id] = st
	}
	maxScroll := max(contentHeight-r.H, 0)
	over := r.Contains(lin.V2(c.mouseX, c.mouseY))
	if over {
		_, dy := c.in.Scroll()
		st.offset -= float32(dy) * 24
		c.nextHot = id // claim the pointer so widgets below do not fight the wheel
	}
	barW := float32(10)
	if maxScroll > 0 {
		track := Rect{X: r.X + r.W - barW, Y: r.Y, W: barW, H: r.H}
		thumbH := max(r.H*r.H/contentHeight, 20)
		thumbY := r.Y + (r.H-thumbH)*(st.offset/maxScroll)
		thumb := Rect{X: track.X, Y: thumbY, W: barW, H: thumbH}
		c.noFocus = true // the thumb is for the pointer; keys move the contents
		hover, held, _ := c.interact(id, thumb)
		c.noFocus = false
		if held {
			if !st.dragging {
				st.dragging, st.dragStart, st.dragOffset = true, c.mouseY, st.offset
			}
			st.offset = st.dragOffset + (c.mouseY-st.dragStart)*(maxScroll/(r.H-thumbH))
		} else {
			st.dragging = false
		}
		sk := c.skin()
		c.box(sk.Track, track, c.Theme.Track, gfx.Color{})
		col := c.Theme.Button
		if hover || held {
			col = c.Theme.ButtonHover
		}
		c.box(or(sk.Thumb, sk.Knob), thumb, col, gfx.Color{})
	}
	inner := Rect{X: r.X, Y: r.Y, W: r.W - barW - c.Theme.Spacing, H: r.H}
	// A row the keys or d-pad moved focus to is brought into view, from
	// where it was last frame.
	if c.navMoved {
		if f, ok := c.lastFocusable(c.navFocus); ok && f.scroll == id {
			st.offset += scrollDelta(f.rect, inner)
		}
	}
	st.offset = max(0, min(st.offset, maxScroll))
	c.g.PushClip(inner)
	c.frameRects = append(c.frameRects, r)
	p := &panel{id: id, rect: Rect{X: inner.X, Y: inner.Y - st.offset, W: inner.W, H: contentHeight}, cursor: inner.Y - st.offset}
	c.panels = append(c.panels, p)
	c.clipDepth++
	outer := c.scrollID
	c.scrollID = id
	contents()
	c.scrollID = outer
	c.clipDepth--
	c.panels = c.panels[:len(c.panels)-1]
	c.g.PopClip()
	// Where the row moved this frame, the correction lands next frame.
	if c.navMoved {
		if f, ok := c.thisFocusable(c.navFocus); ok && f.scroll == id {
			st.offset = max(0, min(st.offset+scrollDelta(f.rect, inner), maxScroll))
		}
	}
}

type scrollState struct {
	offset     float32
	dragging   bool
	dragStart  float32
	dragOffset float32
}

// Dropdown shows the selected option and opens a list on click; it
// reports a change to *selected. While the list is open the arrows move
// through it, Enter chooses and Escape closes it.
func (c *Context) Dropdown(label string, selected *int, options []string) bool {
	id := c.id("dropdown:" + label)
	r := c.next(c.Theme.RowHeight)
	hover, _, clicked := c.interact(id, r)
	itemID := func(i int) widgetID { return id + widgetID(i+1) }
	if clicked {
		if c.open == id {
			c.open = 0
		} else {
			c.open = id
			// Focus starts on the chosen option; the activation that opened
			// the list must not also choose it.
			c.navFocus = itemID(max(*selected, 0))
			c.activate = false
		}
	}
	col := c.Theme.Field
	if hover {
		col = c.Theme.ButtonHover
	}
	c.box(c.skin().Field, r, col, c.Theme.FieldBorder)
	text := label
	if *selected >= 0 && *selected < len(options) {
		text = options[*selected]
	}
	_, h := c.Theme.Font.Measure(text, gfx.TextOptions{})
	c.text(text, r.X+c.Theme.Padding, r.Y+(r.H-h)/2, c.Theme.Text)
	c.text("v", r.X+r.W-c.Theme.Padding-8, r.Y+(r.H-h)/2, c.Theme.TextDim)
	c.note("dropdown", label, text, c.open == id)
	if c.open != id {
		return false
	}
	if c.keyNav() && c.in.KeyPressed(input.KeyEscape) {
		c.open = 0
		c.navFocus = id
		return false
	}
	// The options take input now, in order, and draw at End so the list
	// overlaps later widgets.
	listH := float32(len(options)) * c.Theme.RowHeight
	list := Rect{X: r.X, Y: r.Y + r.H, W: r.W, H: listH}
	rows := make([]Rect, len(options))
	hovers := make([]bool, len(options))
	changed := false
	saved := c.beginGroup(id, navUpDown, len(options))
	c.noRing = true
	for i := range options {
		rows[i] = Rect{X: list.X, Y: list.Y + float32(i)*c.Theme.RowHeight, W: list.W, H: c.Theme.RowHeight}
		var chosen bool
		hovers[i], _, chosen = c.interact(itemID(i), rows[i])
		if chosen {
			*selected = i
			c.open = 0
			changed = true
			c.navFocus = id
		}
	}
	c.noRing = false
	c.endGroup(saved)
	if c.pressed && !list.Contains(lin.V2(c.mouseX, c.mouseY)) && !r.Contains(lin.V2(c.mouseX, c.mouseY)) {
		c.open = 0
	}
	c.deferred = append(c.deferred, func() {
		c.frameRects = append(c.frameRects, list)
		c.box(c.skin().Panel, list, c.Theme.Panel, c.Theme.PanelBorder)
		for i, opt := range options {
			if hovers[i] {
				c.fill(rows[i], c.Theme.ButtonHover)
			}
			if c.navFocus == itemID(i) {
				c.focusRing(rows[i])
			}
			// The chosen option was measured above for the closed box;
			// carry that height rather than measure the same string twice.
			oh := h
			if opt != text {
				_, oh = c.Theme.Font.Measure(opt, gfx.TextOptions{})
			}
			c.text(opt, rows[i].X+c.Theme.Padding, rows[i].Y+(rows[i].H-oh)/2, c.Theme.Text)
		}
	})
	return changed
}

// Tooltip shows text near the pointer when it has rested on the previous
// widget for a moment.
func (c *Context) Tooltip(text string) {
	r := c.lastRect
	if !r.Contains(lin.V2(c.mouseX, c.mouseY)) {
		return
	}
	if c.hoverID != c.lastID {
		c.hoverID, c.hoverFrames = c.lastID, 0
	}
	c.hoverFrames++
	if c.hoverFrames < 30 {
		return
	}
	c.deferred = append(c.deferred, func() {
		w, h := c.Theme.Font.Measure(text, gfx.TextOptions{})
		box := Rect{X: c.mouseX + 12, Y: c.mouseY + 16, W: w + 2*c.Theme.Padding, H: h + 2*c.Theme.Padding}
		c.fill(box, c.Theme.Panel)
		c.border(box, c.Theme.PanelBorder)
		c.text(text, box.X+c.Theme.Padding, box.Y+c.Theme.Padding, c.Theme.Text)
	})
}

// Columns lays the widgets body creates side by side, one per weight,
// with widths in proportion to the weights:
//
//	ui.Columns([]float32{2, 1}, func() { ui.Dropdown(...); ui.Checkbox(...) })
func (c *Context) Columns(weights []float32, body func()) {
	p := c.currentPanel()
	if p == nil || len(weights) == 0 {
		body()
		return
	}
	var total float32
	for _, w := range weights {
		total += w
	}
	inner := p.rect.W - 2*c.Theme.Padding - float32(len(weights)-1)*c.Theme.Spacing
	p.row = &row{x: p.rect.X + c.Theme.Padding, y: p.cursor, count: len(weights), weights: weights, total: total, inner: inner}
	body()
	c.endRow(p)
}

// Separator draws a thin line.
func (c *Context) Separator() {
	r := c.next(c.Theme.Spacing * 2)
	c.fill(Rect{X: r.X, Y: r.Y + r.H/2, W: r.W, H: 1}, c.Theme.PanelBorder)
}
