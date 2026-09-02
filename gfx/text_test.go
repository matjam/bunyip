package gfx

import (
	"os"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// arial returns a system font with Arabic and Hebrew glyphs, or skips.
func arial(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("/System/Library/Fonts/Supplemental/Arial.ttf")
	if err != nil {
		t.Skip("no Arial.ttf on this system for right-to-left scripts")
	}
	return data
}

func TestShapeKerning(t *testing.T) {
	g := newHeadless(t, 64, 64)
	// Go Regular has no kern pair for AV; Arial does.
	f, err := g.NewFont(arial(t), 20, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	// HarfBuzz applies the font's kern pairs: "AV" is narrower than "AA"
	// less the difference between V and A alone.
	av, _ := f.Measure("AV")
	aa, _ := f.Measure("AA")
	a, _ := f.Measure("A")
	v, _ := f.Measure("V")
	if av >= aa-a+v-0.01 {
		t.Errorf("AV %.2f is not kerned tighter than AA %.2f adjusted %.2f", av, aa, aa-a+v)
	}
	f.Destroy()
	if f, err = g.NewFont(goregular.TTF, 20, FontOptions{}); err != nil {
		t.Fatal(err)
	}
	glyphs := f.Shape("Hi there", TextOptions{})
	if len(glyphs) != 8 {
		t.Fatalf("got %d glyphs for 8 runes", len(glyphs))
	}
	if !glyphs[2].Empty || glyphs[0].Empty {
		t.Errorf("space should be empty and H drawn: %+v %+v", glyphs[0], glyphs[2])
	}
	if glyphs[1].Pos.X <= glyphs[0].Pos.X || glyphs[3].Index != 3 {
		t.Errorf("glyphs are not positioned left to right with text indices: %+v", glyphs[:4])
	}
}

func TestShapeRightToLeft(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(arial(t), 20, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	// Arabic joins letters into forms and lam-alef into a ligature: fewer
	// glyphs than runes, laid out so the first letter is rightmost.
	text := "السلام"
	glyphs := f.Shape(text, TextOptions{})
	if len(glyphs) >= len([]rune(text)) {
		t.Errorf("Arabic shaped into %d glyphs for %d runes; expected joining and a lam-alef ligature", len(glyphs), len([]rune(text)))
	}
	first, last := glyphs[0], glyphs[len(glyphs)-1]
	if first.Index <= last.Index {
		t.Errorf("visual order should run from the last rune on the left to the first on the right: %d .. %d", first.Index, last.Index)
	}
	if first.Pos.X >= last.Pos.X {
		t.Errorf("glyph positions should increase left to right: %.1f .. %.1f", first.Pos.X, last.Pos.X)
	}
	// Mixed text: the Hebrew word inside a Latin sentence is reversed, the rest is not.
	mixed := f.Shape("say שלום now", TextOptions{})
	var hebrew []Glyph
	for _, gl := range mixed {
		if gl.Index >= 4 && gl.Index < 4+len("שלום") {
			hebrew = append(hebrew, gl)
		}
	}
	if len(hebrew) != 4 || hebrew[0].Index < hebrew[3].Index {
		t.Errorf("Hebrew run is not reversed within Latin text: %+v", hebrew)
	}
	if w, _ := f.Measure(text); w <= 0 {
		t.Errorf("measure of RTL text is %.1f", w)
	}
}

func TestFallbackFont(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 18, FontOptions{Fallbacks: [][]byte{arial(t)}})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	// Go Regular has no Arabic; the fallback supplies it, and Latin stays
	// with the main face.
	glyphs := f.Shape("ok مرحبا", TextOptions{})
	drawn := 0
	for _, gl := range glyphs {
		if !gl.Empty {
			drawn++
		}
	}
	if drawn < 6 {
		t.Errorf("only %d glyphs drawn; the fallback font should supply the Arabic", drawn)
	}
}

func TestLayoutWrapsByUnicodeRules(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 16, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	text := "the quick brown fox jumps over the lazy dog"
	full, _ := f.Measure(text)
	lines := f.Layout(text, TextOptions{Width: full / 3})
	if len(lines) < 3 {
		t.Fatalf("wrapped into %d lines at a third of the width: %q", len(lines), lines)
	}
	for _, l := range lines {
		if w, _ := f.Measure(l); w > full/3+1 {
			t.Errorf("line %q is %.1f wide, over the limit %.1f", l, w, full/3)
		}
	}
	joined := ""
	for i, l := range lines {
		if i > 0 {
			joined += " "
		}
		joined += l
	}
	if joined != text {
		t.Errorf("lines do not reassemble the text: %q", joined)
	}
	w, h := f.MeasureBlock(text, TextOptions{Width: full / 3})
	if w > full/3+1 || h < float32(len(lines))*f.LineHeight-0.01 {
		t.Errorf("block %.1fx%.1f for %d lines of %.1f", w, h, len(lines), f.LineHeight)
	}
}

func TestVerticalText(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 16, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	glyphs := f.Shape("abc", TextOptions{Direction: DirectionTTB})
	if len(glyphs) != 3 || glyphs[2].Pos.Y <= glyphs[0].Pos.Y {
		t.Errorf("vertical text should step down: %+v", glyphs)
	}
}

func TestDrawTextRenders(t *testing.T) {
	g := newHeadless(t, 96, 48)
	g.SetView(96, 48)
	f, err := g.NewFont(goregular.TTF, 24, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	if ok, err := g.Begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	g.DrawText(f, "Hi", 4, 4, White)
	img, err := g.End(true)
	if err != nil {
		t.Fatal(err)
	}
	lit := 0
	for y := 4; y < 30; y++ {
		for x := 4; x < 40; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r > 0x8000 {
				lit++
			}
		}
	}
	if lit < 40 {
		t.Errorf("only %d bright pixels where 'Hi' was drawn", lit)
	}
}
