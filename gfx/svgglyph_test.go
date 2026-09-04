package gfx

import (
	"encoding/binary"
	"fmt"
	"testing"

	ot "github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/font/gofont/goregular"
)

// svgTable builds an SVG table holding one document for one glyph.
func svgTable(gid uint16, doc string) ot.Table {
	list := binary.BigEndian.AppendUint16(nil, 1) // one document record
	list = binary.BigEndian.AppendUint16(list, gid)
	list = binary.BigEndian.AppendUint16(list, gid)
	const recordEnd = 2 + 12
	list = binary.BigEndian.AppendUint32(list, recordEnd) // the document follows
	list = binary.BigEndian.AppendUint32(list, uint32(len(doc)))
	list = append(list, doc...)
	var b []byte
	b = binary.BigEndian.AppendUint16(b, 0)  // version
	b = binary.BigEndian.AppendUint32(b, 10) // offset to the document list
	b = binary.BigEndian.AppendUint32(b, 0)  // reserved
	b = append(b, list...)
	return ot.Table{Tag: ot.MustNewTag("SVG "), Content: b}
}

// TestSVGGlyph draws a glyph the font describes as an SVG document and
// checks that its shapes come out in their own colours and in the right
// places.
func TestSVGGlyph(t *testing.T) {
	g := newHeadless(t, 96, 96)
	g.SetView(96, 96)
	a := gidOf(t, g, goregular.TTF, 'A')
	doc := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<defs><linearGradient id="fade" x1="0" y1="0" x2="1" y2="0">
			<stop offset="0" stop-color="#00ff00"/><stop offset="1" stop-color="#00ff00" stop-opacity="0"/>
		</linearGradient></defs>
		<g id="glyph%d">
			<rect x="5" y="5" width="40" height="40" fill="#ff0000"/>
			<circle cx="70" cy="70" r="25" fill="rgb(0, 0, 255)"/>
			<path d="M5 55 h40 v40 h-40 z" fill="url(#fade)"/>
		</g>
	</svg>`, a)
	ttf := withTables(t, goregular.TTF, svgTable(a, doc))
	f, err := g.NewFont(ttf, 48, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	img := frame2D(t, g, func() { g.DrawText(f, "A", 4, 4, Color{1, 1, 1, 1}) })
	gl, ok := f.glyphs[glyphKey{0, mustGlyph(t, f, 'A')}]
	if !ok || !gl.color {
		t.Fatalf("the glyph was not made as a colour glyph: %+v", gl)
	}
	red, green, blue, lit := counts(img)
	if lit < 100 {
		t.Fatalf("only %d lit pixels; the SVG glyph did not draw", lit)
	}
	if red < 50 || green < 20 || blue < 50 {
		t.Errorf("%d red, %d green and %d blue pixels of %d lit; want all three shapes", red, green, blue, lit)
	}
	// The document's own coordinates say where each shape lands: the red
	// square is above and left, the blue circle below and right, and the
	// gradient square below and left.
	var redX, redY, blueX, blueY, greenX int
	for y := range 96 {
		for x := range 96 {
			c := img.RGBAAt(x, y)
			switch {
			case c.R > 120 && c.G < 90 && c.B < 90:
				redX, redY = redX+x, redY+y
			case c.B > 120 && c.R < 90 && c.G < 90:
				blueX, blueY = blueX+x, blueY+y
			case c.G > 120 && c.R < 90 && c.B < 90:
				greenX += x
			}
		}
	}
	if redX*blue >= blueX*red || redY*blue >= blueY*red {
		t.Errorf("the red square is not above and left of the blue circle")
	}
	if greenX*red >= redX*green*2 {
		t.Errorf("the fading square is not on the left, where the document puts it")
	}
}

// TestSVGGlyphInheritsAncestors checks that a glyph element deeper in
// the document is drawn through the transform and the fill its ancestors
// give it.
func TestSVGGlyphInheritsAncestors(t *testing.T) {
	g := newHeadless(t, 96, 96)
	g.SetView(96, 96)
	a := gidOf(t, g, goregular.TTF, 'A')
	// The square is drawn at the origin and pushed right and down by the
	// group above it, and takes the group's colour.
	doc := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<g transform="translate(50 50)" fill="#0000ff">
			<g id="glyph%d"><rect width="40" height="40"/></g>
		</g>
	</svg>`, a)
	ttf := withTables(t, goregular.TTF, svgTable(a, doc))
	f, err := g.NewFont(ttf, 40, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	img := frame2D(t, g, func() { g.DrawText(f, "A", 4, 4, White) })
	red, _, blue, lit := counts(img)
	if blue < 50 || red > 0 {
		t.Errorf("%d blue and %d red pixels of %d lit; the group's fill should reach the glyph", blue, red, lit)
	}
	glyphs := f.Shape("A", TextOptions{})
	if len(glyphs) != 1 || glyphs[0].Empty {
		t.Fatalf("the SVG glyph did not shape: %+v", glyphs)
	}
	// Half the viewBox across and down: the em is forty view units, so
	// the square starts twenty in and twenty down from the em's top.
	if x := glyphs[0].Pos.X; x < 19 || x > 21 {
		t.Errorf("the glyph starts at x %.1f, want about 20 from the group's translate", x)
	}
	if top := glyphs[0].Pos.Y - f.Ascent; top < -21 || top > -19 {
		t.Errorf("the glyph's top is %.1f from the baseline, want about -20", top)
	}
}

// TestSVGGlyphWithoutViewBox checks that a document with no viewBox is
// drawn in font units, y down from the baseline.
func TestSVGGlyphWithoutViewBox(t *testing.T) {
	g := newHeadless(t, 96, 96)
	g.SetView(96, 96)
	a := gidOf(t, g, goregular.TTF, 'A')
	// A square of half an em, sitting on the baseline.
	doc := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg">
		<rect id="glyph%d" x="0" y="-1024" width="1024" height="1024" fill="#ff0000"/>
	</svg>`, a)
	ttf := withTables(t, goregular.TTF, svgTable(a, doc))
	f, err := g.NewFont(ttf, 40, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	glyphs := f.Shape("A", TextOptions{})
	if len(glyphs) != 1 || glyphs[0].Empty {
		t.Fatalf("the SVG glyph did not shape: %+v", glyphs)
	}
	// Half an em at forty view units is twenty, give or take the pixel
	// the rasteriser pads with.
	if w, h := glyphs[0].Size.X, glyphs[0].Size.Y; w < 19 || w > 22 || h < 19 || h > 22 {
		t.Errorf("the glyph is %.1fx%.1f, want about 20x20 for half an em", w, h)
	}
	// It sits on the baseline, so its top is half an em above it.
	if top := glyphs[0].Pos.Y - f.Ascent; top < -21 || top > -19 {
		t.Errorf("the glyph's top is %.1f from the baseline, want about -20", top)
	}
}
