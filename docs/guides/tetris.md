---
title: Building Tetris
order: 3
summary: a complete game in one file, step by step
---

This guide writes Tetris from an empty file. Along the way it uses the
loop, input, drawing, the timer and tween packages, the UI and the mixer.
The finished program is `examples/tetris` in the repository; run it with
`go run ./examples/tetris`.

![The finished game](../tetris.png)

## 1. The board

Tetris is a grid of cells. Ten columns, twenty rows, each cell either
empty or holding the colour of the piece that settled there. Fixed-size
arrays keep the state simple and copyable:

```go
const (
	cols, rows = 10, 20
	cell       = 28 // pixels per cell on screen
)

type game struct {
	board [rows][cols]int // colour index + 1, or 0 for empty
	// ...
}
```

The seven pieces are easiest to read as strings, with `#` for a filled
cell. Each has a colour; `gfx.Hex` takes the familiar `0xRRGGBB`:

```go
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

## 2. Pieces that move and rotate

A falling piece is a shape at a position. Rotation is a matrix transpose
with a flip; a square boolean grid makes that a four-line loop:

```go
type piece struct {
	kind  int
	cells [][]bool // cells[y][x]
	x, y  int
}

func (p piece) rotated() piece {
	n := len(p.cells)
	out := piece{kind: p.kind, x: p.x, y: p.y, cells: make([][]bool, n)}
	for y := range n {
		out.cells[y] = make([]bool, n)
		for x := range n {
			out.cells[y][x] = p.cells[n-1-x][y]
		}
	}
	return out
}
```

Everything else in Tetris is "would this piece fit?". One collision test
against walls, the floor and settled cells answers movement, rotation,
hard drops and game over:

```go
func (g *game) collides(p piece) bool {
	for y, row := range p.cells {
		for x, on := range row {
			if !on {
				continue
			}
			bx, by := p.x+x, p.y+y
			if bx < 0 || bx >= cols || by >= rows || (by >= 0 && g.board[by][bx] != 0) {
				return true
			}
		}
	}
	return false
}

// try moves to the candidate if it fits and reports whether it did.
func (g *game) try(p piece) bool {
	if g.collides(p) {
		return false
	}
	g.cur = p
	return true
}
```

Pieces come from a seeded generator so a run is reproducible. The
[rng](../pkg/rng.html) package gives the same sequence on every platform:

```go
g.random = rng.New(7)
g.next = newPiece(g.random.Intn(len(shapes)))
```

## 3. Gravity on a timer

The engine's `Update` runs sixty times a second, but the piece should fall
every 600 ms. A [timer.Scheduler](../pkg/timer.html) runs callbacks on
game time, and `Every` repeats until cancelled:

```go
g.drop = g.timers.Every(0.6, g.gravity)

func (g *game) gravity() {
	down := g.cur
	down.y++
	if !g.try(down) {
		g.lockPiece()
	}
}
```

Advance the scheduler from `Update` with the frame's delta. Because it
runs on game time, pausing the game is a matter of not calling it:

```go
func (g *game) Update(ctx *bunyip.Context) error {
	// ... input ...
	g.timers.Update(ctx.Delta)
	return nil
}
```

## 4. Input

`ctx.Input` reports edges (`KeyPressed`) and levels (`KeyDown`). Left,
right and rotate are edges; rotating tries a few horizontal nudges so a
piece against the wall still turns, the "wall kick" players expect:

```go
if in.KeyPressed(input.KeyLeft) {
	p := g.cur
	p.x--
	g.try(p)
}
if in.KeyPressed(input.KeyUp) {
	r := g.cur.rotated()
	for _, dx := range []int{0, -1, 1, -2, 2} {
		r.x = g.cur.x + dx
		if g.try(r) {
			break
		}
	}
}
if in.KeyPressed(input.KeySpace) { // hard drop
	for {
		p := g.cur
		p.y++
		if !g.try(p) {
			break
		}
	}
	g.lockPiece()
}
```

## 5. Locking and clearing lines

When a piece cannot fall it is written into the board. Then every full
row is removed by copying the rows above it down one; the `y++` re-checks
the row that just fell into the slot:

```go
func (g *game) lockPiece() {
	for y, row := range g.cur.cells {
		for x, on := range row {
			if on && g.cur.y+y >= 0 {
				g.board[g.cur.y+y][g.cur.x+x] = g.cur.kind + 1
			}
		}
	}
	cleared := 0
	for y := rows - 1; y >= 0; y-- {
		full := true
		for x := range cols {
			full = full && g.board[y][x] != 0
		}
		if full {
			copy(g.board[1:y+1], g.board[0:y])
			g.board[0] = [cols]int{}
			cleared++
			y++
		}
	}
	if cleared > 0 {
		g.lines += cleared
		g.score += []int{0, 100, 300, 500, 800}[cleared]
		g.flash = tween.New(1, 0, 0.4, tween.OutQuad)
		g.mixer.Play(g.clear, audio.PlayOptions{Volume: 0.5, Pitch: 1 + 0.2*float32(cleared)})
	} else {
		g.mixer.Play(g.lock, audio.PlayOptions{Volume: 0.4})
	}
	g.spawn()
}
```

Two touches make the clear feel good. A [tween](../pkg/tween.html) drives
a white flash from full to nothing over 0.4 s, and the mixer plays a
higher-pitched tone for bigger clears. The tones are synthesised at start
with `audio.Sine`, so the game has no sound files:

```go
g.lock, _ = ctx.Audio.NewSound(audio.Sine(220, 0.06, ctx.Audio.Rate()))
g.clear, _ = ctx.Audio.NewSound(audio.Sine(660, 0.25, ctx.Audio.Rate()))
```

## 6. Drawing

Everything on screen is a filled rectangle, which `gfx.FillRect` draws in
one batched call. The board is drawn centred, settled cells first, then a
translucent ghost where the piece will land, then the piece itself:

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	ox, oy := (ctx.Width-cols*cell)/2-80, (ctx.Height-rows*cell)/2
	gr.FillRect(ox, oy, cols*cell, rows*cell, gfx.RGB(16, 16, 24))
	drawCell := func(x, y, kind int, alpha float32) {
		c := shapes[kind].color
		c.A = alpha
		gr.FillRect(ox+float32(x*cell)+1, oy+float32(y*cell)+1, cell-2, cell-2, c)
	}
	for y := range rows {
		for x := range cols {
			if k := g.board[y][x]; k != 0 {
				drawCell(x, y, k-1, 1)
			}
		}
	}
	ghost := g.cur
	for !g.collides(ghost) {
		ghost.y++
	}
	ghost.y--
	// draw ghost at alpha 0.25, then g.cur at alpha 1 ...
	if g.flash != nil {
		gr.FillRect(ox, oy, cols*cell, rows*cell, gfx.Color{R: 1, G: 1, B: 1, A: 0.35 * g.flash.Value()})
	}
	// ...
}
```

Colours are `gfx.Color` values with float components, so alpha is a
field to set. Drawing order is submission order within a layer, which is
all this game needs.

## 7. A score panel with the UI

The [ui](../pkg/ui.html) package is immediate mode: build the panel every
frame inside `Begin`, and widgets report what happened. Containers take
closures, so their extent is visible in the code:

```go
u.Begin(ctx.Input, func() {
	u.Panel("Tetris", ui.Rect{X: ox + cols*cell + 24, Y: oy, W: 200, H: 260}, func() {
		u.Label(fmt.Sprintf("Score %d", g.score))
		u.Label(fmt.Sprintf("Lines %d", g.lines))
		u.Label("Next: " + shapes[g.next.kind].name)
		u.Separator()
		if g.over {
			u.Label("Game over")
		}
		if u.Button("Restart (R)") {
			g.restart()
		}
	})
})
```

The context is created once in `Init` with a font and a theme:
`ui.New(ctx.Gfx, ui.DarkTheme(font))`. Swap the theme for any of the
built-in palettes with `ui.NamedTheme("nord", font)`, or give it a `Skin`
of textures to draw the panel and button from art.

## 8. Running and shipping

The whole game is about 300 lines. It accepts the same `-seconds` and
`-shot` flags as every example, so a script can run it and save a
screenshot without a person at the keyboard:

```
go run ./examples/tetris -seconds 3 -shot tetris.png
```

To hand it to someone, build it and wrap it:

```
go build -o tetris ./examples/tetris
go run ./cmd/bunyip-bundle -name Tetris -exe ./tetris -o dist
```

## Where to take it

- Hold a piece: store it and swap on a key, a few lines with the same
  `try` guard.
- Levels: shorten the gravity interval by cancelling and re-creating the
  timer as lines accumulate.
- Music: stream a file with `ctx.Audio.OpenMusic` and `PlayStream`, or
  write a `Stream` that synthesises it, as `examples/audio` does.
- Settings: the [save](../pkg/save.html) package keeps a high score in the
  platform's data directory in three lines.
