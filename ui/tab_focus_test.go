package ui_test

import (
	"testing"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/ui"
)

type tabFixture struct {
	h         *harness
	roles     [2]string
	values    [2]string
	omit      [2]bool
	clicks    int
	textRects []ui.Rect
	body      func()
}

func newTabFixture(t *testing.T, roles [2]string) *tabFixture {
	t.Helper()
	f := &tabFixture{h: newHarness(t), roles: roles, values: [2]string{"first", "second"}}
	c := f.h.c
	c.OnTextInputRect = func(x, y, w, h float32) {
		f.textRects = append(f.textRects, ui.Rect{X: x, Y: y, W: w, H: h})
	}
	f.body = func() {
		c.Panel("Navigation", ui.Rect{X: 10, Y: 10, W: 300, H: 300}, func() {
			for i, label := range []string{"A", "B"} {
				if f.omit[i] {
					continue
				}
				if f.roles[i] == "textarea" {
					c.TextArea(label, &f.values[i], 65)
				} else {
					c.TextField(label, &f.values[i])
				}
			}
			if c.Button("Go") {
				f.clicks++
			}
		})
	}
	f.frame(t)
	return f
}

func (f *tabFixture) frame(t *testing.T) {
	t.Helper()
	f.textRects = f.textRects[:0]
	f.h.frame(t, f.body)
}

func (f *tabFixture) click(t *testing.T, i int) {
	t.Helper()
	r := rectOf(t, f.h.c.Accessible(), f.roles[i], []string{"A", "B"}[i])
	f.h.click(t, f.body, r.X+r.W-10, r.Y+r.H/2)
}

func (f *tabFixture) key(t *testing.T, key input.Key, mods input.Mods) {
	t.Helper()
	f.h.in.FeedKey(uint8(key), true, false, uint8(mods))
	f.h.in.FeedKey(uint8(key), false, false, uint8(mods))
	f.frame(t)
}

func TestTabTransfersTextFocus(t *testing.T) {
	for _, roles := range [][2]string{{"textfield", "textfield"}, {"textfield", "textarea"}, {"textarea", "textfield"}, {"textarea", "textarea"}} {
		t.Run(roles[0]+"_"+roles[1], func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				reverse bool
				pad     bool
			}{
				{name: "Tab"}, {name: "ShiftTab", reverse: true},
				{name: "DpadDown", pad: true}, {name: "DpadUp", reverse: true, pad: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					f := newTabFixture(t, roles)
					from, to := 0, 1
					mods := input.Mods(0)
					button := input.ButtonDpadDown
					if tc.reverse {
						from, to = 1, 0
						mods, button = input.ModShift, input.ButtonDpadUp
					}
					f.click(t, from)
					before := f.values
					if tc.pad {
						var buttons [input.GamepadButtonCount]bool
						buttons[button] = true
						f.h.in.FeedGamepad(0, true, "pad", buttons, [input.GamepadAxisCount]float32{})
					} else {
						f.h.in.FeedKey(uint8(input.KeyTab), true, false, uint8(mods))
						f.h.in.FeedKey(uint8(input.KeyTab), false, false, uint8(mods))
					}
					// Input in the navigation frame must already belong to the destination,
					// including when it is submitted before the old editor.
					f.h.in.FeedChar('x')
					f.frame(t)
					f.h.in.FeedGamepad(0, true, "pad", [input.GamepadButtonCount]bool{}, [input.GamepadAxisCount]float32{})
					f.h.in.FeedChar('y')
					f.frame(t)
					if f.values[from] != before[from] || f.values[to] != "xy"+before[to] {
						t.Fatalf("navigation then typing: %q, want unchanged source and xy at destination caret", f.values)
					}
					r := rectOf(t, f.h.c.Accessible(), roles[to], []string{"A", "B"}[to])
					if !f.h.c.WantsKeyboard() || len(f.textRects) != 1 || f.textRects[0] != r {
						t.Errorf("destination text/IME ownership: keyboard=%v rects=%v", f.h.c.WantsKeyboard(), f.textRects)
					}
					for _, n := range f.h.c.Accessible() {
						if n.Role == "textfield" || n.Role == "textarea" {
							want := n.Label == []string{"A", "B"}[to]
							if n.State != want || n.Focused != want {
								t.Errorf("editor focus disagrees with navigation: %+v", n)
							}
						}
					}
				})
			}
		})
	}
}

func TestTabReleasesTextForButton(t *testing.T) {
	for _, role := range []string{"textfield", "textarea"} {
		t.Run(role, func(t *testing.T) {
			f := newTabFixture(t, [2]string{role, role})
			f.click(t, 1)
			f.key(t, input.KeyTab, 0)
			f.h.in.FeedChar('x')
			f.frame(t)
			if f.h.c.WantsKeyboard() || f.values != [2]string{"first", "second"} || len(f.textRects) != 0 {
				t.Fatalf("button retained editor input: values=%q keyboard=%v", f.values, f.h.c.WantsKeyboard())
			}
			f.key(t, input.KeyEnter, 0)
			if f.clicks != 1 {
				t.Fatalf("button Enter activations=%d, want 1", f.clicks)
			}
			f.key(t, input.KeyTab, input.ModShift)
			f.h.in.FeedChar('!')
			f.frame(t)
			if f.values[1] != "second!" {
				t.Errorf("return to editor lost its caret: %q", f.values[1])
			}
		})
	}
}

func TestTabRetainsTextSelection(t *testing.T) {
	for _, role := range []string{"textfield", "textarea"} {
		t.Run(role, func(t *testing.T) {
			f := newTabFixture(t, [2]string{role, role})
			f.click(t, 0)
			f.key(t, input.KeyLeft, input.ModShift)
			f.key(t, input.KeyLeft, input.ModShift)
			f.key(t, input.KeyTab, 0)
			f.key(t, input.KeyTab, input.ModShift)
			f.h.in.FeedChar('!')
			f.frame(t)
			if f.values[0] != "fir!" {
				t.Fatalf("navigation did not preserve selection: %q", f.values[0])
			}
			// A subsequent mouse click still places the caret at the pointer.
			r := rectOf(t, f.h.c.Accessible(), role, "A")
			f.h.click(t, f.body, r.X+f.h.c.Theme.Padding, r.Y+f.h.c.Theme.Padding)
			f.h.in.FeedChar('x')
			f.frame(t)
			if f.values[0] != "xfir!" {
				t.Errorf("mouse click did not replace navigation caret: %q", f.values[0])
			}
		})
	}
}

func TestTabDoesNotRestoreReleasedTextFocus(t *testing.T) {
	for _, tc := range []struct {
		role string
		key  input.Key
	}{{"textfield", input.KeyEnter}, {"textfield", input.KeyEscape}, {"textarea", input.KeyEscape}} {
		t.Run(tc.role+"_"+tc.key.String(), func(t *testing.T) {
			f := newTabFixture(t, [2]string{tc.role, tc.role})
			f.key(t, input.KeyTab, 0)
			if !f.h.c.WantsKeyboard() {
				t.Fatal("initial Tab did not focus editor")
			}
			f.key(t, tc.key, 0)
			f.h.in.FeedChar('x')
			f.frame(t)
			if f.h.c.WantsKeyboard() || f.values != [2]string{"first", "second"} {
				t.Fatal("retained navigation ring restored released text focus")
			}
		})
	}
}

func TestTabTargetDisappears(t *testing.T) {
	f := newTabFixture(t, [2]string{"textfield", "textarea"})
	f.click(t, 0)
	f.omit[1] = true
	f.key(t, input.KeyTab, 0)
	if f.h.c.WantsKeyboard() {
		t.Fatal("absent navigation target retained old editor focus")
	}
	f.omit[1] = false
	f.h.in.FeedChar('x')
	f.frame(t)
	if f.h.c.WantsKeyboard() || f.values != [2]string{"first", "second"} {
		t.Fatal("returning target acquired stale navigation focus")
	}
}

func TestTabReleasesTextForArrowWidget(t *testing.T) {
	h := newHarness(t)
	text, value := "text", 5
	body := func() {
		h.c.Panel("P", ui.Rect{X: 10, Y: 10, W: 300, H: 200}, func() {
			h.c.TextField("Text", &text)
			h.c.IntSlider("Value", &value, 0, 10)
		})
	}
	h.frame(t, body)
	r := rectOf(t, h.c.Accessible(), "textfield", "Text")
	h.click(t, body, r.X+r.W/2, r.Y+r.H/2)
	h.key(input.KeyTab)
	h.frame(t, body)
	h.key(input.KeyRight)
	h.frame(t, body)
	if value != 6 || h.c.WantsKeyboard() {
		t.Fatalf("slider keyboard blocked by old editor: value=%d keyboard=%v", value, h.c.WantsKeyboard())
	}
}

func TestTabTextFocusInModal(t *testing.T) {
	f := newTabFixture(t, [2]string{"textfield", "textarea"})
	open := false
	a, b, clicks := "inside", "area", 0
	background := f.body
	f.body = func() {
		background()
		f.h.c.Modal("Modal", ui.Rect{X: 30, Y: 30, W: 300, H: 250}, &open, func() {
			f.h.c.TextField("Inside", &a)
			f.h.c.TextArea("Area", &b, 65)
			if f.h.c.Button("Done") {
				clicks++
			}
		})
	}
	f.click(t, 0)
	open = true
	f.frame(t)
	f.frame(t)
	f.key(t, input.KeyTab, 0)
	f.h.in.FeedChar('x')
	f.frame(t)
	f.key(t, input.KeyTab, 0)
	f.h.in.FeedChar('y')
	f.frame(t)
	f.key(t, input.KeyEnter, 0)
	if a != "xinside" || b != "y\narea" || f.values != [2]string{"first", "second"} {
		t.Fatalf("modal navigation edited wrong field: inside=%q area=%q background=%q", a, b, f.values)
	}
	f.key(t, input.KeyTab, input.ModShift)
	f.key(t, input.KeyTab, input.ModShift) // wrap to Done within modal
	f.key(t, input.KeyEnter, 0)
	if clicks != 1 || f.h.c.WantsKeyboard() {
		t.Fatalf("modal button activation: clicks=%d keyboard=%v", clicks, f.h.c.WantsKeyboard())
	}
	f.key(t, input.KeyTab, 0)
	open = false
	f.frame(t)
	f.h.in.FeedChar('z')
	f.frame(t)
	if f.h.c.WantsKeyboard() || f.values != [2]string{"first", "second"} || a != "xinside" {
		t.Fatal("closing modal restored stale editor focus")
	}
}
