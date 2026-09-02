// Command tetris is the game the Tetris guide builds, step by step: a
// board of cells, seven pieces that rotate, gravity on a timer, line
// clears with a flash, a score panel and sound. Left and right move, Up
// rotates, Down soft-drops, Space hard-drops, R restarts, Escape quits.
package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/rng"
	"github.com/matjam/bunyip/timer"
	"github.com/matjam/bunyip/tween"
	"github.com/matjam/bunyip/ui"
)

// The board is ten cells wide and twenty tall; each cell is a colour
// index, zero for empty.
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

// piece is a shape at a position with a rotation applied to its cells.
type piece struct {
	kind  int
	cells [][]bool // cells[y][x]
	x, y  int
}

func newPiece(kind int) piece {
	rows := shapes[kind].cells
	p := piece{kind: kind, x: cols/2 - len(rows[0])/2}
	for _, row := range rows {
		var r []bool
		for _, ch := range row {
			r = append(r, ch == '#')
		}
		p.cells = append(p.cells, r)
	}
	return p
}

// rotated returns the piece turned clockwise.
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

type game struct {
	seconds float64
	shot    string

	font   *gfx.Font
	ui     *ui.Context
	board  [rows][cols]int // colour index + 1, or 0
	cur    piece
	next   piece
	random *rng.Rand
	timers timer.Scheduler
	drop   timer.Handle
	flash  *tween.Tween
	lines  int
	score  int
	over   bool
	mixer  *audio.Mixer
	lock   *audio.Sound
	clear  *audio.Sound

	shotDone bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 16, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	g.mixer = ctx.Audio
	if g.lock, err = ctx.Audio.NewSound(audio.Sine(220, 0.06, ctx.Audio.Rate())); err != nil {
		return err
	}
	if g.clear, err = ctx.Audio.NewSound(audio.Sine(660, 0.25, ctx.Audio.Rate())); err != nil {
		return err
	}
	g.restart()
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) { g.font.Destroy() }

func (g *game) restart() {
	g.board = [rows][cols]int{}
	g.random = rng.New(uint64(g.score) + 7)
	g.next = newPiece(g.random.Intn(len(shapes)))
	g.lines, g.score, g.over = 0, 0, false
	g.spawn()
	g.timers = timer.Scheduler{}
	g.drop = g.timers.Every(0.6, g.gravity)
}

// spawn brings in the next piece; if it cannot be placed the game is over.
func (g *game) spawn() {
	g.cur, g.next = g.next, newPiece(g.random.Intn(len(shapes)))
	if g.collides(g.cur) {
		g.over = true
	}
}

// collides reports whether the piece overlaps walls, floor or settled cells.
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

func (g *game) gravity() {
	if g.over {
		return
	}
	down := g.cur
	down.y++
	if !g.try(down) {
		g.lockPiece()
	}
}

// lockPiece settles the piece into the board and clears full lines.
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
			copy(g.board[1:y+1], g.board[0:y]) // everything above falls one row
			g.board[0] = [cols]int{}
			cleared++
			y++ // check the row that just fell into this slot
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
	if g.over {
		return nil
	}
	if in.KeyPressed(input.KeyLeft) {
		p := g.cur
		p.x--
		g.try(p)
	}
	if in.KeyPressed(input.KeyRight) {
		p := g.cur
		p.x++
		g.try(p)
	}
	if in.KeyPressed(input.KeyUp) {
		r := g.cur.rotated()
		// Wall kick: nudge sideways when the rotation would clip a wall.
		for _, dx := range []int{0, -1, 1, -2, 2} {
			r.x = g.cur.x + dx
			if g.try(r) {
				break
			}
		}
	}
	if in.KeyPressed(input.KeyDown) {
		g.gravity()
	}
	if in.KeyPressed(input.KeySpace) {
		for {
			p := g.cur
			p.y++
			if !g.try(p) {
				break
			}
		}
		g.lockPiece()
	}
	g.timers.Update(ctx.Delta)
	if g.flash != nil {
		if g.flash.Update(float32(ctx.Delta)); g.flash.Done() {
			g.flash = nil
		}
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	ox, oy := (ctx.Width-cols*cell)/2-80, (ctx.Height-rows*cell)/2
	gr.FillRect(ox-4, oy-4, cols*cell+8, rows*cell+8, gfx.RGB(40, 42, 56))
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
	// The ghost shows where the piece will land.
	ghost := g.cur
	for !g.collides(ghost) {
		ghost.y++
	}
	ghost.y--
	for _, p := range []struct {
		piece piece
		alpha float32
	}{{ghost, 0.25}, {g.cur, 1}} {
		for y, row := range p.piece.cells {
			for x, on := range row {
				if on && p.piece.y+y >= 0 {
					drawCell(p.piece.x+x, p.piece.y+y, p.piece.kind, p.alpha)
				}
			}
		}
	}
	if g.flash != nil {
		gr.FillRect(ox, oy, cols*cell, rows*cell, gfx.Color{R: 1, G: 1, B: 1, A: 0.35 * g.flash.Value()})
	}
	u := g.ui
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
