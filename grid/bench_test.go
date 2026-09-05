package grid_test

import (
	"testing"

	"github.com/matjam/bunyip/grid"
)

// benchMap builds a deterministic w by h map with pillars every fourth
// cell, so the search has to work without being blocked outright.
func benchMap(w, h int) (grid.Cost, func(grid.Point) bool) {
	walls := grid.New[bool](w, h)
	for y := range h {
		for x := range w {
			if x%4 == 2 && y%7 != 3 {
				walls.Set(x, y, true)
			}
		}
	}
	walls.Set(0, 0, false)
	walls.Set(w-1, h-1, false)
	cost := func(from, to grid.Point) float32 {
		if walls.At(to.X, to.Y) {
			return grid.Blocked
		}
		return 1
	}
	opaque := func(p grid.Point) bool {
		return !walls.In(p.X, p.Y) || walls.At(p.X, p.Y)
	}
	return cost, opaque
}

func BenchmarkAStar256(b *testing.B) {
	const w, h = 256, 256
	cost, _ := benchMap(w, h)
	start, goal := grid.Point{X: 0, Y: 0}, grid.Point{X: w - 1, Y: h - 1}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if p := grid.AStar(w, h, start, goal, false, cost); p == nil {
			b.Fatal("no path")
		}
	}
}

func BenchmarkAStar256Diagonal(b *testing.B) {
	const w, h = 256, 256
	cost, _ := benchMap(w, h)
	start, goal := grid.Point{X: 0, Y: 0}, grid.Point{X: w - 1, Y: h - 1}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if p := grid.AStar(w, h, start, goal, true, cost); p == nil {
			b.Fatal("no path")
		}
	}
}

// BenchmarkAStarPathfinder reuses one Pathfinder and appends into a
// caller's slice, which is the allocation-free way to search each frame.
func BenchmarkAStarPathfinder(b *testing.B) {
	const w, h = 256, 256
	cost, _ := benchMap(w, h)
	start, goal := grid.Point{X: 0, Y: 0}, grid.Point{X: w - 1, Y: h - 1}
	want := grid.Dijkstra(w, h, []grid.Point{start}, false, cost).At(goal.X, goal.Y)
	for _, name := range []string{"safe_default", "min_cost_1"} {
		b.Run(name, func(b *testing.B) {
			pf := grid.NewPathfinder(w, h)
			search := pf.AStar
			if name == "min_cost_1" {
				search = func(out []grid.Point, start, goal grid.Point, diagonal bool, cost grid.Cost) ([]grid.Point, bool) {
					return pf.AStarWithMinCost(out, start, goal, diagonal, cost, 1)
				}
			}
			out := make([]grid.Point, 0, 1024)
			out, ok := search(out, start, goal, false, cost)
			if !ok {
				b.Fatal("no path")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				out, ok = search(out[:0], start, goal, false, cost)
				if !ok {
					b.Fatal("no path")
				}
			}
			b.StopTimer()
			var total float32
			for i := 1; i < len(out); i++ {
				total += cost(out[i-1], out[i])
			}
			if total != want {
				b.Fatalf("path cost %g, want %g", total, want)
			}
		})
	}
}

func BenchmarkDijkstra256(b *testing.B) {
	const w, h = 256, 256
	cost, _ := benchMap(w, h)
	sources := []grid.Point{{X: 0, Y: 0}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		grid.Dijkstra(w, h, sources, false, cost)
	}
}

// BenchmarkDijkstraPathfinder fills a map the caller owns, so nothing
// is allocated after the first call.
func BenchmarkDijkstraPathfinder(b *testing.B) {
	const w, h = 256, 256
	cost, _ := benchMap(w, h)
	sources := []grid.Point{{X: 0, Y: 0}}
	pf := grid.NewPathfinder(w, h)
	dist := grid.New[float32](w, h)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		pf.DijkstraInto(dist, sources, false, cost)
	}
}

func BenchmarkFOV20(b *testing.B) {
	const w, h = 256, 256
	_, opaque := benchMap(w, h)
	origin := grid.Point{X: 128, Y: 128}
	n := 0
	// The closure is made once; a closure made inside the loop would be
	// the caller's allocation, not the package's.
	visit := func(grid.Point) { n++ }
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		grid.FOV(origin, 20, opaque, visit)
	}
	b.StopTimer()
	if n == 0 {
		b.Fatal("nothing visible")
	}
}

// BenchmarkFOV20Vision keeps the caster, which is what a game that
// recomputes sight every frame does.
func BenchmarkFOV20Vision(b *testing.B) {
	const w, h = 256, 256
	_, opaque := benchMap(w, h)
	origin := grid.Point{X: 128, Y: 128}
	n := 0
	visit := func(grid.Point) { n++ }
	var v grid.Vision
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		v.FOV(origin, 20, opaque, visit)
	}
	b.StopTimer()
	if n == 0 {
		b.Fatal("nothing visible")
	}
}

func BenchmarkLine(b *testing.B) {
	a, z := grid.Point{X: 0, Y: 0}, grid.Point{X: 255, Y: 137}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		grid.Line(a, z)
	}
}
