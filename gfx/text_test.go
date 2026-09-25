package gfx

import (
	"os"
	"slices"
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
	// f is reassigned below, so the cleanup must read it then, not now:
	// a plain defer would destroy the first font twice and leak the
	// second, which the validation layers report at device teardown.
	defer func() { f.Destroy() }()
	// HarfBuzz applies the font's kern pairs: "AV" is narrower than "AA"
	// less the difference between V and A alone.
	av, _ := f.Measure("AV", TextOptions{})
	aa, _ := f.Measure("AA", TextOptions{})
	a, _ := f.Measure("A", TextOptions{})
	v, _ := f.Measure("V", TextOptions{})
	if av >= aa-a+v-0.01 {
		t.Errorf("AV %.2f is not kerned tighter than AA %.2f adjusted %.2f", av, aa, aa-a+v)
	}
	f.Destroy()
	if f, err = g.NewFont(goregular.TTF, 20, FontOptions{}); err != nil {
		t.Fatal(err)
	}
	glyphs := shapeGlyphs(t, f, "Hi there", TextOptions{})
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
	glyphs := shapeGlyphs(t, f, text, TextOptions{})
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
	mixed := shapeGlyphs(t, f, "say שלום now", TextOptions{})
	var hebrew []Glyph
	for _, gl := range mixed {
		if gl.Index >= 4 && gl.Index < 4+len("שלום") {
			hebrew = append(hebrew, gl)
		}
	}
	if len(hebrew) != 4 || hebrew[0].Index < hebrew[3].Index {
		t.Errorf("Hebrew run is not reversed within Latin text: %+v", hebrew)
	}
	if w, _ := f.Measure(text, TextOptions{}); w <= 0 {
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
	glyphs := shapeGlyphs(t, f, "ok مرحبا", TextOptions{})
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
	full, _ := f.Measure(text, TextOptions{})
	lines := layoutStrings(t, f, text, TextOptions{Width: full / 3})
	if len(lines) < 3 {
		t.Fatalf("wrapped into %d lines at a third of the width: %q", len(lines), lines)
	}
	for _, l := range lines {
		if w, _ := f.Measure(l, TextOptions{}); w > full/3+1 {
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
	w, h := f.Measure(text, TextOptions{Width: full / 3})
	if w > full/3+1 || h < float32(len(lines))*f.LineHeight-0.01 {
		t.Errorf("block %.1fx%.1f for %d lines of %.1f", w, h, len(lines), f.LineHeight)
	}
}

// TestTextCacheMatchesFreshLayout checks that the layout a repeated draw
// takes from the layout cache is the one a fresh layout would produce,
// for every alignment and for sized, spaced and multi-paragraph text, so
// that caching a layout cannot move what is drawn or what Measure says.
func TestTextCacheMatchesFreshLayout(t *testing.T) {
	g := newHeadless(t, 256, 256)
	f, err := g.NewFont(goregular.TTF, 16, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	text := "the quick brown fox jumps over the lazy dog\nand does it again for a second paragraph"
	cases := []struct {
		name string
		opts TextOptions
	}{
		{"left", TextOptions{Width: 120}},
		{"center", TextOptions{Width: 120, Align: AlignCenter}},
		{"right", TextOptions{Width: 120, Align: AlignRight}},
		{"justify", TextOptions{Width: 120, Align: AlignJustify}},
		{"unwrapped", TextOptions{}},
		{"sized and spaced", TextOptions{Width: 120, Size: 24, LetterSpacing: 1.5, LineSpacing: 1.4}},
		{"baseline", TextOptions{Width: 120, Baseline: true}},
		{"vertical", TextOptions{Direction: DirectionTTB}},
		{"hyphenated", TextOptions{Width: 60, Align: AlignJustify, Hyphenate: EnglishHyphenator()}},
	}
	layout := func(t *testing.T, opts TextOptions) *TextLayout {
		t.Helper()
		l, err := f.Layout(text, opts)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f.dropLayouts()
			first := layout(t, c.opts)  // laid out; its key is noted
			cached := layout(t, c.opts) // laid out again and kept
			if again := layout(t, c.opts); again != cached {
				t.Fatalf("third call laid the text out again instead of using the cache")
			}
			f.dropLayouts()
			fresh := layout(t, c.opts)
			for _, l := range []*TextLayout{first, fresh} {
				if !slices.Equal(l.glyphs, cached.glyphs) {
					t.Errorf("cached glyphs differ from a fresh layout: %d vs %d glyphs", len(cached.glyphs), len(l.glyphs))
					for i := range min(len(l.glyphs), len(cached.glyphs)) {
						if l.glyphs[i] != cached.glyphs[i] {
							t.Fatalf("glyph %d: cached %+v, fresh %+v", i, cached.glyphs[i], l.glyphs[i])
						}
					}
				}
				if !slices.Equal(l.carets, cached.carets) || !slices.Equal(l.lines, cached.lines) || l.measure != cached.measure || l.quads != cached.quads {
					t.Errorf("cached carets, lines or sizes differ from a fresh layout")
				}
			}
			if w, h := f.Measure(text, c.opts); w != cached.measure.X || h != cached.measure.Y {
				t.Errorf("Measure %vx%v, layout measured %v", w, h, cached.measure)
			}
		})
	}
}

// TestTextCacheEvictionKeepsHotEntries checks that a string drawn every
// frame survives many one-off strings passing through the cache, one a
// frame, which clearing the whole map at a limit did not, and that the
// one-off strings are dropped rather than kept for ever.
func TestTextCacheEvictionKeepsHotEntries(t *testing.T) {
	var c genCache[int, int]
	c.put(-1, -1, 0)
	for i := range textCacheEntries * 4 {
		now := uint64(i + 1)
		c.put(i, i, now)
		if v, ok := c.get(-1, now); !ok || v != -1 {
			t.Fatalf("the hot entry was evicted after %d one-off entries", i+1)
		}
	}
	if n := len(c.cur) + len(c.prev); n > 2*textCacheEntries+2 {
		t.Errorf("cache holds %d entries for a limit of %d", n, textCacheEntries)
	}
}

// TestTextCacheKeepsAFrameLargerThanAGeneration checks that a frame that
// uses more entries than a generation holds keeps all of them: the cache
// grows rather than retiring, mid-frame, entries the frame has just used,
// so the next frames find everything. It shrinks back once frames use
// less.
func TestTextCacheKeepsAFrameLargerThanAGeneration(t *testing.T) {
	var c genCache[int, int]
	const n = textCacheEntries*2 + 100
	for frame := uint64(1); frame <= 6; frame++ {
		misses := 0
		for i := range n {
			if _, ok := c.get(i, frame); !ok {
				misses++
				c.put(i, i, frame)
			}
		}
		if frame > 1 && misses != 0 {
			t.Fatalf("frame %d missed %d of %d entries used every frame", frame, misses, n)
		}
	}
	// Later frames use a handful of new strings each; the large working
	// set is retired and the limit falls back to the starting room.
	for frame := uint64(7); frame <= 20; frame++ {
		for i := range textCacheEntries {
			c.put(int(frame)*1_000_000+i, i, frame)
		}
	}
	if c.limit > 2*textCacheEntries {
		t.Errorf("limit stayed at %d after the large frames ended", c.limit)
	}
	if n := len(c.cur) + len(c.prev); n > 4*textCacheEntries {
		t.Errorf("cache holds %d entries after the large frames ended", n)
	}
}

// TestTextCacheAdmitsOnSecondUse checks that with admission on, a key put
// once is noted but not stored, and the second put stores it.
func TestTextCacheAdmitsOnSecondUse(t *testing.T) {
	c := genCache[string, int]{admit: true}
	c.put("counter 1", 1, 1)
	if _, ok := c.get("counter 1", 1); ok {
		t.Fatal("a key put once was stored")
	}
	c.put("label", 2, 1)
	c.put("label", 2, 2)
	if v, ok := c.get("label", 2); !ok || v != 2 {
		t.Fatal("a key put twice was not stored")
	}
}

func TestVerticalText(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 16, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	glyphs := shapeGlyphs(t, f, "abc", TextOptions{Direction: DirectionTTB})
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
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	g.DrawText(f, "Hi", 4, 4, White)
	img, err := g.end(true)
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
