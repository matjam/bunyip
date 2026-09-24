package gfx

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"

	"github.com/matjam/bunyip/lin"
)

// Align positions lines within a text block.
type Align uint8

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
	// AlignJustify widens the spaces of every wrapped line but a
	// paragraph's last so both edges are straight; it needs a Width.
	AlignJustify
)

// Direction is the direction text runs in.
type Direction uint8

const (
	// DirectionAuto reads the text: right to left when it starts with a
	// right-to-left script (Arabic, Hebrew), left to right otherwise.
	DirectionAuto Direction = iota
	DirectionLTR
	DirectionRTL
	// DirectionTTB lays glyphs top to bottom in columns that step from
	// right to left, for vertical Japanese and Chinese.
	DirectionTTB
)

// TextOptions lays out text.
type TextOptions struct {
	Underline     bool
	Strikethrough bool
	OutlineWidth  float32 // view units; zero disables the outline
	OutlineColor  Color   // zero follows the effective text colour
	Width         float32 // wrap width in view units (column height for vertical text); zero means no wrapping
	Align         Align
	LineSpacing   float32 // multiplier; zero means 1
	// Size is the em size to draw at; zero means the font's own. SDF fonts
	// stay crisp at any size; bitmap fonts resample their atlas.
	Size float32
	// Angle rotates the text about its origin, in radians, clockwise on
	// screen.
	Angle float32
	// Baseline puts the first line's baseline at the origin's y instead of
	// the block's top, so text of different sizes lines up.
	Baseline bool
	// LetterSpacing adds view units between glyphs, for tracked-out
	// headings; negative tightens.
	LetterSpacing float32
	// Hyphenate breaks long words at the hyphenator's points when a line
	// wraps, drawing a hyphen at the break.
	Hyphenate *Hyphenator
	// AutoHyphenate breaks long words with the hyphenator for Language, or
	// American English when Language is empty. A language the engine ships
	// no patterns for is not hyphenated. Hyphenate wins when both are set.
	AutoHyphenate bool
	Direction     Direction
	// Language is a BCP 47 tag ("tr", "zh-Hant") that picks language-specific
	// glyph forms; empty means the font's default.
	Language string
}

// resolved fills in the hyphenator AutoHyphenate asks for, so that
// everything below it, the caches included, sees the hyphenator the text
// is laid out with.
func (o TextOptions) resolved() TextOptions {
	if o.Hyphenate == nil && o.AutoHyphenate {
		lang := o.Language
		if lang == "" {
			lang = "en-us"
		}
		o.Hyphenate, _ = HyphenatorFor(lang)
	}
	return o
}

// runKey identifies a shaped paragraph in the cache.
type runKey struct {
	text string
	dir  Direction
	lang string
}

// lineKey identifies a wrapped paragraph in the cache.
type lineKey struct {
	runKey
	width   int
	spacing int32 // letter spacing in 1/64 pixels
	hyph    *Hyphenator
}

// direction resolves DirectionAuto by the first strong character.
func direction(text string, d Direction) di.Direction {
	switch d {
	case DirectionLTR:
		return di.DirectionLTR
	case DirectionRTL:
		return di.DirectionRTL
	case DirectionTTB:
		return di.DirectionTTB
	}
	for _, r := range text {
		if unicode.Is(unicode.Arabic, r) || unicode.Is(unicode.Hebrew, r) || unicode.Is(unicode.Syriac, r) || unicode.Is(unicode.Thaana, r) {
			return di.DirectionRTL
		}
		if unicode.IsLetter(r) {
			break
		}
	}
	return di.DirectionLTR
}

// frame is the frame number the font's caches stamp their generations
// with.
func (f *Font) frame() uint64 { return f.g.frameNo }

// shape runs the segmenter and HarfBuzz over one paragraph, cached.
func (f *Font) shape(text string, opts TextOptions) []shaping.Output {
	key := runKey{text, opts.Direction, opts.Language}
	if runs, ok := f.runs.get(key, f.frame()); ok {
		return runs
	}
	runes := []rune(text)
	in := shaping.Input{
		Text: runes, RunStart: 0, RunEnd: len(runes),
		Direction:    direction(text, opts.Direction),
		Face:         f.faces[0].face,
		Size:         fixed.Int26_6(f.pxPerEm * 64),
		FontFeatures: f.features,
	}
	if opts.Language != "" {
		in.Language = language.NewLanguage(opts.Language)
	}
	var outs []shaping.Output
	for _, run := range f.seg.Split(in, fontmap{f}) {
		outs = append(outs, f.shaper.Shape(run))
	}
	f.runs.put(key, outs, f.frame())
	return outs
}

// wrap breaks a shaped paragraph into lines no wider than width view
// units (or one line when width is zero), cached.
func (f *Font) wrap(text string, opts TextOptions, width float32) []shaping.Line {
	lines, _ := f.wrapText(text, opts, width)
	return lines
}

// wrapText is wrap returning the text that was shaped too, which differs
// from text when hyphenation inserted soft hyphens.
func (f *Font) wrapText(text string, opts TextOptions, width float32) ([]shaping.Line, string) {
	scale := f.sizeScale(opts.Size)
	px := 0
	if width > 0 {
		px = int(width / scale * f.scale)
	}
	spacing := fixed.Int26_6(opts.LetterSpacing / scale * f.scale * 64)
	key := lineKey{runKey{text, opts.Direction, opts.Language}, px, int32(spacing), opts.Hyphenate}
	shaped := text
	if opts.Hyphenate != nil && px > 0 {
		shaped = opts.Hyphenate.SoftHyphens(text)
		// The wrapper does not count the hyphen a break will add, so
		// leave room for one on every line.
		px -= int(f.hyphenAdvance() * f.scale)
	}
	if lines, ok := f.lines.get(key, f.frame()); ok {
		return lines, shaped
	}
	outs := f.shape(shaped, opts)
	var lines []shaping.Line
	if px <= 0 || len(outs) == 0 {
		lines = []shaping.Line{shaping.Line(copyOutputs(outs))}
	} else {
		// The wrapper may alter the runs it is given and reuses its own
		// storage for the lines it returns, so it gets copies and the
		// cache keeps copies.
		runs := copyOutputs(outs)
		track(runs, spacing)
		wrapped, _ := f.wrapper.WrapParagraph(shaping.WrapConfig{Direction: direction(shaped, opts.Direction)}, px, []rune(shaped), shaping.NewSliceIterator(runs))
		lines = make([]shaping.Line, len(wrapped))
		for i, l := range wrapped {
			lines[i] = copyOutputs(l)
		}
		spacing = 0 // already applied
	}
	for _, l := range lines {
		track(l, spacing)
	}
	f.lines.put(key, lines, f.frame())
	return lines, shaped
}

// track adds letter spacing to every glyph's advance.
func track(runs []shaping.Output, spacing fixed.Int26_6) {
	if spacing == 0 {
		return
	}
	for i := range runs {
		if runs[i].Direction.IsVertical() {
			continue
		}
		for j := range runs[i].Glyphs {
			runs[i].Glyphs[j].Advance += spacing
			runs[i].Glyphs[j].XAdvance += spacing
		}
		runs[i].Advance += spacing * fixed.Int26_6(len(runs[i].Glyphs))
	}
}

// copyOutputs deep-copies shaped runs.
func copyOutputs(outs []shaping.Output) []shaping.Output {
	c := make([]shaping.Output, len(outs))
	for i, o := range outs {
		c[i] = o
		c[i].Glyphs = slices.Clone(o.Glyphs)
	}
	return c
}

// sizeScale is the draw scale for a font at an em size: exact for SDF
// fonts, a resampling of the atlas for bitmap ones.
func (f *Font) sizeScale(size float32) float32 {
	if size <= 0 {
		return 1
	}
	return size / f.Size
}

// Glyph is one positioned glyph from Shape: where to draw a piece of the
// atlas relative to the text's origin (the pen at the start of the
// baseline), in view units at the font's size.
type Glyph struct {
	Pos      lin.Vec2 // top-left of the glyph image
	Size     lin.Vec2
	UV0, UV1 lin.Vec2 // region of the font's Texture
	Index    int      // index of the first byte of its text in the string
	// Advance is how far the pen moves after the glyph, in view units at
	// the font's own size, for caret positions and hit-testing. It is the
	// line height for vertical text.
	Advance float32
	Empty   bool // no image (a space)
	Color   bool // a colour glyph such as an emoji, drawn untinted
}

// Shape lays out one line of text and returns its glyphs in visual order,
// for drawing them yourself with Draw and the font's Texture, or for
// hit-testing. This low-level operation ignores block alignment, wrapping and
// decorations; Layout handles those. Rasterization and upload errors are
// returned explicitly. The source must be one valid UTF-8 line.
func (f *Font) Shape(text string, opts TextOptions) ([]Glyph, error) {
	if f == nil || f.destroyed {
		return nil, fmt.Errorf("gfx: shaping requires a live font")
	}
	if !utf8.ValidString(text) || strings.Contains(text, "\n") {
		return nil, fmt.Errorf("gfx: Shape requires one valid UTF-8 line; use Layout for blocks")
	}
	if err := validateTextOptions(opts); err != nil {
		return nil, err
	}
	if _, err := validateTextArithmetic(f, []*Font{f}, nil, opts, text); err != nil {
		return nil, err
	}
	f.glyphErr = nil
	opts = opts.resolved()
	var out []Glyph
	for _, line := range f.wrap(text, opts, 0) {
		out = f.appendLine(out, text, line, lin.V2(0, f.Ascent))
	}
	if f.dirty {
		if err := f.flush(); err != nil {
			return nil, err
		}
	}
	if f.glyphErr != nil {
		return nil, f.glyphErr
	}
	return out, nil
}

// hyphenAdvance is the width of the main face's hyphen in view units.
func (f *Font) hyphenAdvance() float32 {
	ff := f.faces[0]
	gid, ok := ff.face.NominalGlyph('-')
	if !ok {
		return 0
	}
	return ff.face.HorizontalAdvance(gid) * f.pxPerEm / ff.upem / f.scale
}

// appendLine positions a line's glyphs from origin (on the baseline for
// horizontal text, at the top for vertical) in visual order. A line that
// ends on a soft hyphen gets a hyphen drawn after it.
func (f *Font) appendLine(out []Glyph, text string, line shaping.Line, origin lin.Vec2) []Glyph {
	pen := origin
	start := len(out) // this line's first glyph, since out may hold earlier lines
	vertical := len(line) > 0 && line[0].Direction.IsVertical()
	index := &f.scratch.index
	index.reset(text)
	// Runs come in logical order; draw them in visual order.
	ordered := f.scratch.ordered[:0]
	for i := range line {
		ordered = append(ordered, &line[i])
	}
	f.scratch.ordered = ordered
	slices.SortStableFunc(ordered, func(a, b *shaping.Output) int {
		return cmp.Compare(a.VisualIndex, b.VisualIndex)
	})
	for _, run := range ordered {
		face := f.faceIndex(run.Face)
		ff := f.faces[face]
		for _, sg := range run.Glyphs {
			gl := f.glyph(face, sg.GlyphID)
			g := Glyph{Index: index.at(sg.TextIndex()), Empty: gl.empty}
			if vertical {
				// Few fonts carry vertical metrics, so vertical text stacks
				// em boxes on the line height, each glyph centred on the
				// column with its baseline where a horizontal line would be.
				hadv := ff.face.HorizontalAdvance(sg.GlyphID) * f.pxPerEm / ff.upem / f.scale
				if !gl.empty {
					g.Pos = lin.V2(pen.X-hadv/2+gl.bearing.X, pen.Y+f.Ascent+gl.bearing.Y)
					g.Size = gl.size
					g.UV0, g.UV1 = gl.uv0, gl.uv1
				}
				g.Advance = f.LineHeight
				out = append(out, g)
				pen.Y += f.LineHeight
				continue
			}
			ox := fixedToFloat(sg.XOffset) / f.scale
			oy := -fixedToFloat(sg.YOffset) / f.scale
			if !gl.empty {
				g.Pos = lin.V2(pen.X+ox+gl.bearing.X, pen.Y+oy+gl.bearing.Y)
				g.Size = gl.size
				g.UV0, g.UV1 = gl.uv0, gl.uv1
				g.Color = gl.color
			}
			g.Advance = fixedToFloat(sg.Advance) / f.scale
			out = append(out, g)
			pen.X += g.Advance
		}
	}
	// A line that wrapped at a soft hyphen shows the hyphen.
	if !vertical && len(out) > start && len(ordered) > 0 {
		if last := out[len(out)-1]; last.Index < len(text) && strings.HasPrefix(text[last.Index:], "­") {
			face := f.faceIndex(ordered[len(ordered)-1].Face)
			ff := f.faces[face]
			if gid, ok := ff.face.NominalGlyph('-'); ok {
				gl := f.glyph(face, gid)
				adv := ff.face.HorizontalAdvance(gid) * f.pxPerEm / ff.upem / f.scale
				g := Glyph{Index: last.Index, Advance: adv, Empty: gl.empty}
				if !gl.empty {
					g.Pos = lin.V2(pen.X+gl.bearing.X, pen.Y+gl.bearing.Y)
					g.Size, g.UV0, g.UV1 = gl.size, gl.uv0, gl.uv1
				}
				out = append(out, g)
			}
		}
	}
	return out
}

// DrawText draws one line with its top-left corner at (x, y).
func (g *Graphics) DrawText(f *Font, text string, x, y float32, c Color) {
	g.drawLines(f, text, x, y, TextOptions{}, c)
}

// DrawGlyphs draws glyphs from Shape with the text's origin at (x, y),
// scaled by scale (1, or zero, for the font's own size).
func (g *Graphics) DrawGlyphs(f *Font, glyphs []Glyph, x, y, scale float32, c Color) {
	if f == nil || f.destroyed || f.g != g {
		g.recordDrawError(fmt.Errorf("gfx: drawing glyphs requires a live font owned by this Graphics"))
		return
	}
	k := scale
	if k <= 0 {
		k = 1
	}
	for _, gl := range glyphs {
		if gl.Empty {
			continue
		}
		tint := c
		if gl.Color {
			tint = Color{1, 1, 1, c.A} // a colour glyph keeps its own colours
		}
		g.Draw(f.atlas, Sprite{Pos: lin.V2(x+gl.Pos.X*k, y+gl.Pos.Y*k), Size: gl.Size.Mul(k), UV0: gl.UV0, UV1: gl.UV1, Color: tint})
	}
}

// Measure returns the size text takes when drawn with the options: one
// line with the zero options, or wrapped, spaced and sized as they say.
// The width is the widest line's advance and the height is the line
// height times the line spacing for every line; vertical text swaps the
// two. Measure shares the layout Layout and DrawTextBlock use, so text
// measured and then drawn with the same options is shaped once, and
// measuring the same static label every frame costs a map lookup. Text
// that cannot be laid out (a destroyed font, a full atlas) is measured by
// shaping alone.
func (f *Font) Measure(text string, opts TextOptions) (w, h float32) {
	if l, err := f.Layout(text, opts); err == nil {
		return l.measure.X, l.measure.Y
	}
	if f == nil || validateTextOptions(opts) != nil {
		return 0, 0
	}
	l := measureOnly(RichFonts{Regular: f}, RichText{Runs: []RichRun{{Text: strings.ToValidUTF8(text, "�")}}}, opts.resolved())
	if l == nil {
		return 0, 0
	}
	return l.measure.X, l.measure.Y
}

// DrawTextBlock draws wrapped, aligned text with its top-left at (x, y),
// or its first baseline there with Baseline set. With a Width, alignment
// is within that width; without, lines align to x. Size scales the text
// and Angle rotates it about (x, y). Vertical text runs down from (x, y)
// in columns stepping left, so x is the right edge.
func (g *Graphics) DrawTextBlock(f *Font, text string, x, y float32, opts TextOptions, c Color) {
	g.drawLines(f, text, x, y, opts, c)
}

// drawLines shapes, wraps, aligns and draws text. Glyphs rasterised by
// the layout are uploaded before the sprites are queued, so a glyph first
// drawn this frame is in the atlas the frame samples.
func (g *Graphics) drawLines(f *Font, text string, x, y float32, opts TextOptions, c Color) {
	if !g.checkDrawFont(f) {
		return
	}
	l, err := f.Layout(text, opts)
	if err != nil {
		g.recordDrawError(err)
		return
	}
	g.DrawTextLayout(l, x, y, c)
}
