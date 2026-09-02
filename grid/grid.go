// Package grid holds the tile-map helpers roguelikes and strategy games
// need: a generic cell grid, A* and Dijkstra pathfinding, Bresenham
// lines, shadowcasting field of view and flood fill.
package grid

import (
	"container/heap"
	"math"
)

// Point is a cell coordinate.
type Point struct{ X, Y int }

// Add offsets the point.
func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }

// Manhattan is the four-way step distance.
func (p Point) Manhattan(q Point) int { return abs(p.X-q.X) + abs(p.Y-q.Y) }

// Chebyshev is the eight-way step distance.
func (p Point) Chebyshev(q Point) int { return max(abs(p.X-q.X), abs(p.Y-q.Y)) }

// Dirs4 and Dirs8 are the neighbour offsets, cardinals first.
var (
	Dirs4 = []Point{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	Dirs8 = []Point{{1, 0}, {0, 1}, {-1, 0}, {0, -1}, {1, 1}, {-1, 1}, {-1, -1}, {1, -1}}
)

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Grid is a W by H array of cells in row-major order.
type Grid[T any] struct {
	W, H  int
	Cells []T
}

// New makes a grid of zero cells.
func New[T any](w, h int) *Grid[T] {
	return &Grid[T]{W: w, H: h, Cells: make([]T, w*h)}
}

// In reports whether the coordinate is inside the grid.
func (g *Grid[T]) In(x, y int) bool { return x >= 0 && y >= 0 && x < g.W && y < g.H }

// At returns the cell, or the zero value outside the grid.
func (g *Grid[T]) At(x, y int) T {
	if !g.In(x, y) {
		var zero T
		return zero
	}
	return g.Cells[y*g.W+x]
}

// Set writes a cell; writes outside the grid are ignored.
func (g *Grid[T]) Set(x, y int, v T) {
	if g.In(x, y) {
		g.Cells[y*g.W+x] = v
	}
}

// Fill sets every cell.
func (g *Grid[T]) Fill(v T) {
	for i := range g.Cells {
		g.Cells[i] = v
	}
}

// Each visits every cell in row-major order.
func (g *Grid[T]) Each(fn func(x, y int, v T)) {
	for y := range g.H {
		for x := range g.W {
			fn(x, y, g.Cells[y*g.W+x])
		}
	}
}

// Blocked is the step cost of an impassable move.
var Blocked = float32(math.Inf(1))

// Cost gives the price of stepping from one cell to a neighbour, or
// Blocked. Diagonal moves are only offered when the search allows them.
type Cost func(from, to Point) float32

// AStar finds the cheapest path from start to goal on a w by h grid,
// including both endpoints, or returns nil when there is none. With
// diagonal set, eight-way moves are considered.
func AStar(w, h int, start, goal Point, diagonal bool, cost Cost) []Point {
	if start == goal {
		return []Point{start}
	}
	dirs := Dirs4
	if diagonal {
		dirs = Dirs8
	}
	heuristic := func(p Point) float32 {
		if diagonal {
			dx, dy := abs(p.X-goal.X), abs(p.Y-goal.Y)
			return float32(max(dx, dy)) + (math.Sqrt2-1)*float32(min(dx, dy))
		}
		return float32(p.Manhattan(goal))
	}
	idx := func(p Point) int { return p.Y*w + p.X }
	g := make([]float32, w*h)
	for i := range g {
		g[i] = Blocked
	}
	came := make([]int32, w*h)
	for i := range came {
		came[i] = -1
	}
	closed := make([]bool, w*h)
	open := &pointHeap{}
	g[idx(start)] = 0
	heap.Push(open, item{p: start, f: heuristic(start)})
	for open.Len() > 0 {
		cur := heap.Pop(open).(item)
		ci := idx(cur.p)
		if closed[ci] {
			continue
		}
		if cur.p == goal {
			var path []Point
			for i := ci; i >= 0; i = int(came[i]) {
				path = append(path, Point{i % w, i / w})
			}
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return path
		}
		closed[ci] = true
		for _, d := range dirs {
			n := cur.p.Add(d)
			if n.X < 0 || n.Y < 0 || n.X >= w || n.Y >= h {
				continue
			}
			ni := idx(n)
			if closed[ni] {
				continue
			}
			c := cost(cur.p, n)
			if c >= Blocked || c < 0 {
				continue
			}
			ng := g[ci] + c
			if ng < g[ni] {
				g[ni] = ng
				came[ni] = int32(ci)
				heap.Push(open, item{p: n, f: ng + heuristic(n)})
			}
		}
	}
	return nil
}

// Dijkstra returns the cost from the nearest source to every cell, with
// Blocked for cells that cannot be reached. Roguelikes use the result as
// a Dijkstra map: monsters walk downhill toward the sources.
func Dijkstra(w, h int, sources []Point, diagonal bool, cost Cost) *Grid[float32] {
	dirs := Dirs4
	if diagonal {
		dirs = Dirs8
	}
	dist := New[float32](w, h)
	dist.Fill(Blocked)
	open := &pointHeap{}
	for _, s := range sources {
		if dist.In(s.X, s.Y) {
			dist.Set(s.X, s.Y, 0)
			heap.Push(open, item{p: s, f: 0})
		}
	}
	for open.Len() > 0 {
		cur := heap.Pop(open).(item)
		if cur.f > dist.At(cur.p.X, cur.p.Y) {
			continue
		}
		for _, d := range dirs {
			n := cur.p.Add(d)
			if !dist.In(n.X, n.Y) {
				continue
			}
			c := cost(cur.p, n)
			if c >= Blocked || c < 0 {
				continue
			}
			nd := cur.f + c
			if nd < dist.At(n.X, n.Y) {
				dist.Set(n.X, n.Y, nd)
				heap.Push(open, item{p: n, f: nd})
			}
		}
	}
	return dist
}

// Downhill returns the neighbour of p with the lowest value in a
// Dijkstra map, and false when none is lower (p is a source or cut off).
// Multiplying a map by a negative factor before descending makes
// creatures flee instead.
func Downhill(dist *Grid[float32], p Point, diagonal bool) (Point, bool) {
	dirs := Dirs4
	if diagonal {
		dirs = Dirs8
	}
	best, bestV, ok := p, dist.At(p.X, p.Y), false
	for _, d := range dirs {
		n := p.Add(d)
		if !dist.In(n.X, n.Y) {
			continue
		}
		if v := dist.At(n.X, n.Y); v < bestV {
			best, bestV, ok = n, v, true
		}
	}
	return best, ok
}

type item struct {
	p Point
	f float32
}

type pointHeap []item

func (h pointHeap) Len() int           { return len(h) }
func (h pointHeap) Less(i, j int) bool { return h[i].f < h[j].f }
func (h pointHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *pointHeap) Push(x any)        { *h = append(*h, x.(item)) }
func (h *pointHeap) Pop() any          { old := *h; n := len(old); it := old[n-1]; *h = old[:n-1]; return it }

// Line returns the cells on a straight line from a to b inclusive
// (Bresenham), in order from a.
func Line(a, b Point) []Point {
	dx, dy := abs(b.X-a.X), -abs(b.Y-a.Y)
	sx, sy := 1, 1
	if a.X > b.X {
		sx = -1
	}
	if a.Y > b.Y {
		sy = -1
	}
	err := dx + dy
	var out []Point
	p := a
	for {
		out = append(out, p)
		if p == b {
			return out
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			p.X += sx
		}
		if e2 <= dx {
			err += dx
			p.Y += sy
		}
	}
}

// FOV computes which cells are visible from origin within radius by
// recursive shadowcasting, calling visit once for each (the origin
// included). opaque says whether a cell blocks sight; it is only asked
// about cells the caster reaches, so it must tolerate coordinates off
// the map.
func FOV(origin Point, radius int, opaque func(Point) bool, visit func(Point)) {
	seen := map[Point]struct{}{origin: {}}
	visit(origin)
	once := func(p Point) {
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		visit(p)
	}
	for oct := range 8 {
		castLight(origin, radius, 1, 1, 0, octants[0][oct], octants[1][oct], octants[2][oct], octants[3][oct], opaque, once)
	}
}

var octants = [4][8]int{
	{1, 0, 0, -1, -1, 0, 0, 1},
	{0, 1, -1, 0, 0, -1, 1, 0},
	{0, 1, 1, 0, 0, -1, -1, 0},
	{1, 0, 0, 1, -1, 0, 0, -1},
}

func castLight(o Point, radius, row int, start, end float64, xx, xy, yx, yy int, opaque func(Point) bool, visit func(Point)) {
	if start < end {
		return
	}
	r2 := radius * radius
	for j := row; j <= radius; j++ {
		dx, dy := -j-1, -j
		blocked := false
		var newStart float64
		for dx <= 0 {
			dx++
			p := Point{o.X + dx*xx + dy*xy, o.Y + dx*yx + dy*yy}
			lSlope := (float64(dx) - 0.5) / (float64(dy) + 0.5)
			rSlope := (float64(dx) + 0.5) / (float64(dy) - 0.5)
			if start < rSlope {
				continue
			} else if end > lSlope {
				break
			}
			if dx*dx+dy*dy <= r2 {
				visit(p)
			}
			if blocked {
				if opaque(p) {
					newStart = rSlope
					continue
				}
				blocked = false
				start = newStart
			} else if opaque(p) && j < radius {
				blocked = true
				castLight(o, radius, j+1, start, lSlope, xx, xy, yx, yy, opaque, visit)
				newStart = rSlope
			}
		}
		if blocked {
			break
		}
	}
}

// FloodFill returns every cell reachable from start through passable
// cells by four-way moves, start included when passable.
func FloodFill(w, h int, start Point, passable func(Point) bool) []Point {
	if start.X < 0 || start.Y < 0 || start.X >= w || start.Y >= h || !passable(start) {
		return nil
	}
	seen := make([]bool, w*h)
	seen[start.Y*w+start.X] = true
	out := []Point{start}
	for i := 0; i < len(out); i++ {
		for _, d := range Dirs4 {
			n := out[i].Add(d)
			if n.X < 0 || n.Y < 0 || n.X >= w || n.Y >= h || seen[n.Y*w+n.X] || !passable(n) {
				continue
			}
			seen[n.Y*w+n.X] = true
			out = append(out, n)
		}
	}
	return out
}
