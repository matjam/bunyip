package grid

import (
	"math/rand/v2"
	"testing"
)

// A 7x5 map with a wall down column 3 except a gap at the bottom row.
func wallMap() (*Grid[bool], Cost) {
	walls := New[bool](7, 5)
	for y := range 4 {
		walls.Set(3, y, true)
	}
	cost := func(from, to Point) float32 {
		if walls.At(to.X, to.Y) {
			return Blocked
		}
		if from.X != to.X && from.Y != to.Y {
			return 1.41
		}
		return 1
	}
	return walls, cost
}

func TestAStar(t *testing.T) {
	_, cost := wallMap()
	path := AStar(7, 5, Point{0, 0}, Point{6, 0}, false, cost)
	if path == nil || path[0] != (Point{0, 0}) || path[len(path)-1] != (Point{6, 0}) {
		t.Fatalf("path %v", path)
	}
	// Around the wall: down four, across, up four, plus the width.
	if len(path) != 15 {
		t.Fatalf("path length %d, want 15: %v", len(path), path)
	}
	for i := 1; i < len(path); i++ {
		if path[i-1].Manhattan(path[i]) != 1 {
			t.Fatalf("path not contiguous at %d: %v", i, path)
		}
		if path[i].X == 3 && path[i].Y != 4 {
			t.Fatalf("path through wall: %v", path)
		}
	}
	if diag := AStar(7, 5, Point{0, 0}, Point{6, 0}, true, cost); len(diag) >= len(path) {
		t.Fatalf("diagonal path %d not shorter than %d", len(diag), len(path))
	}
	sealed := func(from, to Point) float32 {
		if to.X == 3 {
			return Blocked
		}
		return 1
	}
	if p := AStar(7, 5, Point{0, 0}, Point{6, 0}, false, sealed); p != nil {
		t.Fatalf("found a path through a sealed wall: %v", p)
	}
	if p := AStar(7, 5, Point{2, 2}, Point{2, 2}, false, cost); len(p) != 1 {
		t.Fatal("start == goal should be a one-cell path")
	}
}

// TestPathfinderReuse searches many random maps with one Pathfinder, so
// a generation stamp left behind by an earlier search would show up as a
// path that is not the cheapest, or as none at all.
func TestPathfinderReuse(t *testing.T) {
	const w, h = 24, 18
	r := rand.New(rand.NewPCG(1, 2))
	pf := NewPathfinder(w, h)
	dist := New[float32](w, h)
	var path []Point
	for trial := range 200 {
		walls := New[bool](w, h)
		for i := range walls.Cells {
			walls.Cells[i] = r.IntN(4) == 0
		}
		start := Point{X: r.IntN(w), Y: r.IntN(h)}
		goal := Point{X: r.IntN(w), Y: r.IntN(h)}
		walls.Set(start.X, start.Y, false)
		walls.Set(goal.X, goal.Y, false)
		cost := func(from, to Point) float32 {
			if walls.At(to.X, to.Y) {
				return Blocked
			}
			return 1
		}

		// Dijkstra from the start gives the cheapest cost to the goal,
		// which the path A* returns must match step for step.
		pf.DijkstraInto(dist, []Point{start}, false, cost)
		want := dist.At(goal.X, goal.Y)

		var ok bool
		path, ok = pf.AStar(path[:0], start, goal, false, cost)
		if !ok {
			if want != Blocked {
				t.Fatalf("trial %d: no path but Dijkstra says %v", trial, want)
			}
			continue
		}
		if want == Blocked {
			t.Fatalf("trial %d: path %v where Dijkstra found none", trial, path)
		}
		if path[0] != start || path[len(path)-1] != goal {
			t.Fatalf("trial %d: path runs %v to %v", trial, path[0], path[len(path)-1])
		}
		var total float32
		for i := 1; i < len(path); i++ {
			if path[i-1].Manhattan(path[i]) != 1 {
				t.Fatalf("trial %d: path not contiguous at %d", trial, i)
			}
			total += cost(path[i-1], path[i])
		}
		if total != want {
			t.Fatalf("trial %d: path costs %v, cheapest is %v", trial, total, want)
		}

		// The package functions must agree with the reused pathfinder.
		if free := AStar(w, h, start, goal, false, cost); len(free) != len(path) {
			t.Fatalf("trial %d: AStar returned %d cells, Pathfinder %d", trial, len(free), len(path))
		}
	}
}

// TestVisionReuse casts sight repeatedly with one Vision and checks it
// visits each cell once and agrees with the package function.
func TestVisionReuse(t *testing.T) {
	walls, _ := wallMap()
	opaque := func(p Point) bool { return !walls.In(p.X, p.Y) || walls.At(p.X, p.Y) }
	var v Vision
	for _, radius := range []int{0, 1, 3, 10, 2} {
		want := map[Point]int{}
		FOV(Point{X: 1, Y: 4}, radius, opaque, func(p Point) { want[p]++ })
		got := map[Point]int{}
		v.FOV(Point{X: 1, Y: 4}, radius, opaque, func(p Point) { got[p]++ })
		if len(got) != len(want) {
			t.Fatalf("radius %d: saw %d cells, want %d", radius, len(got), len(want))
		}
		for p, n := range got {
			if n != 1 {
				t.Fatalf("radius %d: visited %v %d times", radius, p, n)
			}
			if want[p] != 1 {
				t.Fatalf("radius %d: %v visible to Vision but not FOV", radius, p)
			}
		}
	}
}

func TestDijkstraAndDownhill(t *testing.T) {
	_, cost := wallMap()
	dist := Dijkstra(7, 5, []Point{{6, 0}}, false, cost)
	if dist.At(6, 0) != 0 || dist.At(5, 0) != 1 || dist.At(0, 0) != 14 {
		t.Fatalf("distances %v %v %v", dist.At(6, 0), dist.At(5, 0), dist.At(0, 0))
	}
	if dist.At(3, 0) != Blocked {
		t.Fatal("wall reachable")
	}
	p := Point{0, 0}
	for range 14 {
		next, ok := Downhill(dist, p, false)
		if !ok {
			t.Fatalf("stuck at %v", p)
		}
		p = next
	}
	if p != (Point{6, 0}) {
		t.Fatalf("downhill ended at %v", p)
	}
	if _, ok := Downhill(dist, p, false); ok {
		t.Fatal("source should have no downhill")
	}
}

func TestLineFOVFlood(t *testing.T) {
	line := Line(Point{0, 0}, Point{4, 2})
	if len(line) != 5 || line[0] != (Point{0, 0}) || line[4] != (Point{4, 2}) {
		t.Fatalf("line %v", line)
	}
	walls, _ := wallMap()
	seen := map[Point]bool{}
	// Looking from the bottom-left corner: the gap row is open to the
	// right edge, the wall's face is lit, and cells past the wall are dark.
	FOV(Point{1, 4}, 10, func(p Point) bool { return walls.At(p.X, p.Y) }, func(p Point) {
		if seen[p] {
			t.Fatalf("visited %v twice", p)
		}
		seen[p] = true
	})
	if !seen[Point{1, 4}] || !seen[Point{2, 0}] || !seen[Point{3, 2}] {
		t.Fatal("open cells and the wall face should be visible")
	}
	if seen[Point{5, 0}] || seen[Point{6, 1}] {
		t.Fatal("cell behind the wall visible")
	}
	if !seen[Point{5, 4}] || !seen[Point{6, 4}] {
		t.Fatal("cells along the gap row should be visible")
	}
	region := FloodFill(7, 5, Point{0, 0}, func(p Point) bool { return !walls.At(p.X, p.Y) })
	if len(region) != 7*5-4 {
		t.Fatalf("flood filled %d cells, want %d", len(region), 7*5-4)
	}
	if FloodFill(7, 5, Point{3, 0}, func(p Point) bool { return !walls.At(p.X, p.Y) }) != nil {
		t.Fatal("flood from a wall should be empty")
	}
}
