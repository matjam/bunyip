package gfx

import (
	"unicode/utf8"

	"github.com/go-text/typesetting/shaping"
)

// textCacheEntries is the room a text cache's generation starts with.
const textCacheEntries = 2048

// textBlockGlyphs is the room a generation of the layout cache starts
// with, counted in glyphs and carets rather than entries because a layout
// is anything from a two-word label to a paragraph.
const textBlockGlyphs = 64 * 1024

// genCache is a map that evicts a generation at a time. Entries go into
// the current generation; when it fills, it becomes the previous one and
// a fresh generation starts, and a hit in the previous generation is
// promoted back into the current. Text drawn every frame therefore stays
// resident however many one-off strings pass through.
//
// Generations are stamped with the frame they began in. A generation is
// only retired once it has been filling for two whole frames, so it holds
// everything the last frame used; a frame that uses more than a
// generation holds grows the limit instead of retiring what it has just
// used. The limit falls back to its starting room at the next retirement,
// so a burst of text does not keep the cache large.
//
// With admit set, an entry is only stored the second time its key is
// put, so a string seen once (a counter that changes every frame) is
// never stored and cannot push out what is drawn every frame.
type genCache[K comparable, V any] struct {
	cur, prev map[K]V
	// weigh reports the room a value takes, for a cache whose values vary
	// in size; nil counts every entry as one.
	weigh  func(V) int
	base   int    // room a generation starts with; zero means textCacheEntries
	limit  int    // room the current generation may take before retiring
	filled int    // room the current generation has taken
	born   uint64 // frame the current generation began in
	admit  bool
	seen   *genCache[K, struct{}] // keys put once, when admit is set
}

// get returns an entry, promoting it out of the previous generation so
// that using it keeps it. now is the current frame number.
func (c *genCache[K, V]) get(k K, now uint64) (V, bool) {
	if v, ok := c.cur[k]; ok {
		return v, true
	}
	v, ok := c.prev[k]
	if ok {
		c.store(k, v, now)
	}
	return v, ok
}

// put stores an entry, or with admit set notes its key the first time.
func (c *genCache[K, V]) put(k K, v V, now uint64) {
	if c.admit {
		if c.seen == nil {
			c.seen = &genCache[K, struct{}]{}
		}
		if _, ok := c.seen.get(k, now); !ok {
			c.seen.store(k, struct{}{}, now)
			return
		}
	}
	c.store(k, v, now)
}

// store puts an entry into the current generation, retiring it first
// when it is full and old enough, or growing its limit when it is not.
func (c *genCache[K, V]) store(k K, v V, now uint64) {
	base := c.base
	if base <= 0 {
		base = textCacheEntries
	}
	switch {
	case c.cur == nil:
		c.cur, c.limit, c.born = make(map[K]V), base, now
	case c.filled >= c.limit:
		if now > c.born+1 {
			// Everything used since the generation began, which covers the
			// whole of the last frame, moves to the previous generation.
			c.prev, c.cur, c.filled = c.cur, make(map[K]V), 0
			c.limit, c.born = base, now
		} else {
			c.limit *= 2
		}
	}
	room := 1
	if c.weigh != nil {
		room = c.weigh(v)
	}
	c.cur[k] = v
	c.filled += room
}

// drop empties the cache.
func (c *genCache[K, V]) drop() {
	c.cur, c.prev, c.filled, c.limit = nil, nil, 0, 0
	if c.seen != nil {
		c.seen.drop()
	}
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

// textScratch is the working storage Shape reuses between calls. It is
// used within a single call and never escapes.
type textScratch struct {
	ordered []*shaping.Output
	index   runeIndex
}
