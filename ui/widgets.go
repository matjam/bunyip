package ui

import (
	"fmt"

	"github.com/matjam/bunyip/input"
)

// Label draws a line of text.
func (c *Context) Label(text string) {
	_, h := c.Theme.Font.Measure(text)
	r := c.next(h)
	c.text(text, r.X, r.Y+(r.H-h)/2, c.Theme.Text)
}

// Button draws a push button and reports a click.
func (c *Context) Button(label string) bool {
	id := c.id(label)
	r := c.next(c.Theme.RowHeight)
	hover, held, clicked := c.interact(id, r)
	col := c.Theme.Button
	switch {
	case held:
		col = c.Theme.ButtonActive
	case hover:
		col = c.Theme.ButtonHover
	}
	c.fill(r, col)
	c.border(r, c.Theme.PanelBorder)
	c.textCentred(label, r, c.Theme.Text)
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
	c.fill(box, col)
	c.border(box, c.Theme.FieldBorder)
	if *value {
		c.fill(Rect{X: box.X + 4, Y: box.Y + 4, W: 10, H: 10}, c.Theme.Accent)
	}
	_, h := c.Theme.Font.Measure(label)
	c.text(label, box.X+box.W+c.Theme.Spacing, r.Y+(r.H-h)/2, c.Theme.Text)
	return clicked
}

// Slider drags *value across [lo, hi] and reports a change.
func (c *Context) Slider(label string, value *float32, lo, hi float32) bool {
	id := c.id(label)
	r := c.next(c.Theme.RowHeight)
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
	track := Rect{X: r.X, Y: r.Y + r.H/2 - 3, W: r.W, H: 6}
	c.fill(track, c.Theme.Track)
	t := (*value - lo) / (hi - lo)
	c.fill(Rect{X: r.X, Y: track.Y, W: r.W * max(0, min(1, t)), H: track.H}, c.Theme.Accent)
	knob := Rect{X: r.X + r.W*max(0, min(1, t)) - 6, Y: r.Y + r.H/2 - 9, W: 12, H: 18}
	c.fill(knob, c.Theme.Text)
	caption := fmt.Sprintf("%s: %.2f", label, *value)
	_, h := c.Theme.Font.Measure(caption)
	c.text(caption, r.X, r.Y-h+4, c.Theme.TextDim)
	return changed
}

// TextField edits *value with the keyboard while focused and reports a change.
func (c *Context) TextField(label string, value *string) bool {
	id := c.id(label)
	r := c.next(c.Theme.RowHeight)
	_, _, clicked := c.interact(id, r)
	if clicked {
		c.focus = id
	}
	changed := false
	if c.focus == id {
		for _, ch := range c.in.Chars() {
			if ch >= ' ' {
				*value += string(ch)
				changed = true
			}
		}
		if c.in.KeyPressed(input.KeyBackspace) && len(*value) > 0 {
			runes := []rune(*value)
			*value = string(runes[:len(runes)-1])
			changed = true
		}
		if c.in.KeyPressed(input.KeyEnter) || c.in.KeyPressed(input.KeyEscape) {
			c.focus = 0
		}
	}
	c.fill(r, c.Theme.Field)
	border := c.Theme.FieldBorder
	if c.focus == id {
		border = c.Theme.Accent
	}
	c.border(r, border)
	shown := *value
	if c.focus == id {
		shown += "_"
	}
	if shown == "" {
		shown = label
	}
	_, h := c.Theme.Font.Measure(shown)
	col := c.Theme.Text
	if *value == "" && c.focus != id {
		col = c.Theme.TextDim
	}
	c.text(shown, r.X+c.Theme.Padding, r.Y+(r.H-h)/2, col)
	return changed
}

// Progress draws a bar filled to t in [0,1].
func (c *Context) Progress(label string, t float32) {
	r := c.next(c.Theme.RowHeight)
	c.fill(r, c.Theme.Track)
	c.fill(Rect{X: r.X, Y: r.Y, W: r.W * max(0, min(1, t)), H: r.H}, c.Theme.Accent)
	c.border(r, c.Theme.PanelBorder)
	c.textCentred(label, r, c.Theme.Text)
}
