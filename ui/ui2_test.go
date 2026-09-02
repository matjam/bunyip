package ui

import (
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
)

// fakeClipboard stands in for the system clipboard.
type fakeClipboard struct{ s string }

func (f *fakeClipboard) Clipboard() (string, error)  { return f.s, nil }
func (f *fakeClipboard) SetClipboard(s string) error { f.s = s; return nil }

// run runs one interface frame with body as the whole interface.
func run(t *testing.T, c *Context, in *feeder, body func()) {
	t.Helper()
	in.EndUpdate()
	in.SetDrawing(true)
	defer func() {
		in.SetDrawing(false)
		in.EndFrame()
	}()
	ok, err := beginFrame(c)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return
	}
	c.Begin(in.state, body)
	if err := endFrame(c); err != nil {
		t.Fatal(err)
	}
}

func press(in *feeder, k input.Key, mods input.Mods) {
	in.FeedKey(k, true, false, mods)
	in.FeedKey(k, false, false, mods)
}

func TestTextEditing(t *testing.T) {
	c := newContext(t)
	clip := &fakeClipboard{}
	c.Clipboard = clip
	in := newFeeder()
	text := "hello"
	field := func() {
		c.Panel("T", Rect{X: 10, Y: 10, W: 200, H: 200}, func() { c.TextField("name", &text) })
	}
	// Click into the field to focus it: the field is the first row. A
	// frame with the pointer over it first, since hover is a frame behind.
	in.FeedMouseMove(60, 44)
	run(t, c, in, field)
	in.FeedMouseButton(input.MouseLeft, true, 60, 44)
	run(t, c, in, field)
	in.FeedMouseButton(input.MouseLeft, false, 60, 44)
	run(t, c, in, field)
	if !c.WantsKeyboard() {
		t.Fatal("field did not take focus")
	}
	// Select all, copy, move to the end, paste: "hellohello".
	press(in, input.KeyA, input.ModControl)
	run(t, c, in, field)
	press(in, input.KeyC, input.ModControl)
	run(t, c, in, field)
	if clip.s != "hello" {
		t.Errorf("copied %q", clip.s)
	}
	press(in, input.KeyEnd, 0)
	run(t, c, in, field)
	press(in, input.KeyV, input.ModControl)
	run(t, c, in, field)
	if text != "hellohello" {
		t.Errorf("after paste %q", text)
	}
	// Shift+Left twice selects "lo"; typing replaces it.
	press(in, input.KeyLeft, input.ModShift)
	run(t, c, in, field)
	press(in, input.KeyLeft, input.ModShift)
	run(t, c, in, field)
	in.FeedChar('X')
	run(t, c, in, field)
	if text != "hellohelX" {
		t.Errorf("after replacing a selection %q", text)
	}
	// Undo twice restores the paste, redo brings it back.
	press(in, input.KeyZ, input.ModControl)
	run(t, c, in, field)
	if text != "hellohello" {
		t.Errorf("after undo %q", text)
	}
	press(in, input.KeyZ, input.ModControl)
	run(t, c, in, field)
	if text != "hello" {
		t.Errorf("after second undo %q", text)
	}
	press(in, input.KeyY, input.ModControl)
	run(t, c, in, field)
	if text != "hellohello" {
		t.Errorf("after redo %q", text)
	}
	// Word jump and Home.
	press(in, input.KeyHome, 0)
	run(t, c, in, field)
	press(in, input.KeyDelete, 0)
	run(t, c, in, field)
	if text != "ellohello" {
		t.Errorf("after Home and Delete %q", text)
	}
	press(in, input.KeyEscape, 0)
	run(t, c, in, field)
	if c.WantsKeyboard() {
		t.Error("Escape did not drop focus")
	}
}

func TestWrapRunes(t *testing.T) {
	c := newContext(t)
	lines := wrapRunes(c.Theme.Font, "one two three four five\nsix", 80)
	if len(lines) < 3 {
		t.Fatalf("got %d lines: %v", len(lines), lines)
	}
	last := lines[len(lines)-1]
	if string([]rune("one two three four five\nsix")[last[0]:last[1]]) != "six" {
		t.Errorf("last line is not the hard break: %v", last)
	}
	if lineStep(lines, []rune("one two three four five\nsix"), 1, 1) < lines[1][0] {
		t.Error("down did not move to the next line")
	}
}

func TestLayoutWidgets(t *testing.T) {
	c := newContext(t)
	in := newFeeder()
	tab, radio, sel, spin := 0, 0, -1, 5
	col := gfx.RGB(255, 0, 0)
	modal := true
	var clicks int
	var nodes []AccessibleNode
	body := func() {
		c.MenuBar(Rect{X: 0, Y: 0, W: 320, H: 24}, func() {
			c.Menu("File", func() { c.MenuItem("Quit") })
		})
		c.Panel("L", Rect{X: 10, Y: 30, W: 300, H: 200}, func() {
			c.Tabs([]string{"One", "Two"}, &tab)
			c.RadioGroup(&radio, []string{"a", "b"})
			c.Spinner("n", &spin, 0, 10, 1)
			c.ListBox("items", 60, []string{"x", "y", "z"}, &sel)
			c.Tree("node", func() { c.Label("inside") })
			c.ColorPicker("tint", &col)
			if c.Button("Hidden") {
				clicks++
			}
		})
		c.Modal("Sure?", Rect{X: 60, Y: 60, W: 200, H: 100}, &modal, func() {
			if c.Button("OK") {
				modal = false
			}
		})
		nodes = c.Accessible()
	}
	run(t, c, in, body)
	run(t, c, in, body)
	roles := map[string]int{}
	for _, n := range nodes {
		roles[n.Role]++
	}
	for _, want := range []string{"menu", "tab", "radio", "spinner", "listbox", "tree", "colorpicker", "button"} {
		if roles[want] == 0 {
			t.Errorf("no %s node in %v", want, roles)
		}
	}
	// With the modal open, a click on the panel's button does nothing.
	in.FeedMouseMove(160, 200)
	in.FeedMouseButton(input.MouseLeft, true, 160, 200)
	run(t, c, in, body)
	in.FeedMouseButton(input.MouseLeft, false, 160, 200)
	run(t, c, in, body)
	if clicks != 0 {
		t.Error("a button behind the modal took a click")
	}
	if !modal {
		t.Error("the modal closed on a click outside it")
	}
	if got := Anchored(Rect{W: 100, H: 100}, BottomRight, 10, 10, 2); got != (Rect{X: 88, Y: 88, W: 10, H: 10}) {
		t.Errorf("Anchored = %v", got)
	}
}
