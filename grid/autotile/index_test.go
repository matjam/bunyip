package autotile

import (
	"math/rand/v2"
	"testing"
)

// scanWangFrame is the Wang matcher as it was before the index: work
// out the wanted colours, score every tile and choose among the best by
// weight. The indexed matcher must agree with it on every cell.
func scanWangFrame(m *Mapper, x, y, w, h int, terrain func(x, y int) int) int {
	t := terrain(x, y)
	var want [8]int
	positions := m.positions()
	empty := t == 0
	for _, p := range positions {
		if p%2 == 1 && !m.Layout.Hex() {
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
	var best []weighted
	score := -1
	for i := range m.Rules.wang {
		tile := &m.Rules.wang[i]
		s := 0
		for _, p := range positions {
			if tile.Colors[p] == want[p] {
				s++
			}
		}
		if s > score {
			score, best = s, []weighted{weightedTile(tile)}
		} else if s == score {
			best = append(best, weightedTile(tile))
		}
	}
	return m.pick(x, y, best)
}

var allLayouts = []Layout{Square, HexRowsOdd, HexRowsEven, HexColsOdd, HexColsEven, HexAxial, IsoDiamond}

// randomWangSet builds a set over colours 1..colors: some complete, some
// with tiles missing, with duplicated tiles as weighted variants
// (including zero and negative weights) and colours on positions the
// type does not match.
func randomWangSet(rng *rand.Rand, t WangType, colors int) []WangTile {
	full := allWang(t, colors+1) // colours 0..colors, 0 being empty
	keep := rng.Float64()
	var tiles []WangTile
	for _, tile := range full {
		if rng.Float64() > keep {
			continue
		}
		tile.Frame = rng.IntN(1000)
		// Noise on the unmatched positions must not change matching.
		for p := range 8 {
			if tile.Colors[p] == 0 && rng.IntN(4) == 0 {
				tile.Colors[p] = rng.IntN(colors + 1)
			}
		}
		switch rng.IntN(4) {
		case 0:
			tile.Weight = 0
		case 1:
			tile.Weight = -1
		default:
			tile.Weight = rng.Float64() * 3
		}
		tiles = append(tiles, tile)
		for rng.IntN(3) == 0 {
			v := tile
			v.Frame = rng.IntN(1000)
			v.Weight = rng.Float64() * 2
			tiles = append(tiles, v)
		}
	}
	rng.Shuffle(len(tiles), func(i, j int) { tiles[i], tiles[j] = tiles[j], tiles[i] })
	return tiles
}

func randomTerrain(rng *rand.Rand, w, h, colors int) []int {
	cells := make([]int, w*h)
	for i := range cells {
		cells[i] = rng.IntN(colors + 1)
	}
	return cells
}

func TestWangIndexMatchesScan(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := range 300 {
		wt := WangType(rng.IntN(3))
		colors := 1 + rng.IntN(3)
		if wt == WangFull && colors > 2 {
			colors = 2
		}
		tiles := randomWangSet(rng, wt, colors)
		m := Mapper{
			Rules:        Wang(wt, tiles),
			Layout:       allLayouts[rng.IntN(len(allLayouts))],
			Seed:         rng.Uint64(),
			OutsideFixed: rng.IntN(2) == 0,
			Outside:      rng.IntN(colors + 1),
		}
		w, h := 1+rng.IntN(14), 1+rng.IntN(14)
		cells := randomTerrain(rng, w, h, colors)
		terrain := func(x, y int) int { return cells[y*w+x] }
		m.Apply(w, h, terrain, func(x, y, f int) {
			if want := scanWangFrame(&m, x, y, w, h, terrain); f != want {
				t.Fatalf("trial %d (%v, %v, %d tiles): Apply cell %d,%d frame %d, scan %d",
					trial, wt, m.Layout, len(tiles), x, y, f, want)
			}
		})
		cx, cy := rng.IntN(w), rng.IntN(h)
		m.Cell(cx, cy, w, h, terrain, func(x, y, f int) {
			if want := scanWangFrame(&m, x, y, w, h, terrain); f != want {
				t.Fatalf("trial %d: Cell cell %d,%d frame %d, scan %d", trial, x, y, f, want)
			}
		})
	}
}

// TestWangIndexFullSets checks the complete sets the benchmarks use,
// where every cell takes the indexed path.
func TestWangIndexFullSets(t *testing.T) {
	for _, tc := range []struct {
		wt     WangType
		colors int
	}{{WangCorners, 2}, {WangCorners, 4}, {WangEdges, 3}, {WangFull, 3}} {
		g := benchTerrain(tc.colors)
		const w, h = 48, 40
		terrain := func(x, y int) int { return g.At(x, y) }
		m := Mapper{Rules: Wang(tc.wt, allWang(tc.wt, tc.colors)), Seed: 9}
		m.Apply(w, h, terrain, func(x, y, f int) {
			if want := scanWangFrame(&m, x, y, w, h, terrain); f != want {
				t.Fatalf("%v %d colours: cell %d,%d frame %d, scan %d", tc.wt, tc.colors, x, y, f, want)
			}
		})
	}
}

// TestWangIndexSeesEdits checks that edits to the retained tile slice
// reach the next call, as the Wang documentation promises.
func TestWangIndexSeesEdits(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	tiles := allWang(WangCorners, 3)
	m := Mapper{Rules: Wang(WangCorners, tiles)}
	const w, h = 12, 12
	cells := randomTerrain(rng, w, h, 2)
	terrain := func(x, y int) int { return cells[y*w+x] }
	check := func(step string) {
		t.Helper()
		m.Apply(w, h, terrain, func(x, y, f int) {
			if want := scanWangFrame(&m, x, y, w, h, terrain); f != want {
				t.Fatalf("%s: cell %d,%d frame %d, scan %d", step, x, y, f, want)
			}
		})
	}
	check("initial")
	for i := range tiles {
		tiles[i].Frame += 100
	}
	check("frames changed")
	tiles[5].Colors[DirNE], tiles[6].Colors[DirNE] = tiles[6].Colors[DirNE], tiles[5].Colors[DirNE]
	check("colours swapped")
	tiles[7].Weight = 5
	tiles = append(tiles, tiles[7])
	m.Rules.wang = tiles
	check("variant added")
	m.Rules = Wang(WangEdges, allWang(WangEdges, 3))
	check("rules replaced")
	m.Layout = HexAxial
	check("layout changed")
}

// cellsFromCell runs Cell for every cell of a rectangle and records the
// last frame set for each cell.
func cellsFromCell(m *Mapper, x, y, rw, rh, w, h int, terrain func(x, y int) int) map[[2]int]int {
	got := map[[2]int]int{}
	for ey := y; ey < y+rh; ey++ {
		for ex := x; ex < x+rw; ex++ {
			m.Cell(ex, ey, w, h, terrain, func(cx, cy, f int) { got[[2]int{cx, cy}] = f })
		}
	}
	return got
}

func TestRegionMatchesCells(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	var blob [47]int
	for i := range blob {
		blob[i] = i
	}
	var e16 [16]int
	var e64 [64]int
	for i := range e16 {
		e16[i] = i
	}
	for i := range e64 {
		e64[i] = i
	}
	rules := func() *Rules {
		switch rng.IntN(6) {
		case 0:
			return Edge16(1, e16)
		case 1:
			return Edge64(1, e64)
		case 2:
			return Corner16(1, e16).Variant(15, 99, 2)
		case 3:
			return Blob47(1, blob).Connect(2)
		case 4:
			return Wang(WangCorners, randomWangSet(rng, WangCorners, 2))
		}
		return Wang(WangEdges, allWang(WangEdges, 3))
	}
	for trial := range 500 {
		m := Mapper{Rules: rules(), Layout: allLayouts[rng.IntN(len(allLayouts))], Seed: rng.Uint64(),
			OutsideFixed: rng.IntN(2) == 0, Outside: rng.IntN(3)}
		w, h := 1+rng.IntN(12), 1+rng.IntN(12)
		cells := randomTerrain(rng, w, h, 2)
		terrain := func(x, y int) int { return cells[y*w+x] }
		x, y := rng.IntN(w+6)-3, rng.IntN(h+6)-3
		rw, rh := rng.IntN(8), rng.IntN(8)
		want := cellsFromCell(&m, x, y, rw, rh, w, h, terrain)
		got := map[[2]int]int{}
		m.Region(x, y, rw, rh, w, h, terrain, func(cx, cy, f int) {
			k := [2]int{cx, cy}
			if _, dup := got[k]; dup {
				t.Fatalf("trial %d: Region set %v twice", trial, k)
			}
			got[k] = f
		})
		if len(got) != len(want) {
			t.Fatalf("trial %d (%v, rect %d,%d %dx%d on %dx%d): Region set %d cells, Cell set %d",
				trial, m.Layout, x, y, rw, rh, w, h, len(got), len(want))
		}
		for k, v := range want {
			if g, ok := got[k]; !ok || g != v {
				t.Fatalf("trial %d: cell %v: Region %d (set %v), Cell %d", trial, k, g, ok, v)
			}
		}
	}
}

// TestRegionComputesOnce checks the saving Region exists for: a 16 by
// 16 edit computes the 18 by 18 affected cells once each.
func TestRegionComputesOnce(t *testing.T) {
	var blob [47]int
	m := Mapper{Rules: Blob47(1, blob)}
	calls := 0
	m.Region(100, 100, 16, 16, 256, 256, func(x, y int) int { return 1 }, func(x, y, f int) { calls++ })
	if calls != 18*18 {
		t.Errorf("Region computed %d frames, want %d", calls, 18*18)
	}
}

func TestWangIndexAllocationFree(t *testing.T) {
	g := benchTerrain(3)
	terrain := func(x, y int) int { return g.At(x, y) }
	m := Mapper{Rules: Wang(WangFull, allWang(WangFull, 3))}
	set := func(x, y, f int) {}
	m.Apply(32, 32, terrain, set)
	if n := testing.AllocsPerRun(5, func() {
		m.Apply(32, 32, terrain, set)
		m.Region(4, 4, 8, 8, 32, 32, terrain, set)
	}); n != 0 {
		t.Errorf("Apply and Region allocate %v times after the first call", n)
	}
}
