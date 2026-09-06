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

	"github.com/matjam/bunyip/engine"
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

func (g *game) Init(ctx *engine.Context) error {
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

func (g *game) Shutdown(ctx *engine.Context) { g.font.Destroy() }

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

func (g *game) cost(from, to grid.Point) float32 {
	if g.walls.At(to.X, to.Y) {
		return grid.Blocked
	}
	if from.X != to.X && from.Y != to.Y {
		return math.Sqrt2
	}
	return 1
}

func (g *game) Update(ctx *engine.Context) error {
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

func (g *game) Draw(ctx *engine.Context) error {
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

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := engine.Run(engine.Config{Title: "Bunyip pathfinding", Width: cols * cell, Height: rows*cell + 60, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pathfinding:", err)
		os.Exit(1)
	}
}
