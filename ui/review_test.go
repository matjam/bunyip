package ui_test

import (
	"io"
	"log/slog"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/ui"
)

// harness drives a Context the way the engine loop does: feed events,
// run an update, draw a frame.
type harness struct {
	c  *ui.Context
	gd hook.Graphics
	in hook.Input
	st *input.State
}

func newHarness(t testing.TB) *harness {
	t.Helper()
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := render.Config{AppName: "review", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	r, err := render.NewRenderer(cfg, render.HeadlessSurfaceExtensions(), render.NewHeadlessSurface, vk.VkExtent2D{Width: 400, Height: 400}, true)
	if err != nil {
		t.Skipf("no renderer: %v", err)
	}
	gd, err := hook.NewGraphics(r)
	if err != nil {
		t.Fatal(err)
	}
	g := gd.Game().(*gfx.Graphics)
	font, err := g.NewFont(goregular.TTF, 14, gfx.FontOptions{AtlasSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	in := hook.NewInput()
	h := &harness{c: ui.New(g, ui.DarkTheme(font)), gd: gd, in: in, st: in.Game().(*input.State)}
	t.Cleanup(func() { font.Destroy(); gd.Destroy(); r.Destroy() })
	return h
}

// frame runs one update, then one drawn frame with body.
func (h *harness) frame(t testing.TB, body func()) {
	t.Helper()
	h.in.EndUpdate()
	h.in.SetDrawing(true)
	defer func() { h.in.SetDrawing(false); h.in.EndFrame() }()
	ok, err := h.gd.Begin([4]float32{0, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return
	}
	h.c.Begin(h.st, body)
	if _, err := h.gd.End(false); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) move(x, y float32)    { h.in.FeedMouseMove(x, y) }
func (h *harness) press(x, y float32)   { h.in.FeedMouseButton(uint8(input.MouseLeft), true, x, y) }
func (h *harness) release(x, y float32) { h.in.FeedMouseButton(uint8(input.MouseLeft), false, x, y) }
func (h *harness) key(k input.Key) {
	h.in.FeedKey(uint8(k), true, false, 0)
	h.in.FeedKey(uint8(k), false, false, 0)
}

func rectOf(t testing.TB, nodes []ui.AccessibleNode, role, label string) ui.Rect {
	t.Helper()
	for _, n := range nodes {
		if n.Role == role && n.Label == label {
			return n.Rect
		}
	}
	t.Fatalf("no %s %q", role, label)
	return ui.Rect{}
}

// click moves to a point, presses and releases over three frames.
func (h *harness) click(t testing.TB, body func(), x, y float32) {
	h.move(x, y)
	h.frame(t, body)
	h.press(x, y)
	h.frame(t, body)
	h.release(x, y)
	h.frame(t, body)
}

// A modal owns the input even for widgets submitted before it.
func TestModalTrapsEarlierWidgets(t *testing.T) {
	h := newHarness(t)
	c := h.c
	open := true
	clicks := 0
	body := func() {
		c.Panel("P", ui.Rect{X: 10, Y: 10, W: 300, H: 300}, func() {
			if c.Button("Behind") {
				clicks++
			}
		})
		c.Modal("Sure?", ui.Rect{X: 100, Y: 200, W: 200, H: 100}, &open, func() { c.Button("OK") })
	}
	h.frame(t, body)
	h.frame(t, body)
	r := rectOf(t, c.Accessible(), "button", "Behind")
	h.click(t, body, r.X+r.W/2, r.Y+r.H/2)
	if clicks != 0 {
		t.Errorf("button behind the modal clicked %d times", clicks)
	}
}

// Rows scrolled out of a list box's clip do not take the pointer.
func TestClippedRowsIgnorePointer(t *testing.T) {
	h := newHarness(t)
	c := h.c
	items := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	sel := -1
	body := func() {
		c.Panel("P", ui.Rect{X: 10, Y: 10, W: 300, H: 350}, func() {
			c.ListBox("items", 70, items, &sel)
			c.Label("below the list")
		})
	}
	h.frame(t, body)
	h.frame(t, body)
	r := rectOf(t, c.Accessible(), "listbox", "items")
	h.click(t, body, r.X+20, r.Y+r.H+40) // well below the box
	if sel != -1 {
		t.Errorf("a hidden row took the click: selected %d", sel)
	}
}

// Dragging in a focused text field selects text.
func TestTextDragSelects(t *testing.T) {
	h := newHarness(t)
	c := h.c
	text := "hello world"
	body := func() {
		c.Panel("P", ui.Rect{X: 10, Y: 10, W: 300, H: 200}, func() { c.TextField("name", &text) })
	}
	h.frame(t, body)
	h.frame(t, body)
	r := rectOf(t, c.Accessible(), "textfield", "name")
	x0, x1, y := r.X+c.Theme.Padding+1, r.X+c.Theme.Padding+70, r.Y+r.H/2
	h.click(t, body, x0, y)
	if !c.WantsKeyboard() {
		t.Fatal("field did not take focus")
	}
	h.press(x0, y)
	h.frame(t, body)
	h.move(x1, y)
	h.frame(t, body)
	h.frame(t, body)
	h.release(x1, y)
	h.frame(t, body)
	before := text
	h.key(input.KeyBackspace)
	h.frame(t, body)
	if len(before)-len(text) <= 1 {
		t.Errorf("backspace after a drag removed %d characters; no selection was made", len(before)-len(text))
	}
}

// A move, press and release arriving in one frame, as a touchpad tap
// does, still clicks.
func TestTapWithinOneFrameClicks(t *testing.T) {
	h := newHarness(t)
	c := h.c
	clicks := 0
	body := func() {
		c.Panel("P", ui.Rect{X: 10, Y: 10, W: 300, H: 200}, func() {
			if c.Button("Go") {
				clicks++
			}
		})
	}
	h.frame(t, body)
	h.frame(t, body)
	r := rectOf(t, c.Accessible(), "button", "Go")
	x, y := r.X+r.W/2, r.Y+r.H/2
	h.move(x, y)
	h.press(x, y)
	h.release(x, y)
	h.frame(t, body)
	h.frame(t, body)
	if clicks != 1 {
		t.Errorf("one-frame tap gave %d clicks, want 1", clicks)
	}
}

// Keyboard focus ends when its field is no longer submitted.
func TestFocusEndsWithField(t *testing.T) {
	h := newHarness(t)
	c := h.c
	show := true
	text := ""
	body := func() {
		c.Panel("P", ui.Rect{X: 10, Y: 10, W: 300, H: 200}, func() {
			if show {
				c.TextField("name", &text)
			}
		})
	}
	h.frame(t, body)
	h.frame(t, body)
	r := rectOf(t, c.Accessible(), "textfield", "name")
	h.click(t, body, r.X+r.W/2, r.Y+r.H/2)
	if !c.WantsKeyboard() {
		t.Fatal("field did not take focus")
	}
	show = false
	h.frame(t, body)
	if c.WantsKeyboard() {
		t.Error("focus outlived the field")
	}
}
