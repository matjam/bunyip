package ui

import (
	"os"
	"testing"
	"unicode/utf8"

	"github.com/matjam/bunyip/gfx"
)

// clusterStarts is the rune offsets a caret may stand at in text as the
// font's layout shapes it: every offset some caret of the layout names.
func clusterStarts(t *testing.T, f *gfx.Font, text string) map[int]bool {
	t.Helper()
	l, err := f.Layout(text, gfx.TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	out := map[int]bool{}
	for b := 0; b <= len(text); b++ {
		if b < len(text) && !utf8.RuneStart(text[b]) {
			continue
		}
		// Caret snaps an offset to a cluster edge; an offset that stays
		// put is one.
		r := l.Caret(gfx.TextCaret{Index: b})
		if hit := l.HitTest(r.Min().Add(r.Size().Mul(0.5))); hit.Index == b {
			out[utf8.RuneCountInString(text[:b])] = true
		}
	}
	return out
}

// The caret hit test asks the text's layout once instead of measuring
// every prefix, and lands where measuring prefixes did: on the same
// offset for plain text, and on the same offset for text with combining
// marks and emoji wherever measuring did not split a cluster, which it
// could and the layout cannot.
func TestCaretHitTestMatchesPrefixMeasurement(t *testing.T) {
	c := newContext(t)
	f := c.Theme.Font
	for _, text := range []string{
		"The quick brown fox, 42 times.",
		"café and naïve",
		"flag 🇦🇺 family 👩‍👩‍👧 end",
	} {
		width, _ := f.Measure(text, gfx.TextOptions{})
		clusters := clusterStarts(t, f, text)
		for i := 0; i <= utf8.RuneCountInString(text); i++ {
			if !clusters[i] {
				continue
			}
			if got, want := c.caretX(text, i), c.measuredCaretX(text, i); abs32(got-want) > 0.01 {
				t.Errorf("%q: caret %d at x %v, prefix measures %v", text, i, got, want)
			}
		}
		for x := float32(-4); x <= width+4; x += 0.25 {
			got, want := c.indexAt(text, x), c.measuredIndexAt(text, x)
			if !clusters[got] {
				t.Fatalf("%q: x %v hit offset %d inside a cluster", text, x, got)
			}
			if clusters[want] && got != want {
				t.Errorf("%q: x %v hit offset %d, prefix measurement %d", text, x, got, want)
			}
		}
	}
}

// Right-to-left text puts its first caret at the right, which measuring
// prefixes from the left could not; clicking at a caret finds it again.
func TestCaretHitTestRightToLeft(t *testing.T) {
	data, err := os.ReadFile("/System/Library/Fonts/Supplemental/Arial.ttf")
	if err != nil {
		t.Skip("no Arial.ttf on this system for right-to-left scripts")
	}
	c := newContext(t)
	f, err := c.g.NewFont(data, 16, gfx.FontOptions{AtlasSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	c.Theme.Font = f
	text := "שלום עולם"
	n := utf8.RuneCountInString(text)
	if c.caretX(text, 0) <= c.caretX(text, n) {
		t.Fatalf("right-to-left text starts at x %v and ends at %v", c.caretX(text, 0), c.caretX(text, n))
	}
	for i := 0; i <= n; i++ {
		if got := c.indexAt(text, c.caretX(text, i)); got != i {
			t.Errorf("caret %d at x %v hit offset %d", i, c.caretX(text, i), got)
		}
	}
}
