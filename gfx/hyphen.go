package gfx

import (
	"embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

//go:embed hyph/*.tex
var hyphFS embed.FS

// hyphFiles maps the language tags the engine ships TeX patterns for to
// their files under hyph/. A tag with a region ("en-gb") is listed where
// the patterns differ from the plain language's.
var hyphFiles = map[string]string{
	"da":    "da.tex",
	"de":    "de-1996.tex",
	"en":    "en-us.tex",
	"en-gb": "en-gb.tex",
	"en-us": "en-us.tex",
	"es":    "es.tex",
	"fi":    "fi.tex",
	"fr":    "fr.tex",
	"it":    "it.tex",
	"nb":    "nb.tex",
	"nl":    "nl.tex",
	"no":    "no.tex",
	"pl":    "pl.tex",
	"pt":    "pt.tex",
	"ru":    "ru.tex",
	"sv":    "sv.tex",
}

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
	hyphMu    sync.Mutex
	hyphCache = map[string]*Hyphenator{}
)

// EnglishHyphenator returns the shared American English hyphenator,
// built from the standard TeX patterns on first use.
func EnglishHyphenator() *Hyphenator {
	h, _ := HyphenatorFor("en-us")
	return h
}

// HyphenatorFor returns the shared hyphenator for a BCP 47 language tag,
// built from the TeX patterns the engine ships on first use. A tag with
// no patterns of its own falls back to its primary language, so "de-AT"
// gives the German hyphenator and "en-AU" the American English one;
// "en-GB" has patterns of its own. Languages the engine ships no
// patterns for return an error, and the shipped set is listed in
// gfx/hyph/README.md. The hyphenator is shared, so treat MinLeft and
// MinRight as read-only.
func HyphenatorFor(lang string) (*Hyphenator, error) {
	tag := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(lang), "_", "-"))
	name, ok := hyphFiles[tag]
	if !ok {
		if i := strings.IndexByte(tag, '-'); i > 0 {
			name, ok = hyphFiles[tag[:i]]
		}
	}
	if !ok {
		return nil, fmt.Errorf("gfx: no hyphenation patterns for %q", lang)
	}
	hyphMu.Lock()
	defer hyphMu.Unlock()
	if h, ok := hyphCache[name]; ok {
		return h, nil
	}
	src, err := readPatternFile(name, 0)
	if err != nil {
		return nil, err
	}
	h := ParseTeXPatterns(src)
	hyphCache[name] = h
	return h, nil
}

// readPatternFile reads a shipped pattern file and the files it inputs,
// returning the inputs first so that the file's own exceptions win.
func readPatternFile(name string, depth int) (string, error) {
	if depth > 4 {
		return "", fmt.Errorf("gfx: hyphenation patterns %q input too deeply", name)
	}
	data, err := hyphFS.ReadFile("hyph/" + name)
	if err != nil {
		return "", fmt.Errorf("gfx: read hyphenation patterns: %w", err)
	}
	src := string(data)
	var inputs strings.Builder
	for line := range strings.SplitSeq(src, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `\input`) {
			continue
		}
		// The shipped files drop the "hyph-" prefix TeX distributions use.
		in := strings.TrimSpace(strings.TrimPrefix(line, `\input`))
		in = strings.TrimPrefix(in, "hyph-")
		if !strings.HasSuffix(in, ".tex") {
			in += ".tex"
		}
		text, err := readPatternFile(in, depth+1)
		if err != nil {
			return "", err
		}
		inputs.WriteString(text)
		inputs.WriteByte('\n')
	}
	return inputs.String() + src, nil
}

// ParseTeXPatterns reads a TeX hyphenation file: every \patterns{...}
// block and every \hyphenation{...} block of exceptions. A hyphenmins
// comment in the file's header, which the hyph-utf8 pattern files carry,
// sets MinLeft and MinRight from its typesetting values; without one
// they are TeX's 2 and 3.
func ParseTeXPatterns(src string) *Hyphenator {
	left, right, hasMins := texHyphenmins(src)
	// Comments are stripped whole so that a block's braces and the words
	// inside it are read from the code alone.
	var code strings.Builder
	code.Grow(len(src))
	for line := range strings.SplitSeq(src, "\n") {
		if i := strings.IndexByte(line, '%'); i >= 0 {
			line = line[:i]
		}
		code.WriteString(line)
		code.WriteByte('\n')
	}
	words := func(name string) []string {
		var out []string
		rest := code.String()
		for {
			i := strings.Index(rest, name)
			if i < 0 {
				return out
			}
			rest = rest[i+len(name):]
			j := strings.IndexByte(rest, '}')
			if j < 0 {
				return out
			}
			out = append(out, strings.Fields(rest[:j])...)
			rest = rest[j+1:]
		}
	}
	h := NewHyphenator(words(`\patterns{`), words(`\hyphenation{`))
	if hasMins {
		h.MinLeft, h.MinRight = left, right
	}
	return h
}

// texHyphenmins reads the typesetting hyphenmins from the YAML comment
// header of a hyph-utf8 pattern file.
func texHyphenmins(src string) (left, right int, ok bool) {
	inMins, inTypeset := false, false
	for line := range strings.SplitSeq(src, "\n") {
		if !strings.HasPrefix(line, "%") {
			continue
		}
		// The header is YAML behind "% ", so the single space after the
		// per cent sign is not indentation.
		body := strings.TrimPrefix(strings.TrimRight(line[1:], " \t\r"), " ")
		key := strings.TrimSpace(body)
		indented := strings.HasPrefix(body, " ") || strings.HasPrefix(body, "\t")
		if !indented {
			if inMins {
				return left, right, left > 0 && right > 0
			}
			inMins = key == "hyphenmins:"
			continue
		}
		if !inMins {
			continue
		}
		switch {
		case strings.HasSuffix(key, ":"):
			inTypeset = key == "typesetting:"
		case inTypeset && strings.HasPrefix(key, "left:"):
			left, _ = strconv.Atoi(strings.TrimSpace(key[len("left:"):]))
		case inTypeset && strings.HasPrefix(key, "right:"):
			right, _ = strconv.Atoi(strings.TrimSpace(key[len("right:"):]))
		}
	}
	return left, right, left > 0 && right > 0
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
