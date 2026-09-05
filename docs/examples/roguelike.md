---
title: Roguelike
example: roguelike
summary: a turn-based dungeon crawl with generated rooms, line of sight, chasing goblins and a message log, running in the engine's turn-based mode
---

This is the engine's turn-based mode in a complete small game. It
generates rooms joined by corridors, places a player and some goblins,
computes what the player can see, moves the monsters when the player
moves, and prints a message log under the map. Nothing happens between
turns in the dungeon. The engine loop can block in the operating system
between events instead of continually updating and redrawing an idle game.

Turn-based mode is a single field, `Config.TurnBased`, and its effect
runs through everything else. `Update` runs when an event arrives rather
than at a fixed rate, `Draw` follows it, and a game that needs a frame
without an event asks for one with `ctx.RequestRedraw()`. The rest is
[gfx](../pkg/gfx.html) text drawing and [input](../pkg/input.html) key
edges. The guides are [The window](../guides/window.html) for the loop
and [2D graphics](../guides/graphics-2d.html) for the drawing.

The program is in two files. `main.go` is the game type and the
presentation; `dungeon.go` is the map, the generator, the monsters and
the field of view, with no reference to the engine except colours.

Run it with:

```bash
go run ./examples/roguelike -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`. Move with the arrow
keys, HJKL and YUBN, or the numpad; `.` or numpad 5 waits a turn;
Escape quits.

## Constants and the game type

The map is 60 by 32 cells drawn at 20 units each, and the window is
sized from those constants in `main`, with 120 units left under the map
for the status line and the log.

`game` holds the font, the dungeon, the turn counter and the last four
log messages.

```go
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
```

## Init and the log

The font is Go Mono, because a fixed-width face makes a grid of glyphs
line up. `FontOptions.Ranges` asks for two extra Unicode blocks, the box
drawing and block element characters, to be included in the atlas, which
is what a game that draws walls as box characters needs.

The dungeon is generated from a fixed seed, so every run is the same
map. `say` appends to the log and keeps the last four lines.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	g.font, err = ctx.Gfx.NewFont(gomono.TTF, 18, gfx.FontOptions{Ranges: [][2]rune{{0x2500, 0x257F}, {0x2580, 0x259F}}})
	if err != nil {
		return err
	}
	g.dungeon = newDungeon(rand.New(rand.NewPCG(7, 11)))
	g.say("You descend into the dark. Goblins stir.")
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) { g.font.Destroy() }

func (g *game) say(msg string) {
	g.log = append(g.log, msg)
	if len(g.log) > 4 {
		g.log = g.log[1:]
	}
}
```

## The movement table

Every movement key maps to a step. One table covers the arrow keys, the
`hjkl` and `yubn` sets and the numpad, so the input code below is one
loop rather than twenty cases. `input.Key` values name physical
positions on the keyboard, so the numpad entries are distinct from the
digits above the letters.

```go
var moves = map[input.Key][2]int{
	input.KeyLeft: {-1, 0}, input.KeyRight: {1, 0}, input.KeyUp: {0, -1}, input.KeyDown: {0, 1},
	input.KeyH: {-1, 0}, input.KeyL: {1, 0}, input.KeyK: {0, -1}, input.KeyJ: {0, 1},
	input.KeyY: {-1, -1}, input.KeyU: {1, -1}, input.KeyB: {-1, 1}, input.KeyN: {1, 1},
	input.KeyKeypad4: {-1, 0}, input.KeyKeypad6: {1, 0}, input.KeyKeypad8: {0, -1}, input.KeyKeypad2: {0, 1},
	input.KeyKeypad7: {-1, -1}, input.KeyKeypad9: {1, -1}, input.KeyKeypad1: {-1, 1}, input.KeyKeypad3: {1, 1},
}
```

## Update: one keypress, one turn

`Update` runs when an event arrives. Each movement key is tested with
`KeyPressed`, which includes OS key-repeat events, so holding a key can
advance further turns at the platform's repeat rate. The loop breaks
after the first matching movement key.

`ctx.RequestRedraw()` asks for another pass even though nothing has
happened. It is only used when `-seconds` is set: without it the program
would sleep in the operating system and never reach the deadline check,
so an unattended run would never exit. A real turn-based game leaves it
out, and only calls it when an animation is running.

```go
func (g *game) Update(ctx *bunyip.Context) error {
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
```

## A turn

A turn is the whole world moving once: the player acts, every monster
acts, and the field of view is recomputed. Doing it in one function is
what makes a turn-based game simple to reason about, since nothing
happens between two of these calls.

`ctx.Log` is the engine's structured logger, and the line here records
the turn number, the player's position and hit points, which is a
readable trace of a session.

```go
func (g *game) takeTurn(ctx *bunyip.Context, dx, dy int) {
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
```

## Draw: glyphs on a grid

Every visible or remembered tile draws its glyph. A tile that has been
seen but is not currently visible draws in a dim grey, which is the
usual roguelike memory: the map stays on screen, the contents do not.
Monsters are only drawn where the player can see them.

The HUD is below the map, using `Font.LineHeight` to space the log
lines, so the layout follows the font rather than a hard-coded number.
The death overlay is a translucent rectangle over the whole view
followed by a line of text.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
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
```

`cell` measures each glyph and centres it in its 20 unit cell, so the
map lines up even where a glyph is narrower than the cell.

## main

`TurnBased: true` is the whole difference from the other examples. The
window height leaves 120 units under the map for the status line and the
log.

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{
		Title: "Bunyip roguelike", Width: mapW * cellSize, Height: mapH*cellSize + 120,
		TurnBased: true, Validation: true,
	}, &game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "roguelike:", err)
		os.Exit(1)
	}
}
```

## The dungeon: tiles and actors

The second file has no engine dependency beyond `gfx.Color`. A tile is
a wall flag plus two visibility flags: `seen` is whether it has ever
been visible and `visible` is whether it is visible now. `glyph` maps a
tile to the character and colour it draws with, which is the only
presentation decision in this file.

An actor is a position and hit points, used for both the player and the
monsters. The dungeon owns a fixed-size tile array, the player by value,
the monsters as pointers so they can be mutated through the slice, and
the random source used to generate it.

<!-- file: dungeon.go -->
```go
package main

import (
	"math/rand/v2"

	"github.com/matjam/bunyip/gfx"
)

type tile struct {
	wall    bool
	seen    bool
	visible bool
}

func (t tile) glyph() (string, gfx.Color) {
	if t.wall {
		return "#", gfx.RGB(150, 140, 130)
	}
	return ".", gfx.RGB(110, 110, 120)
}

type actor struct {
	x, y      int
	hp, maxHP int
}

type dungeon struct {
	tiles    [mapH][mapW]tile
	player   actor
	monsters []*actor
	rng      *rand.Rand
}

type room struct{ x, y, w, h int }

func (r room) center() (int, int) { return r.x + r.w/2, r.y + r.h/2 }
```

## Generating the map

The generator is the classic one: fill everything with wall, then try
forty random rooms, discarding any that leave the map or overlap one
already placed, and join each accepted room to the previous one with two
straight corridors in a random order, which gives an L-shaped passage.
The player starts in the first room, and each later room has a two in
three chance of a goblin.

<!-- file: dungeon.go -->
```go
// newDungeon carves random rooms joined by L-shaped corridors.
func newDungeon(rng *rand.Rand) *dungeon {
	d := &dungeon{rng: rng}
	for y := range mapH {
		for x := range mapW {
			d.tiles[y][x].wall = true
		}
	}
	var rooms []room
	for range 40 {
		r := room{x: 1 + rng.IntN(mapW-12), y: 1 + rng.IntN(mapH-8), w: 4 + rng.IntN(8), h: 3 + rng.IntN(5)}
		if r.x+r.w >= mapW-1 || r.y+r.h >= mapH-1 {
			continue
		}
		overlaps := false
		for _, o := range rooms {
			if r.x <= o.x+o.w && o.x <= r.x+r.w && r.y <= o.y+o.h && o.y <= r.y+r.h {
				overlaps = true
				break
			}
		}
		if overlaps {
			continue
		}
		d.carve(r)
		if len(rooms) > 0 {
			px, py := rooms[len(rooms)-1].center()
			cx, cy := r.center()
			if rng.IntN(2) == 0 {
				d.corridorH(px, cx, py)
				d.corridorV(py, cy, cx)
			} else {
				d.corridorV(py, cy, px)
				d.corridorH(px, cx, cy)
			}
		}
		rooms = append(rooms, r)
	}
	px, py := rooms[0].center()
	d.player = actor{x: px, y: py, hp: 10, maxHP: 10}
	for _, r := range rooms[1:] {
		if rng.IntN(3) > 0 {
			cx, cy := r.center()
			d.monsters = append(d.monsters, &actor{x: cx, y: cy, hp: 3, maxHP: 3})
		}
	}
	d.computeFOV()
	return d
}

func (d *dungeon) carve(r room) {
	for y := r.y; y < r.y+r.h; y++ {
		for x := r.x; x < r.x+r.w; x++ {
			d.tiles[y][x].wall = false
		}
	}
}

func (d *dungeon) corridorH(x1, x2, y int) {
	for x := min(x1, x2); x <= max(x1, x2); x++ {
		d.tiles[y][x].wall = false
	}
}

func (d *dungeon) corridorV(y1, y2, x int) {
	for y := min(y1, y2); y <= max(y1, y2); y++ {
		d.tiles[y][x].wall = false
	}
}

func (d *dungeon) inBounds(x, y int) bool { return x >= 0 && y >= 0 && x < mapW && y < mapH }

func (d *dungeon) monsterAt(x, y int) *actor {
	for _, m := range d.monsters {
		if m.hp > 0 && m.x == x && m.y == y {
			return m
		}
	}
	return nil
}
```

The overlap test compares the rooms with one cell of slack on each side,
so accepted rooms always have a wall between them.

## Acting

The player's move is the standard roguelike one: a wall stops the move
with no message, but `takeTurn` still advances the turn and the monsters.
A monster in the way is attacked
instead of being walked into, and otherwise the player moves. Returning
the message rather than logging it keeps this file free of presentation.

Monsters act only where the player can see them, which is a simple way
to keep the far side of the map quiet. A goblin adjacent to the player
attacks; otherwise it tries the diagonal step towards the player first
and then the two straight steps, so it slides along a wall instead of
sticking to it.

<!-- file: dungeon.go -->
```go
func (d *dungeon) movePlayer(dx, dy int) string {
	nx, ny := d.player.x+dx, d.player.y+dy
	if !d.inBounds(nx, ny) || d.tiles[ny][nx].wall {
		return ""
	}
	if m := d.monsterAt(nx, ny); m != nil {
		m.hp -= 2
		if m.hp <= 0 {
			return "You slay the goblin."
		}
		return "You hit the goblin."
	}
	d.player.x, d.player.y = nx, ny
	return ""
}

// monstersAct moves each visible goblin one step toward the player, or
// attacks when adjacent.
func (d *dungeon) monstersAct() []string {
	var msgs []string
	for _, m := range d.monsters {
		if m.hp <= 0 || !d.tiles[m.y][m.x].visible {
			continue
		}
		dx, dy := sign(d.player.x-m.x), sign(d.player.y-m.y)
		if m.x+dx == d.player.x && m.y+dy == d.player.y {
			d.player.hp--
			msgs = append(msgs, "The goblin bites you!")
			continue
		}
		for _, step := range [][2]int{{dx, dy}, {dx, 0}, {0, dy}} {
			nx, ny := m.x+step[0], m.y+step[1]
			if (step[0] != 0 || step[1] != 0) && d.inBounds(nx, ny) && !d.tiles[ny][nx].wall && d.monsterAt(nx, ny) == nil && !(nx == d.player.x && ny == d.player.y) {
				m.x, m.y = nx, ny
				break
			}
		}
	}
	return msgs
}

func (d *dungeon) alive() int {
	n := 0
	for _, m := range d.monsters {
		if m.hp > 0 {
			n++
		}
	}
	return n
}
```

## Field of view

Visibility is recomputed from scratch each turn: clear every tile, then
for each cell within a radius of eight, keep it if a straight line from
the player reaches it without crossing a wall. A tile that is visible is
also marked seen, which is what the dim rendering in `Draw` uses.

`lineClear` is Bresenham's line walked one cell at a time. A wall
strictly between the endpoints blocks the line, but a wall at either end
does not, which is what makes the walls of a lit room visible rather
than a room of floor tiles surrounded by darkness.

<!-- file: dungeon.go -->
```go
// computeFOV marks tiles within radius 8 that a straight line reaches.
func (d *dungeon) computeFOV() {
	const radius = 8
	for y := range mapH {
		for x := range mapW {
			d.tiles[y][x].visible = false
		}
	}
	px, py := d.player.x, d.player.y
	for y := py - radius; y <= py+radius; y++ {
		for x := px - radius; x <= px+radius; x++ {
			if !d.inBounds(x, y) || (x-px)*(x-px)+(y-py)*(y-py) > radius*radius {
				continue
			}
			if d.lineClear(px, py, x, y) {
				d.tiles[y][x].visible = true
				d.tiles[y][x].seen = true
			}
		}
	}
}

// lineClear walks a Bresenham line and reports whether no wall sits
// strictly between the endpoints.
func (d *dungeon) lineClear(x0, y0, x1, y1 int) bool {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := sign(x1-x0), sign(y1-y0)
	err := dx + dy
	x, y := x0, y0
	for {
		if x == x1 && y == y1 {
			return true
		}
		if (x != x0 || y != y0) && d.tiles[y][x].wall {
			return false
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x += sx
		}
		if e2 <= dx {
			err += dx
			y += sy
		}
	}
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
```

Casting a line to every cell in the radius is more work than a shadow
casting algorithm needs, but at 17 by 17 cells per turn it is free, and
it is short enough to read.

## What to try

- Give the goblins their own field of view in `monstersAct` instead of
  using the player's, so they keep chasing after the player steps out of
  sight.
- Add an item type to `dungeon` and place one per room in `newDungeon`,
  then pick it up in `movePlayer`.
- Replace the box glyphs with tiles: draw a sprite per cell in `Draw`
  and keep the same map code.
- Widen the field of view radius in `computeFOV`, or make it depend on a
  light source the player carries.
- Remove the `RequestRedraw` call in `Update` and confirm that the game
  still plays but no longer honours `-seconds`, which is what turn-based
  mode means.
