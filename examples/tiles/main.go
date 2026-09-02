// Command tiles is the 2D tour: a generated sprite sheet, a tilemap with
// view culling, a Camera2D that follows the player (scroll to zoom, Q and
// E to rotate), a walking animation, draw layers, particles driven by a
// timer and tweens, and a nine-slice HUD with wrapped, centred text.
// Move with WASD or the arrow keys; Escape quits.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/rng"
	"github.com/matjam/bunyip/timer"
	"github.com/matjam/bunyip/tween"
)

const (
	tile     = 16 // sheet frame size
	tileDraw = 32 // on-screen tile size
	mapW     = 64
	mapH     = 48
)

// Sheet frames.
const (
	frameGrass = iota
	frameDirt
	frameWater
	frameWall
	frameWalk0 // four walking frames follow
)

type particle struct {
	pos   lin.Vec2
	vel   lin.Vec2
	life  *tween.Tween
	color gfx.Color
}

type game struct {
	seconds float64
	shot    string

	font      *gfx.Font
	sheetTex  *gfx.Texture
	hudTex    *gfx.Texture
	sheet     *gfx.Sheet
	tilemap   *gfx.Tilemap
	walk      gfx.Animation
	anim      gfx.AnimState
	player    lin.Vec2
	facing    float32
	cam       gfx.Camera2D
	timers    timer.Scheduler
	particles []particle
	bob       *tween.Tween
	random    *rng.Rand
	shotDone  bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	if g.sheetTex, err = ctx.Gfx.NewTexture(makeSheet(), gfx.TextureOptions{}); err != nil {
		return err
	}
	if g.hudTex, err = ctx.Gfx.NewTexture(makeHUD(), gfx.TextureOptions{Linear: true, NoMipmaps: true}); err != nil {
		return err
	}
	g.sheet = gfx.NewSheet(g.sheetTex, tile, tile)
	g.random = rng.New(7)
	g.tilemap = gfx.NewTilemap(g.sheet, mapW, mapH)
	g.tilemap.TileW, g.tilemap.TileH = tileDraw, tileDraw
	for y := range mapH {
		for x := range mapW {
			f := frameGrass
			switch {
			case x == 0 || y == 0 || x == mapW-1 || y == mapH-1:
				f = frameWall
			case (x-40)*(x-40)+(y-30)*(y-30) < 40:
				f = frameWater
			case g.random.Chance(0.12):
				f = frameDirt
			}
			g.tilemap.Set(x, y, f)
		}
	}
	g.walk = gfx.Animation{Frames: []int{frameWalk0, frameWalk0 + 1, frameWalk0 + 2, frameWalk0 + 3}, FPS: 8, Loop: true}
	g.anim.Play(&g.walk)
	g.player = lin.V2(mapW*tileDraw/2, mapH*tileDraw/2)
	g.cam = gfx.Camera2D{Position: g.player, Zoom: 1.5}
	g.bob = tween.New(0, 1, 0.6, tween.InOutSine)
	g.bob.Repeat, g.bob.YoYo = -1, true
	// A timer sprinkles particles behind the player while it moves.
	g.timers.Every(0.05, func() {
		if g.anim.Anim == nil {
			return
		}
		g.particles = append(g.particles, particle{
			pos:   g.player.Add(lin.V2(tileDraw/2, tileDraw)),
			vel:   lin.V2(g.random.Between(-40, 40), g.random.Between(-60, -20)),
			life:  tween.New(1, 0, 0.8, tween.OutQuad),
			color: gfx.RGB(uint8(200+g.random.Intn(55)), uint8(160+g.random.Intn(60)), 80),
		})
	})
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.font.Destroy()
	g.sheetTex.Destroy()
	g.hudTex.Destroy()
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
	dt := float32(ctx.Delta)
	var move lin.Vec2
	if in.KeyDown(input.KeyA) || in.KeyDown(input.KeyLeft) {
		move.X--
	}
	if in.KeyDown(input.KeyD) || in.KeyDown(input.KeyRight) {
		move.X++
	}
	if in.KeyDown(input.KeyW) || in.KeyDown(input.KeyUp) {
		move.Y--
	}
	if in.KeyDown(input.KeyS) || in.KeyDown(input.KeyDown) {
		move.Y++
	}
	if g.seconds > 0 && move == (lin.Vec2{}) {
		move = lin.V2(float32(math.Cos(ctx.Time)), float32(math.Sin(ctx.Time*0.7))) // wander for screenshots
	}
	if move != (lin.Vec2{}) {
		next := g.player.Add(move.Norm().Mul(160 * dt))
		if g.walkable(next) {
			g.player = next
		}
		g.facing = move.X
		if g.anim.Anim == nil {
			g.anim.Play(&g.walk)
		}
		g.anim.Advance(ctx.Delta)
	} else {
		g.anim.Anim = nil
	}
	if in.KeyDown(input.KeyQ) {
		g.cam.Rotation += dt
	}
	if in.KeyDown(input.KeyE) {
		g.cam.Rotation -= dt
	}
	_, dy := in.Scroll()
	g.cam.Zoom = lin.Clamp(g.cam.Zoom*float32(math.Pow(1.1, dy)), 0.4, 4)
	// The camera eases toward the player.
	g.cam.Position = g.cam.Position.Lerp(g.player.Add(lin.V2(tileDraw/2, tileDraw/2)), 1-float32(math.Pow(0.02, ctx.Delta)))
	g.timers.Update(ctx.Delta)
	g.bob.Update(dt)
	live := g.particles[:0]
	for _, p := range g.particles {
		p.life.Update(dt)
		p.pos = p.pos.Add(p.vel.Mul(dt))
		if !p.life.Done() {
			live = append(live, p)
		}
	}
	g.particles = live
	return nil
}

// walkable keeps the player off walls and water; the map cell under the
// sprite's feet decides.
func (g *game) walkable(p lin.Vec2) bool {
	x, y := int((p.X+tileDraw/2)/tileDraw), int((p.Y+tileDraw)/tileDraw)
	f := g.tilemap.Get(x, y)
	return f != frameWall && f != frameWater && f >= 0
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	gr.SetCamera2D(g.cam)
	gr.SetLayer(0)
	gr.DrawTilemap(g.tilemap, 0, 0, gfx.White)
	gr.SetLayer(1)
	for _, p := range g.particles {
		a := p.life.Value()
		c := p.color
		c.A = a
		gr.FillRect(p.pos.X-2, p.pos.Y-2, 4, 4, c)
	}
	gr.SetLayer(2)
	frame := frameWalk0
	if g.anim.Anim != nil {
		frame = g.anim.Frame()
	}
	bob := g.bob.Value() * 2
	s := gfx.Sprite{Pos: lin.V2(g.player.X, g.player.Y-bob), Size: lin.V2(tileDraw, tileDraw), Color: gfx.White}
	if g.facing < 0 { // flip by swapping the horizontal UVs
		uv0, uv1 := g.sheet.UV(frame)
		s.UV0, s.UV1 = lin.V2(uv1.X, uv0.Y), lin.V2(uv0.X, uv1.Y)
		gr.Draw(g.sheetTex, s)
	} else {
		gr.DrawFrame(g.sheet, frame, s)
	}
	// The HUD is in screen space, above everything.
	gr.ScreenSpace()
	gr.SetLayer(10)
	gr.DrawNineSlice(g.hudTex, 12, 12, 300, 92, 8, 8, 8, 8, gfx.White)
	text := fmt.Sprintf("WASD moves, Q/E rotate, scroll zooms. Camera zoom %.2f, %d particles, %d×%d tiles culled to the view.",
		g.cam.Zoom, len(g.particles), mapW, mapH)
	gr.DrawTextBlock(g.font, text, 22, 22, gfx.TextOptions{Width: 280, Align: gfx.AlignCenter}, gfx.RGB(240, 235, 220))
	gr.SetLayer(0)
	return nil
}

// makeSheet paints the tile and character frames.
func makeSheet() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, tile*8, tile))
	set := func(frame, x, y int, c color.RGBA) { img.SetRGBA(frame*tile+x, y, c) }
	r := rng.New(3)
	for y := range tile {
		for x := range tile {
			v := uint8(r.Intn(20))
			set(frameGrass, x, y, color.RGBA{70 + v, 140 + v, 60, 255})
			set(frameDirt, x, y, color.RGBA{120 + v, 90 + v/2, 50, 255})
			w := uint8(20 * math.Abs(math.Sin(float64(x+y)*0.8)))
			set(frameWater, x, y, color.RGBA{40, 90 + w, 180 + w/2, 255})
			edge := x%8 == 0 || y%8 == 0
			c := color.RGBA{110 + v, 105 + v, 100, 255}
			if edge {
				c = color.RGBA{60, 58, 55, 255}
			}
			set(frameWall, x, y, c)
		}
	}
	// A little walker: head, body, and legs that alternate per frame.
	for f := range 4 {
		frame := frameWalk0 + f
		for y := 2; y < 7; y++ {
			for x := 5; x < 11; x++ {
				set(frame, x, y, color.RGBA{250, 220, 180, 255})
			}
		}
		for y := 7; y < 12; y++ {
			for x := 4; x < 12; x++ {
				set(frame, x, y, color.RGBA{200, 60, 60, 255})
			}
		}
		stride := []int{0, 1, 0, -1}[f]
		for y := 12; y < 16; y++ {
			set(frame, 5+stride, y, color.RGBA{40, 40, 90, 255})
			set(frame, 6+stride, y, color.RGBA{40, 40, 90, 255})
			set(frame, 9-stride, y, color.RGBA{40, 40, 90, 255})
			set(frame, 10-stride, y, color.RGBA{40, 40, 90, 255})
		}
	}
	return img
}

// makeHUD draws a 24×24 bordered box for nine-slicing.
func makeHUD() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	for y := range 24 {
		for x := range 24 {
			d := min(x, y, 23-x, 23-y)
			switch {
			case d < 2:
				img.SetNRGBA(x, y, color.NRGBA{230, 200, 120, 255})
			case d < 4:
				img.SetNRGBA(x, y, color.NRGBA{90, 60, 30, 255})
			default:
				img.SetNRGBA(x, y, color.NRGBA{30, 24, 20, 220})
			}
		}
	}
	return img
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip tiles", Width: 960, Height: 640, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "tiles:", err)
		os.Exit(1)
	}
}
