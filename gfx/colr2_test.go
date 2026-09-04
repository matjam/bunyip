package gfx

import (
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// colrPalette is the palette the COLR tests paint with: red, green and
// blue, none of which is the tint the text is drawn in.
var colrPalette = []color.RGBA{
	{R: 255, A: 255},
	{G: 255, A: 255},
	{B: 255, A: 255},
}

// gidOf returns a rune's glyph in a font built from the given bytes.
func gidOf(t *testing.T, g *Graphics, ttf []byte, r rune) uint16 {
	t.Helper()
	f, err := g.NewFont(ttf, 8, FontOptions{AtlasSize: 64})
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Destroy()
	gid, ok := f.faces[0].face.NominalGlyph(r)
	if !ok {
		t.Fatalf("the test font has no glyph for %q", r)
	}
	return uint16(gid)
}

// counts returns how many pixels of an image are mostly red, green or
// blue, and how many are lit at all.
func counts(img *image.RGBA) (red, green, blue, lit int) {
	for y := range img.Bounds().Dy() {
		for x := range img.Bounds().Dx() {
			c := img.RGBAAt(x, y)
			if int(c.R)+int(c.G)+int(c.B) < 60 {
				continue
			}
			lit++
			switch {
			case c.R > 120 && c.G < 90 && c.B < 90:
				red++
			case c.G > 120 && c.R < 90 && c.B < 90:
				green++
			case c.B > 120 && c.R < 90 && c.G < 90:
				blue++
			}
		}
	}
	return red, green, blue, lit
}

// TestCOLRv0Glyph draws a glyph whose COLR layers name palette colours
// and checks that the layers come out in their own colours rather than
// the colour the text is drawn in.
func TestCOLRv0Glyph(t *testing.T) {
	g := newHeadless(t, 96, 64)
	g.SetView(96, 64)
	a := gidOf(t, g, goregular.TTF, 'A')
	dot := gidOf(t, g, goregular.TTF, '.')
	ttf := withTables(t, goregular.TTF,
		colrV0Table(a, []colrLayer{{gid: a, palette: 0}, {gid: dot, palette: 2}}),
		cpalTable(colrPalette))
	f, err := g.NewFont(ttf, 48, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	// Green, so a tinted glyph could not be mistaken for a painted one.
	green := Color{0, 1, 0, 1}
	img := frame2D(t, g, func() { g.DrawText(f, "A", 4, 4, green) })
	gl, ok := f.glyphs[glyphKey{0, mustGlyph(t, f, 'A')}]
	if !ok || !gl.color {
		t.Fatalf("the glyph was not made as a colour glyph: %+v", gl)
	}
	red, _, blue, lit := counts(img)
	if lit < 50 {
		t.Fatalf("only %d lit pixels; the colour glyph did not draw", lit)
	}
	if red < 20 {
		t.Errorf("%d red pixels of %d lit; the first layer should paint the outline red", red, lit)
	}
	if blue < 3 {
		t.Errorf("%d blue pixels of %d lit; the second layer should paint the full stop blue", blue, lit)
	}
	// Nothing took the tint, which no palette entry holds.
	for y := range 64 {
		for x := range 96 {
			c := img.RGBAAt(x, y)
			if c.G > 120 && c.R < 90 && c.B < 90 {
				t.Fatalf("pixel %d,%d is the tint %v, so a layer drew tinted", x, y, c)
			}
		}
	}
	// The ink sits where the glyph was drawn.
	w, h := f.Measure("A", TextOptions{})
	for y := range 64 {
		for x := range 96 {
			if c := img.RGBAAt(x, y); int(c.R)+int(c.G)+int(c.B) > 60 {
				if x < 3 || y < 3 || float32(x) > 4+w+2 || float32(y) > 4+h+2 {
					t.Fatalf("ink at %d,%d is outside the %.1fx%.1f box at 4,4", x, y, w, h)
				}
			}
		}
	}
}

// TestCOLRv1Glyph draws a version 1 colour glyph, whose layers are paint
// tables: a solid fill and a linear gradient between two palette
// entries.
func TestCOLRv1Glyph(t *testing.T) {
	g := newHeadless(t, 96, 64)
	g.SetView(96, 64)
	a := gidOf(t, g, goregular.TTF, 'A')
	bar := gidOf(t, g, goregular.TTF, 'l')
	// The gradient runs up the em, from green on the baseline to blue at
	// the top; the third point turns the colour line to face that way.
	const em = 2048
	ttf := withTables(t, goregular.TTF,
		colrV1Table(a, []colrV1Layer{
			{gid: a, palette: 0},
			{gid: bar, gradient: true, from: 1, to: 2, x0: 0, y0: 0, x1: 0, y1: em, x2: em, y2: 0},
		}),
		cpalTable(colrPalette))
	f, err := g.NewFont(ttf, 48, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	img := frame2D(t, g, func() { g.DrawText(f, "A", 4, 4, White) })
	red, green, blue, lit := counts(img)
	if lit < 50 {
		t.Fatalf("only %d lit pixels; the colour glyph did not draw", lit)
	}
	if red < 20 {
		t.Errorf("%d red pixels of %d lit; the solid layer should paint the outline red", red, lit)
	}
	if green < 2 || blue < 2 {
		t.Errorf("%d green and %d blue pixels of %d lit; the gradient should run from green to blue", green, blue, lit)
	}
	// The gradient runs up the glyph, so its blue end is above its green
	// one on the screen.
	greenY, blueY := 0, 0
	for y := range 64 {
		for x := range 96 {
			c := img.RGBAAt(x, y)
			switch {
			case c.G > 120 && c.R < 90 && c.B < 90:
				greenY += y
			case c.B > 120 && c.R < 90 && c.G < 90:
				blueY += y
			}
		}
	}
	if greenY*blue <= blueY*green { // the mean rows, without dividing
		t.Errorf("the gradient's green end is not below its blue one: %d/%d against %d/%d", greenY, green, blueY, blue)
	}
}

// TestCOLRGlyphFallsBackToTheOutline checks that a colour glyph the
// engine cannot paint, here one whose only layer names a glyph the font
// does not have, still draws as its outline.
func TestCOLRGlyphFallsBackToTheOutline(t *testing.T) {
	g := newHeadless(t, 96, 64)
	g.SetView(96, 64)
	a := gidOf(t, g, goregular.TTF, 'A')
	ttf := withTables(t, goregular.TTF,
		colrV0Table(a, []colrLayer{{gid: 0xFFFE, palette: 0}}),
		cpalTable(colrPalette))
	f, err := g.NewFont(ttf, 48, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	gl := f.glyphs[glyphKey{0, mustGlyph(t, f, 'A')}]
	if gl.color {
		t.Fatal("a font with no palette painted a colour glyph")
	}
	img := frame2D(t, g, func() { g.DrawText(f, "A", 4, 4, White) })
	if _, _, _, lit := counts(img); lit < 50 {
		t.Errorf("only %d lit pixels; the outline should have drawn", lit)
	}
}
