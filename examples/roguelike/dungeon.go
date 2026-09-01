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
