// Command autotile paints terrain that picks its own tiles. Grass is a
// 47-tile blob set composed from a six-tile template by ExpandBlob, with
// a flower variant mixed into the filled tiles; walls are a 16-tile edge
// set. Both read the same terrain grid through one autotile.Mapper each.
// Paint with the left mouse button, erase with the right; 1 selects the
// grass brush and 2 the wall brush. Escape quits.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/grid/autotile"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/rng"
)

const (
	tile = 16
	mapW = 40
	mapH = 22
)

const (
	empty = iota
	grass
	wall
)

type game struct {
	seconds float64
	shot    string

	terrain  []int
	grassTex *gfx.Texture
	wallTex  *gfx.Texture
	grassMap *gfx.Tilemap
	wallMap  *gfx.Tilemap
	grassRul *autotile.Mapper
	wallRul  *autotile.Mapper
	brush    int
	shotDone bool
}

func (g *game) at(x, y int) int { return g.terrain[y*mapW+x] }

func (g *game) Init(ctx *bunyip.Context) error {
	// The grass sheet: 47 blob tiles composed from a drawn template,
	// with a 48th flower tile added as a variant of the filled case.
	img, frames := autotile.ExpandBlob(makeTemplate(), tile)
	addFlowers(img)
	var err error
	if g.grassTex, err = ctx.Gfx.NewTexture(img, gfx.TextureOptions{}); err != nil {
		return err
	}
	var grassFrames [47]int
	copy(grassFrames[:], frames[:])
	g.grassRul = &autotile.Mapper{Rules: autotile.Blob47(grass, grassFrames).Variant(255, 47, 0.4)}
	g.grassMap = gfx.NewTilemap(gfx.NewSheet(g.grassTex, tile, tile), mapW, mapH)

	if g.wallTex, err = ctx.Gfx.NewTexture(makeWalls(), gfx.TextureOptions{}); err != nil {
		return err
	}
	var wallFrames [16]int
	for i := range wallFrames {
		wallFrames[i] = i
	}
	g.wallRul = &autotile.Mapper{Rules: autotile.Edge16(wall, wallFrames)}
	g.wallMap = gfx.NewTilemap(gfx.NewSheet(g.wallTex, tile, tile), mapW, mapH)

	// Seed the map so there is something to look at: grass blobs from a
	// few random walks, and a wall run.
	g.terrain = make([]int, mapW*mapH)
	r := rng.New(7)
	for range 6 {
		x, y := r.Intn(mapW), r.Intn(mapH)
		for range 90 {
			if x >= 0 && y >= 0 && x < mapW && y < mapH {
				g.terrain[y*mapW+x] = grass
			}
			x += r.Intn(3) - 1
			y += r.Intn(3) - 1
		}
	}
	for x := 6; x < 26; x++ {
		g.terrain[8*mapW+x] = wall
	}
	for y := 8; y < 16; y++ {
		g.terrain[y*mapW+26] = wall
	}
	g.brush = grass
	g.refresh()
	return nil
}

// refresh reapplies both rule sets to the whole map.
func (g *game) refresh() {
	g.grassRul.Apply(mapW, mapH, g.at, g.grassMap.Set)
	g.wallRul.Apply(mapW, mapH, g.at, g.wallMap.Set)
}

func (g *game) Update(ctx *bunyip.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if in.KeyPressed(input.Key1) {
		g.brush = grass
	}
	if in.KeyPressed(input.Key2) {
		g.brush = wall
	}
	mx, my := in.Mouse()
	x, y := int(mx)/tile, int(my)/tile
	if x >= 0 && y >= 0 && x < mapW && y < mapH {
		want := -1
		if in.MouseDown(input.MouseLeft) {
			want = g.brush
		} else if in.MouseDown(input.MouseRight) {
			want = empty
		}
		if want >= 0 && g.terrain[y*mapW+x] != want {
			g.terrain[y*mapW+x] = want
			// One changed cell only needs its neighbourhood redone.
			g.grassRul.Cell(x, y, mapW, mapH, g.at, g.grassMap.Set)
			g.wallRul.Cell(x, y, mapW, mapH, g.at, g.wallMap.Set)
		}
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	gr.FillRect(0, 0, float32(mapW*tile), float32(mapH*tile), gfx.RGB(38, 30, 26))
	gr.DrawTilemap(g.grassMap, 0, 0, gfx.Color{})
	gr.DrawTilemap(g.wallMap, 0, 0, gfx.Color{})
	mx, my := ctx.Input.Mouse()
	x, y := int(mx)/tile, int(my)/tile
	if x >= 0 && y >= 0 && x < mapW && y < mapH {
		gr.FillRect(float32(x*tile), float32(y*tile), tile, tile, gfx.RGBA(255, 255, 255, 60))
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		g.shotDone = true
		ctx.Screenshot(g.shot)
	}
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.grassTex.Destroy()
	g.wallTex.Destroy()
}

// makeTemplate draws the six-tile grass template ExpandBlob expects:
// the inside-corner tile, a preview, and the four corners of a filled
// two-by-two block. Edges get a dark rim and rounded silhouettes.
func makeTemplate() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 2*tile, 3*tile))
	light := color.RGBA{R: 96, G: 168, B: 72, A: 255}
	dark := color.RGBA{R: 52, G: 112, B: 48, A: 255}
	rim := color.RGBA{R: 34, G: 74, B: 40, A: 255}
	// fill paints one template tile: grass with a subtle checker, a rim
	// on the exposed sides, and rounded corners where two rims meet.
	fill := func(tx, ty int, top, right, bottom, left bool) {
		const r = 5
		for py := range tile {
			for px := range tile {
				c := light
				if (px/2+py/2)%2 == 0 {
					c = dark
				}
				edge := top && py < 3 || bottom && py >= tile-3 || left && px < 3 || right && px >= tile-3
				if edge {
					c = rim
				}
				// Round the silhouette at outer corners.
				cx, cy, corner := 0, 0, false
				if top && left && px < r && py < r {
					cx, cy, corner = r, r, true
				}
				if top && right && px >= tile-r && py < r {
					cx, cy, corner = tile-1-r, r, true
				}
				if bottom && left && px < r && py >= tile-r {
					cx, cy, corner = r, tile-1-r, true
				}
				if bottom && right && px >= tile-r && py >= tile-r {
					cx, cy, corner = tile-1-r, tile-1-r, true
				}
				if corner {
					dx, dy := px-cx, py-cy
					switch d := dx*dx + dy*dy; {
					case d > r*r:
						continue // outside the rounded corner
					case d > (r-3)*(r-3):
						c = rim
					}
				}
				img.SetRGBA(tx*tile+px, ty*tile+py, c)
			}
		}
	}
	fill(1, 0, true, true, true, true) // preview: the island look
	fill(0, 1, true, false, false, true)
	fill(1, 1, true, true, false, false)
	fill(0, 2, false, false, true, true)
	fill(1, 2, false, true, true, false)
	// The inner tile: interior grass with a rim block in each corner.
	// Where two grass edges meet around an empty diagonal cell, the
	// 3-pixel rims arriving from both neighbours turn the corner
	// through this block at the same width. The empty cell itself lies
	// entirely in the neighbour, so nothing here is transparent.
	fill(0, 0, false, false, false, false)
	for _, c := range [4][2]int{{0, 0}, {tile - 3, 0}, {0, tile - 3}, {tile - 3, tile - 3}} {
		for dy := range 3 {
			for dx := range 3 {
				img.SetRGBA(c[0]+dx, c[1]+dy, rim)
			}
		}
	}
	return img
}

// addFlowers copies the filled tile into the sheet's spare 48th slot and
// scatters flowers on it, for the Variant of the full neighbourhood.
func addFlowers(img *image.RGBA) {
	src := image.Pt(46%8*tile, 46/8*tile) // frame 46 is the filled tile
	dst := image.Rect(47%8*tile, 47/8*tile, 47%8*tile+tile, 47/8*tile+tile)
	draw.Draw(img, dst, img, src, draw.Src)
	pink := color.RGBA{R: 235, G: 140, B: 190, A: 255}
	gold := color.RGBA{R: 245, G: 210, B: 90, A: 255}
	for i, p := range [][2]int{{4, 5}, {11, 3}, {8, 10}, {13, 12}, {3, 12}} {
		c := pink
		if i%2 == 1 {
			c = gold
		}
		img.SetRGBA(dst.Min.X+p[0], dst.Min.Y+p[1], c)
		img.SetRGBA(dst.Min.X+p[0]+1, dst.Min.Y+p[1], c)
		img.SetRGBA(dst.Min.X+p[0], dst.Min.Y+p[1]+1, c)
	}
}

// makeWalls draws the 16 edge tiles: a stone block with arms toward each
// connected side, indexed by the Edge16 mask. The shape is filled first
// and outlined afterwards, so a straight run reads as one wall rather
// than a chain of blocks.
func makeWalls() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 16*tile, tile))
	stone := color.RGBA{R: 120, G: 116, B: 128, A: 255}
	shade := color.RGBA{R: 86, G: 82, B: 96, A: 255}
	const a, b = 5, 11 // the core block
	for mask := range 16 {
		var solid [tile][tile]bool
		fill := func(x0, y0, x1, y1 int) {
			for py := y0; py < y1; py++ {
				for px := x0; px < x1; px++ {
					solid[py][px] = true
				}
			}
		}
		fill(a, a, b, b)
		if mask&1 != 0 { // north
			fill(a, 0, b, a)
		}
		if mask&2 != 0 { // east
			fill(b, a, tile, b)
		}
		if mask&4 != 0 { // south
			fill(a, b, b, tile)
		}
		if mask&8 != 0 { // west
			fill(0, a, a, b)
		}
		for py := range tile {
			for px := range tile {
				if !solid[py][px] {
					continue
				}
				// Outline pixels are those with an empty neighbour inside
				// the tile; at the tile border the arm continues next door.
				c := stone
				for _, o := range [4][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
					nx, ny := px+o[0], py+o[1]
					if nx >= 0 && ny >= 0 && nx < tile && ny < tile && !solid[ny][nx] {
						c = shade
					}
				}
				img.SetRGBA(mask*tile+px, py, c)
			}
		}
	}
	return img
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write the frame at -seconds/2 to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip autotile", Width: mapW * tile, Height: mapH * tile},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "autotile:", err)
		os.Exit(1)
	}
}
