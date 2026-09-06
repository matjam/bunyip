package gfx

import (
	"errors"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"golang.org/x/image/font/gofont/goregular"
)

// layoutStrings keeps the wrapping assertions about displayed source lines
// while exercising the reusable layout's new source-byte ranges.
func layoutStrings(t *testing.T, f *Font, text string, opts TextOptions) []string {
	t.Helper()
	l, err := f.Layout(text, opts)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range l.Lines() {
		lines = append(lines, strings.TrimRight(strings.ReplaceAll(text[line.Start:line.End], "\u00ad", ""), " \t"))
	}
	return lines
}

func shapeGlyphs(t *testing.T, f *Font, text string, opts TextOptions) []Glyph {
	t.Helper()
	glyphs, err := f.Shape(text, opts)
	if err != nil {
		t.Fatal(err)
	}
	return glyphs
}

func TestTextLayoutSourceCaretsAndImmutability(t *testing.T) {
	g := newHeadless(t, 160, 120)
	f, err := g.NewFont(goregular.TTF, 18, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	text := "é e\u0301\n\nBundesregierung "
	l, err := f.Layout(text, TextOptions{Width: 95, Language: "de", AutoHyphenate: true})
	if err != nil {
		t.Fatal(err)
	}
	if l.Text() != text || len(l.Lines()) < 4 {
		t.Fatalf("source/lines %q %+v", l.Text(), l.Lines())
	}
	for _, c := range l.carets {
		if c.position.Index < 0 || c.position.Index > len(text) || !utf8.ValidString(text[:c.position.Index]) {
			t.Fatalf("non-source boundary %+v", c)
		}
		if c.position.Index == 4 || c.position.Index == 5 {
			t.Fatalf("caret split combining cluster: %+v", c)
		}
	}
	if got, want := l.Caret(TextCaret{Index: 1, Affinity: CaretTrailing}), l.Caret(TextCaret{Index: 0, Affinity: CaretTrailing}); got != want {
		t.Fatalf("mid-byte tie did not choose lower boundary: %v != %v", got, want)
	}
	lines := l.Lines()
	original := lines[0]
	lines[0].Start = 999
	if l.Lines()[0] != original {
		t.Fatal("Lines exposed mutable layout data")
	}
	for i, line := range l.Lines() {
		if !utf8.ValidString(text[line.Start:line.End]) {
			t.Fatal("line range split UTF-8")
		}
		if i > 0 && line.Start < l.Lines()[i-1].End {
			t.Fatal("source ranges overlap")
		}
	}
	blank := l.Lines()[1]
	if blank.Start != len("é e\u0301\n") || blank.End != blank.Start {
		t.Fatalf("blank newline lost source position: %+v", blank)
	}
	for _, c := range l.carets {
		got := l.HitTest(c.rect.Center())
		if l.Caret(got).Center().Sub(l.rotation.TransformRect(c.rect).Center()).Len() > 1 {
			t.Fatalf("caret/hit geometry mismatch: %+v -> %+v", c, got)
		}
	}
}

func TestTextLayoutMixedBidiAffinity(t *testing.T) {
	g := newHeadless(t, 128, 64)
	f, err := g.NewFont(goregular.TTF, 20, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Glyph availability does not affect the bidi ordering of these source
	// clusters: missing Hebrew glyphs still carry their direction and advances.
	const text = "abc אבג"
	l, err := f.Layout(text, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	boundary := len("abc ")
	leading := l.Caret(TextCaret{Index: boundary, Affinity: CaretLeading})
	trailing := l.Caret(TextCaret{Index: boundary, Affinity: CaretTrailing})
	end := l.Caret(TextCaret{Index: len(text), Affinity: CaretTrailing})
	if leading.X <= trailing.X+1 || math.Abs(float64(end.X-trailing.X)) > 0.01 {
		t.Fatalf("mixed bidi carets: leading%v trailing%v end%v", leading, trailing, end)
	}
	if end.X >= l.Bounds().X+l.Bounds().W-1 {
		t.Fatal("logical paragraph end was forced to the visual right edge")
	}
}

func TestTextLayoutVerticalRotationAndClusterStyle(t *testing.T) {
	g := newHeadless(t, 128, 128)
	f, err := g.NewFont(goregular.TTF, 20, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rich := RichText{Runs: []RichRun{{Text: "e", Color: RGB(255, 0, 0), Link: "first"}, {Text: "\u0301", Color: RGB(0, 255, 0), Strikethrough: true, Link: "second"}}}
	l, err := (RichFonts{Regular: f}).Layout(rich, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.glyphs) == 0 {
		t.Fatal("no combined glyph")
	}
	for _, gl := range l.glyphs {
		if gl.style.Link != "first" || gl.style.Strikethrough {
			t.Fatal("cluster did not retain first source style")
		}
	}
	if len(l.Links()) != 1 || l.Links()[0].Name != "first" {
		t.Fatal(l.Links())
	}
	vertical, err := f.Layout("ABC\nDE", TextOptions{Direction: DirectionTTB, Angle: 0.3})
	if err != nil {
		t.Fatal(err)
	}
	if len(vertical.Lines()) != 2 || vertical.Lines()[1].Baseline.X >= vertical.Lines()[0].Baseline.X {
		t.Fatal("vertical columns do not progress left", vertical.Lines())
	}
	for _, c := range vertical.carets {
		point := vertical.rotation.Apply(c.rect.Center())
		got := vertical.HitTest(point)
		if vertical.Caret(got).Center().Sub(vertical.Caret(c.position).Center()).Len() > 1 {
			t.Fatalf("rotated vertical caret mismatch: %+v -> %+v", c.position, got)
		}
	}
}

func TestTextLayoutOutlineRenderingAndBuckets(t *testing.T) {
	for _, sdf := range []bool{false, true} {
		t.Run(map[bool]string{false: "bitmap", true: "sdf"}[sdf], func(t *testing.T) {
			g := newHeadless(t, 160, 80)
			create := g.NewFont
			if sdf {
				create = g.NewSDFFont
			}
			f, err := create(goregular.TTF, 24, FontOptions{AtlasSize: 512})
			if err != nil {
				t.Fatal(err)
			}
			base, err := f.Layout("Hi", TextOptions{})
			if err != nil {
				t.Fatal(err)
			}
			l, err := f.Layout("Hi", TextOptions{OutlineWidth: 2, OutlineColor: RGB(255, 0, 0), Strikethrough: true})
			if err != nil {
				t.Fatal(err)
			}
			if l.Bounds() != base.Bounds() || l.InkBounds().W <= base.InkBounds().W {
				t.Fatal("outline changed logical bounds or did not expand ink")
			}
			img := frame2D(t, g, func() { g.DrawTextLayout(l, 20, 20, White) })
			red, white := 0, 0
			bounds := l.InkBounds()
			bounds.X += 20
			bounds.Y += 20
			for y := range 80 {
				for x := range 160 {
					c := img.RGBAAt(x, y)
					if c.R > 150 {
						if c.G < 60 {
							red++
						} else if c.G > 150 {
							white++
						}
						if float32(x) < bounds.X-2 || float32(y) < bounds.Y-2 || float32(x) > bounds.X+bounds.W+2 || float32(y) > bounds.Y+bounds.H+2 {
							t.Fatalf("ink escaped measured bounds at %d,%d: %v", x, y, bounds)
						}
					}
				}
			}
			if red < 20 || white < 20 {
				t.Fatalf("outline/fill missing: red%d white%d", red, white)
			}
			for i := 1; i <= 120; i++ {
				if _, err := f.Layout("Hi", TextOptions{OutlineWidth: float32(i) / 20}); err != nil {
					t.Fatal(err)
				}
			}
			if len(f.outlinePages) > 2 {
				t.Fatalf("width animation created %d pages instead of two spread buckets", len(f.outlinePages))
			}
			// The queued fill/outline descriptors retain their images after early
			// font destruction, and immutable queries do not depend on live atlases.
			want := l.InkBounds()
			img = frame2D(t, g, func() { g.DrawTextLayout(l, 20, 20, White); f.Destroy() })
			if l.InkBounds() != want || img.RGBAAt(24, 35).R < 20 {
				t.Fatal("queued outline or immutable bounds lost on font destruction")
			}
		})
	}
}

func TestTextLayoutUploadFailureAndFramePropagation(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 18, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	create := vk.VkCreateImage
	vk.VkCreateImage = func(vk.VkDevice, *vk.VkImageCreateInfo, *vk.VkAllocationCallbacks, *vk.VkImage) vk.VkResult {
		return vk.VK_ERROR_DEVICE_LOST
	}
	t.Cleanup(func() { vk.VkCreateImage = create })
	opts := TextOptions{OutlineWidth: 2}
	if l, err := f.Layout("fail", opts); err == nil || l != nil {
		t.Fatalf("constructor lost upload failure: %v %v", l, err)
	}
	if len(f.layouts.cur) != 0 {
		t.Fatal("failed layout entered cache")
	}
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	g.DrawTextBlock(f, "fail", 0, 0, opts, White)
	if _, err := g.end(false); !errors.Is(err, render.ErrDeviceLost) {
		t.Fatalf("frame did not propagate classified upload failure: %v", err)
	}
}

func TestTextLayoutRejectsExtremeDerivedArithmetic(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 18, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before := f.packer
	for _, opts := range []TextOptions{{Size: math.SmallestNonzeroFloat32}, {Size: math.MaxFloat32}, {Width: math.MaxFloat32}, {LetterSpacing: math.MaxFloat32}, {OutlineWidth: math.MaxFloat32}, {Size: 1e-30, OutlineWidth: 1}, {LineSpacing: math.MaxFloat32}} {
		if _, err := f.Layout("extreme", opts); err == nil {
			t.Errorf("accepted unsupported options %+v", opts)
		}
		if opts.OutlineWidth == 0 {
			if _, err := f.Shape("extreme", opts); err == nil {
				t.Errorf("Shape accepted unsupported options %+v", opts)
			}
		}
		if f.packer != before || len(f.outlinePages) != 0 || len(f.layouts.cur) != 0 {
			t.Fatal("invalid arithmetic mutated atlas/cache")
		}
	}
}

func TestTextLayoutWhitespaceSourceBoundaries(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 18, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const text = "A  \nB\n"
	l, err := f.Layout(text, TextOptions{Width: 100})
	if err != nil {
		t.Fatal(err)
	}
	lines := l.Lines()
	if len(lines) != 3 || lines[0].Start != 0 || lines[0].End != 3 || lines[1].Start != 4 || lines[1].End != 5 || lines[2].Start != len(text) {
		t.Fatalf("newline/whitespace source ranges: %+v", lines)
	}
	for _, index := range []int{1, 2, 3, 4, 5, len(text)} {
		found := false
		for _, c := range l.carets {
			found = found || c.position.Index == index
		}
		if !found {
			t.Errorf("missing source boundary %d", index)
		}
	}
	if a, b := l.Caret(TextCaret{Index: 1}), l.Caret(TextCaret{Index: 3, Affinity: CaretTrailing}); a.X != b.X {
		t.Fatalf("trimmed spaces retained advance: %v %v", a, b)
	}
}
