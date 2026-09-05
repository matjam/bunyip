---
title: Tetris
example: tetris
summary: a complete game on the entity component system, with components, resources, events, systems, timers, tweens, sound and a panel
---

This is a finished game in one file. Settled blocks are entities, the
falling piece is an entity, and the rules are four systems that run over
resources: the occupancy grid, the score, this update's controls, the
gravity clock and the bag of upcoming pieces. Two events carry what
happened from the rules to the effects, which play a sound and start a
flash. The panel beside the board is immediate-mode interface rebuilt
every frame.

The guide [Building Tetris](../guides/tetris.html) writes this program
from an empty file and explains the design decisions as they are made;
this page reads the finished source in order. The packages are
[ecs](../pkg/ecs.html) for the world, [timer](../pkg/timer.html) for
gravity, [tween](../pkg/tween.html) for the flash,
[rng](../pkg/rng.html) for the bag, [audio](../pkg/audio.html) for the
two sounds, [ui](../pkg/ui.html) for the panel and
[gfx](../pkg/gfx.html) for the board.

Run it with:

```bash
go run ./examples/tetris -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`. Left and right move,
Up rotates, Down soft-drops, Space hard-drops, R restarts, Escape quits.

## The board and the pieces

The board is ten by twenty cells of 28 units each. The seven pieces are
written as strings, one rotation each, because the game rotates them at
run time rather than storing four rotations per piece. `gfx.Hex` takes
an sRGB hex value and returns a linear colour, which is why the colours
can be written the way a palette lists them.

```go
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
```

The I piece is written in a 4 by 4 grid and the O piece in a 2 by 2, so
each rotates about a sensible centre when the square grid is turned.

## Components, resources and events

The two components are the things there can be many of, or one of at a
time: a settled `Cell` and the `Falling` piece.

The five resources are the singletons. `Board` is the occupancy grid,
rebuilt each update from the cells so the rules test a plain array
rather than querying. `Score` is what the panel shows and whether the
game is over. `Controls` is this update's input, filled in from
`ctx.Input` before the world runs, which is what keeps the input system
a function of the world alone. `Clock` owns a timer scheduler and the
handle of the repeating gravity timer. `Bag` deals pieces from a seeded
source.

The two events let the rules report what happened without knowing that
sound exists. Later systems can read events in the update that emitted
them; they remain available until the next `World.Update` starts.

```go
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
```

## Piece geometry

`newFalling` turns a shape's strings into a grid of booleans and centres
it at the top of the board. `rotated` returns a new piece turned
clockwise by transposing and reversing the square grid, which is why
each shape is square. `fits` is the one collision test the whole game
uses: it rejects a piece that leaves the sides, passes the floor, or
overlaps a settled cell. Rows above the top of the board are allowed,
which is what lets a piece spawn partly off screen.

```go
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
```

## The game type and Init

Two cached queries, the font, the interface context, two sounds and the
flash tween.

`audio.Sine(freq, seconds, rate)` generates a tone at the mixer's own
sample rate, which `ctx.Audio.Rate()` reports, so the example needs no
sound files. `NewSound` uploads it to the mixer.

The four systems are registered in the order they must run: rebuild the
board, apply input, advance gravity, then turn the resulting events into
effects. Registration order is execution order, so the ordering is the
whole scheduling model.

```go
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
	g.cells = ecs.NewQuery1[Cell](w)
	g.falling = ecs.NewQuery1[Falling](w)
	// Systems run in this order every update.
	w.AddSystem("board", boardSystem)
	w.AddSystem("input", inputSystem)
	w.AddSystem("gravity", gravitySystem)
	w.AddSystem("effects", g.effectsSystem(ctx.Audio))
	g.restart()
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) { g.font.Destroy() }
```

The effects system is a method returning a closure, because it needs the
mixer and the game's sounds while still having the plain
`func(*ecs.World, float64)` shape a system must have.

## Restarting and spawning

`restart` despawns everything and resets every resource, so one function
covers both the first game and a later restart. The gravity timer is
registered with `Every(0.6, ...)`, which repeats until the scheduler is
dropped, and calls `drop` on the world.

`spawnPiece` deals the next piece, refills `Next`, and sets the game
over flag if the new piece does not fit, which is the losing condition.
The piece is spawned either way, so the board keeps rendering.

```go
// restart clears the board and deals the first piece.
func (g *game) restart() {
	w := g.world
	for _, e := range w.Entities() {
		w.Despawn(e)
	}
	ecs.SetResource(w, Board{})
	ecs.SetResource(w, Score{})
	ecs.SetResource(w, Controls{})
	bag := Bag{Random: rng.New(7)}
	bag.Next = bag.Random.Intn(len(shapes))
	ecs.SetResource(w, bag)
	var clock Clock
	clock.Drop = clock.Timers.Every(0.6, func() { drop(w) })
	ecs.SetResource(w, clock)
	spawnPiece(w)
}

// spawnPiece deals the next piece; if it cannot be placed the game is over.
func spawnPiece(w *ecs.World) {
	bag := ecs.Resource[Bag](w)
	p := newFalling(bag.Next)
	bag.Next = bag.Random.Intn(len(shapes))
	if !fits(ecs.Resource[Board](w), p) {
		ecs.Resource[Score](w).Over = true
	}
	w.SpawnWith(p)
}
```

`ecs.Resource[T](w)` returns a pointer to the resource, so writing
through it updates the world's copy.

## The board and input systems

`boardSystem` clears the grid and walks every `Cell` entity with
`ecs.Each`, which is the query-free form for a single component type.
Cells above the top of the board are skipped, since the array has no row
for them.

`inputSystem` reads the `Controls` resource and the one falling piece.
`Query1.First()` returns the entity, a pointer to its component, and
whether there was one. Every move goes through `try`, which applies the
candidate only if it fits, so the rules are one test in one place.

The rotation is a wall kick: the rotated piece is offered at five
horizontal offsets in turn, so a piece against a wall rotates by
stepping away from it instead of refusing.

The hard drop moves down until the move fails, then locks immediately.

```go
// boardSystem rebuilds the occupancy grid from the Cell entities, so the
// other systems test collisions against a plain array.
func boardSystem(w *ecs.World, dt float64) {
	b := ecs.Resource[Board](w)
	b.Full = [rows][cols]bool{}
	ecs.Each(w, func(e ecs.Entity, c *Cell) {
		if c.Y >= 0 {
			b.Full[c.Y][c.X] = true
		}
	})
}

// try moves the piece to the candidate if it fits.
func try(w *ecs.World, p *Falling, candidate Falling) bool {
	if !fits(ecs.Resource[Board](w), candidate) {
		return false
	}
	*p = candidate
	return true
}

// inputSystem applies the Controls resource to the falling piece.
func inputSystem(w *ecs.World, dt float64) {
	if ecs.Resource[Score](w).Over {
		return
	}
	in := ecs.Resource[Controls](w)
	_, p, ok := ecs.NewQuery1[Falling](w).First()
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
```

## Gravity and locking

`gravitySystem` does nothing but advance the scheduler, which calls
`drop` when the interval elapses. Running the timer on `dt` rather than
on wall-clock time means gravity stops when the game does.

`lockPiece` is the heart of the rules. The falling piece becomes `Cell`
entities, the piece entity is despawned, and the board is rebuilt at
once rather than waiting for the next update, because the line check
that follows needs it. Full rows are removed and everything above falls
one row, inside a single `ecs.Each` walk: despawning the visited entity
is safe, because a query walks its rows last to first. `y++` after a
clear re-checks the row that has just dropped into the same slot.

Finally an event is emitted saying what happened, and the next piece is
dealt.

```go
// gravitySystem advances the clock; its timer calls drop.
func gravitySystem(w *ecs.World, dt float64) {
	if ecs.Resource[Score](w).Over {
		return
	}
	ecs.Resource[Clock](w).Timers.Update(dt)
}

// drop moves the piece down one row, or locks it when it cannot fall.
func drop(w *ecs.World) {
	_, p, ok := ecs.NewQuery1[Falling](w).First()
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
	e, p, ok := ecs.NewQuery1[Falling](w).First()
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
	b := ecs.Resource[Board](w)
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
		ecs.Each(w, func(e ecs.Entity, c *Cell) {
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
		s := ecs.Resource[Score](w)
		s.Lines += cleared
		s.Points += []int{0, 100, 300, 500, 800}[cleared]
		ecs.Emit(w, Cleared{Rows: cleared})
	} else {
		ecs.Emit(w, Locked{})
	}
	spawnPiece(w)
}
```

## Effects

The effects system reads both event types with `ecs.Events[T](w)` and
turns them into sound and a flash. A cleared line raises the pitch by
the number of rows, so a four-line clear sounds different from a single.
The flash is a tween from 1 to 0 over 0.4 seconds, advanced here and
read in `Draw`, and set to nil when it finishes.

Because the events are the only link, the rules above know nothing about
audio: the sound could be removed by dropping this system.

```go
// effectsSystem turns events into sound and the line-clear flash.
func (g *game) effectsSystem(mixer *audio.Mixer) ecs.System {
	return func(w *ecs.World, dt float64) {
		for range ecs.Events[Locked](w) {
			mixer.Play(g.lock, audio.PlayOptions{Volume: 0.4})
		}
		for _, ev := range ecs.Events[Cleared](w) {
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
```

## Update: input into a resource

`Update` collects the frame's key edges into the `Controls` resource and
then runs the world once. Writing input into a resource rather than
reading `ctx.Input` from inside the systems is what makes the rules
testable without a window: a test sets `Controls` and calls
`world.Update`.

Every control uses `KeyPressed`, including Down. OS repeat events count
as presses, so held keys repeat at the platform's configured rate. A game
wanting its own repeat timing can exclude `KeyRepeated` and use a timer.

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
	if in.KeyPressed(input.KeyR) {
		g.restart()
	}
	// Input becomes a resource so the input system stays a pure function
	// of the world.
	*ecs.Resource[Controls](g.world) = Controls{
		Left: in.KeyPressed(input.KeyLeft), Right: in.KeyPressed(input.KeyRight), Rotate: in.KeyPressed(input.KeyUp),
		Down: in.KeyPressed(input.KeyDown), Drop: in.KeyPressed(input.KeySpace),
	}
	g.world.Update(ctx.Delta)
	return nil
}
```

## Draw: the board, the ghost and the panel

The board is centred with an offset, drawn as a border rectangle and a
darker field. `drawCell` is a closure over that offset so every cell is
one call, and it takes an alpha, which is what makes the ghost possible.

The ghost is the piece copied and dropped until it no longer fits, then
backed up one row, drawn at a quarter alpha. The two layers are drawn
from a table so the same nested loop covers both.

The flash is a white rectangle over the board whose alpha comes from the
tween.

The panel is rebuilt inside `u.Begin(ctx.Input, ...)` and
`u.Panel(title, rect, ...)`, both of which take closures; there is no
call to end them. `u.Button` reports a mouse release inside the button
after a press, or keyboard activation,
which is the immediate-mode pattern: the widget's identity comes from
its label, its container and the call order, and its result is a return
value rather than a callback.

```go
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
		for fits(ecs.Resource[Board](w), ghost) {
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
	score := ecs.Resource[Score](w)
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Tetris", ui.Rect{X: ox + cols*cell + 24, Y: oy, W: 200, H: 300}, func() {
			u.Label(fmt.Sprintf("Score %d", score.Points))
			u.Label(fmt.Sprintf("Lines %d", score.Lines))
			u.Label("Next: " + shapes[ecs.Resource[Bag](w).Next].name)
			u.Label(fmt.Sprintf("%d entities", w.Count()))
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
```

Building the interface in `Draw` rather than in `Update` is correct: key
and mouse edges are latched for the whole frame while `Draw` runs, so a
button sees every click even on a frame with no update.

## main

```go
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
```

## What to try

- Make gravity speed up with the level: keep the interval in `Clock`,
  and in `effectsSystem` cancel and re-register the timer with a shorter
  one as lines are cleared.
- Add a hold slot: a new resource, a key in `Update`, and a swap in
  `inputSystem`.
- Deal pieces from a shuffled bag of seven rather than at random, by
  giving `Bag` a slice and refilling it in `spawnPiece`.
- Emit a third event for a hard drop in `inputSystem` and give it its
  own sound in `effectsSystem`.
- Draw the next piece in the panel in `Draw` instead of naming it, using
  `newFalling(ecs.Resource[Bag](w).Next)` and the same `drawCell`
  closure with a different offset.
