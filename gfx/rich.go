package gfx

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/matjam/bunyip/lin"
)

// RichRun is a stretch of text in one style: a colour, a bold or italic
// face, decorations, an outline, or a link a click can hit. A shaping cluster
// crossing a style boundary takes all styles from its first source byte;
// combining sequences and ligatures are never split by a colour or link change.
type RichRun struct {
	Text          string
	Color         Color // zero means the block's colour
	Bold          bool
	Italic        bool
	Underline     bool
	Strikethrough bool
	OutlineWidth  float32 // zero inherits TextOptions.OutlineWidth
	OutlineColor  Color   // zero inherits the block's outline colour
	Link          string  // a name reported back with its rectangle
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
// [u]underlined[/u], [s]struck through[/s], [#ff8800]coloured[/#]
// (or [color=#ff8800]...[/color]),
// and [link=name]text[/link]. Tags nest, "[[" is a literal bracket, and
// an unknown tag is kept as text.
func ParseRich(markup string) RichText {
	var rt RichText
	type style struct {
		color                     Color
		bold, ital, under, strike bool
		link                      string
	}
	var stack []style
	cur := style{}
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		rt.Runs = append(rt.Runs, RichRun{Text: text.String(), Color: cur.color, Bold: cur.bold, Italic: cur.ital, Underline: cur.under, Strikethrough: cur.strike, Link: cur.link})
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
		case tag == "b" || tag == "i" || tag == "u" || tag == "s":
			stack = append(stack, cur)
			switch tag {
			case "b":
				next.bold = true
			case "i":
				next.ital = true
			case "u":
				next.under = true
			case "s":
				next.strike = true
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

// MeasureRich returns logical text dimensions using the same Unicode shaping
// and wrapping as Layout, without rasterizing or uploading glyphs. It returns
// zero for missing fonts or invalid options; Layout reports those errors.
func (rf RichFonts) MeasureRich(rt RichText, opts TextOptions) (w, h float32) {
	if rf.Regular == nil || rf.Regular.destroyed || validateTextOptions(opts) != nil || !utf8.ValidString(rt.Plain()) {
		return 0, 0
	}
	opts = opts.resolved()
	l := &TextLayout{text: rt.Plain(), options: opts, rotation: lin.Identity2()}
	b := layoutBuilder{layout: l, regular: rf.Regular, unit: rf.Regular.scale / rf.Regular.sizeScale(opts.Size), measureOnly: true}
	start := 0
	fonts := []*Font{rf.Regular}
	for _, run := range rt.Runs {
		f := rf.font(run)
		if f.destroyed || f.g != rf.Regular.g {
			return 0, 0
		}
		fonts = append(fonts, f)
		b.spans = append(b.spans, layoutSpan{start: start, end: start + len(run.Text), font: f, style: run})
		start += len(run.Text)
	}
	unit, err := validateTextArithmetic(rf.Regular, fonts, b.spans, opts, l.text)
	if err != nil {
		return 0, 0
	}
	b.unit = unit
	if err := b.build(); err != nil {
		return 0, 0
	}
	return l.bounds.W, l.bounds.H
}
