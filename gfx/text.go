package gfx

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

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
	Width       float32 // wrap width in view units (column height for vertical text); zero means no wrapping
	Align       Align
	LineSpacing float32 // multiplier; zero means 1
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
	Direction Direction
	// Language is a BCP 47 tag ("tr", "zh-Hant") that picks language-specific
	// glyph forms; empty means the font's default.
	Language string
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

// shape runs the segmenter and HarfBuzz over one paragraph, cached.
func (f *Font) shape(text string, opts TextOptions) []shaping.Output {
	key := runKey{text, opts.Direction, opts.Language}
	if runs, ok := f.runs.get(key); ok {
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
	f.runs.put(key, outs)
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
	if lines, ok := f.lines.get(key); ok {
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
	f.lines.put(key, lines)
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

// advance is a line's total advance in view units, at the font's size.
func (f *Font) advance(line shaping.Line) float32 {
	if len(line) > 0 && line[0].Direction.IsVertical() {
		n := 0
		for _, run := range line {
			n += len(run.Glyphs)
		}
		return float32(n) * f.LineHeight
	}
	var a fixed.Int26_6
	for _, run := range line {
		a += run.Advance
	}
	if a < 0 {
		a = -a
	}
	return fixedToFloat(a) / f.scale
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
	Empty    bool     // no image (a space)
	Color    bool     // a colour glyph such as an emoji, drawn untinted
}

// Shape lays out one line of text and returns its glyphs in visual order,
// for drawing them yourself with Draw and the font's Texture, or for
// hit-testing.
func (f *Font) Shape(text string, opts TextOptions) []Glyph {
	var out []Glyph
	for _, line := range f.wrap(text, opts, 0) {
		out = f.appendLine(out, text, line, lin.V2(0, f.Ascent), 0)
	}
	if f.dirty {
		_ = f.flush()
	}
	return out
}

// endsSoftHyphen reports whether a line's last glyph sits on a soft
// hyphen, so a hyphen will be drawn after it. index must already be
// pointed at the line's text.
func endsSoftHyphen(index *runeIndex, line shaping.Line) bool {
	if len(line) == 0 {
		return false
	}
	run := line[len(line)-1]
	if len(run.Glyphs) == 0 {
		return false
	}
	text := index.text
	i := index.at(run.Glyphs[len(run.Glyphs)-1].TextIndex())
	return i < len(text) && strings.HasPrefix(text[i:], "­")
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

// spaceCount counts the spaces in a line, for justification. index must
// already be pointed at the line's text.
func spaceCount(index *runeIndex, line shaping.Line) int {
	n := 0
	for _, run := range line {
		for _, sg := range run.Glyphs {
			if index.isSpace(sg.TextIndex()) {
				n++
			}
		}
	}
	return n
}

// appendLine positions a line's glyphs from origin (on the baseline for
// horizontal text, at the top for vertical) in visual order. spaceExtra
// widens each space, for justified text; a line that ends on a soft
// hyphen gets a hyphen drawn after it.
func (f *Font) appendLine(out []Glyph, text string, line shaping.Line, origin lin.Vec2, spaceExtra float32) []Glyph {
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
			out = append(out, g)
			pen.X += fixedToFloat(sg.Advance) / f.scale
			if spaceExtra != 0 && index.isSpace(sg.TextIndex()) {
				pen.X += spaceExtra
			}
		}
	}
	// A line that wrapped at a soft hyphen shows the hyphen.
	if !vertical && len(out) > start && len(ordered) > 0 {
		if last := out[len(out)-1]; last.Index < len(text) && strings.HasPrefix(text[last.Index:], "­") {
			face := f.faceIndex(ordered[len(ordered)-1].Face)
			ff := f.faces[face]
			if gid, ok := ff.face.NominalGlyph('-'); ok {
				gl := f.glyph(face, gid)
				g := Glyph{Index: last.Index, Empty: gl.empty}
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

// Layout splits text into the lines DrawTextBlock would draw, breaking
// by the Unicode line-breaking rules at the options' width. A line
// broken by a Hyphenator is given without the soft hyphens the
// hyphenator inserted and without the hyphen drawn at the break.
func (f *Font) Layout(text string, opts TextOptions) []string {
	var out []string
	for para := range strings.SplitSeq(text, "\n") {
		// The lines index the shaped text, which carries the soft hyphens
		// hyphenation inserted, so they are cut from that and the soft
		// hyphens taken back out.
		lines, shaped := f.wrapText(para, opts, opts.Width)
		if len(lines) == 0 {
			out = append(out, "")
			continue
		}
		var runes []rune
		for _, line := range lines {
			lo, hi := -1, 0
			for _, run := range line {
				if lo < 0 || run.Runes.Offset < lo {
					lo = run.Runes.Offset
				}
				hi = max(hi, run.Runes.Offset+run.Runes.Count)
			}
			if lo < 0 {
				out = append(out, "")
				continue
			}
			if runes == nil {
				runes = []rune(shaped)
			}
			s := string(runes[lo:min(hi, len(runes))])
			for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
				s = s[:len(s)-1]
			}
			out = append(out, strings.ReplaceAll(s, "­", ""))
		}
	}
	return out
}

// Measure returns the size text takes when drawn with the options: one
// line with the zero options, or wrapped, spaced and sized as they say.
// The result is cached, so measuring the same static label every frame
// costs a map lookup.
func (f *Font) Measure(text string, opts TextOptions) (w, h float32) {
	key := measureKey{
		text: text, lang: opts.Language, hyph: opts.Hyphenate,
		width: opts.Width, size: opts.Size, lineSpacing: opts.LineSpacing,
		letterSpacing: opts.LetterSpacing, dir: opts.Direction,
	}
	if size, ok := f.measures.get(key); ok {
		return size.X, size.Y
	}
	scale := f.sizeScale(opts.Size)
	spacing := opts.LineSpacing
	if spacing == 0 {
		spacing = 1
	}
	n := 0
	for para := range strings.SplitSeq(text, "\n") {
		lines := f.wrap(para, opts, opts.Width)
		for _, line := range lines {
			w = max(w, f.advance(line)*scale)
		}
		n += max(len(lines), 1)
	}
	if opts.Direction == DirectionTTB {
		w, h = float32(n)*f.LineHeight*scale*spacing, w
	} else {
		h = float32(n) * f.LineHeight * scale * spacing
	}
	f.measures.put(key, lin.V2(w, h))
	return w, h
}

// DrawTextBlock draws wrapped, aligned text with its top-left at (x, y),
// or its first baseline there with Baseline set. With a Width, alignment
// is within that width; without, lines align to x. Size scales the text
// and Angle rotates it about (x, y). Vertical text runs down from (x, y)
// in columns stepping left, so x is the right edge.
func (g *Graphics) DrawTextBlock(f *Font, text string, x, y float32, opts TextOptions, c Color) {
	if opts.Angle != 0 {
		g.Transformed(lin.Translate2(x, y).Mul(lin.Rotate2(opts.Angle)).Mul(lin.Translate2(-x, -y)), func() {
			g.drawLines(f, text, x, y, opts, c)
		})
		return
	}
	g.drawLines(f, text, x, y, opts, c)
}

// drawLines shapes, wraps, aligns and draws text.
func (g *Graphics) drawLines(f *Font, text string, x, y float32, opts TextOptions, c Color) {
	g.DrawGlyphs(f, f.blockGlyphs(text, opts), x, y, f.sizeScale(opts.Size), c)
	if f.dirty {
		// New glyphs were rasterised this frame; the atlas uploads for the
		// next one. Losing one frame of a rare glyph beats stalling.
		_ = f.flush()
	}
}

// blockGlyphs returns a block's glyphs positioned relative to its origin
// at the font's own size, cached by the text and the options that move a
// glyph. Drawing the same string again is then a walk of the cached
// glyphs; nothing is shaped, wrapped or aligned a second time. The
// returned slice belongs to the font and must not be modified.
func (f *Font) blockGlyphs(text string, opts TextOptions) []Glyph {
	key := blockKey{
		text: text, lang: opts.Language, hyph: opts.Hyphenate,
		width: opts.Width, size: opts.Size, lineSpacing: opts.LineSpacing,
		letterSpacing: opts.LetterSpacing, dir: opts.Direction,
		align: opts.Align, baseline: opts.Baseline,
	}
	if glyphs, ok := f.blocks.get(key); ok {
		return glyphs
	}
	glyphs := slices.Clone(f.layoutBlock(text, opts))
	f.blocks.put(key, glyphs)
	return glyphs
}

// layoutBlock shapes, wraps and aligns text into glyphs positioned
// relative to the block's origin. The result is the font's scratch
// storage, valid until the next layout.
func (f *Font) layoutBlock(text string, opts TextOptions) []Glyph {
	scale := f.sizeScale(opts.Size)
	spacing := opts.LineSpacing
	if spacing == 0 {
		spacing = 1
	}
	step := f.LineHeight * scale * spacing
	vertical := opts.Direction == DirectionTTB
	s := &f.scratch
	lines, paras, last := s.lines[:0], s.paras[:0], s.last[:0]
	for para := range strings.SplitSeq(text, "\n") {
		pl, shaped := f.wrapText(para, opts, opts.Width)
		if len(pl) == 0 {
			pl = []shaping.Line{nil}
		}
		for i := range pl {
			paras = append(paras, shaped)
			last = append(last, i == len(pl)-1)
		}
		lines = append(lines, pl...)
	}
	s.lines, s.paras, s.last = lines, paras, last
	width := opts.Width
	if width <= 0 {
		for _, line := range lines {
			width = max(width, f.advance(line)*scale)
		}
	}
	glyphs := s.glyphs[:0]
	for i, line := range lines {
		lw := f.advance(line) * scale
		offset, spaceExtra := float32(0), float32(0)
		switch opts.Align {
		case AlignCenter:
			offset = (width - lw) / 2
		case AlignRight:
			offset = width - lw
		case AlignJustify:
			if !last[i] && !vertical && opts.Width > 0 {
				s.index.reset(paras[i])
				avail := width
				if endsSoftHyphen(&s.index, line) {
					avail -= f.hyphenAdvance() * scale // the hyphen drawn after the line
				}
				if n := spaceCount(&s.index, line); n > 0 {
					spaceExtra = (avail - lw) / float32(n) / scale
				}
			}
		}
		var origin lin.Vec2
		if vertical {
			origin = lin.V2(-float32(i)*step-f.LineHeight*scale/2, offset)
		} else if opts.Baseline {
			origin = lin.V2(offset, float32(i)*step)
		} else {
			origin = lin.V2(offset, float32(i)*step+f.Ascent*scale)
		}
		glyphs = f.appendLine(glyphs, paras[i], line, origin.Mul(1/scale), spaceExtra)
	}
	s.glyphs = glyphs
	return glyphs
}
