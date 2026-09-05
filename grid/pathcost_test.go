package grid

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func checkPathCost(t *testing.T, path []Point, start, goal Point, diagonal bool, cost Cost, want float32) {
	t.Helper()
	if len(path) == 0 || path[0] != start || path[len(path)-1] != goal {
		t.Fatalf("wrong endpoints: %v", path)
	}
	var total float32
	for i := 1; i < len(path); i++ {
		from, to := path[i-1], path[i]
		if from.Chebyshev(to) != 1 || (!diagonal && from.Manhattan(to) != 1) {
			t.Fatalf("noncontiguous path: %v", path)
		}
		c := cost(from, to)
		if c < 0 || c >= Blocked {
			t.Fatalf("impassable edge %v to %v", from, to)
		}
		total += c
	}
	if total != want {
		t.Fatalf("path costs %g, want %g: %v", total, want, path)
	}
}

func TestAStarCheapDetour(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cheap    float32
		diagonal bool
	}{
		{"fractional_cardinal", 0.1, false},
		{"zero_cardinal", 0, false},
		{"fractional_diagonal", 0.1, true},
		{"zero_diagonal", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cost := func(from, to Point) float32 {
				if from.Y == 0 && to.Y == 0 {
					return 1
				}
				return tc.cheap
			}
			start, goal := Point{0, 0}, Point{2, 0}
			steps := float32(4)
			if tc.diagonal {
				steps = 2
			}
			want := steps * tc.cheap
			pf := NewPathfinder(3, 2)
			prefix := Point{-7, -9}
			out := make([]Point, 1, 16)
			out[0] = prefix
			path, ok := pf.AStar(out, start, goal, tc.diagonal, cost)
			if !ok || path[0] != prefix || &path[0] != &out[0] {
				t.Fatalf("lost output buffer or prefix: %v, %v", path, ok)
			}
			checkPathCost(t, path[1:], start, goal, tc.diagonal, cost, want)
			checkPathCost(t, AStar(3, 2, start, goal, tc.diagonal, cost), start, goal, tc.diagonal, cost, want)
		})
	}
}

func TestAStarWeightedMatchesDijkstra(t *testing.T) {
	const w, h = 9, 7
	for _, diagonal := range []bool{false, true} {
		pf := NewPathfinder(w, h)
		dist := New[float32](w, h)
		r := rand.New(rand.NewPCG(8, 2026))
		out := make([]Point, 0, w*h)
		for trial := range 200 {
			// Directed edges include zero cycles, fractions and blocked moves.
			edges := make([][9]float32, w*h)
			values := []float32{0, 0.125, 0.25, 0.5, 1, 3, Blocked, -1}
			for i := range edges {
				for j := range edges[i] {
					edges[i][j] = values[r.IntN(len(values))]
				}
			}
			cost := func(from, to Point) float32 {
				return edges[from.Y*w+from.X][(to.Y-from.Y+1)*3+to.X-from.X+1]
			}
			start, goal := Point{r.IntN(w), r.IntN(h)}, Point{r.IntN(w), r.IntN(h)}
			pf.DijkstraInto(dist, []Point{start}, diagonal, cost)
			want := dist.At(goal.X, goal.Y)
			path, ok := pf.AStar(out[:0], start, goal, diagonal, cost)
			if ok != (want != Blocked) {
				t.Fatalf("trial %d diagonal %v: found=%v, Dijkstra=%g", trial, diagonal, ok, want)
			}
			if ok {
				checkPathCost(t, path, start, goal, diagonal, cost, want)
			}
		}
	}
}

func TestAStarCheapDiagonalDetour(t *testing.T) {
	detour := []Point{{0, 0}, {0, 1}, {1, 2}, {2, 2}, {3, 2}, {4, 1}, {4, 0}}
	for _, cheap := range []float32{0, 0.125} {
		cost := func(from, to Point) float32 {
			for i := 1; i < len(detour); i++ {
				if from == detour[i-1] && to == detour[i] {
					return cheap
				}
			}
			return 1
		}
		start, goal := detour[0], detour[len(detour)-1]
		want := float32(len(detour)-1) * cheap
		checkPathCost(t, AStar(5, 3, start, goal, true, cost), start, goal, true, cost, want)
	}
}

func TestAStarWeightedBufferAndAllocations(t *testing.T) {
	pf := NewPathfinder(8, 8)
	start, goal := Point{0, 0}, Point{7, 7}
	out := make([]Point, 2, 128)
	out[0], out[1] = Point{-1, -2}, Point{-3, -4}
	before := slices.Clone(out)
	blocked := func(Point, Point) float32 { return Blocked }
	path, ok := pf.AStar(out, start, goal, true, blocked)
	if ok || !slices.Equal(path, before) || &path[0] != &out[0] {
		t.Fatalf("unreachable changed output: %v", path)
	}
	cost := func(from, to Point) float32 { return float32((from.X+to.Y)%3) / 8 }
	pf.AStar(out[:0], start, goal, true, cost)
	allocs := testing.AllocsPerRun(100, func() {
		path, ok = pf.AStar(out[:0], start, goal, true, cost)
	})
	if !ok || len(path) == 0 {
		t.Fatal("no path")
	}
	if allocs != 0 {
		t.Fatalf("steady search allocated %g times", allocs)
	}
}
