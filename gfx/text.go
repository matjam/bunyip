package gfx

import (
	"slices"
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
	Baseline  bool
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
	width int
}

const textCacheEntries = 2048

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
	if runs, ok := f.runs[key]; ok {
		return runs
	}
	if len(f.runs) >= textCacheEntries {
		clear(f.runs)
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
	f.runs[key] = outs
	return outs
}

// wrap breaks a shaped paragraph into lines no wider than width view
// units (or one line when width is zero), cached.
func (f *Font) wrap(text string, opts TextOptions, width float32) []shaping.Line {
	scale := f.sizeScale(opts.Size)
	px := 0
	if width > 0 {
		px = int(width / scale * f.scale)
	}
	key := lineKey{runKey{text, opts.Direction, opts.Language}, px}
	if lines, ok := f.lines[key]; ok {
		return lines
	}
	if len(f.lines) >= textCacheEntries {
		clear(f.lines)
	}
	outs := f.shape(text, opts)
	var lines []shaping.Line
	if px <= 0 || len(outs) == 0 {
		lines = []shaping.Line{shaping.Line(copyOutputs(outs))}
	} else {
		// The wrapper may alter the runs it is given and reuses its own
		// storage for the lines it returns, so it gets copies and the
		// cache keeps copies.
		wrapped, _ := f.wrapper.WrapParagraph(shaping.WrapConfig{Direction: direction(text, opts.Direction)}, px, []rune(text), shaping.NewSliceIterator(copyOutputs(outs)))
		lines = make([]shaping.Line, len(wrapped))
		for i, l := range wrapped {
			lines[i] = copyOutputs(l)
		}
	}
	f.lines[key] = lines
	return lines
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
}

// Shape lays out one line of text and returns its glyphs in visual order,
// for drawing them yourself with Draw and the font's Texture, or for
// hit-testing.
func (f *Font) Shape(text string, opts TextOptions) []Glyph {
	var out []Glyph
	for _, line := range f.wrap(text, opts, 0) {
		out = f.appendLine(out, text, line, lin.V2(0, f.Ascent))
	}
	if f.dirty {
		_ = f.flush()
	}
	return out
}

// byteIndex maps a rune index in text to a byte index.
func byteIndex(text string, runeIndex int) int {
	i := 0
	for b := range text {
		if i == runeIndex {
			return b
		}
		i++
	}
	return len(text)
}

// appendLine positions a line's glyphs from origin (on the baseline for
// horizontal text, at the top for vertical) in visual order.
func (f *Font) appendLine(out []Glyph, text string, line shaping.Line, origin lin.Vec2) []Glyph {
	pen := origin
	vertical := len(line) > 0 && line[0].Direction.IsVertical()
	// Runs come in logical order; draw them in visual order.
	ordered := make([]*shaping.Output, len(line))
	for i := range line {
		ordered[i] = &line[i]
	}
	for i := range ordered {
		for j := i + 1; j < len(ordered); j++ {
			if ordered[j].VisualIndex < ordered[i].VisualIndex {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}
	for _, run := range ordered {
		face := f.faceIndex(run.Face)
		ff := f.faces[face]
		for _, sg := range run.Glyphs {
			gl := f.glyph(face, sg.GlyphID)
			g := Glyph{Index: byteIndex(text, sg.TextIndex()), Empty: gl.empty}
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
			}
			out = append(out, g)
			pen.X += fixedToFloat(sg.Advance) / f.scale
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
		g.Draw(f.atlas, Sprite{Pos: lin.V2(x+gl.Pos.X*k, y+gl.Pos.Y*k), Size: gl.Size.Mul(k), UV0: gl.UV0, UV1: gl.UV1, Color: c})
	}
}

// Layout splits text into the lines DrawTextBlock would draw, breaking
// by the Unicode line-breaking rules at the options' width.
func (f *Font) Layout(text string, opts TextOptions) []string {
	var out []string
	for _, para := range splitParagraphs(text) {
		lines := f.wrap(para, opts, opts.Width)
		if len(lines) == 0 {
			out = append(out, "")
			continue
		}
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
			runes := []rune(para)
			s := string(runes[lo:hi])
			for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
				s = s[:len(s)-1]
			}
			out = append(out, s)
		}
	}
	return out
}

func splitParagraphs(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	return append(out, text[start:])
}

// Measure returns the size text takes when drawn with the options: one
// line with the zero options, or wrapped, spaced and sized as they say.
func (f *Font) Measure(text string, opts TextOptions) (w, h float32) {
	scale := f.sizeScale(opts.Size)
	spacing := opts.LineSpacing
	if spacing == 0 {
		spacing = 1
	}
	n := 0
	for _, para := range splitParagraphs(text) {
		lines := f.wrap(para, opts, opts.Width)
		for _, line := range lines {
			w = max(w, f.advance(line)*scale)
		}
		n += max(len(lines), 1)
	}
	if opts.Direction == DirectionTTB {
		return float32(n) * f.LineHeight * scale * spacing, w
	}
	return w, float32(n) * f.LineHeight * scale * spacing
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
	scale := f.sizeScale(opts.Size)
	spacing := opts.LineSpacing
	if spacing == 0 {
		spacing = 1
	}
	step := f.LineHeight * scale * spacing
	vertical := opts.Direction == DirectionTTB
	var lines []shaping.Line
	for _, para := range splitParagraphs(text) {
		pl := f.wrap(para, opts, opts.Width)
		if len(pl) == 0 {
			pl = []shaping.Line{nil}
		}
		lines = append(lines, pl...)
	}
	width := opts.Width
	if width <= 0 {
		for _, line := range lines {
			width = max(width, f.advance(line)*scale)
		}
	}
	var glyphs []Glyph
	for i, line := range lines {
		lw := f.advance(line) * scale
		offset := float32(0)
		switch opts.Align {
		case AlignCenter:
			offset = (width - lw) / 2
		case AlignRight:
			offset = width - lw
		}
		var origin lin.Vec2
		if vertical {
			origin = lin.V2(-float32(i)*step-f.LineHeight*scale/2, offset)
		} else if opts.Baseline {
			origin = lin.V2(offset, float32(i)*step)
		} else {
			origin = lin.V2(offset, float32(i)*step+f.Ascent*scale)
		}
		glyphs = f.appendLine(glyphs[:0], text, line, origin.Mul(1/scale))
		g.DrawGlyphs(f, glyphs, x, y, scale, c)
	}
	if f.dirty {
		// New glyphs were rasterised this frame; the atlas uploads for the
		// next one. Losing one frame of a rare glyph beats stalling.
		_ = f.flush()
	}
}
