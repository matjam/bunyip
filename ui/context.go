package ui

import (
	"hash/fnv"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
)

// Rect is an axis-aligned box in view units.
type Rect struct{ X, Y, W, H float32 }

func (r Rect) contains(x, y float32) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H
}

type widgetID uint64

// Context drives one interface. Create one and reuse it every frame.
type Context struct {
	Theme Theme
	g     *gfx.Graphics
	in    *input.State

	mouseX, mouseY float32
	pressed        bool // left button went down this update
	down           bool
	released       bool

	hot     widgetID // under the pointer
	active  widgetID // pressed and not yet released
	focus   widgetID // receives keyboard input
	nextHot widgetID

	panels     []*panel
	frameRects []Rect // every panel opened this frame, for WantsMouse
	seq        map[widgetID]int
}

// New makes a context drawing with g under theme.
func New(g *gfx.Graphics, theme Theme) *Context {
	return &Context{Theme: theme, g: g, seq: map[widgetID]int{}}
}

// Begin starts a frame's interface from the current input.
func (c *Context) Begin(in *input.State) {
	c.in = in
	mx, my := in.Mouse()
	c.mouseX, c.mouseY = float32(mx), float32(my)
	c.pressed = in.MousePressed(input.MouseLeft)
	c.down = in.MouseDown(input.MouseLeft)
	c.released = in.MouseReleased(input.MouseLeft)
	c.hot = c.nextHot
	c.nextHot = 0
	clear(c.seq)
	c.panels = c.panels[:0]
	c.frameRects = c.frameRects[:0]
	if c.pressed {
		c.focus = 0 // clicking anywhere else drops keyboard focus
	}
}

// End finishes the frame. Call after the last widget.
func (c *Context) End() {
	if c.released {
		c.active = 0
	}
}

// WantsMouse reports whether the pointer is over a panel, so the game can
// ignore clicks the interface consumed.
func (c *Context) WantsMouse() bool {
	for _, r := range c.frameRects {
		if r.contains(c.mouseX, c.mouseY) {
			return true
		}
	}
	return false
}

// WantsKeyboard reports whether a text field has focus.
func (c *Context) WantsKeyboard() bool { return c.focus != 0 }

// id derives a widget identity from its label, its panel and how many
// times that label has been used in the panel this frame.
func (c *Context) id(label string) widgetID {
	h := fnv.New64a()
	if p := c.currentPanel(); p != nil {
		var b [8]byte
		for i := range b {
			b[i] = byte(p.id >> (8 * i))
		}
		h.Write(b[:])
	}
	h.Write([]byte(label))
	base := widgetID(h.Sum64())
	n := c.seq[base]
	c.seq[base] = n + 1
	return base + widgetID(n)*0x9E3779B97F4A7C15
}

// interact runs the hot/active state machine for a rectangle and reports
// whether it was clicked (pressed and released inside).
func (c *Context) interact(id widgetID, r Rect) (hover, held, clicked bool) {
	over := r.contains(c.mouseX, c.mouseY)
	if over {
		c.nextHot = id
	}
	hover = c.hot == id && over
	if hover && c.pressed {
		c.active = id
	}
	held = c.active == id && c.down
	if c.active == id && c.released && over {
		clicked = true
	}
	return hover, held, clicked
}

func (c *Context) fill(r Rect, col gfx.Color) { c.g.FillRect(r.X, r.Y, r.W, r.H, col) }

func (c *Context) border(r Rect, col gfx.Color) {
	w := c.Theme.BorderWidth
	if w <= 0 {
		return
	}
	c.g.FillRect(r.X, r.Y, r.W, w, col)
	c.g.FillRect(r.X, r.Y+r.H-w, r.W, w, col)
	c.g.FillRect(r.X, r.Y, w, r.H, col)
	c.g.FillRect(r.X+r.W-w, r.Y, w, r.H, col)
}

func (c *Context) text(s string, x, y float32, col gfx.Color) {
	c.g.DrawText(c.Theme.Font, s, x, y, col)
}

// textCentred draws s centred in r.
func (c *Context) textCentred(s string, r Rect, col gfx.Color) {
	w, h := c.Theme.Font.Measure(s)
	c.text(s, r.X+(r.W-w)/2, r.Y+(r.H-h)/2, col)
}
