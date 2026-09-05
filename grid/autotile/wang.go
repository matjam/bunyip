package autotile

// WangType says which positions of a Wang tile carry terrain colours.
type WangType int

const (
	// WangCorners matches colours on the four diagonal positions; the
	// usual form for overlapping terrains.
	WangCorners WangType = iota
	// WangEdges matches colours on the four edge positions; the usual
	// form for paths and pipes.
	WangEdges
	// WangFull matches all eight positions.
	WangFull
)

// WangTile is one tile of a Wang set: its frame and the terrain colour
// at each of the eight positions, in direction order (DirN through
// DirNW). Colour zero means the position is empty. Tiles with equal
// colours are variants; Weight biases the choice and zero counts as 1.
type WangTile struct {
	Frame  int
	Colors [8]int
	Weight float64
}

// Wang builds rules from a Wang tile set. Cells hold terrain colours,
// zero for empty. For each cell the mapper works out the colour every
// relevant position should show, then places the tile matching the most
// positions; a complete set always matches exactly, an incomplete one
// falls back to the closest tile. Where two terrains meet at a corner
// or an edge, the higher colour wins, so higher colours overlap lower
// ones. A cell whose own and surrounding colours are all zero gets -1.
//
// A hexagonal layout has six sides and no diagonals, so every type
// matches those six positions, in the direction slots the layout uses.
// t must be WangCorners, WangEdges or WangFull. The tiles slice is retained,
// not copied; edits affect later mapping. Use finite weights; nonpositive
// weights count as 1. An empty set maps every cell to -1.
func Wang(t WangType, tiles []WangTile) *Rules {
	return &Rules{kind: kindWang, wangType: t, wang: tiles}
}

// wangPositions is the set of positions each type matches on a square
// or isometric layout.
var wangPositions = [3][]int{
	WangCorners: {DirNE, DirSE, DirSW, DirNW},
	WangEdges:   {DirN, DirE, DirS, DirW},
	WangFull:    {DirN, DirNE, DirE, DirSE, DirS, DirSW, DirW, DirNW},
}

// positions lists the positions the rules match under the mapper's
// layout.
func (m *Mapper) positions() []int {
	if m.Layout.Hex() {
		return m.Layout.dirs()
	}
	return wangPositions[m.Rules.wangType]
}

// wangFrame picks the tile for one cell: compute the wanted colour at
// each relevant position, score every tile by matches, and choose among
// the best by weight.
func (m *Mapper) wangFrame(x, y, w, h, t int, terrain func(x, y int) int) int {
	var want [8]int
	positions := m.positions()
	empty := t == 0
	for _, p := range positions {
		if p%2 == 1 && !m.Layout.Hex() {
			// A diagonal position is a corner, shared with the two cells
			// beside it and the one across it.
			c := 0
			for _, d := range [3]int{(p + 7) % 8, p, (p + 1) % 8} {
				c = max(c, m.look(x, y, w, h, t, d, terrain))
			}
			want[p] = max(c, t)
		} else {
			want[p] = max(t, m.look(x, y, w, h, t, p, terrain))
		}
		if want[p] != 0 {
			empty = false
		}
	}
	if empty {
		return -1
	}
	best, score := m.scratch[:0], -1
	for i := range m.Rules.wang {
		tile := &m.Rules.wang[i]
		s := 0
		for _, p := range positions {
			if tile.Colors[p] == want[p] {
				s++
			}
		}
		if s > score {
			score, best = s, append(best[:0], weightedTile(tile))
		} else if s == score {
			best = append(best, weightedTile(tile))
		}
	}
	m.scratch = best
	return m.pick(x, y, best)
}

func weightedTile(t *WangTile) weighted {
	w := t.Weight
	if w <= 0 {
		w = 1
	}
	return weighted{frame: t.Frame, weight: w}
}
