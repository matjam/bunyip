package gfx

import (
	"encoding/binary"
	"os"
	"runtime"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// buildCollection writes a TrueType collection (.ttc) holding the given
// fonts in order: the collection header, then each font with the table
// offsets of its directory moved to where the font now starts.
func buildCollection(t *testing.T, fonts ...[]byte) []byte {
	t.Helper()
	header := 12 + 4*len(fonts)
	out := make([]byte, header)
	copy(out, "ttcf")
	binary.BigEndian.PutUint32(out[4:], 0x00010000)
	binary.BigEndian.PutUint32(out[8:], uint32(len(fonts)))
	for i, f := range fonts {
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		base := len(out)
		binary.BigEndian.PutUint32(out[12+4*i:], uint32(base))
		out = append(out, f...)
		tables := int(binary.BigEndian.Uint16(f[4:]))
		for k := range tables {
			rec := base + 12 + 16*k
			off := binary.BigEndian.Uint32(out[rec+8:])
			binary.BigEndian.PutUint32(out[rec+8:], off+uint32(base))
		}
	}
	return out
}

// advanceOf is the advance of a rune's glyph in a parsed face, in font
// units, which tells two faces of a collection apart.
func advanceOf(t *testing.T, data []byte, r rune) float32 {
	t.Helper()
	face, err := parseFace(data)
	if err != nil {
		t.Fatal(err)
	}
	gid, ok := face.NominalGlyph(r)
	if !ok {
		t.Fatalf("no glyph for %q", r)
	}
	return face.HorizontalAdvance(gid)
}

// A collection fallback uses its first face, the same as before the
// parse was limited to that face.
func TestCollectionUsesFirstFace(t *testing.T) {
	ttc := buildCollection(t, gobold.TTF, goregular.TTF)
	if got, want := advanceOf(t, ttc, 'm'), advanceOf(t, gobold.TTF, 'm'); got != want {
		t.Fatalf("collection's face advances 'm' by %v, its first font by %v", got, want)
	}
	if advanceOf(t, gobold.TTF, 'm') == advanceOf(t, goregular.TTF, 'm') {
		t.Fatal("the two test fonts cannot be told apart")
	}
	g := newHeadless(t, 32, 32)
	f, err := g.NewFont(goregular.TTF, 16, FontOptions{AtlasSize: 256, Fallbacks: [][]byte{ttc}})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	if len(f.faces) != 2 {
		t.Fatalf("%d faces, want the main face and one from the collection", len(f.faces))
	}
}

// Fonts made from the same bytes share one parse of their tables, and
// bytes rewritten in place with another font are parsed afresh.
func TestParsedFacesAreShared(t *testing.T) {
	a, err := parseFace(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseFace(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	if a.Font != b.Font {
		t.Error("the same bytes were parsed twice")
	}
	if a == b {
		t.Error("two fonts share one face, whose size and variations are their own")
	}
	buf := make([]byte, max(len(goregular.TTF), len(gobold.TTF)))
	copy(buf, goregular.TTF)
	regular, err := parseFace(buf)
	if err != nil {
		t.Fatal(err)
	}
	clear(buf)
	copy(buf, gobold.TTF)
	bold, err := parseFace(buf)
	if err != nil {
		t.Fatal(err)
	}
	if regular.Font == bold.Font {
		t.Fatal("a buffer rewritten with another font returned the old parse")
	}
	runtime.KeepAlive(a)
	runtime.KeepAlive(b)
}

// A system emoji collection as a fallback costs its first face's tables
// and little more. Parsing every face of the collection, as before,
// allocated more than twice the file.
func TestCollectionFallbackAllocations(t *testing.T) {
	emoji, err := os.ReadFile(emojiCollection)
	if err != nil {
		t.Skipf("no system font collection: %v", err)
	}
	g := newHeadless(t, 32, 32)
	allocated := func(make func()) uint64 {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		make()
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	var first, second *Font
	opts := FontOptions{AtlasSize: 256, Fallbacks: [][]byte{emoji}}
	// The parser copies the tables of the face it reads, and the bitmap
	// strikes of a colour emoji face are nearly the whole file; its glyph
	// substitution tables add about another fifteen megabytes.
	const faceSlack, slack = 32 << 20, 10 << 20
	if n := allocated(func() { first, _ = g.NewFont(goregular.TTF, 16, opts) }); n > uint64(len(emoji))+faceSlack {
		t.Errorf("the first font allocated %d MB for a %d MB collection", n>>20, len(emoji)>>20)
	}
	if first == nil {
		t.Fatal("NewFont failed")
	}
	defer first.Destroy()
	// A second size shares the parse.
	if n := allocated(func() { second, _ = g.NewFont(goregular.TTF, 24, opts) }); n > slack {
		t.Errorf("a second font on the same collection allocated %d MB", n>>20)
	}
	if second == nil {
		t.Fatal("NewFont failed")
	}
	defer second.Destroy()
}

// A font with no colour glyph keeps its atlas as coverage, a byte a
// texel; the first colour glyph turns it into colour texels without
// changing the plain glyphs already in it.
func TestAtlasMirrorStaysCoverageUntilColour(t *testing.T) {
	g := newHeadless(t, 96, 64)
	g.SetView(96, 64)
	plain, err := g.NewFont(goregular.TTF, 16, FontOptions{AtlasSize: 256})
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Destroy()
	if plain.pix != nil || plain.mask == nil {
		t.Fatal("a font with only plain glyphs holds colour texels")
	}
	// The colour glyph is a letter outside ASCII, which a font does not
	// rasterise until it is drawn.
	base := gidOf(t, g, goregular.TTF, 'é')
	a := gidOf(t, g, goregular.TTF, 'A')
	bar := gidOf(t, g, goregular.TTF, 'l')
	ttf := withTables(t, goregular.TTF,
		colrV1Table(base, []colrV1Layer{{gid: a, palette: 0}, {gid: bar, palette: 1}}),
		cpalTable(colrPalette))
	f, err := g.NewFont(ttf, 16, FontOptions{AtlasSize: 256})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	if f.mask == nil {
		t.Fatal("the font holds colour texels before drawing a colour glyph")
	}
	before := append([]byte(nil), f.mask.Pix...)
	frame2D(t, g, func() { g.DrawText(f, "é", 4, 4, White) })
	if f.pix == nil || f.mask != nil {
		t.Fatal("a colour glyph did not turn the atlas into colour texels")
	}
	for i, cov := range before {
		if cov == 0 {
			continue // empty before, and where the colour glyph may have gone
		}
		p := f.pix.Pix[4*i : 4*i+4]
		if p[0] != cov || p[1] != cov || p[2] != cov || p[3] != cov {
			t.Fatalf("texel %d was coverage %d and became %v", i, cov, p)
		}
	}
}
