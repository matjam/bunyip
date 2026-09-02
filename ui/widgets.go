package ui

import (
	"fmt"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
)

// Label draws text, wrapped to the width available to it.
func (c *Context) Label(text string) {
	opts := gfx.TextOptions{Width: c.nextWidth()}
	_, h := c.Theme.Font.MeasureBlock(text, opts)
	r := c.next(h)
	c.g.DrawTextBlock(c.Theme.Font, text, r.X, r.Y+(r.H-h)/2, opts, c.Theme.Text)
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
	sk := c.skin()
	border, slice := c.Theme.FieldBorder, sk.Field
	if c.focus == id {
		border, slice = c.Theme.Accent, or(sk.FieldFocus, sk.Field)
	}
	c.box(slice, r, c.Theme.Field, border)
	shown := *value
	composing := ""
	if c.focus == id {
		composing = c.in.Composition()
		if composing == "" {
			shown += "_"
		}
		if c.OnTextInputRect != nil {
			c.OnTextInputRect(r.X, r.Y, r.W, r.H)
		}
	}
	if shown == "" && composing == "" {
		shown = label
	}
	_, h := c.Theme.Font.Measure(shown)
	col := c.Theme.Text
	if *value == "" && c.focus != id {
		col = c.Theme.TextDim
	}
	c.text(shown, r.X+c.Theme.Padding, r.Y+(r.H-h)/2, col)
	if composing != "" {
		// The input method's uncommitted text sits after the value with an
		// underline, the way native fields show a word mid-conversion.
		w, _ := c.Theme.Font.Measure(shown)
		x := r.X + c.Theme.Padding + w
		cw, ch := c.Theme.Font.Measure(composing)
		c.text(composing, x, r.Y+(r.H-h)/2, c.Theme.Accent)
		c.fill(Rect{X: x, Y: r.Y + (r.H-h)/2 + ch, W: cw, H: 1}, c.Theme.Accent)
	}
	return changed
}

// Progress draws a bar filled to t in [0,1].
func (c *Context) Progress(label string, t float32) {
	r := c.next(c.Theme.RowHeight)
	sk := c.skin()
	c.box(sk.Track, r, c.Theme.Track, gfx.Color{})
	c.box(sk.Fill, Rect{X: r.X, Y: r.Y, W: r.W * max(0, min(1, t)), H: r.H}, c.Theme.Accent, gfx.Color{})
	if sk.Track == nil {
		c.border(r, c.Theme.PanelBorder)
	}
	c.textCentred(label, r, c.Theme.Text)
}
