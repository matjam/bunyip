package ui

import (
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
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
	over := r.contains(c.mouseX, c.mouseY)
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
		hover, held, _ := c.interact(id, thumb)
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
	st.offset = max(0, min(st.offset, maxScroll))
	inner := Rect{X: r.X, Y: r.Y, W: r.W - barW - c.Theme.Spacing, H: r.H}
	c.g.PushClip(inner.X, inner.Y, inner.W, inner.H)
	c.frameRects = append(c.frameRects, r)
	p := &panel{id: id, rect: Rect{X: inner.X, Y: inner.Y - st.offset, W: inner.W, H: contentHeight}, cursor: inner.Y - st.offset}
	c.panels = append(c.panels, p)
	c.clipDepth++
	contents()
	c.clipDepth--
	c.panels = c.panels[:len(c.panels)-1]
	c.g.PopClip()
}

type scrollState struct {
	offset     float32
	dragging   bool
	dragStart  float32
	dragOffset float32
}

// Dropdown shows the selected option and opens a list on click; it
// reports a change to *selected.
func (c *Context) Dropdown(label string, selected *int, options []string) bool {
	id := c.id("dropdown:" + label)
	r := c.next(c.Theme.RowHeight)
	hover, _, clicked := c.interact(id, r)
	if clicked {
		if c.open == id {
			c.open = 0
		} else {
			c.open = id
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
	_, h := c.Theme.Font.Measure(text)
	c.text(text, r.X+c.Theme.Padding, r.Y+(r.H-h)/2, c.Theme.Text)
	c.text("v", r.X+r.W-c.Theme.Padding-8, r.Y+(r.H-h)/2, c.Theme.TextDim)
	if c.open != id {
		return false
	}
	// The list draws after everything else so it overlaps later widgets.
	changed := false
	c.deferred = append(c.deferred, func() {
		listH := float32(len(options)) * c.Theme.RowHeight
		list := Rect{X: r.X, Y: r.Y + r.H, W: r.W, H: listH}
		c.frameRects = append(c.frameRects, list)
		c.box(c.skin().Panel, list, c.Theme.Panel, c.Theme.PanelBorder)
		for i, opt := range options {
			row := Rect{X: list.X, Y: list.Y + float32(i)*c.Theme.RowHeight, W: list.W, H: c.Theme.RowHeight}
			over := row.contains(c.mouseX, c.mouseY)
			if over {
				c.fill(row, c.Theme.ButtonHover)
				c.nextHot = 0
				if c.released {
					*selected = i
					c.open = 0
					changed = true
				}
			}
			_, oh := c.Theme.Font.Measure(opt)
			c.text(opt, row.X+c.Theme.Padding, row.Y+(row.H-oh)/2, c.Theme.Text)
		}
		if c.pressed && !list.contains(c.mouseX, c.mouseY) && !r.contains(c.mouseX, c.mouseY) {
			c.open = 0
		}
	})
	return changed
}

// Tooltip shows text near the pointer when it has rested on the previous
// widget for a moment.
func (c *Context) Tooltip(text string) {
	r := c.lastRect
	if !r.contains(c.mouseX, c.mouseY) {
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
		w, h := c.Theme.Font.Measure(text)
		box := Rect{X: c.mouseX + 12, Y: c.mouseY + 16, W: w + 2*c.Theme.Padding, H: h + 2*c.Theme.Padding}
		c.fill(box, c.Theme.Panel)
		c.border(box, c.Theme.PanelBorder)
		c.text(text, box.X+c.Theme.Padding, box.Y+c.Theme.Padding, c.Theme.Text)
	})
}

// Columns lays the next len(weights) widgets side by side with widths in
// proportion to weights.
func (c *Context) Columns(weights ...float32) {
	p := c.currentPanel()
	if p == nil || len(weights) == 0 {
		return
	}
	var total float32
	for _, w := range weights {
		total += w
	}
	inner := p.rect.W - 2*c.Theme.Padding - float32(len(weights)-1)*c.Theme.Spacing
	p.row = &row{x: p.rect.X + c.Theme.Padding, y: p.cursor, count: len(weights), weights: weights, total: total, inner: inner}
}

// Separator draws a thin line.
func (c *Context) Separator() {
	r := c.next(c.Theme.Spacing * 2)
	c.fill(Rect{X: r.X, Y: r.Y + r.H/2, W: r.W, H: 1}, c.Theme.PanelBorder)
}

// navigate moves keyboard and gamepad focus between interactive widgets.
func (c *Context) navigate() {
	if c.in == nil || len(c.focusables) == 0 {
		return
	}
	next, prev, activate := false, false, false
	if c.in.KeyPressed(input.KeyTab) {
		if c.in.Mods()&input.ModShift != 0 {
			prev = true
		} else {
			next = true
		}
	}
	if pad := c.in.Gamepad(0); pad.Connected {
		next = next || pad.Pressed(input.ButtonDpadDown)
		prev = prev || pad.Pressed(input.ButtonDpadUp)
		activate = pad.Pressed(input.ButtonA)
	}
	if c.focus == 0 || !c.WantsKeyboard() {
		activate = activate || c.in.KeyPressed(input.KeyEnter) || c.in.KeyPressed(input.KeySpace)
	}
	idx := -1
	for i, f := range c.focusables {
		if f.id == c.navFocus {
			idx = i
		}
	}
	switch {
	case next:
		idx = (idx + 1) % len(c.focusables)
	case prev:
		idx = (idx - 1 + len(c.focusables)) % len(c.focusables)
	}
	if idx >= 0 {
		c.navFocus = c.focusables[idx].id
	}
	c.activate = activate && idx >= 0
}
