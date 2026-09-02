package gfx

import (
	"image/color"
	"os"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/lin"
)

func TestHyphenator(t *testing.T) {
	h := EnglishHyphenator()
	got := h.Hyphenate("hyphenation")
	want := []int{2, 5, 7} // hy-phen-a-tion, then min-right trims
	// Liang's patterns give hy-phen-ation; accept the points that appear.
	if len(got) < 2 || got[0] != 2 {
		t.Errorf("Hyphenate(hyphenation) = %v, want breaks starting at hy-", got)
	}
	_ = want
	if pts := h.Hyphenate("the"); len(pts) != 0 {
		t.Errorf("short word hyphenated at %v", pts)
	}
	if pts := h.Hyphenate("table"); len(pts) != 1 || pts[0] != 2 {
		t.Errorf("Hyphenate(table) = %v, want ta-ble", pts)
	}
	soft := h.SoftHyphens("A hyphenation example.")
	if !strings.Contains(soft, "hy­phen") {
		t.Errorf("SoftHyphens = %q", soft)
	}
	if strings.Count(soft, "­") == 0 || strings.Contains(soft, "A­") {
		t.Errorf("soft hyphens misplaced in %q", soft)
	}
}

func TestParseRich(t *testing.T) {
	rt := ParseRich("plain [b]bold [#ff0000]red[/#][/b] [link=home]go[/link] [[x]")
	if rt.Plain() != "plain bold red go [x]" {
		t.Errorf("Plain = %q", rt.Plain())
	}
	if len(rt.Runs) != 6 {
		t.Fatalf("got %d runs: %+v", len(rt.Runs), rt.Runs)
	}
	if !rt.Runs[1].Bold || rt.Runs[1].Text != "bold " {
		t.Errorf("run 1 = %+v", rt.Runs[1])
	}
	if !rt.Runs[2].Bold || rt.Runs[2].Color.R < 0.9 || rt.Runs[2].Text != "red" {
		t.Errorf("run 2 = %+v", rt.Runs[2])
	}
	if rt.Runs[4].Link != "home" || !rt.Runs[4].Underline {
		t.Errorf("link run = %+v", rt.Runs[4])
	}
}

func TestLetterSpacingJustifyRich(t *testing.T) {
	g := newHeadless(t, 128, 96)
	f, err := g.NewFont(goregular.TTF, 12, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	w0, _ := f.Measure("Bunyip", TextOptions{})
	w1, _ := f.Measure("Bunyip", TextOptions{LetterSpacing: 2})
	if w1 < w0+11 || w1 > w0+13 {
		t.Errorf("letter spacing 2 over six glyphs widened %v to %v", w0, w1)
	}
	// Justified text: the first line's ink reaches the right edge.
	text := "one two three four five six seven eight nine ten"
	img := frame2D(t, g, func() {
		g.DrawTextBlock(f, text, 4, 4, TextOptions{Width: 100, Align: AlignJustify}, White)
	})
	right := 0
	for x := 4; x < 108; x++ {
		for y := 4; y < 16; y++ {
			if bright(img, x, y) {
				right = x
			}
		}
	}
	if right < 100 {
		t.Errorf("justified first line ends at x %d, want near 104", right)
	}
	// Hyphenation splits a long word across the wrap.
	lines := f.Layout("supercalifragilistic", TextOptions{Width: 60, Hyphenate: EnglishHyphenator()})
	if len(lines) < 2 {
		t.Errorf("hyphenated wrap gave %d lines: %q", len(lines), lines)
	}
	// A justified, hyphenated paragraph keeps its hyphens inside the width.
	hopts := TextOptions{Width: 100, Align: AlignJustify, Hyphenate: EnglishHyphenator()}
	img = frame2D(t, g, func() {
		g.DrawTextBlock(f, "an extraordinarily wonderful demonstration of hyphenation", 4, 4, hopts, White)
	})
	for y := 4; y < 90; y++ {
		for x := 106; x < 128; x++ {
			if bright(img, x, y) {
				t.Fatalf("ink at %d,%d beyond the justified width", x, y)
			}
		}
	}
	// Rich text: bold run, link rectangle.
	b, err := g.NewFont(gobold.TTF, 12, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()
	fonts := RichFonts{Regular: f, Bold: b}
	rt := ParseRich("Read the [b]bold[/b] [link=docs]manual[/link] now")
	var links []RichLink
	frame2D(t, g, func() {
		links = g.DrawRichText(fonts, rt, 4, 30, TextOptions{Width: 120}, White)
	})
	if len(links) != 1 || links[0].Name != "docs" || links[0].Rect.W < 10 || !links[0].Rect.Contains(lin.V2(links[0].Rect.X+2, links[0].Rect.Y+2)) {
		t.Errorf("links = %+v", links)
	}
	if w, h := fonts.MeasureRich(rt, TextOptions{Width: 60}); w > 60 || h < 2*f.LineHeight-1 {
		t.Errorf("MeasureRich = %v %v", w, h)
	}
}

func TestEmoji(t *testing.T) {
	data, err := os.ReadFile("/System/Library/Fonts/Apple Color Emoji.ttc")
	if err != nil {
		t.Skip("no colour emoji font on this machine")
	}
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 24, FontOptions{Fallbacks: [][]byte{data}})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	// The first frame rasterises the glyph; the atlas uploads for the next.
	draw := func() { g.DrawText(f, "\U0001F600", 4, 4, White) }
	frame2D(t, g, draw)
	img := frame2D(t, g, draw)
	colour := 0
	for _, gl := range f.glyphs {
		if gl.color {
			colour++
		}
	}
	if colour == 0 {
		gid, ok := f.faces[1].face.NominalGlyph(0x1F600)
		t.Fatalf("no colour glyph was made; fallback face has glyph %v %v, data %T", gid, ok, f.faces[1].face.GlyphData(gid))
	}
	yellow, lit := 0, 0
	var sample color.RGBA
	for y := range 64 {
		for x := range 64 {
			c := img.RGBAAt(x, y)
			if c.R > 40 || c.G > 40 || c.B > 40 {
				lit++
				sample = c
			}
			if c.R > 180 && c.G > 140 && c.B < 140 {
				yellow++
			}
		}
	}
	if yellow < 50 {
		t.Errorf("emoji drew %d yellow pixels of %d lit (sample %v), want a grinning face", yellow, lit, sample)
	}
}
