package ui

import (
	"hash/fnv"
	"strconv"

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
// Build it during the game's Draw on the rendering goroutine; it is
// not safe for concurrent use and does not own fonts or skin textures.
type Context struct {
	Theme Theme // live widget styling; referenced resources are managed separately
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
	// engine.Context.SetTextInputRect.
	OnTextInputRect func(x, y, w, h float32)

	scroll      map[widgetID]*scrollState
	open        widgetID // the dropdown showing its list
	deferred    []func() // overlays drawn at End, above everything
	lastRect    Rect     // the previous widget, for tooltips
	lastID      widgetID
	hoverID     widgetID
	hoverFrames int
	clipDepth   int
	clips       []Rect   // clip stack of the containers being built; the last applies to hit tests
	nextModal   widgetID // the modal that ran this frame, which owns input from the next frame's first widget
	focusSeen   bool     // the focused text field was submitted this frame
	focusRect   Rect     // where it was
	pressTaken  bool     // a widget took this frame's press without having been hot

	// Keyboard and gamepad navigation: focusables are collected each
	// frame in submission order; navFocus is the highlighted one. A
	// container (a list, a row of tabs) submits its items under a group,
	// which Tab treats as one stop and the arrows move within.
	focusables     []focusable
	lastFocusables []focusable // the previous frame's, for scrolling into view
	navFocus       widgetID
	navTextFocus   widgetID // this frame's navigation destination, claimed only by a submitted editor
	activate       bool     // the focused widget was activated this frame
	group          navGroup // the container whose items are being submitted
	groupFocus     map[widgetID]widgetID
	navMoved       bool // the keys or d-pad moved focus this frame
	navMovedNext   bool // a widget moved the focused item; scroll to it next frame
	scrollID       widgetID
	noFocus        bool // widgets submitted while set are not navigation stops
	noRing         bool // the focus ring is drawn by the caller instead

	// Drag and drop: where the button went down, the drag in progress and
	// a list being reordered.
	pressSource    widgetID
	pressX, pressY float32
	drag           *dragging
	reorder        *reorderState
	bounds         []bounds
	dropHover      bool

	// DragGhost, when set, draws what follows the pointer during a drag
	// in place of the source's label.
	DragGhost func(label string, payload any, x, y float32)

	// Clipboard, when set, is what text fields cut, copy and paste
	// through; the engine's Context satisfies it.
	Clipboard Clipboard

	edits    map[widgetID]*editState
	expanded map[widgetID]bool // tree nodes
	drags    map[widgetID]*dragState
	hues     map[widgetID]float32    // colour pickers' last hue
	curves   map[widgetID]curveState // curve editors' dragged point

	openMenu    widgetID
	menuX       float32
	menuBar     Rect
	menuItems   []string
	menuMeasure bool
	modal       widgetID // the open modal this frame, which alone takes input
	inModal     bool

	nodes, lastNodes []AccessibleNode

	// rich holds the markup RichLabel has parsed, keyed by the markup
	// string: the runs, the plain text and the last size measured for
	// them. The maps are two generations, swapped every frame, so an
	// entry not used for two frames is dropped.
	rich, richOld map[string]*richEntry

	// buf is scratch for formatting numbers into captions without fmt.
	buf []byte
}

// richEntry is one markup string's parsed runs, its plain text and the
// size those runs took the last time they were measured.
type richEntry struct {
	rt    gfx.RichText
	plain string
	fonts gfx.RichFonts
	opts  gfx.TextOptions
	w, h  float32
	sized bool
}

type focusable struct {
	id     widgetID
	rect   Rect
	group  widgetID // the container navigated with the arrows, or 0
	keys   navKeys  // which arrows move within the container
	page   int      // rows a PageUp or PageDown moves
	scroll widgetID // the enclosing ScrollArea, or 0
}

// navGroup is a container whose items the arrows move between.
type navGroup struct {
	id   widgetID
	keys navKeys
	page int
}

// navKeys says which arrow pairs move within a container.
type navKeys uint8

const (
	navUpDown navKeys = 1 << iota
	navLeftRight
)

// New makes a context drawing with g under theme.
func New(g *gfx.Graphics, theme Theme) *Context {
	return &Context{Theme: theme, g: g, seq: map[widgetID]int{}, scroll: map[widgetID]*scrollState{},
		edits: map[widgetID]*editState{}, expanded: map[widgetID]bool{}, drags: map[widgetID]*dragState{}, hues: map[widgetID]float32{},
		curves:     map[widgetID]curveState{},
		groupFocus: map[widgetID]widgetID{},
		rich:       map[string]*richEntry{}, richOld: map[string]*richEntry{}}
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
		// Keyboard focus is dropped at the end of the frame if the press
		// landed outside the focused field, so a drag inside it selects.
		c.pressSource = 0
		c.pressX, c.pressY = c.mouseX, c.mouseY
	}
	c.pressTaken = false
	c.focusSeen = false
	c.clips = c.clips[:0]
	if in.KeyPressed(input.KeyEscape) && (c.drag != nil || c.reorder != nil) {
		c.drag, c.reorder = nil, nil // cancelled: nothing is dropped
		c.pressSource = 0
	}
	c.navigate()
	c.lastFocusables, c.focusables = c.focusables, c.lastFocusables[:0]
	c.group = navGroup{}
	c.scrollID = 0
	c.noFocus, c.noRing = false, false
	c.bounds = c.bounds[:0]
	c.dropHover = false
	// A modal that ran last frame owns input from this frame's first
	// widget, whatever order the body submits them in.
	c.modal, c.nextModal = c.nextModal, 0
	c.nodes = c.nodes[:0]
	// Rich text used this frame moves into the current map; whatever
	// stayed behind in the older one goes.
	c.rich, c.richOld = c.richOld, c.rich
	clear(c.rich)
}

// end finishes the frame: deferred overlays draw above everything, the
// drag ghost above those, and the press state settles.
func (c *Context) end() {
	for i := 0; i < len(c.deferred); i++ {
		c.deferred[i]() // an overlay may add more overlays
	}
	c.drawGhost()
	if c.released {
		c.active = 0
		c.drag = nil
		c.reorder = nil
		c.pressSource = 0
	}
	// Keyboard focus ends when its field is gone, or when the press
	// landed somewhere else.
	if c.focus != 0 && (!c.focusSeen || c.pressed && !c.focusRect.Contains(lin.V2(c.pressX, c.pressY))) {
		c.focus = 0
	}
	c.activate = false
	c.lastNodes = append(c.lastNodes[:0], c.nodes...)
}

// pushClip and popClip track the clip a container draws its contents
// under, so hit testing ignores what is scrolled out of view.
func (c *Context) pushClip(r Rect) {
	if n := len(c.clips); n > 0 {
		r = r.Intersect(c.clips[n-1])
	}
	c.clips = append(c.clips, r)
}

func (c *Context) popClip() {
	if n := len(c.clips); n > 0 {
		c.clips = c.clips[:n-1]
	}
}

// visible reports whether a point is inside the current clip.
func (c *Context) visible(p lin.Vec2) bool {
	n := len(c.clips)
	return n == 0 || c.clips[n-1].Contains(p)
}

// WantsMouse reports whether the pointer is over a panel or a drag is in
// progress, so the game can ignore clicks the interface consumed.
func (c *Context) WantsMouse() bool {
	if c.drag != nil || c.reorder != nil {
		return true
	}
	for _, r := range c.frameRects {
		if r.Contains(lin.V2(c.mouseX, c.mouseY)) {
			return true
		}
	}
	return false
}

// WantsKeyboard reports whether a text editor has focus after the most
// recent Begin. It does not report navigation focus or a modal without
// a focused editor; gate gameplay separately while a modal is open.
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
	c.register(id, r)
	if c.navFocus == id {
		if !c.noRing {
			c.focusRing(r)
		}
		if c.activate {
			clicked = true
		}
	}
	mouse := lin.V2(c.mouseX, c.mouseY)
	over := r.Contains(mouse) && c.visible(mouse)
	if c.open != 0 && c.open != id && c.group.id != c.open {
		over = false // an open dropdown list owns the pointer
	}
	if c.drag != nil || c.reorder != nil {
		over = false // a drag owns the pointer until it is released
	}
	if over {
		c.nextHot = id
	}
	hover = c.hot == id && over
	// A press goes to the hot widget, or, when nothing was hot (the
	// pointer arrived and pressed within one frame, as a tap does), to
	// the first widget under it.
	if c.pressed && over && (c.hot == id || c.hot == 0 && !c.pressTaken) {
		c.active = id
		c.pressTaken = true
	}
	held = c.active == id && c.down
	if c.active == id && c.released && over {
		clicked = true
		c.setFocus(id)
	}
	return hover, held, clicked
}

// register adds a navigation stop for id at r under the current group,
// unless a modal excludes it or the caller asked for none.
func (c *Context) register(id widgetID, r Rect) {
	if c.modal != 0 && !c.inModal || c.noFocus {
		return
	}
	c.focusables = append(c.focusables, focusable{id: id, rect: r, group: c.group.id, keys: c.group.keys, page: c.group.page, scroll: c.scrollID})
}

// setFocus moves the navigation focus to id, remembering it for the
// current group so Tab returns to it.
func (c *Context) setFocus(id widgetID) {
	c.navFocus = id
	if c.group.id != 0 {
		c.groupFocus[c.group.id] = id
	}
}

// focusRing outlines r as the focused widget.
func (c *Context) focusRing(r Rect) {
	fw := c.Theme.FocusWidth
	if fw <= 0 {
		fw = 2
	}
	c.ring(Rect{X: r.X - fw, Y: r.Y - fw, W: r.W + 2*fw, H: r.H + 2*fw}, fw, c.Theme.Accent)
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

// richText returns the parsed form of markup, parsing it the first time
// it is seen and keeping it while the interface keeps asking for it.
func (c *Context) richText(markup string) *richEntry {
	if e, ok := c.rich[markup]; ok {
		return e
	}
	if e, ok := c.richOld[markup]; ok {
		c.rich[markup] = e
		return e
	}
	rt := gfx.ParseRich(markup)
	e := &richEntry{rt: rt, plain: rt.Plain()}
	c.rich[markup] = e
	return e
}

// The formatting helpers below build captions in a buffer the context
// reuses, which fmt cannot do because its arguments go through
// interfaces. Each returns one string; the digits match what fmt's
// %.*f, %d and %02x produce.

// formatFloat renders v with prec digits after the point.
func (c *Context) formatFloat(v float32, prec int) string {
	c.buf = strconv.AppendFloat(c.buf[:0], float64(v), 'f', prec, 32)
	return string(c.buf)
}

// formatInt renders v in base ten.
func (c *Context) formatInt(v int) string {
	c.buf = strconv.AppendInt(c.buf[:0], int64(v), 10)
	return string(c.buf)
}

// formatPercent renders v with no digits after the point and a per cent
// sign, for a progress bar's note.
func (c *Context) formatPercent(v float32) string {
	c.buf = strconv.AppendFloat(c.buf[:0], float64(v), 'f', 0, 32)
	c.buf = append(c.buf, '%')
	return string(c.buf)
}

// labelFloat renders "label: value" for a slider's caption.
func (c *Context) labelFloat(label string, v float32, prec int) string {
	c.buf = append(append(c.buf[:0], label...), ':', ' ')
	c.buf = strconv.AppendFloat(c.buf, float64(v), 'f', prec, 32)
	return string(c.buf)
}

// labelInt renders "label: value" for a whole-number slider's caption.
func (c *Context) labelInt(label string, v int) string {
	c.buf = append(append(c.buf[:0], label...), ':', ' ')
	c.buf = strconv.AppendInt(c.buf, int64(v), 10)
	return string(c.buf)
}

// formatHex renders a colour as "#rrggbb".
func (c *Context) formatHex(r, g, b uint8) string {
	const digits = "0123456789abcdef"
	c.buf = append(c.buf[:0], '#',
		digits[r>>4], digits[r&15],
		digits[g>>4], digits[g&15],
		digits[b>>4], digits[b&15])
	return string(c.buf)
}

// textCentred draws s centred in r.
func (c *Context) textCentred(s string, r Rect, col gfx.Color) {
	w, h := c.Theme.Font.Measure(s, gfx.TextOptions{})
	c.text(s, r.X+(r.W-w)/2, r.Y+(r.H-h)/2, col)
}
