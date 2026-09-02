// Command tiled loads a map made in the Tiled editor from an embedded
// .tmj, builds it into tilemaps with the tiled package, and draws it
// under a Camera2D. The tileset image is painted at start-up: a grid of
// coloured tiles with an arrow so flipped cells show their flips, and a
// two-frame animated water tile. Objects from the map's object layer are
// outlined. The arrow keys move the camera; Escape quits.
//
// With -map, a .tmx or .tmj file is loaded from disk instead, with its
// tileset images read from next to it.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/tiled"
)

//go:embed level.tmj
var levelJSON []byte

const tile = 16

// Tileset frames, matching level.tmj's tileset.
const (
	frameGrass = iota
	frameDirt
	frameWall
	frameArrow
	frameWaterA
	frameWaterB
	frameFlower
	frameSand
)

type game struct {
	seconds  float64
	shot     string
	shotDone bool
	mapFile  string

	font  *gfx.Font
	level *tiled.Level
	cam   gfx.Camera2D
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	var m *tiled.Map
	// The embedded map names tiles.png; the image comes from makeTiles
	// instead. A map from disk reads its images from its own directory.
	images := func(string) (image.Image, error) { return makeTiles(), nil }
	if g.mapFile != "" {
		m, err = tiled.Load(g.mapFile)
		images = fileImages(filepath.Dir(g.mapFile))
	} else {
		m, err = tiled.Parse(levelJSON, nil)
	}
	if err != nil {
		return err
	}
	g.level, err = tiled.Build(ctx.Gfx, m, images)
	if err != nil {
		return err
	}
	bg := m.BackgroundColor
	ctx.Clear = gfx.RGBA(bg.R, bg.G, bg.B, bg.A)
	g.cam = gfx.Camera2D{Position: g.level.Size().Mul(0.5), Zoom: 3}
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.level.Destroy()
	g.font.Destroy()
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
	var move lin.Vec2
	if in.KeyDown(input.KeyLeft) {
		move.X--
	}
	if in.KeyDown(input.KeyRight) {
		move.X++
	}
	if in.KeyDown(input.KeyUp) {
		move.Y--
	}
	if in.KeyDown(input.KeyDown) {
		move.Y++
	}
	if g.seconds > 0 && move == (lin.Vec2{}) {
		move = lin.V2(float32(math.Sin(ctx.Time*0.8)), 0) // drift a little for screenshots
	}
	g.cam.Position = g.cam.Position.Add(move.Mul(float32(ctx.Delta) * 120 / g.cam.Zoom))
	g.level.Advance(ctx.Delta)
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	gr.SetCamera2D(g.cam)
	g.level.Draw(gr, 0, 0, gfx.White)
	for _, l := range g.level.Layers {
		if l.Kind != tiled.ObjectLayer || !l.Visible {
			continue
		}
		for _, o := range l.Objects {
			drawObject(gr, o, l.Offset)
		}
	}
	gr.ScreenSpace()
	text := fmt.Sprintf("%s: %dx%d tiles, %d layers. Arrow keys move the camera.",
		g.level.Map.Properties.String("title"), g.level.Map.Width, g.level.Map.Height, len(g.level.Layers))
	gr.DrawText(g.font, text, 12, 12, gfx.RGB(240, 235, 220))
	return nil
}

// fileImages reads tileset images from disk, relative to the map's
// directory.
func fileImages(dir string) tiled.Images {
	return func(p string) (image.Image, error) {
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		return img, err
	}
}

// drawObject outlines a map object so its placement can be checked.
func drawObject(gr *gfx.Graphics, o tiled.Object, off lin.Vec2) {
	c := gfx.RGBA(255, 220, 80, 200)
	x, y := o.X+off.X, o.Y+off.Y
	switch {
	case o.Point:
		gr.FillCircle(x, y, 2, c)
		gr.StrokeCircle(x, y, 4, 0.5, c)
	case len(o.Polygon) > 0:
		pts := make([]lin.Vec2, len(o.Polygon))
		for i, p := range o.Polygon {
			pts[i] = p.Add(lin.V2(x, y))
		}
		gr.FillPolygon(pts, c.WithAlpha(0.35))
	case o.Ellipse:
		gr.StrokeCircle(x+o.Width/2, y+o.Height/2, o.Width/2, 0.5, c)
	default:
		gr.StrokeRect(x, y, o.Width, o.Height, 0.5, c)
	}
}

// makeTiles paints the eight 16x16 tiles level.tmj's tileset expects, in
// two rows of four.
func makeTiles() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 4*tile, 2*tile))
	set := func(frame, x, y int, c color.NRGBA) {
		img.SetNRGBA((frame%4)*tile+x, (frame/4)*tile+y, c)
	}
	for y := range tile {
		for x := range tile {
			v := uint8((x*7 + y*13) % 16)
			set(frameGrass, x, y, color.NRGBA{60 + v, 140 + v, 60, 255})
			set(frameDirt, x, y, color.NRGBA{120 + v, 85 + v, 50, 255})
			set(frameSand, x, y, color.NRGBA{210 + v/2, 190 + v/2, 130, 255})
			set(frameFlower, x, y, color.NRGBA{60 + v, 140 + v, 60, 255})
			wall := color.NRGBA{110 + v, 105 + v, 100, 255}
			if y%8 == 0 || (x+(y/8)*8)%16 == 0 {
				wall = color.NRGBA{60, 58, 55, 255}
			}
			set(frameWall, x, y, wall)
			// Two water frames whose ripple sits at different heights.
			for f, ripple := range []int{4, 10} {
				c := color.NRGBA{40, 90, 190, 255}
				if y == ripple && x%4 < 2 || y == ripple+6 && x%4 >= 2 {
					c = color.NRGBA{150, 200, 240, 255}
				}
				set(frameWaterA+f, x, y, c)
			}
			set(frameArrow, x, y, color.NRGBA{30, 30, 40, 255})
		}
	}
	// An arrow pointing up with a red dot in its top-right corner so every
	// flip and rotation looks different.
	white := color.NRGBA{240, 240, 240, 255}
	for y := 3; y < 14; y++ {
		set(frameArrow, 7, y, white)
		set(frameArrow, 8, y, white)
	}
	for i := range 4 {
		for x := 7 - i; x <= 8+i; x++ {
			set(frameArrow, x, 3+i, white)
		}
	}
	for y := 1; y < 4; y++ {
		for x := 12; x < 15; x++ {
			set(frameArrow, x, y, color.NRGBA{230, 60, 60, 255})
		}
	}
	// A flower: petals around a yellow centre.
	pink := color.NRGBA{240, 120, 180, 255}
	for _, p := range [][2]int{{7, 5}, {5, 7}, {9, 7}, {7, 9}, {8, 5}, {5, 8}, {9, 8}, {8, 9}} {
		set(frameFlower, p[0], p[1], pink)
	}
	for y := 6; y < 9; y++ {
		for x := 6; x < 9; x++ {
			set(frameFlower, x, y, color.NRGBA{250, 220, 60, 255})
		}
	}
	return img
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	mapFile := flag.String("map", "", "load this .tmx or .tmj map instead of the embedded one")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip tiled", Width: 960, Height: 640, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, mapFile: *mapFile})
	if err != nil {
		fmt.Fprintln(os.Stderr, "tiled:", err)
		os.Exit(1)
	}
}
