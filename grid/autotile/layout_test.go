package autotile

import (
	"slices"
	"testing"
)

// Every layout's neighbour offsets, checked against the cells the Tiled
// editor treats as adjacent. The hexagonal cases differ between the
// shifted and unshifted rows or columns.
func TestLayoutNeighbours(t *testing.T) {
	cases := []struct {
		name   string
		layout Layout
		x, y   int
		want   map[int][2]int // direction to the neighbour's coordinates
	}{
		{"square", Square, 4, 4, map[int][2]int{
			DirN: {4, 3}, DirNE: {5, 3}, DirE: {5, 4}, DirSE: {5, 5},
			DirS: {4, 5}, DirSW: {3, 5}, DirW: {3, 4}, DirNW: {3, 3},
		}},
		{"iso", IsoDiamond, 4, 4, map[int][2]int{
			DirN: {3, 3}, DirNE: {4, 3}, DirE: {5, 3}, DirSE: {5, 4},
			DirS: {5, 5}, DirSW: {4, 5}, DirW: {3, 5}, DirNW: {3, 4},
		}},
		{"hex rows odd, even row", HexRowsOdd, 4, 4, map[int][2]int{
			DirNE: {4, 3}, DirE: {5, 4}, DirSE: {4, 5}, DirSW: {3, 5}, DirW: {3, 4}, DirNW: {3, 3},
		}},
		{"hex rows odd, odd row", HexRowsOdd, 4, 5, map[int][2]int{
			DirNE: {5, 4}, DirE: {5, 5}, DirSE: {5, 6}, DirSW: {4, 6}, DirW: {3, 5}, DirNW: {4, 4},
		}},
		{"hex rows even, even row", HexRowsEven, 4, 4, map[int][2]int{
			DirNE: {5, 3}, DirE: {5, 4}, DirSE: {5, 5}, DirSW: {4, 5}, DirW: {3, 4}, DirNW: {4, 3},
		}},
		{"hex cols odd, even column", HexColsOdd, 4, 4, map[int][2]int{
			DirN: {4, 3}, DirNE: {5, 3}, DirSE: {5, 4}, DirS: {4, 5}, DirSW: {3, 4}, DirNW: {3, 3},
		}},
		{"hex cols odd, odd column", HexColsOdd, 5, 4, map[int][2]int{
			DirN: {5, 3}, DirNE: {6, 4}, DirSE: {6, 5}, DirS: {5, 5}, DirSW: {4, 5}, DirNW: {4, 4},
		}},
		{"axial", HexAxial, 4, 4, map[int][2]int{
			DirNE: {5, 3}, DirE: {5, 4}, DirSE: {4, 5}, DirSW: {3, 5}, DirW: {3, 4}, DirNW: {4, 3},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, d := range c.layout.Dirs() {
				want, ok := c.want[d]
				if !ok {
					t.Fatalf("direction %d is used but the case does not list it", d)
				}
				if x, y := c.layout.Neighbour(c.x, c.y, d); [2]int{x, y} != want {
					t.Errorf("direction %d: (%d,%d), want %v", d, x, y, want)
				}
			}
			if len(c.want) != len(c.layout.Dirs()) {
				t.Errorf("%d directions used, %d listed", len(c.layout.Dirs()), len(c.want))
			}
			// Every step has a step back: the neighbour's opposite
			// direction returns to the cell.
			for _, d := range c.layout.Dirs() {
				nx, ny := c.layout.Neighbour(c.x, c.y, d)
				if bx, by := c.layout.Neighbour(nx, ny, (d+4)%8); bx != c.x || by != c.y {
					t.Errorf("direction %d does not come back: (%d,%d)", d, bx, by)
				}
			}
		})
	}
	// A direction the layout has no neighbour in stays where it is.
	if x, y := HexRowsOdd.Neighbour(2, 2, DirN); x != 2 || y != 2 {
		t.Errorf("north on a rows hexagon moved to (%d,%d)", x, y)
	}
	if !HexAxial.Hex() || Square.Hex() || IsoDiamond.Hex() {
		t.Error("Hex reports the wrong layouts")
	}
	if got := Square.Dirs(); !slices.Equal(got, []int{DirN, DirNE, DirE, DirSE, DirS, DirSW, DirW, DirNW}) {
		t.Errorf("square directions %v", got)
	}
	// Dirs hands out a copy, so a caller cannot corrupt the tables.
	d := HexRowsOdd.Dirs()
	d[0] = 99
	if HexRowsOdd.Dirs()[0] == 99 {
		t.Error("Dirs shares its slice")
	}
}

// A hexagon whose six neighbours are all the same terrain takes the full
// mask; each neighbour on its own sets its own bit.
func TestEdge64Hex(t *testing.T) {
	var frames [64]int
	for i := range frames {
		frames[i] = i // frame = mask, so results are legible
	}
	const w, h = 5, 5
	// A ring of terrain 1 around the middle cell of a rows layout, the
	// middle cell included.
	ring := map[[2]int]bool{{2, 2}: true}
	for _, d := range HexRowsOdd.Dirs() {
		x, y := HexRowsOdd.Neighbour(2, 2, d)
		ring[[2]int{x, y}] = true
	}
	terrain := func(x, y int) int {
		if ring[[2]int{x, y}] {
			return 1
		}
		return 0
	}
	m := Mapper{Rules: Edge64(1, frames), Layout: HexRowsOdd, OutsideFixed: true}
	got := map[[2]int]int{}
	m.Apply(w, h, terrain, func(x, y, f int) { got[[2]int{x, y}] = f })
	if got[[2]int{2, 2}] != 63 {
		t.Errorf("the middle of a full ring got frame %d, want 63", got[[2]int{2, 2}])
	}
	// Each ring cell touches the middle, so its frame has exactly the bit
	// of the direction pointing back at it, plus the bits of the ring
	// neighbours beside it.
	for i, d := range HexRowsOdd.Dirs() {
		x, y := HexRowsOdd.Neighbour(2, 2, d)
		back := slices.Index(HexRowsOdd.Dirs(), (d+4)%8)
		if got[[2]int{x, y}]&(1<<back) == 0 {
			t.Errorf("ring cell %d at (%d,%d) does not connect back: frame %d", i, x, y, got[[2]int{x, y}])
		}
	}
	// A lone hexagon connects to nothing.
	lone := func(x, y int) int {
		if x == 2 && y == 2 {
			return 1
		}
		return 0
	}
	got = map[[2]int]int{}
	m.Apply(w, h, lone, func(x, y, f int) { got[[2]int{x, y}] = f })
	if got[[2]int{2, 2}] != 0 {
		t.Errorf("a lone hexagon got frame %d, want 0", got[[2]int{2, 2}])
	}
	// One neighbour to the east sets the east bit alone.
	pair := func(x, y int) int {
		if y == 2 && (x == 2 || x == 3) {
			return 1
		}
		return 0
	}
	m.Apply(w, h, pair, func(x, y, f int) { got[[2]int{x, y}] = f })
	east := slices.Index(HexRowsOdd.Dirs(), DirE)
	if got[[2]int{2, 2}] != 1<<east {
		t.Errorf("a hexagon with an eastern neighbour got frame %d, want %d", got[[2]int{2, 2}], 1<<east)
	}
	// Edge64 under a square layout has masks its frames do not cover.
	sq := Mapper{Rules: Edge64(1, frames), OutsideFixed: true}
	sq.Apply(w, h, terrain, func(x, y, f int) { got[[2]int{x, y}] = f })
	if got[[2]int{2, 2}] != -1 {
		t.Errorf("Edge64 on a square grid gave frame %d, want -1", got[[2]int{2, 2}])
	}
}

// An isometric layout matches the same neighbourhood as a square one,
// turned a quarter turn: the cell an isometric map draws to the north is
// (x-1, y-1).
func TestIsoDiamondRotatesNeighbours(t *testing.T) {
	var frames [16]int
	for i := range frames {
		frames[i] = i
	}
	const w, h = 5, 5
	// Terrain 1 at the middle and at the cell drawn north of it.
	terrain := func(x, y int) int {
		if (x == 2 && y == 2) || (x == 1 && y == 1) {
			return 1
		}
		return 0
	}
	got := map[[2]int]int{}
	iso := Mapper{Rules: Edge16(1, frames), Layout: IsoDiamond, OutsideFixed: true}
	iso.Apply(w, h, terrain, func(x, y, f int) { got[[2]int{x, y}] = f })
	if got[[2]int{2, 2}] != 1 {
		t.Errorf("iso middle got frame %d, want the north bit", got[[2]int{2, 2}])
	}
	// The same terrain on a square layout connects nothing: (1,1) is the
	// square grid's north-west, which Edge16 does not match.
	sq := Mapper{Rules: Edge16(1, frames), OutsideFixed: true}
	sq.Apply(w, h, terrain, func(x, y, f int) { got[[2]int{x, y}] = f })
	if got[[2]int{2, 2}] != 0 {
		t.Errorf("square middle got frame %d, want no connection", got[[2]int{2, 2}])
	}
	// A blob mask rotates with the layout: the cell the square grid calls
	// north-west, the isometric one calls north.
	var blob [47]int
	for i := range blob {
		blob[i] = i
	}
	isoBlob := Mapper{Rules: Blob47(1, blob), Layout: IsoDiamond, OutsideFixed: true}
	isoBlob.Apply(w, h, terrain, func(x, y, f int) { got[[2]int{x, y}] = f })
	if got[[2]int{2, 2}] != BlobIndex(1<<DirN) {
		t.Errorf("iso blob got frame %d, want %d", got[[2]int{2, 2}], BlobIndex(1<<DirN))
	}
	// Cell patches the neighbourhood the layout gives, not the square
	// block: the isometric neighbours of (2,2) are its diagonals and the
	// cells two steps away in x and y.
	touched := map[[2]int]bool{}
	iso.Cell(2, 2, w, h, terrain, func(x, y, f int) { touched[[2]int{x, y}] = true })
	for _, want := range [][2]int{{2, 2}, {1, 1}, {3, 3}, {1, 3}, {3, 1}, {2, 1}, {2, 3}, {1, 2}, {3, 2}} {
		if !touched[want] {
			t.Errorf("Cell missed %v", want)
		}
	}
	if len(touched) != 9 {
		t.Errorf("Cell touched %d cells, want 9", len(touched))
	}
}

// A hexagonal Wang set matches the six sides whatever its type.
func TestWangHexPositions(t *testing.T) {
	const land, water = 1, 2
	// One tile per combination of water sides, frame = the mask.
	var tiles []WangTile
	dirs := HexRowsOdd.Dirs()
	for mask := range 64 {
		tile := WangTile{Frame: mask}
		for i, d := range dirs {
			tile.Colors[d] = land
			if mask&(1<<i) != 0 {
				tile.Colors[d] = water
			}
		}
		tiles = append(tiles, tile)
	}
	m := Mapper{Rules: Wang(WangEdges, tiles), Layout: HexRowsOdd, Outside: land, OutsideFixed: true}
	// Water east of the middle, land everywhere else.
	terrain := func(x, y int) int {
		if x == 3 && y == 2 {
			return water
		}
		return land
	}
	got := map[[2]int]int{}
	m.Apply(5, 5, terrain, func(x, y, f int) { got[[2]int{x, y}] = f })
	east := slices.Index(dirs, DirE)
	if got[[2]int{2, 2}] != 1<<east {
		t.Errorf("hex Wang cell got frame %d, want %d", got[[2]int{2, 2}], 1<<east)
	}
	// The water cell itself shows water on every side, because its own
	// colour wins at each of them.
	if got[[2]int{3, 2}] != 63 {
		t.Errorf("the water hexagon got frame %d, want 63", got[[2]int{3, 2}])
	}
}
