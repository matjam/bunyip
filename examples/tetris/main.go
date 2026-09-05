// Command tetris is the game the Tetris guide builds on the entity
// component system: settled blocks are entities, the falling piece is an
// entity, and systems for input, gravity, locking and effects run each
// update over resources such as the score and the board. Left and right
// move, Up rotates, Down soft-drops, Space hard-drops, R restarts,
// Escape quits.
package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/rng"
	"github.com/matjam/bunyip/timer"
	"github.com/matjam/bunyip/tween"
	"github.com/matjam/bunyip/ui"
)

// The board is ten cells wide and twenty tall.
const (
	cols, rows = 10, 20
	cell       = 28
)

// Piece shapes as strings, one rotation each; the game rotates them.
var shapes = []struct {
	name  string
	cells []string
	color gfx.Color
}{
	{"I", []string{"....", "####", "....", "...."}, gfx.Hex(0x5BC0EB)},
	{"O", []string{"##", "##"}, gfx.Hex(0xFDE74C)},
	{"T", []string{".#.", "###", "..."}, gfx.Hex(0x9B5DE5)},
	{"S", []string{".##", "##.", "..."}, gfx.Hex(0x9BC53D)},
	{"Z", []string{"##.", ".##", "..."}, gfx.Hex(0xE55934)},
	{"J", []string{"#..", "###", "..."}, gfx.Hex(0x3A86FF)},
	{"L", []string{"..#", "###", "..."}, gfx.Hex(0xFA7921)},
}

// Components.

// Cell is one settled block on the board.
type Cell struct{ X, Y, Kind int }

// Falling is the piece the player controls; there is one at a time.
type Falling struct {
	Kind  int
	Cells [][]bool // Cells[y][x]
	X, Y  int
}

// Resources: singletons the systems share.

// Board is the occupancy grid rebuilt from Cell entities each update.
type Board struct{ Full [rows][cols]bool }

// Score is what the panel shows.
type Score struct {
	Points, Lines int
	Over          bool
}

// Controls is this update's input, filled in from ctx.Input.
type Controls struct{ Left, Right, Rotate, Down, Drop bool }

// Clock drives gravity on game time.
type Clock struct {
	Timers timer.Scheduler
	Drop   timer.Handle
}

// Bag deals the next piece.
type Bag struct {
	Random *rng.Rand
	Next   int
}

// Events: how systems tell each other what happened.

// Locked says a piece settled without clearing lines.
type Locked struct{}

// Cleared says lines were removed.
type Cleared struct{ Rows int }

// newFalling builds a piece at the top of the board.
func newFalling(kind int) Falling {
	src := shapes[kind].cells
	p := Falling{Kind: kind, X: cols/2 - len(src[0])/2}
	for _, row := range src {
		var r []bool
		for _, ch := range row {
			r = append(r, ch == '#')
		}
		p.Cells = append(p.Cells, r)
	}
	return p
}

// rotated returns the piece turned clockwise.
func (p Falling) rotated() Falling {
	n := len(p.Cells)
	out := Falling{Kind: p.Kind, X: p.X, Y: p.Y, Cells: make([][]bool, n)}
	for y := range n {
		out.Cells[y] = make([]bool, n)
		for x := range n {
			out.Cells[y][x] = p.Cells[n-1-x][y]
		}
	}
	return out
}

// fits reports whether the piece overlaps walls, floor or settled cells.
func fits(b *Board, p Falling) bool {
	for y, row := range p.Cells {
		for x, on := range row {
			if !on {
				continue
			}
			bx, by := p.X+x, p.Y+y
			if bx < 0 || bx >= cols || by >= rows || (by >= 0 && b.Full[by][bx]) {
				return false
			}
		}
	}
	return true
}

type game struct {
	seconds float64
	shot    string

	world   *ecs.World
	cells   *ecs.Query1[Cell]
	falling *ecs.Query1[Falling]

	font     *gfx.Font
	ui       *ui.Context
	lock     *audio.Sound
	clear    *audio.Sound
	flash    *tween.Tween
	shotDone bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 16, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	if g.lock, err = ctx.Audio.NewSound(audio.Sine(220, 0.06, ctx.Audio.Rate())); err != nil {
		return err
	}
	if g.clear, err = ctx.Audio.NewSound(audio.Sine(660, 0.25, ctx.Audio.Rate())); err != nil {
		return err
	}
	w := ecs.NewWorld()
	g.world = w
	g.cells = w.Query1[Cell]()
	g.falling = w.Query1[Falling]()
	// Systems run in this order every update.
	w.AddSystem("board", boardSystem)
	w.AddSystem("input", inputSystem)
	w.AddSystem("gravity", gravitySystem)
	w.AddSystem("effects", g.effectsSystem(ctx.Audio))
	g.restart()
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) { g.font.Destroy() }

// restart clears the board and deals the first piece.
func (g *game) restart() {
	w := g.world
	for _, e := range w.Entities() {
		w.Despawn(e)
	}
	w.SetResource(Board{})
	w.SetResource(Score{})
	w.SetResource(Controls{})
	bag := Bag{Random: rng.New(7)}
	bag.Next = bag.Random.Intn(len(shapes))
	w.SetResource(bag)
	var clock Clock
	clock.Drop = clock.Timers.Every(0.6, func() { drop(w) })
	w.SetResource(clock)
	spawnPiece(w)
}

// spawnPiece deals the next piece; if it cannot be placed the game is over.
func spawnPiece(w *ecs.World) {
	bag := w.Resource[Bag]()
	p := newFalling(bag.Next)
	bag.Next = bag.Random.Intn(len(shapes))
	if !fits(w.Resource[Board](), p) {
		w.Resource[Score]().Over = true
	}
	w.SpawnWith(p)
}

// boardSystem rebuilds the occupancy grid from the Cell entities, so the
// other systems test collisions against a plain array.
func boardSystem(w *ecs.World, dt float64) {
	b := w.Resource[Board]()
	b.Full = [rows][cols]bool{}
	w.Each(func(e ecs.Entity, c *Cell) {
		if c.Y >= 0 {
			b.Full[c.Y][c.X] = true
		}
	})
}

// try moves the piece to the candidate if it fits.
func try(w *ecs.World, p *Falling, candidate Falling) bool {
	if !fits(w.Resource[Board](), candidate) {
		return false
	}
	*p = candidate
	return true
}

// inputSystem applies the Controls resource to the falling piece.
func inputSystem(w *ecs.World, dt float64) {
	if w.Resource[Score]().Over {
		return
	}
	in := w.Resource[Controls]()
	_, p, ok := w.Query1[Falling]().First()
	if !ok {
		return
	}
	if in.Left {
		c := *p
		c.X--
		try(w, p, c)
	}
	if in.Right {
		c := *p
		c.X++
		try(w, p, c)
	}
	if in.Rotate {
		r := p.rotated()
		// Wall kick: nudge sideways when the rotation would clip a wall.
		for _, dx := range []int{0, -1, 1, -2, 2} {
			r.X = p.X + dx
			if try(w, p, r) {
				break
			}
		}
	}
	if in.Down {
		drop(w)
	}
	if in.Drop {
		for {
			c := *p
			c.Y++
			if !try(w, p, c) {
				break
			}
		}
		lockPiece(w)
	}
}

// gravitySystem advances the clock; its timer calls drop.
func gravitySystem(w *ecs.World, dt float64) {
	if w.Resource[Score]().Over {
		return
	}
	w.Resource[Clock]().Timers.Update(dt)
}

// drop moves the piece down one row, or locks it when it cannot fall.
func drop(w *ecs.World) {
	_, p, ok := w.Query1[Falling]().First()
	if !ok {
		return
	}
	c := *p
	c.Y++
	if !try(w, p, c) {
		lockPiece(w)
	}
}

// lockPiece turns the falling piece into Cell entities, clears full
// lines and emits an event saying what happened.
func lockPiece(w *ecs.World) {
	e, p, ok := w.Query1[Falling]().First()
	if !ok {
		return
	}
	for y, row := range p.Cells {
		for x, on := range row {
			if on && p.Y+y >= 0 {
				w.SpawnWith(Cell{X: p.X + x, Y: p.Y + y, Kind: p.Kind})
			}
		}
	}
	w.Despawn(e)
	boardSystem(w, 0)
	b := w.Resource[Board]()
	cleared := 0
	for y := rows - 1; y >= 0; y-- {
		full := true
		for x := range cols {
			full = full && b.Full[y][x]
		}
		if !full {
			continue
		}
		// Despawn this row's cells and let everything above fall one row.
		// Despawning the visited entity inside a query is safe.
		w.Each(func(e ecs.Entity, c *Cell) {
			switch {
			case c.Y == y:
				w.Despawn(e)
			case c.Y < y:
				c.Y++
			}
		})
		boardSystem(w, 0)
		cleared++
		y++ // re-check the row that just fell into this slot
	}
	if cleared > 0 {
		s := w.Resource[Score]()
		s.Lines += cleared
		s.Points += []int{0, 100, 300, 500, 800}[cleared]
		w.Emit(Cleared{Rows: cleared})
	} else {
		w.Emit(Locked{})
	}
	spawnPiece(w)
}

// effectsSystem turns events into sound and the line-clear flash.
func (g *game) effectsSystem(mixer *audio.Mixer) ecs.System {
	return func(w *ecs.World, dt float64) {
		for range w.Events[Locked]() {
			mixer.Play(g.lock, audio.PlayOptions{Volume: 0.4})
		}
		for _, ev := range w.Events[Cleared]() {
			mixer.Play(g.clear, audio.PlayOptions{Volume: 0.5, Pitch: 1 + 0.2*float32(ev.Rows)})
			g.flash = tween.New(1, 0, 0.4, tween.OutQuad)
		}
		if g.flash != nil {
			if g.flash.Update(float32(dt)); g.flash.Done() {
				g.flash = nil
			}
		}
	}
}

func (g *game) Update(ctx *bunyip.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	if in.KeyPressed(input.KeyR) {
		g.restart()
	}
	// Input becomes a resource so the input system stays a pure function
	// of the world.
	*g.world.Resource[Controls]() = Controls{
		Left: in.KeyPressed(input.KeyLeft), Right: in.KeyPressed(input.KeyRight), Rotate: in.KeyPressed(input.KeyUp),
		Down: in.KeyPressed(input.KeyDown), Drop: in.KeyPressed(input.KeySpace),
	}
	g.world.Update(ctx.Delta)
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	w := g.world
	ox, oy := (ctx.Width-cols*cell)/2-80, (ctx.Height-rows*cell)/2
	gr.FillRect(ox-4, oy-4, cols*cell+8, rows*cell+8, gfx.RGB(40, 42, 56))
	gr.FillRect(ox, oy, cols*cell, rows*cell, gfx.RGB(16, 16, 24))
	drawCell := func(x, y, kind int, alpha float32) {
		c := shapes[kind].color
		c.A = alpha
		gr.FillRect(ox+float32(x*cell)+1, oy+float32(y*cell)+1, cell-2, cell-2, c)
	}
	g.cells.Each(func(e ecs.Entity, c *Cell) {
		if c.Y >= 0 {
			drawCell(c.X, c.Y, c.Kind, 1)
		}
	})
	if _, p, ok := g.falling.First(); ok {
		// The ghost shows where the piece will land.
		ghost := *p
		for fits(w.Resource[Board](), ghost) {
			ghost.Y++
		}
		ghost.Y--
		for _, layer := range []struct {
			piece Falling
			alpha float32
		}{{ghost, 0.25}, {*p, 1}} {
			for y, row := range layer.piece.Cells {
				for x, on := range row {
					if on && layer.piece.Y+y >= 0 {
						drawCell(layer.piece.X+x, layer.piece.Y+y, layer.piece.Kind, layer.alpha)
					}
				}
			}
		}
	}
	if g.flash != nil {
		gr.FillRect(ox, oy, cols*cell, rows*cell, gfx.Color{R: 1, G: 1, B: 1, A: 0.35 * g.flash.Value()})
	}
	score := w.Resource[Score]()
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Tetris", ui.Rect{X: ox + cols*cell + 24, Y: oy, W: 200, H: 300}, func() {
			u.Label(fmt.Sprintf("Score %d", score.Points))
			u.Label(fmt.Sprintf("Lines %d", score.Lines))
			u.Label("Next: " + shapes[w.Resource[Bag]().Next].name)
			u.Label(fmt.Sprintf("%d entities", w.Len()))
			u.Separator()
			if score.Over {
				u.Label("Game over")
			}
			if u.Button("Restart (R)") {
				g.restart()
			}
			u.Label("Arrows move and rotate, Space drops.")
		})
	})
	return nil
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip Tetris", Width: 640, Height: 640, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "tetris:", err)
		os.Exit(1)
	}
}
