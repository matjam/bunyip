---
title: Pathfinding
example: pathfinding
summary: A*, Dijkstra maps, field of view, flood fill and lines on a grid painted with the mouse, saved and loaded through the save package
---

This program puts five of the [grid](../pkg/grid.html) package's
algorithms on one screen at once. Walls are painted with the mouse; A*
routes from the start to the cell under the pointer; a Dijkstra map
shades every reachable cell by its distance from the start; field of
view brightens what the start can see; a flood fill counts the connected
region; and a Bresenham line is drawn from the start to the goal. D
switches between four-way and eight-way movement and R rolls a new map.

The map is saved and loaded through [save](../pkg/save.html), and the
random scattering comes from [rng](../pkg/rng.html), seeded so every run
produces the same map. Both are covered by
[the game services guide](../guides/services.html).

Everything is recomputed from scratch every frame, which is honest for a
grid of 40 by 24 and wrong for a large map. The package's doc comments
say what to keep instead: a `grid.Pathfinder` for repeated searches and a
`grid.Vision` for repeated casts, both of which allocate nothing per
call.

Run it:

```bash
go run ./examples/pathfinding -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`. A left click paints a
wall, a right click erases one, a middle click moves the start, D toggles
diagonal moves, R rolls a new map, F5 saves and F9 loads.

## Package, constants and state

The map is 40 by 24 cells of 24 view units each, which fixes the window
size in `main`. `mapFile` is the shape written to disk: a version number
so a later format can be recognised, the wall cells, and the start.

The grid itself is `*grid.Grid[bool]`, a dense two-dimensional slice with
bounds checking. `grid.Point` is an integer cell coordinate, which is a
different thing from the float view units the drawing uses.

```go
// Command pathfinding shows the grid package: paint walls with the left
// mouse button (right erases), and watch A* route from the start to the
// cell under the pointer, a Dijkstra map colour every reachable cell by
// distance, field of view light what the start can see, and flood fill
// count the connected region. D toggles diagonal moves, R rolls a new
// random map, F5 saves the map and F9 loads it (through the save package).
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/grid"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/rng"
	"github.com/matjam/bunyip/save"
)

const (
	cols, rows = 40, 24
	cell       = 24
)

type mapFile struct {
	Version int
	Walls   []bool
	Start   grid.Point
}

type game struct {
	seconds float64
	shot    string

	font     *gfx.Font
	walls    *grid.Grid[bool]
	start    grid.Point
	diagonal bool
	random   *rng.Rand
	store    *save.Store
	status   string
	shotDone bool
}
```

## Init: the store and the first map

`save.Open` returns a store rooted in the operating system's own place
for a game's saved data, named for the string it is given.
`rng.New(42)` seeds a generator explicitly, so the map is the same on
every run and a screenshot of it is reproducible; a game that wants a
different world each time seeds from the clock instead.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	if g.store, err = save.Open("bunyip-pathfinding"); err != nil {
		return err
	}
	g.random = rng.New(42)
	g.walls = grid.New[bool](cols, rows)
	g.start = grid.Point{X: 3, Y: 3}
	g.roll()
	g.status = "Left-click paints walls, right-click erases. D diagonal, R new map, F5 save, F9 load."
	return nil
}
```

```go
func (g *game) Shutdown(ctx *bunyip.Context) { g.font.Destroy() }
```

`roll` scatters single walls with an 18 percent chance each and then
draws six longer barriers, one cell at a time in a random direction, to
make dead ends the search has to work around. `Set` outside the grid is
ignored, so the barrier loop does not have to check the edges. The last
line clears the start cell, because a start inside a wall has no path
anywhere.

```go
// roll scatters walls and a few longer barriers.
func (g *game) roll() {
	g.walls.Fill(false)
	for y := range rows {
		for x := range cols {
			g.walls.Set(x, y, g.random.Chance(0.18))
		}
	}
	for range 6 {
		x, y := g.random.Intn(cols), g.random.Intn(rows)
		for i := range 8 {
			if g.random.Chance(0.5) {
				g.walls.Set(x+i, y, true)
			} else {
				g.walls.Set(x, y+i, true)
			}
		}
	}
	g.walls.Set(g.start.X, g.start.Y, false)
}
```

## The cost function

Every search takes a `grid.Cost`, a function from one cell to a
neighbour. Returning `grid.Blocked`, which is positive infinity, marks a
move as impossible; that is how walls reach the search, rather than the
search knowing about the wall grid. A diagonal step costs the square root
of two so that eight-way paths measure true distance instead of counting
diagonal moves as cheap.

```go
func (g *game) cost(from, to grid.Point) float32 {
	if g.walls.At(to.X, to.Y) {
		return grid.Blocked
	}
	if from.X != to.X && from.Y != to.Y {
		return math.Sqrt2
	}
	return 1
}
```

## Update: painting and saving

`Update` runs at the fixed step. The mouse position comes back in view
units, and integer division by the cell size turns it into a cell. Walls
are painted while the button is held rather than on the press edge, so
dragging paints a line, which is why the test is `MouseDown` rather than
`MousePressed`.

`store.Write` encodes the value and writes it atomically under the name
given; `store.Read` decodes it back. The length check after a load is the
minimum a save format needs: a file written when the map was a different
size is ignored rather than trusted.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	if in.KeyPressed(input.KeyD) {
		g.diagonal = !g.diagonal
	}
	if in.KeyPressed(input.KeyR) {
		g.roll()
	}
	if in.KeyPressed(input.KeyF5) {
		if err := g.store.Write("map", mapFile{Version: 1, Walls: g.walls.Cells, Start: g.start}); err != nil {
			g.status = err.Error()
		} else {
			g.status = "Saved to " + g.store.Path()
		}
	}
	if in.KeyPressed(input.KeyF9) {
		var m mapFile
		if err := g.store.Read("map", &m); err != nil {
			g.status = "Load failed: " + err.Error()
		} else if len(m.Walls) == len(g.walls.Cells) {
			copy(g.walls.Cells, m.Walls)
			g.start = m.Start
			g.status = "Loaded the saved map"
		}
	}
	mx, my := in.Mouse()
	p := grid.Point{X: int(mx) / cell, Y: int(my) / cell}
	if g.walls.In(p.X, p.Y) && p != g.start {
		if in.MouseDown(input.MouseLeft) {
			g.walls.Set(p.X, p.Y, true)
		}
		if in.MouseDown(input.MouseRight) {
			g.walls.Set(p.X, p.Y, false)
		}
	}
	if in.MouseDown(input.MouseMiddle) && g.walls.In(p.X, p.Y) && !g.walls.At(p.X, p.Y) {
		g.start = p
	}
	return nil
}
```

## Draw: running the queries

A timed run pins the goal to a fixed cell so the screenshot shows a
path rather than whatever the pointer happened to be over.

`grid.Dijkstra` floods out from a list of sources and returns the cost to
reach every cell, with `grid.Blocked` where there is none.
`grid.FloodFill` returns the cells connected to the start under a
passability test. `grid.FOV` casts shadows from the start out to a radius
of nine and calls the visit function for every cell it can see; its
`opaque` function is asked about cells off the map as well, which is why
it treats out of bounds as blocking sight. The loop over the distance map
finds the farthest cost, which the colours are scaled against.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	mx, my := ctx.Input.Mouse()
	goal := grid.Point{X: int(mx) / cell, Y: int(my) / cell}
	if g.seconds > 0 { // a fixed goal makes screenshots meaningful
		goal = grid.Point{X: cols - 4, Y: rows - 4}
	}
	dist := grid.Dijkstra(cols, rows, []grid.Point{g.start}, g.diagonal, g.cost)
	region := grid.FloodFill(cols, rows, g.start, func(p grid.Point) bool { return !g.walls.At(p.X, p.Y) })
	visible := map[grid.Point]bool{}
	grid.FOV(g.start, 9, func(p grid.Point) bool { return !g.walls.In(p.X, p.Y) || g.walls.At(p.X, p.Y) }, func(p grid.Point) { visible[p] = true })
	var far float32 = 1
	dist.Each(func(x, y int, d float32) {
		if d != grid.Blocked {
			far = max(far, d)
		}
	})
```

## Draw: the cells and the path

Each cell is a filled rectangle, inset by a unit so the grid lines show
as gaps. Walls and unreachable cells are flat colours; a reachable cell
mixes a colour from its distance, and a visible cell is brightened.
`gfx.Color` here is written out directly rather than through `gfx.RGB`,
because the components are computed rather than sampled from an image:
the fields are linear values from zero to one, which is what the
renderer works in, while `gfx.RGB` converts from sRGB bytes.

`grid.AStar` returns the path including both endpoints, or nil when there
is none. `grid.Line` returns the cells a straight line passes through,
which a game uses for a thrown weapon or a line of sight check.
`gfx.RGBA` takes an alpha byte, so the line is drawn translucent over the
cells.

```go
	// Cells: walls dark, reachable cells shaded by distance, visible cells brighter.
	g.walls.Each(func(x, y int, wall bool) {
		var c gfx.Color
		switch {
		case wall:
			c = gfx.RGB(40, 40, 55)
		case dist.At(x, y) == grid.Blocked:
			c = gfx.RGB(25, 25, 30)
		default:
			t := dist.At(x, y) / far
			c = gfx.Color{R: 0.15 + 0.7*t, G: 0.35 - 0.2*t, B: 0.55 - 0.45*t, A: 1}
		}
		if visible[grid.Point{X: x, Y: y}] {
			c.R, c.G, c.B = min(1, c.R+0.25), min(1, c.G+0.25), min(1, c.B+0.2)
		}
		gr.FillRect(float32(x*cell)+1, float32(y*cell)+1, cell-2, cell-2, c)
	})
	path := grid.AStar(cols, rows, g.start, goal, g.diagonal, g.cost)
	for _, p := range path {
		gr.FillRect(float32(p.X*cell)+7, float32(p.Y*cell)+7, cell-14, cell-14, gfx.RGB(255, 235, 90))
	}
	for _, p := range grid.Line(g.start, goal) {
		gr.FillRect(float32(p.X*cell)+10, float32(p.Y*cell)+10, 4, 4, gfx.RGBA(255, 255, 255, 120))
	}
	gr.FillRect(float32(g.start.X*cell)+4, float32(g.start.Y*cell)+4, cell-8, cell-8, gfx.RGB(80, 255, 120))
	gr.FillRect(float32(goal.X*cell)+4, float32(goal.Y*cell)+4, cell-8, cell-8, gfx.RGB(255, 90, 90))
```

## Draw: the status lines

The two lines under the grid report what the searches found. The path
length is one less than the number of cells, because both endpoints are
included, and it is clamped at zero for the frame where there is no path
at all.

```go
	y := float32(rows*cell) + 8
	gr.DrawText(g.font, g.status, 12, y, gfx.RGB(220, 220, 230))
	summary := fmt.Sprintf("Path %d steps (%s); %d cells reachable; %d visible; middle-click moves the start.",
		max(len(path)-1, 0), map[bool]string{true: "8-way", false: "4-way"}[g.diagonal], len(region), len(visible))
	if path == nil {
		summary = "No path to the goal. " + summary
	}
	gr.DrawText(g.font, summary, 12, y+22, gfx.RGB(180, 180, 200))
	return nil
}
```

## main

The window is exactly the grid plus 60 units for the status lines. It is
not resizable, which is the zero value of `Resizable`.

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip pathfinding", Width: cols * cell, Height: rows*cell + 60, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pathfinding:", err)
		os.Exit(1)
	}
}
```

## What to try

- Give `cost` a third terrain that costs 3 instead of 1 and watch A*
  route around it while the Dijkstra shading shows why.
- Raise the field of view radius in `Draw` and see the shadowcasting cost
  nothing noticeable at this size.
- Keep a `grid.Pathfinder` on the game type and call its `AStar` method
  from `Draw` instead of the package function, which is what a game with
  many actors does.
- Use `AStarWithMinCost` with a final argument of `1` to speed up the
  search: every traversable move in this example costs at least one.
  If cheaper terrain is added, lower that bound to its minimum step
  cost; use zero when the minimum is unknown or zero-cost moves exist.
- Move the searches into `Update` and keep their results on the game, so
  they run at the fixed step rather than once per frame.
- Add a `Version` check to the load in `Update` and refuse a file from a
  future version.
