// Package autotile picks tile frames from a terrain grid, so a map of
// plain terrain ids draws with matching edges, corners and transitions.
// The game keeps one small int per cell; a Mapper turns that into frame
// indices for a tile sheet, whole maps at a time with Apply or one
// changed cell at a time with Cell. The package is pure logic: frames
// are ints for gfx.Tilemap or anything else, and -1 means no tile.
//
// Four rule kinds cover the usual tilesets. Edge16 matches the four
// edge neighbours and needs 16 tiles: walls, pipes, fences. Blob47
// matches all eight neighbours, reduced to the 47 distinct cases: the
// standard blob terrain set. Corner16 is the dual grid: each tile sits
// on a corner between four cells and needs 16 tiles. Wang matches
// terrain colours on tile edges, corners or both, for any number of
// terrains meeting with proper transitions; tilesets authored in the
// Tiled editor's terrain tool convert to it through the tiled package.
// ExpandBlob composes the 47 blob tiles from a six-tile template so an
// artist draws six tiles instead of 47.
//
// Directions follow Tiled's clockwise order from north: N, NE, E, SE,
// S, SW, W, NW. All zero values are usable defaults: a Mapper with only
// Rules set treats the map border as continuing each cell's terrain and
// varies tile variants with seed zero.
package autotile

import "sort"

// Directions, clockwise from north. They index WangTile.Colors and
// number the bits of a blob mask.
const (
	DirN = iota
	DirNE
	DirE
	DirSE
	DirS
	DirSW
	DirW
	DirNW
)

// dirOffset is the cell offset of each direction, y down.
var dirOffset = [8][2]int{{0, -1}, {1, -1}, {1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}}

type ruleKind int

const (
	kindEdge16 ruleKind = iota
	kindCorner16
	kindBlob47
	kindWang
)

type weighted struct {
	frame  int
	weight float64
}

// Rules maps a cell's neighbourhood to tile frames. Build one with
// Edge16, Corner16, Blob47 or Wang, then hand it to a Mapper.
type Rules struct {
	kind     ruleKind
	terrain  int
	friends  map[int]struct{}
	slots    [][]weighted
	wangType WangType
	wang     []WangTile
}

// Edge16 builds rules that match the four edge neighbours of cells with
// the given terrain id. frames is indexed by a 4-bit mask of connected
// neighbours: north 1, east 2, south 4, west 8. Frame -1 leaves a case
// empty.
func Edge16(terrain int, frames [16]int) *Rules {
	return maskRules(kindEdge16, terrain, frames[:])
}

// Corner16 builds dual-grid rules for cells with the given terrain id.
// Each output tile sits on a corner between four cells, so Apply and
// Cell emit a grid one wider and one taller than the map; draw that
// tilemap offset up and left by half a tile. frames is indexed by a
// 4-bit mask of matching cells around the corner: north-west 1,
// north-east 2, south-west 4, south-east 8.
func Corner16(terrain int, frames [16]int) *Rules {
	return maskRules(kindCorner16, terrain, frames[:])
}

// Blob47 builds rules that match all eight neighbours of cells with the
// given terrain id, reduced to the 47 distinct cases: a diagonal only
// matters when both edges beside it connect. frames is in canonical
// order, ascending by normalised mask; BlobIndex maps any raw mask to
// its place, and ExpandBlob emits tiles in this order.
func Blob47(terrain int, frames [47]int) *Rules {
	return maskRules(kindBlob47, terrain, frames[:])
}

func maskRules(kind ruleKind, terrain int, frames []int) *Rules {
	r := &Rules{kind: kind, terrain: terrain, slots: make([][]weighted, len(frames))}
	for i, f := range frames {
		if f >= 0 {
			r.slots[i] = []weighted{{frame: f, weight: 1}}
		}
	}
	return r
}

// Connect makes neighbours of other terrains count as connected, so
// grass rules join up against road cells without drawing their tiles.
// It returns the rules for chaining.
func (r *Rules) Connect(terrains ...int) *Rules {
	if r.friends == nil {
		r.friends = map[int]struct{}{}
	}
	for _, t := range terrains {
		r.friends[t] = struct{}{}
	}
	return r
}

// Variant adds an alternative frame for one neighbourhood, chosen at
// random by cell position with the given weight; the frame passed to
// the constructor keeps weight 1. mask is the scheme's mask: 4 bits for
// Edge16 and Corner16, the raw 8-bit mask for Blob47. A zero weight
// counts as 1. Wang rules take variants as extra tiles instead.
func (r *Rules) Variant(mask, frame int, weight float64) *Rules {
	if weight <= 0 {
		weight = 1
	}
	i := mask
	if r.kind == kindBlob47 {
		i = BlobIndex(uint8(mask))
	}
	if i >= 0 && i < len(r.slots) {
		r.slots[i] = append(r.slots[i], weighted{frame: frame, weight: weight})
	}
	return r
}

func (r *Rules) connected(neighbour, terrain int) bool {
	if neighbour == terrain {
		return true
	}
	_, ok := r.friends[neighbour]
	return ok
}

// Mapper applies one set of rules to a terrain grid. Rules is the only
// required field. The zero values of the rest mean: the border
// continues each cell's own terrain, and variants are seeded with zero.
type Mapper struct {
	Rules *Rules
	// Seed varies which variant each cell picks; the choice is a pure
	// function of Seed and the cell position, so reapplying is stable.
	Seed uint64
	// Outside is the terrain assumed beyond the map edge when
	// OutsideFixed is set. Unset, the outside continues whatever
	// terrain is being matched, so borders connect.
	Outside      int
	OutsideFixed bool

	scratch []weighted // reused by Wang matching to keep Apply allocation-free
}

// Apply computes a frame for every cell and hands each to set, with -1
// for cells the rules leave empty. terrain returns the id at a cell and
// is only called inside the map. Corner16 rules emit one extra row and
// column: x and y run to w and h inclusive.
func (m *Mapper) Apply(w, h int, terrain func(x, y int) int, set func(x, y, frame int)) {
	if m.Rules == nil {
		return
	}
	if m.Rules.kind == kindCorner16 {
		for y := 0; y <= h; y++ {
			for x := 0; x <= w; x++ {
				set(x, y, m.corner(x, y, w, h, terrain))
			}
		}
		return
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			set(x, y, m.frame(x, y, w, h, terrain))
		}
	}
}

// Cell recomputes the cells a change at (x, y) can affect: the cell and
// its eight neighbours, or the four surrounding corners for Corner16
// rules. Call it after editing one cell instead of reapplying the map.
func (m *Mapper) Cell(x, y, w, h int, terrain func(x, y int) int, set func(x, y, frame int)) {
	if m.Rules == nil {
		return
	}
	if m.Rules.kind == kindCorner16 {
		for cy := y; cy <= y+1; cy++ {
			for cx := x; cx <= x+1; cx++ {
				if cx >= 0 && cy >= 0 && cx <= w && cy <= h {
					set(cx, cy, m.corner(cx, cy, w, h, terrain))
				}
			}
		}
		return
	}
	for cy := y - 1; cy <= y+1; cy++ {
		for cx := x - 1; cx <= x+1; cx++ {
			if cx >= 0 && cy >= 0 && cx < w && cy < h {
				set(cx, cy, m.frame(cx, cy, w, h, terrain))
			}
		}
	}
}

// at reads a cell's terrain with the border policy applied; own is the
// terrain the caller is matching against.
func (m *Mapper) at(x, y, w, h, own int, terrain func(x, y int) int) int {
	if x < 0 || y < 0 || x >= w || y >= h {
		if m.OutsideFixed {
			return m.Outside
		}
		return own
	}
	return terrain(x, y)
}

// frame picks the frame for one cell under edge, blob or Wang rules.
func (m *Mapper) frame(x, y, w, h int, terrain func(x, y int) int) int {
	r := m.Rules
	t := terrain(x, y)
	if r.kind == kindWang {
		return m.wangFrame(x, y, w, h, t, terrain)
	}
	if t != r.terrain {
		return -1
	}
	mask := 0
	switch r.kind {
	case kindEdge16:
		for i, d := range []int{DirN, DirE, DirS, DirW} {
			o := dirOffset[d]
			if r.connected(m.at(x+o[0], y+o[1], w, h, t, terrain), t) {
				mask |= 1 << i
			}
		}
	case kindBlob47:
		raw := uint8(0)
		for d := range 8 {
			o := dirOffset[d]
			if r.connected(m.at(x+o[0], y+o[1], w, h, t, terrain), t) {
				raw |= 1 << d
			}
		}
		mask = BlobIndex(raw)
	}
	return m.pick(x, y, r.slots[mask])
}

// corner picks the frame for one dual-grid corner, whose tile shows the
// four cells that meet there.
func (m *Mapper) corner(x, y, w, h int, terrain func(x, y int) int) int {
	r := m.Rules
	t := r.terrain
	mask := 0
	// The corner at (x, y) touches cells (x-1, y-1) through (x, y);
	// the cell up-left of the corner fills the tile's north-west
	// quadrant.
	for i, o := range [4][2]int{{-1, -1}, {0, -1}, {-1, 0}, {0, 0}} {
		if r.connected(m.at(x+o[0], y+o[1], w, h, t, terrain), t) {
			mask |= 1 << i
		}
	}
	if mask == 0 {
		return -1
	}
	return m.pick(x, y, r.slots[mask])
}

// pick chooses among a slot's variants by a hash of the cell position,
// so the choice is stable across reapplies.
func (m *Mapper) pick(x, y int, slot []weighted) int {
	switch len(slot) {
	case 0:
		return -1
	case 1:
		return slot[0].frame
	}
	total := 0.0
	for _, v := range slot {
		total += v.weight
	}
	r := float64(hash(m.Seed, x, y)>>11) / float64(1<<53) * total
	for _, v := range slot {
		if r < v.weight {
			return v.frame
		}
		r -= v.weight
	}
	return slot[len(slot)-1].frame
}

// hash mixes the seed and a cell position, splitmix64 style.
func hash(seed uint64, x, y int) uint64 {
	z := seed ^ uint64(int64(x))*0x9e3779b97f4a7c15 ^ uint64(int64(y))*0xbf58476d1ce4e5b9
	z ^= z >> 30
	z *= 0xbf58476d1ce4e5b9
	z ^= z >> 27
	z *= 0x94d049bb133111eb
	return z ^ z>>31
}

// blobMasks lists the 47 distinct normalised 8-bit masks in ascending
// order; blobIndexTable maps every raw mask to its position.
var (
	blobMasks      []uint8
	blobIndexTable [256]uint8
)

func init() {
	seen := map[uint8]bool{}
	for m := 0; m < 256; m++ {
		n := normalizeBlob(uint8(m))
		if !seen[n] {
			seen[n] = true
			blobMasks = append(blobMasks, n)
		}
	}
	sort.Slice(blobMasks, func(i, j int) bool { return blobMasks[i] < blobMasks[j] })
	for m := 0; m < 256; m++ {
		n := normalizeBlob(uint8(m))
		blobIndexTable[m] = uint8(sort.Search(len(blobMasks), func(i int) bool { return blobMasks[i] >= n }))
	}
}

// normalizeBlob clears each diagonal bit unless both edges beside it
// are set, which is what makes only 47 of the 256 masks distinct.
func normalizeBlob(mask uint8) uint8 {
	for _, c := range [4][3]uint8{
		{1 << DirNE, 1 << DirN, 1 << DirE},
		{1 << DirSE, 1 << DirS, 1 << DirE},
		{1 << DirSW, 1 << DirS, 1 << DirW},
		{1 << DirNW, 1 << DirN, 1 << DirW},
	} {
		if mask&c[1] == 0 || mask&c[2] == 0 {
			mask &^= c[0]
		}
	}
	return mask
}

// BlobIndex maps a raw 8-bit neighbour mask (bit d set when the
// neighbour in direction d connects) to the canonical 0..46 index that
// Blob47 frames use.
func BlobIndex(mask uint8) int { return int(blobIndexTable[mask]) }

// BlobMasks returns the 47 canonical masks in frame order, for building
// or checking a sheet layout.
func BlobMasks() [47]uint8 {
	var out [47]uint8
	copy(out[:], blobMasks)
	return out
}
