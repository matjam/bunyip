package gfx

import (
	"unicode/utf8"

	"github.com/go-text/typesetting/shaping"
)

// textCacheEntries is how many entries a text cache holds in its current
// generation before that generation is retired.
const textCacheEntries = 2048

// textBlockGlyphs is the room a generation of the glyph cache holds,
// counted in glyphs rather than entries because a block is anything from
// a two-word label to a paragraph. Two generations of it are about seven
// megabytes.
const textBlockGlyphs = 64 * 1024

// genCache is a map that evicts a generation at a time. Entries go into
// the current generation; when it fills, it becomes the previous one and
// a fresh generation starts, and a hit in the previous generation is
// promoted back into the current. Text drawn every frame therefore stays
// resident however many one-off strings pass through, which clearing the
// whole map did not: a counter that changes every frame used to evict
// every label with it.
type genCache[K comparable, V any] struct {
	cur, prev map[K]V
	// weigh reports the room a value takes, for a cache whose values vary
	// in size; nil counts every entry as one.
	weigh  func(V) int
	limit  int // room in a generation; zero means textCacheEntries
	filled int // room the current generation has taken
}

// get returns an entry, promoting it out of the previous generation so
// that using it keeps it.
func (c *genCache[K, V]) get(k K) (V, bool) {
	if v, ok := c.cur[k]; ok {
		return v, true
	}
	v, ok := c.prev[k]
	if ok {
		c.put(k, v)
	}
	return v, ok
}

// put stores an entry, retiring the current generation when it is full.
func (c *genCache[K, V]) put(k K, v V) {
	limit := c.limit
	if limit <= 0 {
		limit = textCacheEntries
	}
	if c.cur == nil {
		c.cur = make(map[K]V)
	} else if c.filled >= limit {
		c.prev, c.cur, c.filled = c.cur, make(map[K]V), 0
	}
	room := 1
	if c.weigh != nil {
		room = c.weigh(v)
	}
	c.cur[k] = v
	c.filled += room
}

// runeIndex maps rune indices in one string to byte indices. Shaped
// glyphs carry rune indices and Glyph.Index reports bytes, so the table
// is built once per string and reused for every glyph; walking the string
// from the start for each glyph is quadratic in the length of a
// paragraph. An all-ASCII string needs no table because the two indices
// are equal.
type runeIndex struct {
	text  string
	built bool
	ascii bool
	offs  []int32 // byte offset of each rune; empty when ascii
}

// reset points the index at text, rebuilding the table only when the text
// is not the one it already holds.
func (ri *runeIndex) reset(text string) {
	if ri.built && ri.text == text {
		return
	}
	ri.text, ri.built = text, true
	ri.offs = ri.offs[:0]
	if ri.ascii = len(text) == utf8.RuneCountInString(text); ri.ascii {
		return
	}
	for b := range text {
		ri.offs = append(ri.offs, int32(b))
	}
}

// at returns the byte index of a rune index, or the length of the string
// when the rune index is past its end.
func (ri *runeIndex) at(i int) int {
	if ri.ascii {
		if i < 0 || i >= len(ri.text) {
			return len(ri.text)
		}
		return i
	}
	if i < 0 || i >= len(ri.offs) {
		return len(ri.text)
	}
	return int(ri.offs[i])
}

// isSpace reports whether the rune at a rune index is a plain space.
func (ri *runeIndex) isSpace(i int) bool {
	b := ri.at(i)
	return b < len(ri.text) && ri.text[b] == ' '
}

// textScratch is the working storage text layout reuses between calls, so
// that drawing a block of text allocates nothing once its glyphs are
// cached. It is used within a single layout call and never escapes.
type textScratch struct {
	lines   []shaping.Line
	paras   []string // the shaped text behind each line
	last    []bool   // whether a line ends its paragraph
	glyphs  []Glyph
	ordered []*shaping.Output
	index   runeIndex
}

// blockKey identifies a laid-out block of text in the glyph cache. It
// leaves out what does not move a glyph relative to the block's origin:
// the position, the colour and the angle.
type blockKey struct {
	text          string
	lang          string
	hyph          *Hyphenator
	width         float32
	size          float32
	lineSpacing   float32
	letterSpacing float32
	dir           Direction
	align         Align
	baseline      bool
}

// measureKey identifies a measured block of text. Alignment and the
// baseline flag do not change its size, so they are left out.
type measureKey struct {
	text          string
	lang          string
	hyph          *Hyphenator
	width         float32
	size          float32
	lineSpacing   float32
	letterSpacing float32
	dir           Direction
}
