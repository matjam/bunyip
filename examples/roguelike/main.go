// Command roguelike is a small turn-based dungeon crawl: rooms, corridors,
// line-of-sight, goblins that chase, and a message log. It runs in the
// engine's turn-based mode, so the process sleeps between keypresses.
// Move with the arrow keys, HJKL or the numpad; Escape quits.
package main

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"os"

	"golang.org/x/image/font/gofont/gomono"

	"github.com/matjam/bunyip/engine"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
)

const (
	mapW, mapH = 60, 32
	cellSize   = 20
)

type game struct {
	seconds float64
	shot    string

	font     *gfx.Font
	dungeon  *dungeon
	turn     int
	log      []string
	shotDone bool
}

func (g *game) Init(ctx *engine.Context) error {
	var err error
	g.font, err = ctx.Gfx.NewFont(gomono.TTF, 18, gfx.FontOptions{Ranges: [][2]rune{{0x2500, 0x257F}, {0x2580, 0x259F}}})
	if err != nil {
		return err
	}
	g.dungeon = newDungeon(rand.New(rand.NewPCG(7, 11)))
	g.say("You descend into the dark. Goblins stir.")
	return nil
}

func (g *game) Shutdown(ctx *engine.Context) { g.font.Destroy() }

func (g *game) say(msg string) {
	g.log = append(g.log, msg)
	if len(g.log) > 4 {
		g.log = g.log[1:]
	}
}

var moves = map[input.Key][2]int{
	input.KeyLeft: {-1, 0}, input.KeyRight: {1, 0}, input.KeyUp: {0, -1}, input.KeyDown: {0, 1},
	input.KeyH: {-1, 0}, input.KeyL: {1, 0}, input.KeyK: {0, -1}, input.KeyJ: {0, 1},
	input.KeyY: {-1, -1}, input.KeyU: {1, -1}, input.KeyB: {-1, 1}, input.KeyN: {1, 1},
	input.KeyKeypad4: {-1, 0}, input.KeyKeypad6: {1, 0}, input.KeyKeypad8: {0, -1}, input.KeyKeypad2: {0, 1},
	input.KeyKeypad7: {-1, -1}, input.KeyKeypad9: {1, -1}, input.KeyKeypad1: {-1, 1}, input.KeyKeypad3: {1, 1},
}

func (g *game) Update(ctx *engine.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.seconds > 0 {
		ctx.RequestRedraw() // keep the loop ticking so the timeout can fire
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	for key, d := range moves {
		if in.KeyPressed(key) {
			g.takeTurn(ctx, d[0], d[1])
			break
		}
	}
	if in.KeyPressed(input.KeyPeriod) || in.KeyPressed(input.KeyKeypad5) {
		g.takeTurn(ctx, 0, 0)
	}
	return nil
}

func (g *game) takeTurn(ctx *engine.Context, dx, dy int) {
	d := g.dungeon
	if d.player.hp <= 0 {
		return
	}
	g.turn++
	if msg := d.movePlayer(dx, dy); msg != "" {
		g.say(msg)
	}
	for _, msg := range d.monstersAct() {
		g.say(msg)
	}
	d.computeFOV()
	ctx.Log.Info("roguelike: turn", "n", g.turn, "player", fmt.Sprintf("%d,%d", d.player.x, d.player.y), "hp", d.player.hp)
}

func (g *game) Draw(ctx *engine.Context) error {
	gr := ctx.Gfx
	d := g.dungeon
	for y := range mapH {
		for x := range mapW {
			t := d.tiles[y][x]
			if !t.seen {
				continue
			}
			ch, col := t.glyph()
			if !t.visible {
				col = gfx.RGB(70, 70, 90)
			}
			g.cell(gr, x, y, ch, col)
		}
	}
	for _, m := range d.monsters {
		if m.hp > 0 && d.tiles[m.y][m.x].visible {
			g.cell(gr, m.x, m.y, "g", gfx.RGB(120, 220, 90))
		}
	}
	g.cell(gr, d.player.x, d.player.y, "@", gfx.RGB(255, 240, 160))
	// HUD and log below the map.
	top := float32(mapH*cellSize + 8)
	gr.DrawText(g.font, fmt.Sprintf("Turn %d   HP %d/%d   goblins %d", g.turn, d.player.hp, d.player.maxHP, d.alive()), 8, top, gfx.RGB(200, 200, 220))
	for i, msg := range g.log {
		gr.DrawText(g.font, msg, 8, top+float32(i+1)*(g.font.LineHeight+2), gfx.RGB(160, 170, 190))
	}
	if d.player.hp <= 0 {
		gr.FillRect(0, 0, ctx.Width, ctx.Height, gfx.RGBA(0, 0, 0, 150))
		gr.DrawText(g.font, "You died. Escape to quit.", ctx.Width/2-120, ctx.Height/2, gfx.RGB(255, 80, 80))
	}
	return nil
}

func (g *game) cell(gr *gfx.Graphics, x, y int, s string, c gfx.Color) {
	w, _ := g.font.Measure(s, gfx.TextOptions{})
	gr.DrawText(g.font, s, float32(x*cellSize)+(cellSize-w)/2, float32(y*cellSize), c)
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := engine.Run(engine.Config{
		Title: "Bunyip roguelike", Width: mapW * cellSize, Height: mapH*cellSize + 120,
		TurnBased: true, Validation: true,
	}, &game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "roguelike:", err)
		os.Exit(1)
	}
}
