package autotile

// Benchmarks for reapplying a whole 256 by 256 map against re-autotiling
// a changed 16 by 16 region, for mask rules and for Wang sets of
// increasing size.

import (
	"testing"

	"github.com/matjam/bunyip/grid"
)

const benchW, benchH = 256, 256

func benchTerrain(colors int) *grid.Grid[int] {
	g := grid.New[int](benchW, benchH)
	for y := range benchH {
		for x := range benchW {
			// Blobs of terrain, deterministic.
			v := ((x/7)*31 + (y/5)*17 + (x*y)/97) % colors
			g.Set(x, y, v)
		}
	}
	return g
}

// allWang builds a complete Wang set: every colouring of the positions
// the type matches, with colours 0..colors-1.
func allWang(t WangType, colors int) []WangTile {
	pos := wangPositions[t]
	n := 1
	for range pos {
		n *= colors
	}
	tiles := make([]WangTile, 0, n)
	for i := range n {
		var tile WangTile
		tile.Frame = i
		k := i
		for _, p := range pos {
			tile.Colors[p] = k % colors
			k /= colors
		}
		tiles = append(tiles, tile)
	}
	return tiles
}

func benchRules() []struct {
	name   string
	rules  *Rules
	colors int
} {
	var blob [47]int
	for i := range blob {
		blob[i] = i
	}
	var e16 [16]int
	for i := range e16 {
		e16[i] = i
	}
	return []struct {
		name   string
		rules  *Rules
		colors int
	}{
		{"edge16", Edge16(1, e16), 2},
		{"blob47", Blob47(1, blob), 2},
		{"corner16", Corner16(1, e16), 2},
		{"wang_corners_2c_16tiles", Wang(WangCorners, allWang(WangCorners, 2)), 2},
		{"wang_corners_4c_256tiles", Wang(WangCorners, allWang(WangCorners, 4)), 4},
		{"wang_full_3c_6561tiles", Wang(WangFull, allWang(WangFull, 3)), 3},
	}
}

// BenchmarkApply256 reapplies the whole map.
func BenchmarkApply256(b *testing.B) {
	for _, r := range benchRules() {
		b.Run(r.name, func(b *testing.B) {
			g := benchTerrain(r.colors)
			m := Mapper{Rules: r.rules}
			terrain := func(x, y int) int { return g.At(x, y) }
			sum := 0
			set := func(x, y, f int) { sum += f }
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				m.Apply(benchW, benchH, terrain, set)
			}
		})
	}
}

// BenchmarkRegion16 re-autotiles a 16 by 16 brush stroke by calling Cell
// for every changed cell. It also counts how many frames were computed,
// against the 18 by 18 that are affected.
func BenchmarkRegion16(b *testing.B) {
	for _, r := range benchRules() {
		b.Run(r.name, func(b *testing.B) {
			g := benchTerrain(r.colors)
			m := Mapper{Rules: r.rules}
			terrain := func(x, y int) int { return g.At(x, y) }
			calls := 0
			set := func(x, y, f int) { calls++ }
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				calls = 0
				for y := 100; y < 116; y++ {
					for x := 100; x < 116; x++ {
						m.Cell(x, y, benchW, benchH, terrain, set)
					}
				}
			}
			b.ReportMetric(float64(calls), "frames-computed")
		})
	}
}

// BenchmarkRegionRect16 re-autotiles the same 16 by 16 brush stroke as
// BenchmarkRegion16 with one call to Region.
func BenchmarkRegionRect16(b *testing.B) {
	for _, r := range benchRules() {
		b.Run(r.name, func(b *testing.B) {
			g := benchTerrain(r.colors)
			m := Mapper{Rules: r.rules}
			terrain := func(x, y int) int { return g.At(x, y) }
			calls := 0
			set := func(x, y, f int) { calls++ }
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				calls = 0
				m.Region(100, 100, 16, 16, benchW, benchH, terrain, set)
			}
			b.ReportMetric(float64(calls), "frames-computed")
		})
	}
}

// noisyTerrain is a 256x256 grid of two terrains in noisy blobs.
func noisyTerrain() (w, h int, at func(x, y int) int) {
	w, h = 256, 256
	cells := make([]int, w*h)
	for y := range h {
		for x := range w {
			if hash(7, x/5, y/5)%3 == 0 {
				cells[y*w+x] = 1
			}
		}
	}
	return w, h, func(x, y int) int { return cells[y*w+x] }
}

// cornerSet2 is a complete corner set over two colours: sixteen tiles,
// one per combination.
func cornerSet2() []WangTile {
	var tiles []WangTile
	for m := range 16 {
		var t WangTile
		t.Frame = m
		for i, p := range []int{DirNE, DirSE, DirSW, DirNW} {
			if m&(1<<i) != 0 {
				t.Colors[p] = 1
			} else {
				t.Colors[p] = 2
			}
		}
		tiles = append(tiles, t)
	}
	return tiles
}

// BenchmarkNoisyApply256 reapplies blob and Wang rules to every cell of a
// 256x256 map of noisy blobs, and updates one cell.
func BenchmarkNoisyApply256(b *testing.B) {
	w, h, at := noisyTerrain()
	var frames [47]int
	for i := range frames {
		frames[i] = i
	}
	sink := 0
	set := func(x, y, frame int) { sink += frame }
	b.Run("blob47", func(b *testing.B) {
		m := Mapper{Rules: Blob47(1, frames)}
		b.ReportAllocs()
		for b.Loop() {
			m.Apply(w, h, at, set)
		}
	})
	b.Run("wang_corners", func(b *testing.B) {
		m := Mapper{Rules: Wang(WangCorners, cornerSet2())}
		b.ReportAllocs()
		for b.Loop() {
			m.Apply(w, h, at, set)
		}
	})
	b.Run("cell_blob47", func(b *testing.B) {
		m := Mapper{Rules: Blob47(1, frames)}
		b.ReportAllocs()
		for b.Loop() {
			m.Cell(128, 128, w, h, at, set)
		}
	})
	_ = sink
}
