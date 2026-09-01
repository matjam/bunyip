// Command sprites bounces a few hundred textured sprites around the window
// with a coloured grid behind them. Escape quits; -seconds and -shot make it
// self-verifying.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"os"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

type ball struct {
	pos, vel lin.Vec2
	tint     gfx.Color
	spin     float32
}

type game struct {
	seconds   float64
	shot      string
	tex       *gfx.Texture
	balls     []ball
	shotTaken bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.tex, err = ctx.Gfx.NewTexture(discImage(48), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 300 {
		g.balls = append(g.balls, ball{
			pos:  lin.V2(rng.Float32()*ctx.Width, rng.Float32()*ctx.Height),
			vel:  lin.V2(rng.Float32()*400-200, rng.Float32()*400-200),
			tint: gfx.RGB(uint8(80+rng.IntN(175)), uint8(80+rng.IntN(175)), uint8(80+rng.IntN(175))),
			spin: rng.Float32()*4 - 2,
		})
	}
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) { g.tex.Destroy() }

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotTaken && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotTaken = true
	}
	dt := float32(ctx.Delta)
	for i := range g.balls {
		b := &g.balls[i]
		b.pos = b.pos.Add(b.vel.Mul(dt))
		if b.pos.X < 0 || b.pos.X > ctx.Width {
			b.vel.X = -b.vel.X
			b.pos.X = lin.Clamp(b.pos.X, 0, ctx.Width)
		}
		if b.pos.Y < 0 || b.pos.Y > ctx.Height {
			b.vel.Y = -b.vel.Y
			b.pos.Y = lin.Clamp(b.pos.Y, 0, ctx.Height)
		}
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	const cell = 40
	for y := float32(0); y < ctx.Height; y += cell {
		for x := float32(0); x < ctx.Width; x += cell {
			shade := uint8(30 + 12*(int(x/cell+y/cell)%2))
			ctx.Gfx.FillRect(x, y, cell-1, cell-1, gfx.RGB(shade, shade, shade+8))
		}
	}
	mx, my := ctx.Input.Mouse()
	ctx.Gfx.FillRect(float32(mx)-4, float32(my)-4, 8, 8, gfx.RGB(255, 220, 0))
	for _, b := range g.balls {
		ctx.Gfx.Draw(g.tex, gfx.Sprite{
			Pos: b.pos, Size: lin.V2(48, 48), Origin: lin.V2(0.5, 0.5),
			Color: b.tint, Rotation: b.spin * float32(ctx.Time),
		})
	}
	return nil
}

// discImage draws an anti-aliased disc with a highlight.
func discImage(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			d := math.Sqrt(dx*dx+dy*dy) / r
			a := lin.Clamp(float32((1-d)*r), 0, 1)
			light := 0.6 + 0.4*math.Max(0, 1-math.Hypot(dx+r*0.3, dy+r*0.3)/r)
			v := uint8(255 * light * float64(a))
			img.SetRGBA(x, y, color.RGBA{v, v, v, uint8(255 * a)})
		}
	}
	return img
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip sprites", Width: 960, Height: 600, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "sprites:", err)
		os.Exit(1)
	}
}
