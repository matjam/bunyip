// Package grid provides the tile-map helpers roguelikes and strategy
// games need: a generic cell grid, A* and Dijkstra pathfinding,
// Bresenham lines, shadowcasting field of view and flood fill.
//
// Grid[T] is a rectangular array of cells with bounds checking (In, At,
// Set, Fill, Each). The algorithms take a cost or passability function
// over points, so they work on any map representation, not only a Grid.
// AStar finds one path with four or eight-way movement and a cost per
// step. Dijkstra fills a map of distances from many sources at once, and
// units descend that map with Downhill, which moves many units towards
// the player for the cost of one fill. FOV computes which cells are
// visible from a point against an opacity function. Line walks the cells
// between two points for projectiles and line of sight. FloodFill
// collects a connected region.
//
// The package functions allocate only their result. To search or cast
// sight every frame without allocating at all, keep a Pathfinder for the
// map and a Vision for the viewer, and call their methods: Pathfinder
// appends a path to a slice the caller owns and fills a Dijkstra map the
// caller already has, and Vision reuses the scratch space a field of
// view cast needs. Both hold scratch space rather than results, so give
// each goroutine its own.
//
// Points are integer cell coordinates with +Y down, matching the
// renderer's 2D space and Tilemap; nothing here depends on gfx.
package grid

import (
	"math"
	"sync"
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
// diagonal set, eight-way moves are considered. The scratch space the
// search needs comes from a shared pool, so only the path is allocated.
// A game that searches every frame keeps a Pathfinder instead and calls
// its AStar method, which allocates nothing at all.
func AStar(w, h int, start, goal Point, diagonal bool, cost Cost) []Point {
	pf := getPathfinder(w, h)
	path, ok := pf.AStar(nil, start, goal, diagonal, cost)
	putPathfinder(pf)
	if !ok {
		return nil
	}
	return path
}

// Dijkstra returns the cost from the nearest source to every cell, with
// Blocked for cells that cannot be reached. Roguelikes use the result as
// a Dijkstra map: monsters walk downhill toward the sources. Only the
// returned map is allocated; to reuse a map across frames, keep a
// Pathfinder and call DijkstraInto.
func Dijkstra(w, h int, sources []Point, diagonal bool, cost Cost) *Grid[float32] {
	dist := New[float32](w, h)
	pf := getPathfinder(w, h)
	pf.DijkstraInto(dist, sources, diagonal, cost)
	putPathfinder(pf)
	return dist
}

// Pathfinder holds the scratch space a search needs so that repeated
// searches on a map of one size allocate nothing. Make one per map with
// NewPathfinder and keep it for as long as the map lasts. A Pathfinder
// is not safe for concurrent use; give each goroutine its own.
type Pathfinder struct {
	w, h int
	gen  uint32
	seen []uint32 // cells reached this search, stamped with gen
	shut []uint32 // cells finished this search, stamped with gen
	g    []float32
	came []int32
	open minHeap
}

// NewPathfinder makes a pathfinder for a w by h map.
func NewPathfinder(w, h int) *Pathfinder {
	p := &Pathfinder{}
	p.Resize(w, h)
	return p
}

// Size is the map size the pathfinder is set up for.
func (p *Pathfinder) Size() (w, h int) { return p.w, p.h }

// Resize points the pathfinder at a map of another size, growing its
// scratch space when it has to. Call it when the level changes rather
// than making a new pathfinder.
func (p *Pathfinder) Resize(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	p.w, p.h = w, h
	if n := w * h; len(p.seen) < n {
		p.seen = make([]uint32, n)
		p.shut = make([]uint32, n)
		p.g = make([]float32, n)
		p.came = make([]int32, n)
		p.gen = 0
	}
}

// begin stamps a new search. Cells keep the stamp of the search that
// last touched them, so nothing has to be cleared between searches.
func (p *Pathfinder) begin() {
	p.gen++
	if p.gen == 0 { // the counter wrapped; every old stamp is stale
		clear(p.seen)
		clear(p.shut)
		p.gen = 1
	}
	p.open = p.open[:0]
}

func (p *Pathfinder) in(q Point) bool {
	return q.X >= 0 && q.Y >= 0 && q.X < p.w && q.Y < p.h
}

// AStar finds the cheapest path from start to goal, appends it to out
// including both endpoints, and reports true. With diagonal set,
// eight-way moves are considered. When there is no path it returns out
// unchanged and false, so the caller keeps its buffer. Pass out as
// buf[:0] to search every frame without allocating.
func (p *Pathfinder) AStar(out []Point, start, goal Point, diagonal bool, cost Cost) ([]Point, bool) {
	if !p.in(start) || !p.in(goal) {
		return out, false
	}
	if start == goal {
		return append(out, start), true
	}
	dirs := Dirs4
	if diagonal {
		dirs = Dirs8
	}
	w, h := p.w, p.h
	p.begin()
	si := start.Y*w + start.X
	p.g[si] = 0
	p.came[si] = -1
	p.seen[si] = p.gen
	p.open.push(item{i: int32(si), f: heuristic(start, goal, diagonal)})
	for len(p.open) > 0 {
		cur := p.open.pop()
		ci := int(cur.i)
		if p.shut[ci] == p.gen {
			continue
		}
		cy := ci / w
		cp := Point{X: ci - cy*w, Y: cy}
		if cp == goal {
			first := len(out)
			for i := ci; i >= 0; i = int(p.came[i]) {
				y := i / w
				out = append(out, Point{X: i - y*w, Y: y})
			}
			for i, j := first, len(out)-1; i < j; i, j = i+1, j-1 {
				out[i], out[j] = out[j], out[i]
			}
			return out, true
		}
		p.shut[ci] = p.gen
		gc := p.g[ci]
		for _, d := range dirs {
			n := Point{X: cp.X + d.X, Y: cp.Y + d.Y}
			if n.X < 0 || n.Y < 0 || n.X >= w || n.Y >= h {
				continue
			}
			ni := n.Y*w + n.X
			if p.shut[ni] == p.gen {
				continue
			}
			c := cost(cp, n)
			if c >= Blocked || c < 0 {
				continue
			}
			ng := gc + c
			if p.seen[ni] != p.gen || ng < p.g[ni] {
				p.seen[ni] = p.gen
				p.g[ni] = ng
				p.came[ni] = int32(ci)
				p.open.push(item{i: int32(ni), f: ng + heuristic(n, goal, diagonal)})
			}
		}
	}
	return out, false
}

func heuristic(q, goal Point, diagonal bool) float32 {
	dx, dy := abs(q.X-goal.X), abs(q.Y-goal.Y)
	if diagonal {
		return float32(max(dx, dy)) + (math.Sqrt2-1)*float32(min(dx, dy))
	}
	return float32(dx + dy)
}

// DijkstraInto fills dist with the cost from the nearest source to
// every cell, Blocked where a cell cannot be reached, reusing the map
// the caller already has. dist must be the pathfinder's own width and
// height; a map of another size is left alone.
func (p *Pathfinder) DijkstraInto(dist *Grid[float32], sources []Point, diagonal bool, cost Cost) {
	if dist == nil || dist.W != p.w || dist.H != p.h {
		return
	}
	dirs := Dirs4
	if diagonal {
		dirs = Dirs8
	}
	w, h := p.w, p.h
	p.begin()
	dist.Fill(Blocked)
	for _, s := range sources {
		if p.in(s) {
			dist.Cells[s.Y*w+s.X] = 0
			p.open.push(item{i: int32(s.Y*w + s.X), f: 0})
		}
	}
	for len(p.open) > 0 {
		cur := p.open.pop()
		ci := int(cur.i)
		if cur.f > dist.Cells[ci] {
			continue
		}
		cy := ci / w
		cp := Point{X: ci - cy*w, Y: cy}
		for _, d := range dirs {
			n := Point{X: cp.X + d.X, Y: cp.Y + d.Y}
			if n.X < 0 || n.Y < 0 || n.X >= w || n.Y >= h {
				continue
			}
			c := cost(cp, n)
			if c >= Blocked || c < 0 {
				continue
			}
			ni := n.Y*w + n.X
			if nd := cur.f + c; nd < dist.Cells[ni] {
				dist.Cells[ni] = nd
				p.open.push(item{i: int32(ni), f: nd})
			}
		}
	}
}

// pathfinders lends scratch space to the package-level AStar and
// Dijkstra so those stay safe to call from any goroutine.
var pathfinders sync.Pool

func getPathfinder(w, h int) *Pathfinder {
	p, _ := pathfinders.Get().(*Pathfinder)
	if p == nil {
		p = &Pathfinder{}
	}
	p.Resize(w, h)
	return p
}

func putPathfinder(p *Pathfinder) { pathfinders.Put(p) }

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

// item is one entry in the open set: a cell index and its priority.
// The cell is held as an index rather than a Point so that an item is
// eight bytes, which keeps three times as much of the open set in cache
// as a Point and a float would.
type item struct {
	i int32
	f float32
}

// minHeap is a four-ary heap of items ordered by f. It is typed rather
// than built on container/heap, which would box every item into an
// interface and allocate once per push. Four children per node halve
// the depth of a binary heap, so the sift after a pop touches half as
// many cache lines.
type minHeap []item

func (h *minHeap) push(it item) {
	s := append(*h, it)
	i := len(s) - 1
	for i > 0 {
		parent := (i - 1) / 4
		if s[parent].f <= it.f {
			break
		}
		s[i] = s[parent]
		i = parent
	}
	s[i] = it
	*h = s
}

// pop removes and returns the smallest item; the heap must not be empty.
func (h *minHeap) pop() item {
	s := *h
	top := s[0]
	n := len(s) - 1
	last := s[n]
	s = s[:n]
	*h = s
	if n == 0 {
		return top
	}
	i := 0
	for {
		first := 4*i + 1
		if first >= n {
			break
		}
		end := min(first+4, n)
		small := first
		for c := first + 1; c < end; c++ {
			if s[c].f < s[small].f {
				small = c
			}
		}
		if last.f <= s[small].f {
			break
		}
		s[i] = s[small]
		i = small
	}
	s[i] = last
	return top
}

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
// the map. The scratch space comes from a shared pool, so a cast
// allocates nothing; a game that casts every frame can keep a Vision
// instead and call its FOV method.
func FOV(origin Point, radius int, opaque func(Point) bool, visit func(Point)) {
	v, _ := visions.Get().(*Vision)
	if v == nil {
		v = &Vision{}
	}
	v.FOV(origin, radius, opaque, visit)
	visions.Put(v)
}

var visions sync.Pool

// Vision holds the scratch space a field of view cast needs so that
// repeated casts allocate nothing. The zero value is ready to use, and
// one Vision serves any radius. A Vision is not safe for concurrent
// use; give each goroutine its own.
type Vision struct {
	gen    uint32
	seen   []uint32 // cells already visited this cast, stamped with gen
	side   int      // width of the radius box, 2*radius+1
	radius int
	origin Point
	opaque func(Point) bool
	visit  func(Point)
}

// FOV computes which cells are visible from origin within radius,
// calling visit once for each including the origin. It behaves exactly
// as the package-level FOV and reuses the caster's scratch space.
func (v *Vision) FOV(origin Point, radius int, opaque func(Point) bool, visit func(Point)) {
	if radius < 0 {
		radius = 0
	}
	side := 2*radius + 1
	if n := side * side; len(v.seen) < n {
		v.seen = make([]uint32, n)
		v.gen = 0
	}
	v.gen++
	if v.gen == 0 { // the counter wrapped; every old stamp is stale
		clear(v.seen)
		v.gen = 1
	}
	v.side, v.radius, v.origin = side, radius, origin
	v.opaque, v.visit = opaque, visit
	v.seen[radius*side+radius] = v.gen
	visit(origin)
	for oct := range 8 {
		v.cast(1, 1, 0, octants[0][oct], octants[1][oct], octants[2][oct], octants[3][oct])
	}
	v.opaque, v.visit = nil, nil
}

// once visits a cell the first time this cast reaches it. Every cell a
// cast reaches is inside the radius box, so the box indexes the stamps.
func (v *Vision) once(p Point) {
	dx, dy := p.X-v.origin.X+v.radius, p.Y-v.origin.Y+v.radius
	if dx < 0 || dy < 0 || dx >= v.side || dy >= v.side {
		return
	}
	i := dy*v.side + dx
	if v.seen[i] == v.gen {
		return
	}
	v.seen[i] = v.gen
	v.visit(p)
}

var octants = [4][8]int{
	{1, 0, 0, -1, -1, 0, 0, 1},
	{0, 1, -1, 0, 0, -1, 1, 0},
	{0, 1, 1, 0, 0, -1, -1, 0},
	{1, 0, 0, 1, -1, 0, 0, -1},
}

func (v *Vision) cast(row int, start, end float64, xx, xy, yx, yy int) {
	if start < end {
		return
	}
	o, radius := v.origin, v.radius
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
				v.once(p)
			}
			if blocked {
				if v.opaque(p) {
					newStart = rSlope
					continue
				}
				blocked = false
				start = newStart
			} else if v.opaque(p) && j < radius {
				blocked = true
				v.cast(j+1, start, lSlope, xx, xy, yx, yy)
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
