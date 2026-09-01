package gfx

import (
	"strings"

	"github.com/matjam/bunyip/lin"
)

// Align positions lines within a text block.
type Align uint8

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

// TextOptions lays out multi-line text.
type TextOptions struct {
	Width       float32 // wrap width in view units; zero means no wrapping
	Align       Align
	LineSpacing float32 // multiplier; zero means 1
	Size        float32 // SDF fonts only: em size; zero means the font's size
}

// Layout splits text into lines that fit the options' width, breaking at
// spaces and, when a word alone is too wide, inside the word.
func (f *Font) Layout(text string, opts TextOptions) []string {
	scale := f.sizeScale(opts.Size)
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		if opts.Width <= 0 {
			lines = append(lines, para)
			continue
		}
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := ""
		for _, w := range words {
			candidate := w
			if line != "" {
				candidate = line + " " + w
			}
			if cw, _ := f.Measure(candidate); cw*scale <= opts.Width || line == "" {
				if line == "" && cw*scale > opts.Width {
					// Break a single overlong word by runes.
					for _, r := range w {
						if lw, _ := f.Measure(line + string(r)); lw*scale > opts.Width && line != "" {
							lines = append(lines, line)
							line = ""
						}
						line += string(r)
					}
					continue
				}
				line = candidate
				continue
			}
			lines = append(lines, line)
			line = w
		}
		lines = append(lines, line)
	}
	return lines
}

func (f *Font) sizeScale(size float32) float32 {
	if size <= 0 || !f.sdf {
		return 1
	}
	return size / f.Size
}

// MeasureBlock returns the size of laid-out text.
func (f *Font) MeasureBlock(text string, opts TextOptions) (w, h float32) {
	scale := f.sizeScale(opts.Size)
	spacing := opts.LineSpacing
	if spacing == 0 {
		spacing = 1
	}
	lines := f.Layout(text, opts)
	for _, l := range lines {
		lw, _ := f.Measure(l)
		w = max(w, lw*scale)
	}
	return w, float32(len(lines)) * f.LineHeight * scale * spacing
}

// DrawTextBlock draws wrapped, aligned text with its top-left at (x, y).
// With a Width, alignment is within that width; without, lines align to x.
func (g *Graphics) DrawTextBlock(f *Font, text string, x, y float32, opts TextOptions, c Color) {
	scale := f.sizeScale(opts.Size)
	spacing := opts.LineSpacing
	if spacing == 0 {
		spacing = 1
	}
	lines := f.Layout(text, opts)
	width := opts.Width
	if width <= 0 {
		for _, l := range lines {
			lw, _ := f.Measure(l)
			width = max(width, lw*scale)
		}
	}
	for i, l := range lines {
		lw, _ := f.Measure(l)
		lx := x
		switch opts.Align {
		case AlignCenter:
			lx = x + (width-lw*scale)/2
		case AlignRight:
			lx = x + width - lw*scale
		}
		ly := y + float32(i)*f.LineHeight*scale*spacing
		if f.sdf && opts.Size > 0 {
			g.DrawTextSized(f, l, lx, ly, opts.Size, 0, c)
		} else {
			g.DrawText(f, l, lx, ly, c)
		}
	}
}

var _ = lin.V2
