package gfx

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	textfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	unicodeSeg "github.com/go-text/typesetting/segmenter"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// CaretAffinity chooses the side of a source boundary at a wrap or bidi
// transition. Leading follows the next cluster; Trailing follows the previous.
type CaretAffinity uint8

const (
	CaretLeading CaretAffinity = iota
	CaretTrailing
)

// TextCaret identifies an insertion boundary in the original UTF-8 source.
// Ligatures and combining clusters are atomic. Invalid or interior indices
// snap to the nearest valid cluster boundary, preferring the lower on a tie.
type TextCaret struct {
	Index    int
	Affinity CaretAffinity
}

// TextLine describes one visual line or vertical column. Start and End are
// byte offsets into TextLayout.Text, excluding an explicit terminating newline.
// Bounds includes advances and the line height, not glyph overhangs. Baseline
// is its baseline origin; all coordinates include TextOptions.Angle.
type TextLine struct {
	Start, End int
	Bounds     lin.Rect
	Baseline   lin.Vec2
	Direction  Direction
}

type layoutCaret struct {
	position TextCaret
	rect     lin.Rect // unrotated
	line     int
}

type layoutGlyph struct {
	font                    *Font
	glyph                   glyph
	pos, size               lin.Vec2
	style                   RichRun
	outline                 outlineGlyph
	outlinePos, outlineSize lin.Vec2
}

type layoutDecoration struct {
	rect  lin.Rect
	style RichRun
}

// TextLayout is an immutable, reusable shaped text block. It owns ordinary
// Go data and borrows its fonts and their atlases; it needs no Destroy.
// Queries remain valid after font destruction, but drawing requires live fonts.
// Construct one with Font.Layout or RichFonts.Layout, then DrawTextLayout.
type TextLayout struct {
	text              string
	options           TextOptions
	lines             []TextLine
	glyphs            []layoutGlyph
	decorations       []layoutDecoration
	carets            []layoutCaret
	links             []RichLink
	fonts             []*Font
	bounds, inkBounds lin.Rect
	rotation          lin.Affine
}

// Text returns the original text (RichText.Plain for a styled layout).
func (l *TextLayout) Text() string { return l.text }

// Lines returns an independent copy of the line descriptions.
func (l *TextLayout) Lines() []TextLine { return slices.Clone(l.lines) }

// Bounds returns logical advance and line-box bounds, including alignment,
// spacing and rotation. Wrapped trailing whitespace has zero advance but
// retains source caret boundaries; unwrapped whitespace keeps its advance.
// Glyphs can extend beyond this rectangle.
func (l *TextLayout) Bounds() lin.Rect { return l.bounds }

// InkBounds returns glyph ink and decoration bounds, including outlines and
// rotation. Atlas padding and the sampling filter's antialias fringe are excluded.
func (l *TextLayout) InkBounds() lin.Rect { return l.inkBounds }

// Links returns an independent copy of link rectangles in layout coordinates.
func (l *TextLayout) Links() []RichLink { return slices.Clone(l.links) }

// Caret returns the caret's axis-aligned rectangle in layout coordinates.
// The rectangle is one view unit thick before rotation. Out-of-range indices
// clamp to the source ends; points inside a cluster snap to its closest edge.
func (l *TextLayout) Caret(position TextCaret) lin.Rect {
	if len(l.carets) == 0 {
		return lin.Rect{}
	}
	position.Index = min(max(position.Index, 0), len(l.text))
	best, distance := 0, int(^uint(0)>>1)
	for i, c := range l.carets {
		d := c.position.Index - position.Index
		if d < 0 {
			d = -d
		}
		if d < distance || d == distance && (c.position.Index < l.carets[best].position.Index || c.position.Index == l.carets[best].position.Index && c.position.Affinity == position.Affinity && l.carets[best].position.Affinity != position.Affinity) {
			best, distance = i, d
		}
	}
	return l.rotation.TransformRect(l.carets[best].rect)
}

// HitTest returns the closest cluster boundary to a point in layout
// coordinates. It inverse-rotates the point and clamps outside the text to
// its closest line and caret, preserving wrap and bidi affinity.
func (l *TextLayout) HitTest(point lin.Vec2) TextCaret {
	if len(l.carets) == 0 {
		return TextCaret{}
	}
	point = lin.Rotate2(-l.options.Angle).Apply(point)
	best, distance := 0, float32(math.Inf(1))
	for i, c := range l.carets {
		r := c.rect
		dx := point.X - lin.Clamp(point.X, r.X, r.X+r.W)
		dy := point.Y - lin.Clamp(point.Y, r.Y, r.Y+r.H)
		d := dx*dx + dy*dy
		if d < distance {
			best, distance = i, d
		}
	}
	return l.carets[best].position
}

type textLayoutKey struct {
	text, rich string
	fonts      RichFonts
	options    TextOptions
}

// Layout shapes and wraps text once for drawing, measurement and caret
// queries. Indices always address the original UTF-8 string, including
// paragraphs and wrapping with generated hyphens. Invalid options, exhausted
// atlas capacity and GPU allocation/upload failures are returned to the caller.
func (f *Font) Layout(text string, opts TextOptions) (*TextLayout, error) {
	return (RichFonts{Regular: f}).buildLayout(RichText{Runs: []RichRun{{Text: text}}}, opts, "")
}

// Layout constructs the same reusable result as Font.Layout, preserving
// styles and links. Text indices address RichText.Plain, never markup tags.
// Regular is required; missing style faces fall back to it. Fonts must be
// live and belong to the same Graphics. The result borrows their atlases.
func (rf RichFonts) Layout(text RichText, opts TextOptions) (*TextLayout, error) {
	key, err := json.Marshal(text.Runs)
	if err != nil {
		return nil, fmt.Errorf("gfx: text styles: %w", err)
	}
	return rf.buildLayout(text, opts, string(key))
}

func validateTextOptions(o TextOptions) error {
	for _, n := range []float32{o.Width, o.LineSpacing, o.Size, o.Angle, o.LetterSpacing, o.OutlineWidth, o.OutlineColor.R, o.OutlineColor.G, o.OutlineColor.B, o.OutlineColor.A} {
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			return fmt.Errorf("gfx: text options must be finite")
		}
	}
	if o.Width < 0 || o.Size < 0 || o.LineSpacing < 0 || o.OutlineWidth < 0 || o.Align > AlignJustify || o.Direction > DirectionTTB {
		return fmt.Errorf("gfx: invalid text options")
	}
	return nil
}

type layoutSpan struct {
	start, end int
	font       *Font
	style      RichRun
}

func (rf RichFonts) buildLayout(text RichText, opts TextOptions, richKey string) (*TextLayout, error) {
	if err := validateTextOptions(opts); err != nil {
		return nil, err
	}
	if rf.Regular == nil {
		return nil, fmt.Errorf("gfx: text layout needs a regular font")
	}
	opts = opts.resolved()
	plain := text.Plain()
	if !utf8.ValidString(plain) {
		return nil, fmt.Errorf("gfx: text must be valid UTF-8")
	}
	var spans []layoutSpan
	var fonts []*Font
	start := 0
	for _, run := range text.Runs {
		f := rf.font(run)
		if f.destroyed || f.g != rf.Regular.g {
			return nil, fmt.Errorf("gfx: text layout requires live fonts from one Graphics")
		}
		if !slices.Contains(fonts, f) {
			fonts = append(fonts, f)
		}
		if run.OutlineWidth < 0 || math.IsNaN(float64(run.OutlineWidth)) || math.IsInf(float64(run.OutlineWidth), 0) {
			return nil, fmt.Errorf("gfx: invalid text outline width")
		}
		style := run
		style.Text = ""
		style.Underline = style.Underline || opts.Underline
		style.Strikethrough = style.Strikethrough || opts.Strikethrough
		if style.OutlineWidth == 0 {
			style.OutlineWidth = opts.OutlineWidth
		}
		if style.OutlineColor == (Color{}) {
			style.OutlineColor = opts.OutlineColor
		}
		spans = append(spans, layoutSpan{start: start, end: start + len(run.Text), font: f, style: style})
		start += len(run.Text)
	}
	if len(fonts) == 0 {
		fonts = append(fonts, rf.Regular)
	}
	if rf.Regular.destroyed {
		return nil, fmt.Errorf("gfx: text layout font was destroyed")
	}
	unit, err := validateTextArithmetic(rf.Regular, fonts, spans, opts, plain)
	if err != nil {
		return nil, err
	}
	key := textLayoutKey{text: plain, rich: richKey, fonts: rf, options: opts}
	if l, ok := rf.Regular.layouts.get(key); ok {
		return l, nil
	}
	l := &TextLayout{text: plain, options: opts, fonts: fonts, rotation: lin.Rotate2(opts.Angle)}
	for _, f := range fonts {
		f.glyphErr = nil
	}
	b := layoutBuilder{layout: l, regular: rf.Regular, spans: spans, unit: unit}
	if err := b.build(); err != nil {
		return nil, err
	}
	for _, f := range fonts {
		if f.glyphErr != nil {
			return nil, f.glyphErr
		}
		if err := f.flush(); err != nil {
			return nil, fmt.Errorf("gfx: text atlas upload: %w", err)
		}
		for _, page := range f.outlinePages {
			if err := page.flush(); err != nil {
				return nil, fmt.Errorf("gfx: outline atlas upload: %w", err)
			}
		}
	}
	rf.Regular.layouts.limit = textBlockGlyphs
	rf.Regular.layouts.weigh = func(l *TextLayout) int { return len(l.glyphs) + len(l.carets) + 1 }
	rf.Regular.layouts.put(key, l)
	return l, nil
}

func validateTextArithmetic(regular *Font, fonts []*Font, spans []layoutSpan, o TextOptions, text string) (float32, error) {
	fail := func() (float32, error) {
		return 0, fmt.Errorf("gfx: text size, spacing or width is outside the supported shaping range")
	}
	k := regular.sizeScale(o.Size)
	unit := regular.scale / k
	if k <= 0 || unit <= 0 || math.IsInf(float64(k), 0) || math.IsInf(float64(unit), 0) || math.IsNaN(float64(unit)) {
		return fail()
	}
	fixedLimit := float64(1 << 30)
	if o.Width > 0 && float64(o.Width)*float64(unit)*64 > fixedLimit {
		return fail()
	}
	spacing := math.Abs(float64(o.LetterSpacing) * float64(unit) * 64)
	if spacing > fixedLimit || math.IsInf(spacing, 0) {
		return fail()
	}
	maxAdvance := float64(0)
	for _, f := range fonts {
		scale := f.sizeScale(o.Size)
		size := float64(f.Size) * float64(scale) * float64(unit) * 64
		height := float64(f.LineHeight) * float64(scale) * float64(unit) * 64
		if scale <= 0 || math.IsInf(float64(scale), 0) || size < 1 || size > fixedLimit || height <= 0 || height > fixedLimit || math.IsNaN(size) {
			return fail()
		}
		maxAdvance = max(maxAdvance, height+spacing)
		lineSpacing := o.LineSpacing
		if lineSpacing == 0 {
			lineSpacing = 1
		}
		if float64(f.LineHeight)*float64(scale)*float64(lineSpacing)*float64(len(text)+1) > math.MaxFloat32 {
			return fail()
		}
	}
	if maxAdvance*float64(utf8.RuneCountInString(text)+1) > fixedLimit {
		return fail()
	}
	for _, s := range spans {
		for _, c := range []Color{s.style.Color, s.style.OutlineColor} {
			for _, n := range []float32{c.R, c.G, c.B, c.A} {
				if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
					return 0, fmt.Errorf("gfx: text colours must be finite")
				}
			}
		}
		if s.style.OutlineWidth > 0 {
			needed := float64(s.style.OutlineWidth) * sdfEmPixels / (float64(s.font.Size) * float64(s.font.sizeScale(o.Size)))
			if needed+2 > maxOutlineSpread || float32(needed) == 0 {
				return 0, fmt.Errorf("gfx: text outline is outside the supported distance range")
			}
		}
	}
	return unit, nil
}

type layoutParagraph struct {
	text       string
	runes      []rune
	source     []int // every shaped rune boundary to a full-source byte boundary
	lines      []shaping.Line
	owners     map[*textfont.Face]*Font
	start, end int
}

type layoutBuilder struct {
	layout            *TextLayout
	regular           *Font
	spans             []layoutSpan
	unit              float32 // common shaping pixels per view unit
	inkSet, boundsSet bool
	measureOnly       bool
}

func (b *layoutBuilder) spanAt(index int) layoutSpan {
	for _, s := range b.spans {
		if index < s.end {
			return s
		}
	}
	if len(b.spans) > 0 {
		return b.spans[len(b.spans)-1]
	}
	return layoutSpan{font: b.regular}
}

func (b *layoutBuilder) paragraph(text string, start int) layoutParagraph {
	opts := b.layout.options
	shaped := text
	if opts.Hyphenate != nil && opts.Width > 0 {
		shaped = opts.Hyphenate.SoftHyphens(text)
	}
	p := layoutParagraph{text: shaped, runes: []rune(shaped), owners: map[*textfont.Face]*Font{}, start: start, end: start + len(text)}
	offset := 0
	for _, r := range p.runes {
		p.source = append(p.source, start+offset)
		if offset < len(text) {
			original, n := utf8.DecodeRuneInString(text[offset:])
			if r == original {
				offset += n
			}
		}
	}
	p.source = append(p.source, start+len(text))
	// A style boundary inside a grapheme belongs to its first source byte,
	// including font changes. HarfBuzz may further combine these into ligatures.
	fontAt := make([]*Font, len(p.runes))
	var graphemes unicodeSeg.Segmenter
	graphemes.Init(p.runes)
	for it := graphemes.GraphemeIterator(); it.Next(); {
		cluster := it.Grapheme()
		f := b.spanAt(p.source[cluster.Offset]).font
		for i := cluster.Offset; i < cluster.Offset+len(cluster.Text); i++ {
			fontAt[i] = f
		}
	}
	input := shaping.Input{Text: p.runes, RunEnd: len(p.runes), Direction: direction(text, opts.Direction), Face: b.regular.faces[0].face, Size: fixed.Int26_6(b.regular.pxPerEm * 64)}
	if opts.Language != "" {
		input.Language = language.NewLanguage(opts.Language)
	}
	var segmenter shaping.Segmenter
	var outputs []shaping.Output
	for _, segment := range segmenter.Split(input, fontmap{b.regular}) {
		for from := segment.RunStart; from < segment.RunEnd; {
			f := fontAt[from]
			to := from + 1
			for to < segment.RunEnd && fontAt[to] == f {
				to++
			}
			in := segment
			in.RunStart, in.RunEnd = from, to
			in.Face = f.faces[0].face
			in.Size = fixed.Int26_6(f.Size * f.sizeScale(opts.Size) * b.unit * 64)
			in.FontFeatures = f.features
			for _, sub := range shaping.SplitByFace(in, fontmap{f}) {
				out := f.shaper.Shape(sub)
				if opts.Direction == DirectionTTB {
					previous := -1
					for i := range out.Glyphs {
						sg := &out.Glyphs[i]
						sg.Advance, sg.YAdvance = 0, 0
						if sg.TextIndex() != previous {
							sg.Advance = -fixed.Int26_6(f.LineHeight * f.sizeScale(opts.Size) * b.unit * 64)
							sg.YAdvance = sg.Advance
						}
						previous = sg.TextIndex()
					}
					out.RecomputeAdvance()
				}
				p.owners[out.Face] = f
				outputs = append(outputs, out)
			}
			from = to
		}
	}
	track(outputs, fixed.Int26_6(opts.LetterSpacing*b.unit*64))
	width := float32(1 << 24)
	if opts.Width > 0 {
		width = opts.Width
	}
	if opts.Hyphenate != nil && opts.Width > 0 {
		width = max(width-b.regular.hyphenAdvance()*b.regular.sizeScale(opts.Size), 1/b.unit)
	}
	var wrapper shaping.LineWrapper
	wrapped, _ := wrapper.WrapParagraphF(shaping.WrapConfig{Direction: input.Direction, DisableTrailingWhitespaceTrim: true}, fixed.Int26_6(min(width*b.unit*64, 1<<30)), p.runes, shaping.NewSliceIterator(outputs))
	for _, line := range wrapped {
		line = copyOutputs(line)
		if opts.Width > 0 {
			// Preserve source glyphs and caret boundaries while removing every
			// trailing whitespace advance, including across face/bidi runs.
			end := 0
			for _, run := range line {
				end = max(end, run.Runes.Offset+run.Runes.Count)
			}
			trim := end
			for trim > 0 && unicode.IsSpace(p.runes[trim-1]) {
				trim--
			}
			for i := range line {
				for j := range line[i].Glyphs {
					gl := &line[i].Glyphs[j]
					if gl.TextIndex() >= trim {
						gl.Advance, gl.XAdvance, gl.YAdvance = 0, 0, 0
					}
				}
				line[i].RecomputeAdvance()
			}
		}
		p.lines = append(p.lines, line)
	}
	if len(p.lines) == 0 {
		p.lines = []shaping.Line{nil}
	}
	return p
}

func unionTextRect(current lin.Rect, has *bool, next lin.Rect) lin.Rect {
	if !*has {
		*has = true
		return next
	}
	x, y := min(current.X, next.X), min(current.Y, next.Y)
	return lin.R(x, y, max(current.X+current.W, next.X+next.W)-x, max(current.Y+current.H, next.Y+next.H)-y)
}

func (b *layoutBuilder) build() error {
	var paragraphs []layoutParagraph
	start := 0
	for text := range strings.SplitSeq(b.layout.text, "\n") {
		paragraphs = append(paragraphs, b.paragraph(text, start))
		start += len(text) + 1
	}
	width := b.layout.options.Width
	for _, p := range paragraphs {
		for _, line := range p.lines {
			width = max(width, b.lineAdvance(p, line))
		}
	}
	if b.layout.options.Width > 0 {
		width = b.layout.options.Width
	}
	top := float32(0)
	for _, p := range paragraphs {
		for i, line := range p.lines {
			if err := b.appendLine(p, line, i == len(p.lines)-1, width, &top); err != nil {
				return err
			}
		}
	}
	l := b.layout
	l.bounds = l.rotation.TransformRect(l.bounds)
	if b.inkSet {
		l.inkBounds = l.rotation.TransformRect(l.inkBounds)
	}
	for i := range l.lines {
		l.lines[i].Bounds = l.rotation.TransformRect(l.lines[i].Bounds)
		l.lines[i].Baseline = l.rotation.Apply(l.lines[i].Baseline)
	}
	for i := range l.links {
		l.links[i].Rect = l.rotation.TransformRect(l.links[i].Rect)
	}
	return nil
}

func (b *layoutBuilder) lineAdvance(p layoutParagraph, line shaping.Line) float32 {
	var advance float32
	for _, run := range line {
		f := p.owners[run.Face]
		if b.layout.options.Direction == DirectionTTB {
			previous := -1
			for _, sg := range run.Glyphs {
				if sg.TextIndex() != previous && sg.Advance != 0 {
					advance += f.LineHeight * f.sizeScale(b.layout.options.Size)
				}
				previous = sg.TextIndex()
			}
		} else {
			advance += fixedToFloat(run.Advance) / b.unit
		}
	}
	if index := lineHyphen(p, line); index >= 0 {
		f := b.spanAt(p.source[index]).font
		advance += f.hyphenAdvance() * f.sizeScale(b.layout.options.Size)
	}
	return advance
}

func lineHyphen(p layoutParagraph, line shaping.Line) int {
	hi := 0
	for _, run := range line {
		hi = max(hi, run.Runes.Offset+run.Runes.Count)
	}
	if hi > 0 && hi < len(p.runes) && p.runes[hi-1] == '\u00ad' {
		return hi - 1
	}
	return -1
}

func (b *layoutBuilder) caret(index int, affinity CaretAffinity, at lin.Vec2, height float32, vertical bool, line int) {
	r := lin.R(at.X, at.Y, 1, height)
	if vertical {
		r = lin.R(at.X, at.Y, height, 1)
	}
	position := TextCaret{Index: index, Affinity: affinity}
	for i := len(b.layout.carets) - 1; i >= 0; i-- {
		c := &b.layout.carets[i]
		if c.line != line {
			break
		}
		if c.position == position {
			c.rect = r
			return
		}
	}
	b.layout.carets = append(b.layout.carets, layoutCaret{position: position, rect: r, line: line})
}

func (b *layoutBuilder) appendLine(p layoutParagraph, line shaping.Line, last bool, width float32, top *float32) error {
	l, o := b.layout, b.layout.options
	vertical := o.Direction == DirectionTTB
	height, ascent := b.regular.LineHeight*b.regular.sizeScale(o.Size), b.regular.Ascent*b.regular.sizeScale(o.Size)
	start, end := p.end, p.start
	for _, run := range line {
		f := p.owners[run.Face]
		height = max(height, f.LineHeight*f.sizeScale(o.Size))
		ascent = max(ascent, f.Ascent*f.sizeScale(o.Size))
		start = min(start, p.source[run.Runes.Offset])
		end = max(end, p.source[min(run.Runes.Offset+run.Runes.Count, len(p.source)-1)])
	}
	if len(line) == 0 {
		start, end = p.start, p.end
	}
	advance := b.lineAdvance(p, line)
	hyphenIndex := lineHyphen(p, line)
	spaceExtra := float32(0)
	if o.Align == AlignJustify && !last && !vertical && o.Width > 0 {
		spaces := 0
		for _, run := range line {
			for _, sg := range run.Glyphs {
				if sg.TextIndex() < len(p.runes) && p.runes[sg.TextIndex()] == ' ' && sg.Advance != 0 {
					spaces++
				}
			}
		}
		if spaces > 0 {
			spaceExtra = (width - advance) / float32(spaces)
			advance = width
		}
	}
	offset := float32(0)
	if o.Align == AlignCenter {
		offset = (width - advance) / 2
	} else if o.Align == AlignRight {
		offset = width - advance
	}
	origin := lin.V2(offset, *top+ascent)
	if o.Baseline {
		origin.Y -= b.regular.Ascent * b.regular.sizeScale(o.Size)
	}
	logical := lin.R(offset, origin.Y-ascent, advance, height)
	if vertical {
		origin = lin.V2(-*top-height/2, offset)
		logical = lin.R(-*top-height, offset, height, advance)
	}
	lineIndex := len(l.lines)
	dir := DirectionLTR
	if direction(p.text, o.Direction).Progression() == di.TowardTopLeft {
		dir = DirectionRTL
	}
	if vertical {
		dir = DirectionTTB
	}
	l.lines = append(l.lines, TextLine{Start: start, End: end, Bounds: logical, Baseline: origin, Direction: dir})
	l.bounds = unionTextRect(l.bounds, &b.boundsSet, logical)
	ordered := slices.Clone(line)
	slices.SortStableFunc(ordered, func(a, c shaping.Output) int { return int(a.VisualIndex - c.VisualIndex) })
	pen := origin
	for _, run := range ordered {
		f := p.owners[run.Face]
		k := f.sizeScale(o.Size)
		face := f.faceIndex(run.Face)
		for i := 0; i < len(run.Glyphs); {
			sg := run.Glyphs[i]
			j := i + 1
			for j < len(run.Glyphs) && run.Glyphs[j].TextIndex() == sg.TextIndex() {
				j++
			}
			from := p.source[min(sg.TextIndex(), len(p.source)-1)]
			to := p.source[min(sg.TextIndex()+sg.RunesCount(), len(p.source)-1)]
			style := b.spanAt(from).style
			before := pen
			for _, sg := range run.Glyphs[i:j] {
				advance := fixedToFloat(sg.Advance) / b.unit
				if sg.TextIndex() == hyphenIndex && !vertical {
					if gid, ok := run.Face.NominalGlyph('-'); ok {
						sg.GlyphID = gid
						advance = f.hyphenAdvance() * k
					}
				}
				if sg.TextIndex() < len(p.runes) && p.runes[sg.TextIndex()] == ' ' && sg.Advance != 0 {
					advance += spaceExtra
				}
				gl := glyph{empty: true}
				if !b.measureOnly {
					gl = f.glyph(face, sg.GlyphID)
				}
				pos := lin.V2(pen.X+fixedToFloat(sg.XOffset)/b.unit+gl.bearing.X*k, pen.Y-fixedToFloat(sg.YOffset)/b.unit+gl.bearing.Y*k)
				if vertical {
					pos = lin.V2(pen.X-gl.size.X*k/2, pen.Y+f.Ascent*k+gl.bearing.Y*k)
				}
				if !gl.empty {
					item := layoutGlyph{font: f, glyph: gl, pos: pos, size: gl.size.Mul(k), style: style}
					if style.OutlineWidth > 0 {
						outlined, err := f.outlinedGlyph(face, sg.GlyphID, gl, style.OutlineWidth, k)
						if err != nil {
							return err
						}
						item.outline = outlined
						item.outlinePos = pos.Sub(gl.bearing.Mul(k)).Add(outlined.image.bearing.Mul(k))
						item.outlineSize = outlined.image.size.Mul(k)
					}
					l.glyphs = append(l.glyphs, item)
					ink := f.glyphInk(face, sg.GlyphID, gl)
					ink = lin.Translate2(pos.X-gl.bearing.X*k, pos.Y-gl.bearing.Y*k).Mul(lin.Scale2(k, k)).TransformRect(ink)
					if style.OutlineWidth > 0 {
						w := style.OutlineWidth
						ink = lin.R(ink.X-w, ink.Y-w, ink.W+2*w, ink.H+2*w)
					}
					l.inkBounds = unionTextRect(l.inkBounds, &b.inkSet, ink)
				}
				if !vertical {
					pen.X += advance
				}
			}
			if vertical && sg.Advance != 0 {
				pen.Y += f.LineHeight * k
			}
			leading, trailing := before, pen
			if !vertical && run.Direction.Progression() == di.TowardTopLeft {
				leading, trailing = trailing, leading
			}
			if vertical {
				leading.X, trailing.X = logical.X, logical.X
			} else {
				leading.Y, trailing.Y = logical.Y, logical.Y
			}
			if from != to {
				b.caret(from, CaretLeading, leading, height, vertical, lineIndex)
			}
			b.caret(to, CaretTrailing, trailing, height, vertical, lineIndex)
			b.decorate(style, f, k, before, pen, logical, vertical)
			i = j
		}
	}
	firstPoint, lastPoint := lin.V2(origin.X, logical.Y), lin.V2(pen.X, logical.Y)
	if dir == DirectionRTL {
		firstPoint, lastPoint = lastPoint, firstPoint
	}
	if vertical {
		firstPoint, lastPoint = lin.V2(logical.X, origin.Y), lin.V2(logical.X, pen.Y)
	}
	if !b.hasCaret(start, CaretLeading, lineIndex) {
		b.caret(start, CaretLeading, firstPoint, height, vertical, lineIndex)
	}
	if !b.hasCaret(end, CaretTrailing, lineIndex) {
		b.caret(end, CaretTrailing, lastPoint, height, vertical, lineIndex)
	}
	spacing := o.LineSpacing
	if spacing == 0 {
		spacing = 1
	}
	*top += height * spacing
	return nil
}

func (b *layoutBuilder) hasCaret(index int, affinity CaretAffinity, line int) bool {
	for i := len(b.layout.carets) - 1; i >= 0; i-- {
		c := b.layout.carets[i]
		if c.line != line {
			break
		}
		if c.position.Index == index && c.position.Affinity == affinity {
			return true
		}
	}
	return false
}

func (b *layoutBuilder) decorate(style RichRun, f *Font, k float32, from, to lin.Vec2, line lin.Rect, vertical bool) {
	thickness := max(f.Size*k/14, 1)
	for _, decoration := range []struct {
		enabled bool
		offset  float32
	}{{style.Underline || style.Link != "", thickness}, {style.Strikethrough, -f.Ascent * k / 3}} {
		if !decoration.enabled {
			continue
		}
		r := lin.R(from.X, from.Y+decoration.offset, to.X-from.X, thickness)
		if vertical {
			r = lin.R(from.X+decoration.offset, from.Y, thickness, to.Y-from.Y)
		}
		b.layout.decorations = append(b.layout.decorations, layoutDecoration{rect: r, style: style})
		b.layout.inkBounds = unionTextRect(b.layout.inkBounds, &b.inkSet, r)
	}
	if style.Link != "" {
		r := lin.R(from.X, line.Y, to.X-from.X, line.H)
		if vertical {
			r = lin.R(line.X, from.Y, line.W, to.Y-from.Y)
		}
		if n := len(b.layout.links); n > 0 {
			previous := &b.layout.links[n-1]
			if previous.Name == style.Link && (!vertical && previous.Rect.Y == r.Y && previous.Rect.X+previous.Rect.W >= r.X-0.01 || vertical && previous.Rect.X == r.X && previous.Rect.Y+previous.Rect.H >= r.Y-0.01) {
				set := true
				previous.Rect = unionTextRect(previous.Rect, &set, r)
				return
			}
		}
		b.layout.links = append(b.layout.links, RichLink{Name: style.Link, Rect: r})
	}
}

// DrawTextLayout draws a reusable layout at its origin. Zero tint is white.
// Tint multiplies explicit rich colours; runs without a colour use tint.
// Colour glyphs retain RGB and use the effective alpha. A nil layout is a
// no-op. Invalid fonts or upload failures are reported by frame submission.
func (g *Graphics) DrawTextLayout(l *TextLayout, x, y float32, tint Color) {
	if l == nil {
		return
	}
	for _, f := range l.fonts {
		if f.destroyed || f.g != g {
			g.recordDrawError(fmt.Errorf("gfx: drawing text requires live fonts owned by this Graphics"))
			return
		}
	}
	if tint == (Color{}) {
		tint = White
	}
	draw := func() {
		for _, item := range l.glyphs {
			c := item.style.Color
			if c == (Color{}) {
				c = White
			}
			c = c.Mul(tint)
			if item.outline.page != nil {
				outlineColor := item.style.OutlineColor
				if outlineColor == (Color{}) {
					outlineColor = c
				} else {
					outlineColor = outlineColor.Mul(tint)
				}
				page := item.outline.page
				threshold := float32(0.5) - item.style.OutlineWidth*float32(sdfEmPixels)/(item.font.Size*item.font.sizeScale(l.options.Size)*float32(page.spread)*2)
				g.recordDrawError(g.textOutlineShader.SetUniforms(outlineUniforms{Color: outlineColor.Premultiplied().Vec4(), Parameters: lin.V4(threshold, 0, 0, 0)}))
				g.Shaded(g.textOutlineShader, func() {
					g.Draw(page.texture, Sprite{Pos: lin.V2(x+item.outlinePos.X, y+item.outlinePos.Y), Size: item.outlineSize, UV0: item.outline.image.uv0, UV1: item.outline.image.uv1, Color: White})
				})
			}
			if item.glyph.color {
				c = Color{R: 1, G: 1, B: 1, A: c.A}
			}
			g.Draw(item.font.atlas, Sprite{Pos: lin.V2(x+item.pos.X, y+item.pos.Y), Size: item.size, UV0: item.glyph.uv0, UV1: item.glyph.uv1, Color: c})
		}
		for _, item := range l.decorations {
			c := item.style.Color
			if c == (Color{}) {
				c = White
			}
			r := item.rect
			g.FillRect(x+r.X, y+r.Y, r.W, r.H, c.Mul(tint))
		}
	}
	if l.options.Angle != 0 {
		g.Transformed(lin.Translate2(x, y).Mul(l.rotation).Mul(lin.Translate2(-x, -y)), draw)
	} else {
		draw()
	}
}

func (g *Graphics) recordDrawError(err error) {
	if g.drawErr == nil {
		var ve *vk.Error
		if errors.As(err, &ve) && ve.Result == vk.VK_ERROR_DEVICE_LOST {
			err = fmt.Errorf("%w: %w", render.ErrDeviceLost, err)
		}
		g.drawErr = err
	}
}

// DrawRichText draws a styled block through the common text layout and
// returns its link rectangles translated to the drawing origin. A zero tint
// is white; explicit run colours multiply tint. Layout and upload failures
// are reported by frame submission, as with DrawTextBlock.
func (g *Graphics) DrawRichText(fonts RichFonts, text RichText, x, y float32, opts TextOptions, tint Color) []RichLink {
	if !g.checkDrawFont(fonts.Regular) {
		return nil
	}
	for _, run := range text.Runs {
		if !g.checkDrawFont(fonts.font(run)) {
			return nil
		}
	}
	l, err := fonts.Layout(text, opts)
	if err != nil {
		g.recordDrawError(err)
		return nil
	}
	g.DrawTextLayout(l, x, y, tint)
	links := l.Links()
	for i := range links {
		links[i].Rect.X += x
		links[i].Rect.Y += y
	}
	return links
}
