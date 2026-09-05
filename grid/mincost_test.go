package grid

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestAStarWithMinCostMatchesDijkstra(t *testing.T) {
	const w, h = 9, 7
	for _, tc := range []struct {
		name     string
		diagonal bool
		minCost  float32
	}{
		{"fractional_cardinal", false, 0.125},
		{"fractional_diagonal", true, 0.125},
		{"zero_cardinal", false, 0},
		{"zero_diagonal", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pf := NewPathfinder(w, h)
			dist := New[float32](w, h)
			out := make([]Point, 0, w*h)
			r := rand.New(rand.NewPCG(9, 2026))
			for trial := range 200 {
				edges := make([][9]float32, w*h)
				values := []float32{tc.minCost, 0.125, 0.25, 0.5, 1, 3, Blocked, -1}
				for i := range edges {
					for j := range edges[i] {
						edges[i][j] = values[r.IntN(len(values))]
					}
				}
				cost := func(from, to Point) float32 {
					return edges[from.Y*w+from.X][(to.Y-from.Y+1)*3+to.X-from.X+1]
				}
				start, goal := Point{r.IntN(w), r.IntN(h)}, Point{r.IntN(w), r.IntN(h)}
				pf.DijkstraInto(dist, []Point{start}, tc.diagonal, cost)
				want := dist.At(goal.X, goal.Y)
				path, ok := pf.AStarWithMinCost(out[:0], start, goal, tc.diagonal, cost, tc.minCost)
				if ok != (want != Blocked) {
					t.Fatalf("trial %d: found=%v, Dijkstra=%g", trial, ok, want)
				}
				pooled := AStarWithMinCost(w, h, start, goal, tc.diagonal, cost, tc.minCost)
				if (pooled != nil) != ok {
					t.Fatalf("trial %d: package and method disagree on reachability", trial)
				}
				if ok {
					checkPathCost(t, path, start, goal, tc.diagonal, cost, want)
					checkPathCost(t, pooled, start, goal, tc.diagonal, cost, want)
				}
			}
		})
	}
}

func TestAStarWithMinCostEqualDiagonalCost(t *testing.T) {
	// At (2,2), octile estimates 2.828 remaining, overestimating the
	// two cost-1 diagonal steps and selecting the cost-4.5 direct route.
	detour := []Point{{0, 0}, {1, 1}, {2, 2}, {3, 1}, {4, 0}}
	cost := func(from, to Point) float32 {
		for i := 1; i < len(detour); i++ {
			if from == detour[i-1] && to == detour[i] {
				return 1
			}
		}
		return 1.125
	}
	start, goal := detour[0], detour[len(detour)-1]
	pf := NewPathfinder(5, 3)
	path, ok := pf.AStarWithMinCost(nil, start, goal, true, cost, 1)
	if !ok {
		t.Fatal("no path")
	}
	want := Dijkstra(5, 3, []Point{start}, true, cost).At(goal.X, goal.Y)
	if want != 4 {
		t.Fatalf("Dijkstra cost %g, want 4", want)
	}
	checkPathCost(t, path, start, goal, true, cost, want)
	checkPathCost(t, AStarWithMinCost(5, 3, start, goal, true, cost, 1), start, goal, true, cost, want)
}

func TestAStarWithMinCostInvalidBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bound float32
	}{
		{"zero", 0},
		{"negative", -1},
		{"nan", float32(math.NaN())},
		{"positive_infinity", float32(math.Inf(1))},
		{"negative_infinity", float32(math.Inf(-1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, diagonal := range []bool{false, true} {
				cost := func(from, to Point) float32 {
					if from.Y == 0 && to.Y == 0 {
						return 1
					}
					return 0
				}
				start, goal := Point{0, 0}, Point{2, 0}
				pf := NewPathfinder(3, 2)
				want, _ := pf.AStar(nil, start, goal, diagonal, cost)
				path, ok := pf.AStarWithMinCost(nil, start, goal, diagonal, cost, tc.bound)
				if !ok || !slices.Equal(path, want) {
					t.Fatalf("diagonal %v: fallback path %v, want %v", diagonal, path, want)
				}
				checkPathCost(t, path, start, goal, diagonal, cost, 0)
				pooled := AStarWithMinCost(3, 2, start, goal, diagonal, cost, tc.bound)
				if !slices.Equal(pooled, want) {
					t.Fatalf("package fallback path %v, want %v", pooled, want)
				}
			}
		})
	}
}

func TestAStarWithMinCostBufferAndEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name        string
		start, goal Point
		blocked     bool
		ok          bool
	}{
		{"path", Point{0, 0}, Point{7, 7}, false, true},
		{"same_point", Point{3, 4}, Point{3, 4}, true, true},
		{"outside_start", Point{-1, 0}, Point{7, 7}, false, false},
		{"outside_goal", Point{0, 0}, Point{8, 7}, false, false},
		{"outside_same_point", Point{8, 7}, Point{8, 7}, false, false},
		{"unreachable", Point{0, 0}, Point{7, 7}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pf := NewPathfinder(8, 8)
			out := make([]Point, 2, 128)
			out[0], out[1] = Point{-1, -2}, Point{-3, -4}
			before := slices.Clone(out)
			cost := func(Point, Point) float32 {
				if tc.blocked {
					return Blocked
				}
				return 0.125
			}
			path, ok := pf.AStarWithMinCost(out, tc.start, tc.goal, true, cost, 0.125)
			if ok != tc.ok || len(path) < len(out) || !slices.Equal(path[:len(out)], before) || &path[0] != &out[0] {
				t.Fatalf("lost buffer or prefix: %v, found %v", path, ok)
			}
			pooled := AStarWithMinCost(8, 8, tc.start, tc.goal, true, cost, 0.125)
			if !ok {
				if len(path) != len(out) || pooled != nil {
					t.Fatalf("failure changed output: %v, package %v", path, pooled)
				}
				return
			}
			want := float32(tc.start.Chebyshev(tc.goal)) * 0.125
			checkPathCost(t, path[len(out):], tc.start, tc.goal, true, cost, want)
			checkPathCost(t, pooled, tc.start, tc.goal, true, cost, want)
			owned := slices.Clone(pooled)
			AStarWithMinCost(8, 8, tc.goal, tc.start, false, cost, 0.125)
			if !slices.Equal(pooled, owned) {
				t.Fatal("pooled scratch overwrote returned path")
			}
		})
	}
}

func TestAStarWithMinCostAllocations(t *testing.T) {
	pf := NewPathfinder(16, 16)
	out := make([]Point, 0, 256)
	start, goal := Point{0, 0}, Point{15, 15}
	cost := func(from, to Point) float32 { return float32(1+(from.X+to.Y)%3) / 8 }
	for _, diagonal := range []bool{false, true} {
		pf.AStarWithMinCost(out[:0], start, goal, diagonal, cost, 0.125)
		var ok bool
		allocs := testing.AllocsPerRun(100, func() {
			out, ok = pf.AStarWithMinCost(out[:0], start, goal, diagonal, cost, 0.125)
		})
		if !ok || len(out) == 0 || allocs != 0 {
			t.Fatalf("diagonal %v: found %v, allocated %g times", diagonal, ok, allocs)
		}
	}
}
