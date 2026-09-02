package ui

import (
	"hash/fnv"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

// Rect is an axis-aligned box in view units: lin.Rect under a short name.
type Rect = lin.Rect

func contains(r Rect, x, y float32) bool {
	return r.Contains(lin.V2(x, y))
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

	// OnTextInputRect, when set, is told where the focused text field is
	// so the platform can place input-method candidate windows. Wire it to
	// bunyip.Context.SetTextInputRect.
	OnTextInputRect func(x, y, w, h float32)

	scroll      map[widgetID]*scrollState
	open        widgetID // the dropdown showing its list
	deferred    []func() // overlays drawn at End, above everything
	lastRect    Rect     // the previous widget, for tooltips
	lastID      widgetID
	hoverID     widgetID
	hoverFrames int
	clipDepth   int

	// Keyboard and gamepad navigation: focusables are collected each
	// frame in submission order; navFocus is the highlighted one.
	focusables []focusable
	navFocus   widgetID
	activate   bool // the focused widget was activated this frame

	// Clipboard, when set, is what text fields cut, copy and paste
	// through; the engine's Context satisfies it.
	Clipboard Clipboard

	edits    map[widgetID]*editState
	expanded map[widgetID]bool // tree nodes
	drags    map[widgetID]*dragState
	hues     map[widgetID]float32 // colour pickers' last hue

	openMenu    widgetID
	menuX       float32
	menuBar     Rect
	menuItems   []string
	menuMeasure bool
	modal       widgetID // the open modal this frame, which alone takes input
	inModal     bool

	nodes, lastNodes []AccessibleNode
}

type focusable struct {
	id   widgetID
	rect Rect
}

// New makes a context drawing with g under theme.
func New(g *gfx.Graphics, theme Theme) *Context {
	return &Context{Theme: theme, g: g, seq: map[widgetID]int{}, scroll: map[widgetID]*scrollState{},
		edits: map[widgetID]*editState{}, expanded: map[widgetID]bool{}, drags: map[widgetID]*dragState{}, hues: map[widgetID]float32{}}
}

// Begin runs one frame of interface: body calls the widget methods, and
// the frame is finished (overlays drawn, state settled) when it returns.
//
//	ui.Begin(ctx.Input, func() {
//		ui.Panel("Menu", rect, func() { ... })
//	})
func (c *Context) Begin(in *input.State, body func()) {
	c.begin(in)
	body()
	c.end()
}

func (c *Context) begin(in *input.State) {
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
	c.deferred = c.deferred[:0]
	if c.pressed {
		c.focus = 0 // clicking anywhere else drops keyboard focus
	}
	c.navigate()
	c.focusables = c.focusables[:0]
	c.modal = 0
	c.nodes = c.nodes[:0]
}

// end finishes the frame: deferred overlays draw above everything and
// the press state settles.
func (c *Context) end() {
	for i := 0; i < len(c.deferred); i++ {
		c.deferred[i]() // an overlay may add more overlays
	}
	if c.released {
		c.active = 0
	}
	c.activate = false
	c.lastNodes = append(c.lastNodes[:0], c.nodes...)
}

// WantsMouse reports whether the pointer is over a panel, so the game can
// ignore clicks the interface consumed.
func (c *Context) WantsMouse() bool {
	for _, r := range c.frameRects {
		if r.Contains(lin.V2(c.mouseX, c.mouseY)) {
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
// whether it was clicked (pressed and released inside, or activated by
// keyboard or gamepad while focused). It also registers the widget for
// navigation and tooltips.
func (c *Context) interact(id widgetID, r Rect) (hover, held, clicked bool) {
	c.lastRect, c.lastID = r, id
	if c.modal != 0 && !c.inModal {
		return false, false, false // a modal owns every input
	}
	c.focusables = append(c.focusables, focusable{id: id, rect: r})
	if c.navFocus == id {
		fw := c.Theme.FocusWidth
		if fw <= 0 {
			fw = 2
		}
		c.ring(Rect{X: r.X - fw, Y: r.Y - fw, W: r.W + 2*fw, H: r.H + 2*fw}, fw, c.Theme.Accent)
		if c.activate {
			clicked = true
		}
	}
	over := r.Contains(lin.V2(c.mouseX, c.mouseY))
	if c.open != 0 && c.open != id {
		over = false // an open dropdown list owns the pointer
	}
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
		c.navFocus = id
	}
	return hover, held, clicked
}

func (c *Context) fill(r Rect, col gfx.Color) { c.g.FillRect(r.X, r.Y, r.W, r.H, col) }

func (c *Context) border(r Rect, col gfx.Color) {
	w := c.Theme.BorderWidth
	if w <= 0 {
		return
	}
	c.ring(r, w, col)
}

// ring outlines r with a line of width w.
func (c *Context) ring(r Rect, w float32, col gfx.Color) {
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
	w, h := c.Theme.Font.Measure(s, gfx.TextOptions{})
	c.text(s, r.X+(r.W-w)/2, r.Y+(r.H-h)/2, col)
}
