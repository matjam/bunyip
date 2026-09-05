package ui_test

import (
	"testing"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/ui"
)

type modalClipboard struct {
	text          string
	reads, writes int
}

func (c *modalClipboard) Clipboard() (string, error) {
	c.reads++
	return c.text, nil
}

func (c *modalClipboard) SetClipboard(s string) error {
	c.writes++
	c.text = s
	return nil
}

type modalTextFixture struct {
	h              *harness
	role           string
	behind, inside string
	open, omit     bool
	behindChanged  bool
	textRects      []ui.Rect
	clip           modalClipboard
	body           func()
}

func newModalTextFixture(t *testing.T, role string) *modalTextFixture {
	t.Helper()
	f := &modalTextFixture{h: newHarness(t), role: role, behind: "behind", inside: "inside"}
	c := f.h.c
	c.Clipboard = &f.clip
	c.OnTextInputRect = func(x, y, w, h float32) {
		f.textRects = append(f.textRects, ui.Rect{X: x, Y: y, W: w, H: h})
	}
	widget := func(label string, value *string) bool {
		if role == "textarea" {
			return c.TextArea(label, value, 65)
		}
		return c.TextField(label, value)
	}
	f.body = func() {
		// The background is deliberately submitted before the modal.
		c.Panel("Background", ui.Rect{X: 10, Y: 10, W: 300, H: 180}, func() {
			f.behindChanged = widget("Behind", &f.behind)
		})
		if !f.omit {
			c.Modal("Modal", ui.Rect{X: 50, Y: 210, W: 300, H: 150}, &f.open, func() {
				widget("Inside", &f.inside)
			})
		}
	}
	f.frame(t)
	f.click(t, "Behind")
	if !c.WantsKeyboard() {
		t.Fatal("background editor did not acquire focus")
	}
	return f
}

func (f *modalTextFixture) frame(t *testing.T) {
	t.Helper()
	f.textRects = f.textRects[:0]
	f.h.frame(t, f.body)
}

func (f *modalTextFixture) click(t *testing.T, label string) {
	t.Helper()
	r := rectOf(t, f.h.c.Accessible(), f.role, label)
	f.h.click(t, f.body, r.X+r.W-10, r.Y+r.H/2)
}

func (f *modalTextFixture) key(t *testing.T, key input.Key, mods input.Mods) {
	t.Helper()
	f.h.in.FeedKey(uint8(key), true, false, uint8(mods))
	f.h.in.FeedKey(uint8(key), false, false, uint8(mods))
	f.frame(t)
}

func TestModalBlocksTextEditing(t *testing.T) {
	for _, role := range []string{"textfield", "textarea"} {
		t.Run(role, func(t *testing.T) {
			f := newModalTextFixture(t, role)
			f.open = true
			f.frame(t)
			f.frame(t)
			f.h.in.FeedChar('x')
			f.frame(t)
			if f.behind != "behind" || f.behindChanged {
				t.Fatalf("text behind modal changed to %q (changed=%v)", f.behind, f.behindChanged)
			}
		})
	}
}

func TestModalBlocksTextCommands(t *testing.T) {
	cases := []struct {
		name string
		key  input.Key
		mods input.Mods
		char rune
	}{
		{name: "type", char: 'x'},
		{name: "paste", key: input.KeyV, mods: input.ModControl},
		{name: "cut", key: input.KeyX, mods: input.ModControl},
		{name: "copy", key: input.KeyC, mods: input.ModControl},
		{name: "undo", key: input.KeyZ, mods: input.ModControl},
		{name: "redo", key: input.KeyY, mods: input.ModControl},
		{name: "backspace", key: input.KeyBackspace},
		{name: "delete", key: input.KeyDelete},
		{name: "enter", key: input.KeyEnter},
	}
	for _, role := range []string{"textfield", "textarea"} {
		t.Run(role, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					f := newModalTextFixture(t, role)
					f.h.in.FeedChar('!')
					f.frame(t)
					f.h.in.FeedChar('?')
					f.frame(t)
					f.key(t, input.KeyZ, input.ModControl) // both undo and redo now have history
					f.key(t, input.KeyA, input.ModControl)
					want := f.behind
					f.clip.text = "paste"
					f.open = true
					f.frame(t)
					if tc.char != 0 {
						// Clear the shortcut modifier before committing a character.
						f.h.in.FeedKey(uint8(input.KeyA), false, false, 0)
						f.h.in.FeedChar(tc.char)
						f.frame(t)
					} else {
						f.key(t, tc.key, tc.mods)
					}
					if f.behind != want || f.behindChanged {
						t.Errorf("background changed from %q to %q (changed=%v)", want, f.behind, f.behindChanged)
					}
					if f.clip.reads != 0 || f.clip.writes != 0 || f.clip.text != "paste" {
						t.Errorf("background used clipboard: %+v", f.clip)
					}
				})
			}
		})
	}
}

func TestModalTextFocusLifecycle(t *testing.T) {
	for _, role := range []string{"textfield", "textarea"} {
		t.Run(role, func(t *testing.T) {
			f := newModalTextFixture(t, role)
			f.open = true
			f.frame(t)
			f.h.in.FeedComposition("composition")
			f.frame(t)
			if f.h.c.WantsKeyboard() || len(f.textRects) != 0 {
				t.Errorf("background retained text/IME ownership: keyboard=%v rects=%v", f.h.c.WantsKeyboard(), f.textRects)
			}
			for _, node := range f.h.c.Accessible() {
				if node.Label == "Behind" && (node.State || node.Focused) {
					t.Errorf("background reports focus under modal: %+v", node)
				}
			}
			f.h.in.FeedComposition("")
			f.click(t, "Inside")
			f.h.in.FeedChar('!')
			f.frame(t)
			if f.inside != "inside!" || f.behind != "behind" {
				t.Fatalf("typing in modal: behind=%q inside=%q", f.behind, f.inside)
			}
			insideRect := rectOf(t, f.h.c.Accessible(), role, "Inside")
			if len(f.textRects) != 1 || f.textRects[0] != insideRect {
				t.Errorf("modal IME rectangle = %v, want %v", f.textRects, insideRect)
			}
			f.key(t, input.KeyA, input.ModControl)
			f.key(t, input.KeyX, input.ModControl)
			if f.inside != "" || f.clip.text != "inside!" {
				t.Fatalf("cut inside modal: text=%q clipboard=%q", f.inside, f.clip.text)
			}
			f.key(t, input.KeyV, input.ModControl)
			f.key(t, input.KeyZ, input.ModControl)
			if f.inside != "" {
				t.Fatalf("undo inside modal: %q", f.inside)
			}
			f.key(t, input.KeyY, input.ModControl)
			if f.inside != "inside!" {
				t.Fatalf("redo inside modal: %q", f.inside)
			}
			// Closing releases the modal editor; background focus needs a new click.
			f.open = false
			f.frame(t)
			f.h.in.FeedKey(uint8(input.KeyY), false, false, 0)
			f.h.in.FeedChar('x')
			f.frame(t)
			if f.h.c.WantsKeyboard() || f.behind != "behind" || f.inside != "inside!" {
				t.Fatal("closed modal retained text focus or restored background focus")
			}
			f.click(t, "Behind")
			f.h.in.FeedChar('!')
			f.frame(t)
			if f.behind != "behind!" {
				t.Fatalf("background cannot be focused after close: %q", f.behind)
			}
			f.open = true
			f.frame(t)
			f.frame(t)
			f.click(t, "Inside")
			f.omit = true // removal has the same focus cleanup as clearing open
			f.frame(t)
			if f.h.c.WantsKeyboard() {
				t.Fatal("removed modal retained text focus")
			}
		})
	}
}
