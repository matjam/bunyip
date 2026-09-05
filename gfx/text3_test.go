package gfx

import (
	"testing"

	"github.com/go-text/typesetting/font"
	"golang.org/x/image/font/gofont/goregular"
)

// TestGlyphAppearsInTheFrameThatDrawsIt draws a glyph outside the preload
// set, so it is rasterised during the frame that draws it, and checks that
// its ink is in that frame's image rather than the next one's.
func TestGlyphAppearsInTheFrameThatDrawsIt(t *testing.T) {
	g := newHeadless(t, 96, 48)
	g.SetView(96, 48)
	f, err := g.NewFont(goregular.TTF, 32, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	// "é" is outside the ASCII preload, so the first draw rasterises it.
	if _, ok := f.glyphs[glyphKey{0, mustGlyph(t, f, 'é')}]; ok {
		t.Fatal("the glyph was already in the atlas; the test proves nothing")
	}
	w, h := f.Measure("é", TextOptions{})
	img := frame2D(t, g, func() { g.DrawText(f, "é", 4, 4, White) })
	lit := 0
	for y := range 48 {
		for x := range 96 {
			if bright(img, x, y) {
				lit++
			}
		}
	}
	if lit < 20 {
		t.Errorf("only %d bright pixels in the first frame that drew the glyph", lit)
	}
	// A missing atlas would draw the glyph as a solid white quad, which
	// fills its box; the letter covers a fraction of it.
	if box := int(w * h); lit > box/2 {
		t.Errorf("%d bright pixels for a %d pixel box; the glyph drew as a solid quad", lit, box)
	}
}

// mustGlyph returns the main face's glyph for a rune.
func mustGlyph(t *testing.T, f *Font, r rune) font.GID {
	t.Helper()
	gid, ok := f.faces[0].face.NominalGlyph(r)
	if !ok {
		t.Fatalf("the test font has no glyph for %q", r)
	}
	return gid
}
