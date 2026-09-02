package gfx

import (
	_ "embed"
	"strings"
	"sync"
	"unicode"
)

//go:embed hyph/en-us.tex
var englishPatterns string

// Hyphenator finds the points where a word may break at a line end, by
// Liang's pattern method as TeX does. Set one on TextOptions.Hyphenate
// and wrapped text breaks long words with a hyphen instead of leaving
// ragged gaps.
type Hyphenator struct {
	patterns   map[string][]uint8 // letters (with . for word edges) to odd/even scores between them
	exceptions map[string][]int   // whole words to their break positions
	maxLen     int
	// MinLeft and MinRight are the fewest letters left before and after a
	// break; TeX's defaults for English are 2 and 3.
	MinLeft, MinRight int
}

var (
	englishOnce sync.Once
	english     *Hyphenator
)

// EnglishHyphenator returns the shared American English hyphenator,
// built from the standard TeX patterns on first use.
func EnglishHyphenator() *Hyphenator {
	englishOnce.Do(func() { english = ParseTeXPatterns(englishPatterns) })
	return english
}

// ParseTeXPatterns reads a TeX hyphenation file: the \patterns{...}
// block and an optional \hyphenation{...} block of exceptions.
func ParseTeXPatterns(src string) *Hyphenator {
	var patterns, exceptions []string
	strip := func(block string) []string {
		var out []string
		for _, line := range strings.Split(block, "\n") {
			if i := strings.IndexByte(line, '%'); i >= 0 {
				line = line[:i]
			}
			out = append(out, strings.Fields(line)...)
		}
		return out
	}
	if i := strings.Index(src, `\patterns{`); i >= 0 {
		rest := src[i+len(`\patterns{`):]
		if j := strings.IndexByte(rest, '}'); j >= 0 {
			patterns = strip(rest[:j])
		}
	}
	if i := strings.Index(src, `\hyphenation{`); i >= 0 {
		rest := src[i+len(`\hyphenation{`):]
		if j := strings.IndexByte(rest, '}'); j >= 0 {
			exceptions = strip(rest[:j])
		}
	}
	return NewHyphenator(patterns, exceptions)
}

// NewHyphenator builds a hyphenator from Liang patterns ("hy3ph",
// ".ab1o") and exception words with their breaks marked ("ta-ble").
func NewHyphenator(patterns, exceptions []string) *Hyphenator {
	h := &Hyphenator{patterns: map[string][]uint8{}, exceptions: map[string][]int{}, MinLeft: 2, MinRight: 3}
	for _, p := range patterns {
		var letters []rune
		var scores []uint8
		scores = append(scores, 0)
		for _, r := range p {
			if r >= '0' && r <= '9' {
				scores[len(scores)-1] = uint8(r - '0')
				continue
			}
			letters = append(letters, r)
			scores = append(scores, 0)
		}
		if len(letters) == 0 {
			continue
		}
		key := string(letters)
		h.patterns[key] = scores
		h.maxLen = max(h.maxLen, len(letters))
	}
	for _, e := range exceptions {
		var word []rune
		var breaks []int
		for _, r := range e {
			if r == '-' {
				breaks = append(breaks, len(word))
				continue
			}
			word = append(word, unicode.ToLower(r))
		}
		h.exceptions[string(word)] = breaks
	}
	return h
}

// Hyphenate returns the rune offsets in word where it may break, in
// order, respecting MinLeft and MinRight.
func (h *Hyphenator) Hyphenate(word string) []int {
	if h == nil {
		return nil
	}
	lower := []rune(strings.ToLower(word))
	n := len(lower)
	if n < h.MinLeft+h.MinRight {
		return nil
	}
	if b, ok := h.exceptions[string(lower)]; ok {
		return b
	}
	padded := make([]rune, 0, n+2)
	padded = append(padded, '.')
	padded = append(padded, lower...)
	padded = append(padded, '.')
	scores := make([]uint8, len(padded)+1)
	for i := range padded {
		for l := 1; l <= h.maxLen && i+l <= len(padded); l++ {
			if s, ok := h.patterns[string(padded[i:i+l])]; ok {
				for k, v := range s {
					if v > scores[i+k] {
						scores[i+k] = v
					}
				}
			}
		}
	}
	var out []int
	for i := h.MinLeft; i <= n-h.MinRight; i++ {
		// scores[i+1] is the score before letter i of the unpadded word.
		if scores[i+1]%2 == 1 {
			out = append(out, i)
		}
	}
	return out
}

// SoftHyphens returns text with a soft hyphen (U+00AD) at every break
// point of every word of letters, which is how wrapping is told where
// a word may split; the marks are invisible unless a line ends on one.
func (h *Hyphenator) SoftHyphens(text string) string {
	if h == nil {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + len(text)/8)
	flush := func(word []rune) {
		if len(word) == 0 {
			return
		}
		breaks := h.Hyphenate(string(word))
		bi := 0
		for i, r := range word {
			if bi < len(breaks) && breaks[bi] == i {
				b.WriteRune('­')
				bi++
			}
			b.WriteRune(r)
		}
	}
	var word []rune
	for _, r := range text {
		if unicode.IsLetter(r) {
			word = append(word, r)
			continue
		}
		flush(word)
		word = word[:0]
		b.WriteRune(r)
	}
	flush(word)
	return b.String()
}
