package gfx

import (
	"os"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
)

// kernFont returns a system font with a kern pair for AV, or skips. Go
// Regular has no kern pairs, so a font the platform ships stands in.
func kernFont(t *testing.T) []byte {
	t.Helper()
	for _, path := range []string{
		"/System/Library/Fonts/Supplemental/Arial.ttf",
		"/usr/share/fonts/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
		"/usr/share/fonts/gsfonts/NimbusSans-Regular.otf",
	} {
		if data, err := os.ReadFile(path); err == nil {
			return data
		}
	}
	t.Skip("no kerned system font on this machine")
	return nil
}

// TestRichTextShapesAcrossStyleChanges checks that rich text shapes each
// stretch of one font as a whole, so a kern pair is kerned and a colour
// change inside it does not widen the text.
func TestRichTextShapesAcrossStyleChanges(t *testing.T) {
	g := newHeadless(t, 128, 64)
	f, err := g.NewFont(kernFont(t), 20, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	fonts := RichFonts{Regular: f}
	plain, _ := f.Measure("AV", TextOptions{})
	a, _ := f.Measure("A", TextOptions{})
	v, _ := f.Measure("V", TextOptions{})
	if plain >= a+v-0.01 {
		t.Fatalf("AV %.2f is not kerned in this font; the test proves nothing", plain)
	}
	cases := []struct {
		name string
		text RichText
	}{
		{"one run", RichText{Runs: []RichRun{{Text: "AV"}}}},
		{"colour change", ParseRich("A[#ff0000]V[/#]")},
		{"link change", ParseRich("A[link=x]V[/link]")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.text.Plain() != "AV" {
				t.Fatalf("the case does not spell AV: %q", c.text.Plain())
			}
			w, _ := fonts.MeasureRich(c.text, TextOptions{})
			if w < plain-0.01 || w > plain+0.01 {
				t.Errorf("MeasureRich = %.2f, want the shaped width %.2f", w, plain)
			}
		})
	}
	// A font change still splits the shaping, since the glyphs come from
	// different faces.
	b, err := g.NewFont(gobold.TTF, 20, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()
	bold := RichFonts{Regular: f, Bold: b}
	wb, _ := bold.MeasureRich(ParseRich("A[b]V[/b]"), TextOptions{})
	bv, _ := b.Measure("V", TextOptions{})
	if wb < a+bv-0.01 || wb > a+bv+0.01 {
		t.Errorf("a bold V after a regular A measured %.2f, want %.2f", wb, a+bv)
	}
}

// TestRichTextDrawsWhereDrawTextBlockDoes checks that a rich line of one
// style lands on the same pixels as the same string drawn plainly, so
// shaping the run as a whole moved nothing.
func TestRichTextDrawsWhereDrawTextBlockDoes(t *testing.T) {
	g := newHeadless(t, 192, 48)
	g.SetView(192, 48)
	f, err := g.NewFont(kernFont(t), 20, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	const text = "Away AVAST"
	plain := frame2D(t, g, func() { g.DrawTextBlock(f, text, 6, 6, TextOptions{}, White) })
	rich := frame2D(t, g, func() {
		g.DrawRichText(RichFonts{Regular: f}, ParseRich("Away [#ffffff]AVAST[/#]"), 6, 6, TextOptions{}, White)
	})
	same, lit := 0, 0
	for y := range 48 {
		for x := range 192 {
			a, b := bright(plain, x, y), bright(rich, x, y)
			if a {
				lit++
			}
			if a == b {
				same++
			}
		}
	}
	if lit < 50 {
		t.Fatalf("the plain text drew only %d bright pixels", lit)
	}
	if diff := 192*48 - same; diff > 0 {
		t.Errorf("%d pixels differ between the plain and the rich drawing of %q", diff, text)
	}
}

// TestRichTextWrapsOnShapedWidths checks that wrapping a shaped run cuts
// it at a word boundary and keeps the pieces inside the width.
func TestRichTextWrapsOnShapedWidths(t *testing.T) {
	g := newHeadless(t, 256, 128)
	f, err := g.NewFont(kernFont(t), 16, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	fonts := RichFonts{Regular: f}
	rt := ParseRich("Away [#ff0000]AVAST[/#] away AVAST away")
	full, _ := fonts.MeasureRich(rt, TextOptions{})
	w, h := fonts.MeasureRich(rt, TextOptions{Width: full / 3})
	if w > full/3 {
		t.Errorf("wrapped rich text is %.2f wide, over the limit %.2f", w, full/3)
	}
	if h < 3*f.LineHeight-1 {
		t.Errorf("wrapped rich text is %.2f high, want at least three lines", h)
	}
	// The coloured word is one shaped run, so its width is the plain one.
	word, _ := f.Measure("AVAST", TextOptions{})
	rich, _ := fonts.MeasureRich(ParseRich("[#ff0000]AVAST[/#]"), TextOptions{})
	if rich < word-0.01 || rich > word+0.01 {
		t.Errorf("a coloured word measured %.2f, want %.2f", rich, word)
	}
}
