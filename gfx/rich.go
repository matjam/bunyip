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

// richToken is one word or space of a run, measured with its font.
type richToken struct {
	text    string
	run     int
	font    *Font
	width   float32
	space   bool
	newline bool
}

func (rf RichFonts) tokens(rt RichText, opts TextOptions) []richToken {
	var out []richToken
	measure := TextOptions{Size: opts.Size, LetterSpacing: opts.LetterSpacing}
	for ri, run := range rt.Runs {
		f := rf.font(run)
		start := 0
		emit := func(s string, space bool) {
			if s == "" {
				return
			}
			w, _ := f.Measure(s, measure)
			out = append(out, richToken{text: s, run: ri, font: f, width: w, space: space})
		}
		for i, r := range run.Text {
			switch r {
			case ' ', '\n':
				emit(run.Text[start:i], false)
				if r == '\n' {
					out = append(out, richToken{run: ri, font: f, newline: true})
				} else {
					emit(" ", true)
				}
				start = i + 1
			}
		}
		emit(run.Text[start:], false)
	}
	return out
}

// richLine is a laid-out line of tokens.
type richLine struct {
	toks    []richToken
	width   float32
	ascent  float32
	height  float32
	descent float32
}

func (rf RichFonts) layout(rt RichText, opts TextOptions) []richLine {
	scale := rf.Regular.sizeScale(opts.Size)
	var lines []richLine
	cur := richLine{}
	metrics := func(l *richLine, f *Font) {
		l.ascent = max(l.ascent, f.Ascent*scale)
		l.descent = max(l.descent, f.Descent*scale)
		l.height = max(l.height, f.LineHeight*scale)
	}
	metrics(&cur, rf.Regular)
	flush := func() {
		// Trailing spaces do not count towards a line's width.
		for len(cur.toks) > 0 && cur.toks[len(cur.toks)-1].space {
			cur.width -= cur.toks[len(cur.toks)-1].width
			cur.toks = cur.toks[:len(cur.toks)-1]
		}
		lines = append(lines, cur)
		cur = richLine{}
		metrics(&cur, rf.Regular)
	}
	for _, t := range rf.tokens(rt, opts) {
		if t.newline {
			flush()
			continue
		}
		if opts.Width > 0 && !t.space && len(cur.toks) > 0 && cur.width+t.width > opts.Width {
			flush()
		}
		if t.space && len(cur.toks) == 0 {
			continue // no leading spaces after a wrap
		}
		cur.toks = append(cur.toks, t)
		cur.width += t.width
		metrics(&cur, t.font)
	}
	flush()
	return lines
}

// MeasureRich returns the size rich text takes with the options.
func (rf RichFonts) MeasureRich(rt RichText, opts TextOptions) (w, h float32) {
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
	draw := TextOptions{Size: opts.Size, LetterSpacing: opts.LetterSpacing, Baseline: true}
	var links []RichLink
	lines := fonts.layout(rt, opts)
	width := opts.Width
	if width <= 0 {
		for _, l := range lines {
			width = max(width, l.width)
		}
	}
	top := y
	for _, l := range lines {
		pen := x
		switch opts.Align {
		case AlignCenter:
			pen += (width - l.width) / 2
		case AlignRight:
			pen += width - l.width
		}
		base := top + l.ascent
		for _, t := range l.toks {
			run := rt.Runs[t.run]
			col := run.Color
			if col == (Color{}) {
				col = c
			}
			if !t.space {
				g.DrawTextBlock(t.font, t.text, pen, base, draw, col)
			}
			if run.Underline || run.Link != "" {
				th := max(t.font.Size*fonts.Regular.sizeScale(opts.Size)/14, 1)
				g.FillRect(pen, base+th, t.width, th, col)
			}
			if run.Link != "" {
				r := lin.R(pen, top, t.width, l.height)
				if n := len(links); n > 0 && links[n-1].Name == run.Link && links[n-1].Rect.Y == top && links[n-1].Rect.X+links[n-1].Rect.W >= pen-0.5 {
					links[n-1].Rect.W = pen + t.width - links[n-1].Rect.X
				} else {
					links = append(links, RichLink{Name: run.Link, Rect: r})
				}
			}
			pen += t.width
		}
		top += l.height * spacing
	}
	return links
}
