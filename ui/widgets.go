package ui

import (
	"github.com/matjam/bunyip/gfx"
)

// Label draws text, wrapped to the width available to it.
func (c *Context) Label(text string) {
	opts := gfx.TextOptions{Width: c.nextWidth()}
	_, h := c.Theme.Font.Measure(text, opts)
	r := c.next(h)
	c.g.DrawTextBlock(c.Theme.Font, text, r.X, r.Y+(r.H-h)/2, opts, c.Theme.Text)
	c.lastRect = r
	c.note("label", text, "", false)
}

// Button draws a push button and reports a click.
func (c *Context) Button(label string) bool {
	id := c.id(label)
	r := c.next(c.Theme.RowHeight)
	hover, held, clicked := c.interact(id, r)
	sk := c.skin()
	col, slice := c.Theme.Button, sk.Button
	switch {
	case held:
		col, slice = c.Theme.ButtonActive, or(sk.ButtonActive, sk.ButtonHover, sk.Button)
	case hover:
		col, slice = c.Theme.ButtonHover, or(sk.ButtonHover, sk.Button)
	}
	c.box(slice, r, col, c.Theme.PanelBorder)
	c.textCentred(label, r, c.Theme.Text)
	c.note("button", label, "", false)
	return clicked
}

// Checkbox toggles *value on click and reports a change.
func (c *Context) Checkbox(label string, value *bool) bool {
	id := c.id(label)
	r := c.next(c.Theme.RowHeight)
	hover, _, clicked := c.interact(id, r)
	if clicked {
		*value = !*value
	}
	box := Rect{X: r.X, Y: r.Y + (r.H-18)/2, W: 18, H: 18}
	col := c.Theme.Field
	if hover {
		col = c.Theme.ButtonHover
	}
	sk := c.skin()
	slice := sk.Check
	if *value {
		slice = or(sk.CheckOn, sk.Check)
	}
	c.box(slice, box, col, c.Theme.FieldBorder)
	if *value && (slice == nil || sk.CheckOn == nil) {
		c.fill(Rect{X: box.X + 4, Y: box.Y + 4, W: 10, H: 10}, c.Theme.Accent)
	}
	_, h := c.Theme.Font.Measure(label, gfx.TextOptions{})
	c.text(label, box.X+box.W+c.Theme.Spacing, r.Y+(r.H-h)/2, c.Theme.Text)
	c.note("checkbox", label, "", *value)
	return clicked
}

// Slider drags *value across [lo, hi] and reports a change. While
// focused, the left and right arrows (or the d-pad) step it by a
// twentieth of the range.
func (c *Context) Slider(label string, value *float32, lo, hi float32) bool {
	id := c.id(label)
	// The caption sits above the track inside the widget's own space, so
	// it never collides with the row before.
	_, captionH := c.Theme.Font.Measure(label, gfx.TextOptions{})
	full := c.next(c.Theme.RowHeight + captionH)
	r := Rect{X: full.X, Y: full.Y + captionH, W: full.W, H: full.H - captionH}
	_, held, _ := c.interact(id, r)
	changed := false
	if held {
		t := (c.mouseX - r.X) / r.W
		v := lo + max(0, min(1, t))*(hi-lo)
		if v != *value {
			*value = v
			changed = true
		}
	}
	if d := c.stepKeys(id); d != 0 {
		v := *value + float32(d)*(hi-lo)/20
		v = max(min(lo, hi), min(v, max(lo, hi)))
		if v != *value {
			*value = v
			changed = true
		}
	}
	sk := c.skin()
	track := Rect{X: r.X, Y: r.Y + r.H/2 - 3, W: r.W, H: 6}
	c.box(sk.Track, track, c.Theme.Track, gfx.Color{})
	t := (*value - lo) / (hi - lo)
	c.box(sk.Fill, Rect{X: r.X, Y: track.Y, W: r.W * max(0, min(1, t)), H: track.H}, c.Theme.Accent, gfx.Color{})
	knob := Rect{X: r.X + r.W*max(0, min(1, t)) - 6, Y: r.Y + r.H/2 - 9, W: 12, H: 18}
	if sk.Knob != nil {
		knob = Rect{X: knob.X - 3, Y: knob.Y - 3, W: 18, H: 24}
	}
	c.box(sk.Knob, knob, c.Theme.Text, gfx.Color{})
	c.text(c.labelFloat(label, *value, 2), full.X, full.Y, c.Theme.TextDim)
	c.note("slider", label, c.formatFloat(*value, 2), false)
	return changed
}

// Progress draws a bar filled to t in [0,1].
func (c *Context) Progress(label string, t float32) {
	r := c.next(c.Theme.RowHeight)
	c.lastRect = r
	c.note("progress", label, c.formatPercent(t*100), false)
	sk := c.skin()
	c.box(sk.Track, r, c.Theme.Track, gfx.Color{})
	c.box(sk.Fill, Rect{X: r.X, Y: r.Y, W: r.W * max(0, min(1, t)), H: r.H}, c.Theme.Accent, gfx.Color{})
	if sk.Track == nil {
		c.border(r, c.Theme.PanelBorder)
	}
	c.textCentred(label, r, c.Theme.Text)
}
