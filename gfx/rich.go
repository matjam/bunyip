package gfx

import (
	"strconv"
	"strings"

	"github.com/matjam/bunyip/lin"
)

// RichRun is a stretch of text in one style: a colour, a bold or italic
// face, an underline, or a link a click can hit.
type RichRun struct {
	Text      string
	Color     Color // zero means the block's colour
	Bold      bool
	Italic    bool
	Underline bool
	Link      string // a name reported back with its rectangle
}

// RichText is styled text made of runs, from ParseRich or by hand.
type RichText struct {
	Runs []RichRun
}

// Plain returns the text without styling.
func (rt RichText) Plain() string {
	var b strings.Builder
	for _, r := range rt.Runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// ParseRich reads a small markup: [b]bold[/b], [i]italic[/i],
// [u]underlined[/u], [#ff8800]coloured[/#] (or [color=#ff8800]...[/color]),
// and [link=name]text[/link]. Tags nest, "[[" is a literal bracket, and
// an unknown tag is kept as text.
func ParseRich(markup string) RichText {
	var rt RichText
	type style struct {
		color             Color
		bold, ital, under bool
		link              string
	}
	var stack []style
	cur := style{}
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		rt.Runs = append(rt.Runs, RichRun{Text: text.String(), Color: cur.color, Bold: cur.bold, Italic: cur.ital, Underline: cur.under, Link: cur.link})
		text.Reset()
	}
	for i := 0; i < len(markup); {
		c := markup[i]
		if c != '[' {
			text.WriteByte(c)
			i++
			continue
		}
		if strings.HasPrefix(markup[i:], "[[") {
			text.WriteByte('[')
			i += 2
			continue
		}
		end := strings.IndexByte(markup[i:], ']')
		if end < 0 {
			text.WriteByte(c)
			i++
			continue
		}
		tag := markup[i+1 : i+end]
		i += end + 1
		next := cur
		switch {
		case tag == "b" || tag == "i" || tag == "u":
			stack = append(stack, cur)
			switch tag {
			case "b":
				next.bold = true
			case "i":
				next.ital = true
			case "u":
				next.under = true
			}
		case strings.HasPrefix(tag, "#") && len(tag) == 7:
			if col, ok := parseHexColor(tag[1:]); ok {
				stack = append(stack, cur)
				next.color = col
			} else {
				text.WriteString("[" + tag + "]")
				continue
			}
		case strings.HasPrefix(tag, "color="):
			v := strings.TrimPrefix(tag[6:], "#")
			if col, ok := parseHexColor(v); ok {
				stack = append(stack, cur)
				next.color = col
			} else {
				text.WriteString("[" + tag + "]")
				continue
			}
		case strings.HasPrefix(tag, "link="):
			stack = append(stack, cur)
			next.link = tag[5:]
			next.under = true
		case strings.HasPrefix(tag, "/"):
			if len(stack) == 0 {
				text.WriteString("[" + tag + "]")
				continue
			}
			next = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
		default:
			text.WriteString("[" + tag + "]")
			continue
		}
		flush()
		cur = next
	}
	flush()
	return rt
}

func parseHexColor(s string) (Color, bool) {
	if len(s) != 6 {
		return Color{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return Color{}, false
	}
	return RGB(uint8(v>>16), uint8(v>>8), uint8(v)), true
}

// RichFonts are the faces rich text draws with; a nil variant falls back
// to Regular, so plain text needs only that.
type RichFonts struct {
	Regular, Bold, Italic, BoldItalic *Font
}

func (rf RichFonts) font(run RichRun) *Font {
	switch {
	case run.Bold && run.Italic && rf.BoldItalic != nil:
		return rf.BoldItalic
	case run.Bold && rf.Bold != nil:
		return rf.Bold
	case run.Italic && rf.Italic != nil:
		return rf.Italic
	}
	return rf.Regular
}

// RichLink is where a link was drawn, for hit-testing clicks; a link
// that wraps reports one rectangle per line.
type RichLink struct {
	Name string
	Rect lin.Rect
}

// richShape is a maximal stretch of runs drawn with one font and no line
// break, shaped as a single string so that kerning and ligatures cross
// the colour and link changes inside it. Its glyphs are cut apart again
// by cluster: a glyph belongs to the run holding the first byte of its
// text.
type richShape struct {
	font  *Font
	text  string
	spans []richSpan // where each run's text ends within text
	// glyphs are positioned from the shape's origin on the baseline, at
	// the font's own size; pen holds the position before each glyph and
	// one past the last, scaled to the drawing size.
	glyphs []Glyph
	pen    []float32
	scale  float32
}

// richSpan says which run the bytes before end belong to.
type richSpan struct {
	end int
	run int
}

// runAt returns the run a byte of the shape's text came from.
func (s *richShape) runAt(i int) int {
	for _, sp := range s.spans {
		if i < sp.end {
			return sp.run
		}
	}
	if n := len(s.spans); n > 0 {
		return s.spans[n-1].run
	}
	return 0
}

// shapes splits rich text into the runs that can be shaped together and
// shapes each one.
func (rf RichFonts) shapes(rt RichText, opts TextOptions) []*richShape {
	var out []*richShape
	var cur *richShape
	var parts []string
	flush := func() {
		if cur == nil {
			return
		}
		if len(parts) == 1 {
			cur.text = parts[0] // one run, one line: no copy
		} else {
			cur.text = strings.Join(parts, "")
		}
		parts = parts[:0]
		if cur.text != "" {
			cur.shape(opts)
			out = append(out, cur)
		}
		cur = nil
	}
	for ri, run := range rt.Runs {
		f := rf.font(run)
		if f == nil {
			continue
		}
		first := true
		for part := range strings.SplitSeq(run.Text, "\n") {
			if !first {
				// A newline ends a shape and stands between two of them.
				flush()
				out = append(out, nil)
			}
			first = false
			if part == "" {
				continue
			}
			if cur != nil && cur.font != f {
				flush()
			}
			if cur == nil {
				cur = &richShape{font: f}
			}
			n := 0
			for _, p := range parts {
				n += len(p)
			}
			parts = append(parts, part)
			cur.spans = append(cur.spans, richSpan{end: n + len(part), run: ri})
		}
	}
	flush()
	return out
}

// shape lays the whole run out as one string and records the pen
// position before each glyph.
func (s *richShape) shape(opts TextOptions) {
	s.scale = s.font.sizeScale(opts.Size)
	s.glyphs = s.font.blockGlyphs(s.text, TextOptions{
		Size: opts.Size, LetterSpacing: opts.LetterSpacing, Baseline: true,
	})
	s.pen = make([]float32, len(s.glyphs)+1)
	x := float32(0)
	for i, gl := range s.glyphs {
		s.pen[i] = x * s.scale
		x += gl.Advance
	}
	s.pen[len(s.glyphs)] = x * s.scale
}

// richPiece is a run of glyphs from one shape that wrapping keeps
// together: a word, or the spaces between two words.
type richPiece struct {
	shape    *richShape
	from, to int // glyph indices in the shape
	x        float32
	width    float32
	space    bool
}

// pieces cuts a shape into words and the spaces between them.
func (s *richShape) pieces(out []richPiece) []richPiece {
	i := 0
	isSpace := func(g Glyph) bool {
		return g.Index < len(s.text) && s.text[g.Index] == ' '
	}
	for i < len(s.glyphs) {
		j, space := i, isSpace(s.glyphs[i])
		for j < len(s.glyphs) && isSpace(s.glyphs[j]) == space {
			j++
		}
		out = append(out, richPiece{shape: s, from: i, to: j, space: space, width: s.pen[j] - s.pen[i]})
		i = j
	}
	return out
}

// richLine is a laid-out line of pieces.
type richLine struct {
	pieces  []richPiece
	width   float32
	ascent  float32
	height  float32
	descent float32
}

func (rf RichFonts) layout(rt RichText, opts TextOptions) []richLine {
	var lines []richLine
	cur := richLine{}
	metrics := func(l *richLine, f *Font) {
		scale := f.sizeScale(opts.Size)
		l.ascent = max(l.ascent, f.Ascent*scale)
		l.descent = max(l.descent, f.Descent*scale)
		l.height = max(l.height, f.LineHeight*scale)
	}
	metrics(&cur, rf.Regular)
	flush := func() {
		// Trailing spaces do not count towards a line's width.
		for len(cur.pieces) > 0 && cur.pieces[len(cur.pieces)-1].space {
			cur.width -= cur.pieces[len(cur.pieces)-1].width
			cur.pieces = cur.pieces[:len(cur.pieces)-1]
		}
		lines = append(lines, cur)
		cur = richLine{}
		metrics(&cur, rf.Regular)
	}
	var buf []richPiece
	for _, s := range rf.shapes(rt, opts) {
		if s == nil {
			flush()
			continue
		}
		buf = s.pieces(buf[:0])
		for _, p := range buf {
			if opts.Width > 0 && !p.space && len(cur.pieces) > 0 && cur.width+p.width > opts.Width {
				flush()
			}
			if p.space && len(cur.pieces) == 0 {
				continue // no leading spaces after a wrap
			}
			p.x = cur.width
			cur.pieces = append(cur.pieces, p)
			cur.width += p.width
			metrics(&cur, s.font)
		}
	}
	flush()
	return lines
}

// MeasureRich returns the size rich text takes with the options. It is
// zero without a Regular font, which every run falls back to.
func (rf RichFonts) MeasureRich(rt RichText, opts TextOptions) (w, h float32) {
	if rf.Regular == nil {
		return 0, 0
	}
	spacing := opts.LineSpacing
	if spacing == 0 {
		spacing = 1
	}
	for _, l := range rf.layout(rt, opts) {
		w = max(w, l.width)
		h += l.height * spacing
	}
	return w, h
}

// DrawRichText draws styled text with its top-left at (x, y), wrapping at
// the options' Width and aligning within it, and returns where each link
// landed. Colours in runs override c; underlines and links draw a line
// under their text.
func (g *Graphics) DrawRichText(fonts RichFonts, rt RichText, x, y float32, opts TextOptions, c Color) []RichLink {
	if fonts.Regular == nil {
		return nil
	}
	if c == (Color{}) {
		c = White
	}
	spacing := opts.LineSpacing
	if spacing == 0 {
		spacing = 1
	}
	var links []RichLink
	lines := fonts.layout(rt, opts)
	// Laying the text out rasterised any new glyph, so the atlases upload
	// before the sprites that sample them are queued.
	for _, f := range []*Font{fonts.Regular, fonts.Bold, fonts.Italic, fonts.BoldItalic} {
		if f != nil && f.dirty {
			_ = f.flush()
		}
	}
	width := opts.Width
	if width <= 0 {
		for _, l := range lines {
			width = max(width, l.width)
		}
	}
	top := y
	for _, l := range lines {
		left := x
		switch opts.Align {
		case AlignCenter:
			left += (width - l.width) / 2
		case AlignRight:
			left += width - l.width
		}
		base := top + l.ascent
		for _, p := range l.pieces {
			s := p.shape
			// Every glyph of the piece is placed from one origin, so the
			// shaped positions are kept whatever the piece was cut from.
			origin := left + p.x - s.pen[p.from]
			// A style change inside a shaped run splits its glyphs by
			// cluster; the shaping itself already crossed the change.
			for i := p.from; i < p.to; {
				run := rt.Runs[s.runAt(s.glyphs[i].Index)]
				j := i + 1
				for j < p.to && rt.Runs[s.runAt(s.glyphs[j].Index)] == run {
					j++
				}
				col := run.Color
				if col == (Color{}) {
					col = c
				}
				gx, gw := origin+s.pen[i], s.pen[j]-s.pen[i]
				if !p.space {
					g.DrawGlyphs(s.font, s.glyphs[i:j], origin, base, s.scale, col)
				}
				if run.Underline || run.Link != "" {
					th := max(s.font.Size*s.scale/14, 1)
					g.FillRect(gx, base+th, gw, th, col)
				}
				if run.Link != "" {
					r := lin.R(gx, top, gw, l.height)
					if n := len(links); n > 0 && links[n-1].Name == run.Link && links[n-1].Rect.Y == top && links[n-1].Rect.X+links[n-1].Rect.W >= gx-0.5 {
						links[n-1].Rect.W = gx + gw - links[n-1].Rect.X
					} else {
						links = append(links, RichLink{Name: run.Link, Rect: r})
					}
				}
				i = j
			}
		}
		top += l.height * spacing
	}
	return links
}
