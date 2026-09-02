package grid

import "testing"

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
