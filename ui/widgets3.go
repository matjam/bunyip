package ui

import (
	"fmt"
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// Radio is one button of a group: it sets *value to option when clicked
// and shows filled while they match. RadioGroup stacks several.
func (c *Context) Radio(label string, value *int, option int) bool {
	id := c.id("radio:" + label)
	r := c.next(c.Theme.RowHeight)
	hover, _, clicked := c.interact(id, r)
	if clicked && *value != option {
		*value = option
	}
	on := *value == option
	c.note("radio", label, "", on)
	cx, cy := r.X+9, r.Y+r.H/2
	col := c.Theme.Field
	if hover {
		col = c.Theme.ButtonHover
	}
	c.g.FillCircle(cx, cy, 9, c.Theme.FieldBorder)
	c.g.FillCircle(cx, cy, 8, col)
	if on {
		c.g.FillCircle(cx, cy, 4, c.Theme.Accent)
	}
	_, h := c.Theme.Font.Measure(label, gfx.TextOptions{})
	c.text(label, r.X+18+c.Theme.Spacing, r.Y+(r.H-h)/2, c.Theme.Text)
	return clicked && on
}

// RadioGroup stacks a radio button per option and reports a change.
func (c *Context) RadioGroup(value *int, options []string) bool {
	changed := false
	for i, o := range options {
		if c.Radio(o, value, i) {
			changed = true
		}
	}
	return changed
}

// IntSlider drags *value across [lo, hi] in whole steps.
func (c *Context) IntSlider(label string, value *int, lo, hi int) bool {
	f := float32(*value)
	id := c.id(label)
	_, captionH := c.Theme.Font.Measure(label, gfx.TextOptions{})
	full := c.next(c.Theme.RowHeight + captionH)
	r := Rect{X: full.X, Y: full.Y + captionH, W: full.W, H: full.H - captionH}
	_, held, _ := c.interact(id, r)
	changed := false
	if held && hi > lo {
		t := lin.Clamp((c.mouseX-r.X)/r.W, 0, 1)
		v := lo + int(math.Round(float64(t)*float64(hi-lo)))
		if v != *value {
			*value = v
			changed = true
		}
		f = float32(*value)
	}
	c.note("slider", label, fmt.Sprint(*value), false)
	sk := c.skin()
	track := Rect{X: r.X, Y: r.Y + r.H/2 - 3, W: r.W, H: 6}
	c.box(sk.Track, track, c.Theme.Track, gfx.Color{})
	t := float32(0)
	if hi > lo {
		t = lin.Clamp((f-float32(lo))/float32(hi-lo), 0, 1)
	}
	c.box(sk.Fill, Rect{X: r.X, Y: track.Y, W: r.W * t, H: track.H}, c.Theme.Accent, gfx.Color{})
	knob := Rect{X: r.X + r.W*t - 6, Y: r.Y + r.H/2 - 9, W: 12, H: 18}
	c.box(sk.Knob, knob, c.Theme.Text, gfx.Color{})
	c.text(fmt.Sprintf("%s: %d", label, *value), full.X, full.Y, c.Theme.TextDim)
	return changed
}

// Spinner shows *value between minus and plus buttons that step it
// within [lo, hi]; the label sits before it.
func (c *Context) Spinner(label string, value *int, lo, hi, step int) bool {
	if step <= 0 {
		step = 1
	}
	changed := false
	c.Columns([]float32{3, 1, 1.4, 1}, func() {
		r := c.next(c.Theme.RowHeight)
		_, h := c.Theme.Font.Measure(label, gfx.TextOptions{})
		c.text(label, r.X, r.Y+(r.H-h)/2, c.Theme.Text)
		if c.Button("-") && *value-step >= lo {
			*value -= step
			changed = true
		}
		v := c.next(c.Theme.RowHeight)
		c.box(c.skin().Field, v, c.Theme.Field, c.Theme.FieldBorder)
		c.textCentred(fmt.Sprint(*value), v, c.Theme.Text)
		if c.Button("+") && *value+step <= hi {
			*value += step
			changed = true
		}
	})
	c.note("spinner", label, fmt.Sprint(*value), false)
	return changed
}

// ListBox shows items in a scrolling box of the given height with one
// selected; clicking selects and reports a change. selected may be -1.
func (c *Context) ListBox(label string, height float32, items []string, selected *int) bool {
	r := c.next(height)
	c.box(c.skin().Field, r, c.Theme.Field, c.Theme.FieldBorder)
	changed := false
	inner := Rect{X: r.X + 2, Y: r.Y + 2, W: r.W - 4, H: r.H - 4}
	rowH := c.Theme.RowHeight
	c.ScrollArea("list:"+label, inner, float32(len(items))*rowH+c.Theme.Spacing, func() {
		for i, item := range items {
			id := c.id("listitem:" + item)
			row := c.next(rowH - c.Theme.Spacing)
			hover, _, clicked := c.interact(id, row)
			if clicked && *selected != i {
				*selected = i
				changed = true
			}
			switch {
			case *selected == i:
				c.fill(row, c.Theme.Accent.WithAlpha(0.5))
			case hover:
				c.fill(row, c.Theme.ButtonHover)
			}
			_, h := c.Theme.Font.Measure(item, gfx.TextOptions{})
			c.text(item, row.X+c.Theme.Padding/2, row.Y+(row.H-h)/2, c.Theme.Text)
		}
	})
	sel := ""
	if *selected >= 0 && *selected < len(items) {
		sel = items[*selected]
	}
	c.note("listbox", label, sel, false)
	return changed
}

// ColorPicker edits a colour with a hue bar and a saturation-value
// square, showing a swatch and the hex value; it reports a change.
func (c *Context) ColorPicker(label string, col *gfx.Color) bool {
	id := c.id("color:" + label)
	_, captionH := c.Theme.Font.Measure(label, gfx.TextOptions{})
	const size = 120
	full := c.next(captionH + size + c.Theme.Spacing)
	c.text(label, full.X, full.Y, c.Theme.TextDim)
	top := full.Y + captionH + c.Theme.Spacing
	square := Rect{X: full.X, Y: top, W: size, H: size}
	bar := Rect{X: full.X + size + c.Theme.Spacing, Y: top, W: 16, H: size}
	swatch := Rect{X: bar.X + bar.W + c.Theme.Spacing, Y: top, W: c.Theme.RowHeight * 2, H: c.Theme.RowHeight}
	h, s, v := col.HSV()
	if s == 0 || v == 0 {
		if hue, ok := c.hues[id]; ok {
			h = hue // a grey keeps the last hue chosen
		}
	}
	changed := false
	_, heldSquare, _ := c.interact(id, square)
	if heldSquare {
		s = lin.Clamp((c.mouseX-square.X)/square.W, 0, 1)
		v = lin.Clamp(1-(c.mouseY-square.Y)/square.H, 0, 1)
		changed = true
	}
	_, heldBar, _ := c.interact(id+1, bar)
	if heldBar {
		h = lin.Clamp((c.mouseY-bar.Y)/bar.H, 0, 0.9999) * 360
		changed = true
	}
	if changed {
		a := col.A
		*col = gfx.FromHSV(h, s, v).WithAlpha(a)
		c.hues[id] = h
	}
	// The square: white to the pure hue across, then black over it down.
	pure := gfx.FromHSV(h, 1, 1)
	c.quad(square, gfx.White, pure, pure, gfx.White)
	c.quad(square, gfx.Color{}, gfx.Color{}, gfx.Black, gfx.Black)
	c.border(square, c.Theme.FieldBorder)
	mx, my := square.X+s*square.W, square.Y+(1-v)*square.H
	c.g.StrokeCircle(mx, my, 5, 2, gfx.Black)
	c.g.StrokeCircle(mx, my, 5, 1, gfx.White)
	// The hue bar in six rainbow bands.
	for i := range 6 {
		a, b := gfx.FromHSV(float32(i)*60, 1, 1), gfx.FromHSV(float32(i+1)*60, 1, 1)
		seg := Rect{X: bar.X, Y: bar.Y + float32(i)*bar.H/6, W: bar.W, H: bar.H / 6}
		c.quad(seg, a, a, b, b)
	}
	c.border(bar, c.Theme.FieldBorder)
	hy := bar.Y + h/360*bar.H
	c.fill(Rect{X: bar.X - 2, Y: hy - 1, W: bar.W + 4, H: 2}, gfx.White)
	c.fill(swatch, *col)
	c.border(swatch, c.Theme.FieldBorder)
	r8, g8, b8 := toSRGB8(col.R), toSRGB8(col.G), toSRGB8(col.B)
	hex := fmt.Sprintf("#%02x%02x%02x", r8, g8, b8)
	c.text(hex, swatch.X, swatch.Y+swatch.H+c.Theme.Spacing, c.Theme.Text)
	c.note("colorpicker", label, hex, false)
	return changed
}

// quad draws a rectangle with a colour at each corner: top-left,
// top-right, bottom-right, bottom-left.
func (c *Context) quad(r Rect, tl, tr, br, bl gfx.Color) {
	p := func(x, y float32) lin.Vec2 { return lin.V2(x, y) }
	c.g.DrawTriangles(nil, []gfx.Vertex2D{
		{Pos: p(r.X, r.Y), Color: tl}, {Pos: p(r.X+r.W, r.Y), Color: tr}, {Pos: p(r.X+r.W, r.Y+r.H), Color: br},
		{Pos: p(r.X, r.Y), Color: tl}, {Pos: p(r.X+r.W, r.Y+r.H), Color: br}, {Pos: p(r.X, r.Y+r.H), Color: bl},
	})
}

func toSRGB8(v float32) uint8 {
	v = lin.Clamp(v, 0, 1)
	var s float64
	if v <= 0.0031308 {
		s = float64(v) * 12.92
	} else {
		s = 1.055*math.Pow(float64(v), 1/2.4) - 0.055
	}
	return uint8(s*255 + 0.5)
}

// Image shows a texture at a size; zero size means the texture's own,
// fitted to the width available.
func (c *Context) Image(tex *gfx.Texture, w, h float32) {
	c.ImageRegion(gfx.Region{Tex: tex, UV1: lin.V2(1, 1)}, w, h)
}

// ImageRegion shows a region of a texture at a size, keeping its aspect
// ratio when only one of w and h is given.
func (c *Context) ImageRegion(reg gfx.Region, w, h float32) {
	size := reg.Size()
	if w <= 0 && h <= 0 {
		w = min(size.X, c.nextWidth())
		if size.X > 0 {
			h = size.Y * w / size.X
		}
	} else if w <= 0 && size.Y > 0 {
		w = size.X * h / size.Y
	} else if h <= 0 && size.X > 0 {
		h = size.Y * w / size.X
	}
	r := c.next(h)
	c.g.DrawRegion(reg, gfx.Sprite{Pos: lin.V2(r.X, r.Y), Size: lin.V2(w, h)})
	c.note("image", "", "", false)
}

// IconButton is a Button with an icon before its label.
func (c *Context) IconButton(icon gfx.Region, label string) bool {
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
	iconSize := r.H - 2*c.Theme.Spacing
	tw, th := c.Theme.Font.Measure(label, gfx.TextOptions{})
	total := iconSize + c.Theme.Spacing + tw
	x := r.X + (r.W-total)/2
	c.g.DrawRegion(icon, gfx.Sprite{Pos: lin.V2(x, r.Y+c.Theme.Spacing), Size: lin.V2(iconSize, iconSize)})
	c.text(label, x+iconSize+c.Theme.Spacing, r.Y+(r.H-th)/2, c.Theme.Text)
	c.note("button", label, "", false)
	return clicked
}

// RichLabel draws markup (see gfx.ParseRich) wrapped to the width
// available, in the theme's regular, bold and italic fonts, and returns
// the name of a link clicked this frame, or "".
func (c *Context) RichLabel(markup string) string {
	rt := gfx.ParseRich(markup)
	fonts := gfx.RichFonts{Regular: c.Theme.Font, Bold: c.Theme.BoldFont, Italic: c.Theme.ItalicFont, BoldItalic: c.Theme.BoldItalicFont}
	opts := gfx.TextOptions{Width: c.nextWidth()}
	_, h := fonts.MeasureRich(rt, opts)
	r := c.next(h)
	links := c.g.DrawRichText(fonts, rt, r.X, r.Y+(r.H-h)/2, opts, c.Theme.Text)
	clicked := ""
	for _, l := range links {
		if l.Rect.Contains(lin.V2(c.mouseX, c.mouseY)) {
			c.nextHot = 0
			if c.released {
				clicked = l.Name
			}
		}
	}
	c.note("label", rt.Plain(), "", false)
	return clicked
}
