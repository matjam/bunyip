package autotile

import "fmt"

// Layout is the shape of the grid a Mapper walks: which of the eight
// directions a cell has a neighbour in, and which cell that is. The zero
// value, Square, is a square grid with four edge neighbours and four
// diagonals.
//
// A hexagonal layout has six neighbours and no diagonals. The rows
// layouts hold pointy-top hexagons in staggered rows, which is Tiled's
// stagger axis Y: every cell has an east and a west neighbour, and the
// four remaining sides run to the rows above and below. The columns
// layouts hold flat-top hexagons in staggered columns, Tiled's stagger
// axis X: every cell has a north and a south neighbour. Odd and even
// say which rows or columns are shifted, matching Tiled's stagger index.
//
// IsoDiamond is a square grid of diamond tiles drawn rotated a quarter
// turn, so the direction names are the tile's directions on screen: the
// cell north of (x, y) is (x-1, y-1), and the cell that shares the
// diamond's upper-right edge is (x, y-1).
type Layout int

const (
	Square      Layout = iota // a square grid: four edges and four diagonals
	HexRowsOdd                // hexagons in staggered rows, the odd rows shifted right
	HexRowsEven               // hexagons in staggered rows, the even rows shifted right
	HexColsOdd                // hexagons in staggered columns, the odd columns shifted down
	HexColsEven               // hexagons in staggered columns, the even columns shifted down
	// HexAxial holds hexagons in axial coordinates, where x runs east and
	// y runs south-east, so the offsets are the same for every cell. It
	// has the same six directions as the rows layouts.
	HexAxial
	IsoDiamond // a square grid of diamond tiles, the directions rotated a quarter turn
)

// Neighbour returns the cell one step from (x, y) in a direction. A
// direction the layout has no neighbour in, such as north on a rows
// hexagon, returns the cell itself; Dirs lists the directions a layout
// uses.
func (l Layout) Neighbour(x, y, dir int) (int, int) {
	if dir < 0 || dir >= 8 {
		return x, y
	}
	var o [2]int
	switch l {
	case HexRowsOdd, HexRowsEven:
		o = hexRowOffset[boolIndex((y&1 == 1) == (l == HexRowsOdd))][dir]
	case HexColsOdd, HexColsEven:
		o = hexColOffset[boolIndex((x&1 == 1) == (l == HexColsOdd))][dir]
	case HexAxial:
		o = axialOffset[dir]
	case IsoDiamond:
		o = isoOffset[dir]
	default:
		o = dirOffset[dir]
	}
	return x + o[0], y + o[1]
}

// Dirs lists the directions the layout has neighbours in, clockwise. A
// square or isometric layout lists all eight; a hexagonal one lists its
// six. The result is a fresh slice the caller may keep.
func (l Layout) Dirs() []int { return append([]int(nil), l.dirs()...) }

// Hex reports whether the layout is hexagonal, so a cell has six
// neighbours and no diagonals.
func (l Layout) Hex() bool {
	switch l {
	case HexRowsOdd, HexRowsEven, HexColsOdd, HexColsEven, HexAxial:
		return true
	}
	return false
}

// String names the layout.
func (l Layout) String() string {
	names := [...]string{"square", "hex rows odd", "hex rows even", "hex cols odd", "hex cols even", "hex axial", "iso diamond"}
	if int(l) < len(names) {
		return names[l]
	}
	return fmt.Sprintf("Layout(%d)", int(l))
}

// dirs is Dirs without the copy, for the mapper's inner loops.
func (l Layout) dirs() []int {
	switch l {
	case HexRowsOdd, HexRowsEven, HexAxial:
		return hexRowDirs[:]
	case HexColsOdd, HexColsEven:
		return hexColDirs[:]
	}
	return squareDirs[:]
}

var (
	squareDirs = [8]int{DirN, DirNE, DirE, DirSE, DirS, DirSW, DirW, DirNW}
	hexRowDirs = [6]int{DirNE, DirE, DirSE, DirSW, DirW, DirNW}
	hexColDirs = [6]int{DirN, DirNE, DirSE, DirS, DirSW, DirNW}
)

// The offsets of each layout, by direction. A direction the layout does
// not use stays zero, which Neighbour reports as the cell itself.
var (
	// Rows of pointy-top hexagons: the unshifted rows first, then the
	// rows pushed half a tile to the right.
	hexRowOffset = [2][8][2]int{
		{DirNE: {0, -1}, DirE: {1, 0}, DirSE: {0, 1}, DirSW: {-1, 1}, DirW: {-1, 0}, DirNW: {-1, -1}},
		{DirNE: {1, -1}, DirE: {1, 0}, DirSE: {1, 1}, DirSW: {0, 1}, DirW: {-1, 0}, DirNW: {0, -1}},
	}
	// Columns of flat-top hexagons: the unshifted columns first, then the
	// columns pushed half a tile down.
	hexColOffset = [2][8][2]int{
		{DirN: {0, -1}, DirNE: {1, -1}, DirSE: {1, 0}, DirS: {0, 1}, DirSW: {-1, 0}, DirNW: {-1, -1}},
		{DirN: {0, -1}, DirNE: {1, 0}, DirSE: {1, 1}, DirS: {0, 1}, DirSW: {-1, 1}, DirNW: {-1, 0}},
	}
	// Axial coordinates: x east, y south-east.
	axialOffset = [8][2]int{DirNE: {1, -1}, DirE: {1, 0}, DirSE: {0, 1}, DirSW: {-1, 1}, DirW: {-1, 0}, DirNW: {0, -1}}
	// A diamond grid: the square offsets turned a quarter turn, so north
	// on screen is up and left in the grid.
	isoOffset = [8][2]int{
		DirN: {-1, -1}, DirNE: {0, -1}, DirE: {1, -1}, DirSE: {1, 0},
		DirS: {1, 1}, DirSW: {0, 1}, DirW: {-1, 1}, DirNW: {-1, 0},
	}
)

func boolIndex(b bool) int {
	if b {
		return 1
	}
	return 0
}
