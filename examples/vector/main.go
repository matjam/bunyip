// Command vector draws with paths: filled shapes with both fill rules,
// curves and arcs, strokes with every cap and join, textured fills,
// blend modes and the transform stack, all anti-aliased and all going
// through the same 2D stream as sprites and text. Escape quits.
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
)

type game struct {
	seconds float64
	shot    string

	font     *gfx.Font
	stripes  *gfx.Texture
	shotDone bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 14, gfx.FontOptions{}); err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			c := color.RGBA{240, 200, 90, 255}
			if (x+y)/8%2 == 0 {
				c = color.RGBA{200, 80, 60, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	if g.stripes, err = ctx.Gfx.NewTexture(img, gfx.TextureOptions{Linear: true, Repeat: true}); err != nil {
		return err
	}
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.stripes.Destroy()
	g.font.Destroy()
}

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	return nil
}

func (g *game) label(gr *gfx.Graphics, text string, x, y float32) {
	gr.DrawText(g.font, text, x, y, gfx.RGB(200, 205, 215))
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	t := float32(ctx.Time)
	var p gfx.Path

	// Fill rules: a pentagram is solid under non-zero and hollow under even-odd.
	for i, rule := range []gfx.FillRule{gfx.FillNonZero, gfx.FillEvenOdd} {
		p.Reset()
		cx, cy := 110+float32(i)*180, float32(110)
		for k := range 5 {
			a := float64(k)*4*math.Pi/5 + float64(t)*0.3
			x, y := cx+80*float32(math.Sin(a)), cy-80*float32(math.Cos(a))
			if k == 0 {
				p.MoveTo(x, y)
			} else {
				p.LineTo(x, y)
			}
		}
		p.Close()
		gr.FillPath(&p, gfx.RGB(255, 200, 80), gfx.FillOptions{Rule: rule})
		gr.StrokePath(&p, gfx.RGB(255, 255, 255), gfx.StrokeOptions{Width: 1.5, Join: gfx.JoinRound})
	}
	g.label(gr, "non-zero", 80, 200)
	g.label(gr, "even-odd", 262, 200)

	// A ring: two circles, the inner one a hole under even-odd, then a
	// textured fill of a rounded shape.
	p.Reset()
	p.Circle(470, 110, 80).Circle(470, 110, 45)
	gr.FillPath(&p, gfx.RGB(90, 170, 220), gfx.FillOptions{Rule: gfx.FillEvenOdd})
	p.Reset()
	p.RoundRect(590, 40, 160, 140, 30)
	gr.FillPath(&p, gfx.White, gfx.FillOptions{Texture: g.stripes, TextureOrigin: lin.V2(590+t*20, 40), TextureSize: lin.V2(64, 64)})
	g.label(gr, "hole by even-odd", 410, 200)
	g.label(gr, "textured fill", 625, 200)

	// Curves: a quadratic, a cubic and arcs, stroked.
	p.Reset()
	p.MoveTo(40, 340).QuadTo(140, 220, 240, 340).CubicTo(300, 420, 360, 220, 420, 340)
	gr.StrokePath(&p, gfx.RGB(120, 220, 160), gfx.StrokeOptions{Width: 6, Cap: gfx.CapRound})
	p.Reset()
	p.MoveTo(470, 340).ArcTo(560, 240, 650, 340, 60).LineTo(700, 340)
	gr.StrokePath(&p, gfx.RGB(220, 140, 220), gfx.StrokeOptions{Width: 6})
	p.Reset()
	p.Arc(760, 300, 50, math.Pi, math.Pi*1.5*(0.6+0.4*float32(math.Sin(float64(t)))))
	gr.StrokePath(&p, gfx.RGB(255, 180, 120), gfx.StrokeOptions{Width: 10, Cap: gfx.CapButt})
	g.label(gr, "quadratic and cubic", 100, 360)
	g.label(gr, "arcTo corner", 520, 360)
	g.label(gr, "arc", 745, 360)

	// Caps and joins.
	caps := []gfx.LineCap{gfx.CapButt, gfx.CapRound, gfx.CapSquare}
	capNames := []string{"butt", "round", "square"}
	for i, c := range caps {
		y := 420 + float32(i)*36
		gr.StrokeLine(60, y, 200, y, 1, gfx.RGB(90, 90, 100))
		p.Reset()
		p.MoveTo(60, y).LineTo(200, y)
		gr.StrokePath(&p, gfx.RGBA(255, 255, 255, 180), gfx.StrokeOptions{Width: 18, Cap: c})
		g.label(gr, capNames[i], 215, y-8)
	}
	joins := []gfx.LineJoin{gfx.JoinMiter, gfx.JoinRound, gfx.JoinBevel}
	joinNames := []string{"miter", "round", "bevel"}
	for i, j := range joins {
		x := 330 + float32(i)*150
		p.Reset()
		p.MoveTo(x, 520).LineTo(x+40, 420).LineTo(x+80, 520)
		gr.StrokePath(&p, gfx.RGB(160, 200, 255), gfx.StrokeOptions{Width: 18, Join: j})
		g.label(gr, joinNames[i], x+20, 530)
	}

	// Blend modes over a striped background, and the transform stack.
	gr.Draw(g.stripes, gfx.Sprite{Pos: lin.V2(60, 580), Size: lin.V2(700, 120), UV1: lin.V2(11, 2)})
	modes := []gfx.Blend{gfx.BlendAlpha, gfx.BlendAdd, gfx.BlendMultiply, gfx.BlendScreen, gfx.BlendLighten, gfx.BlendDarken, gfx.BlendErase}
	for i, m := range modes {
		x := 100 + float32(i)*100
		gr.Blended(m, func() {
			gr.FillCircle(x, 640, 38, gfx.RGBA(80, 140, 230, 200))
		})
		g.label(gr, m.String(), x-20, 700)
	}
	gr.Transformed(lin.Translate2(880, 620).Mul(lin.Rotate2(t*0.7)).Mul(lin.Shear2(0.3, 0)), func() {
		p.Reset()
		p.RoundRect(-50, -40, 100, 80, 16)
		gr.FillPath(&p, gfx.RGB(90, 200, 120), gfx.FillOptions{})
		gr.StrokePath(&p, gfx.White, gfx.StrokeOptions{Width: 3})
		gr.DrawText(g.font, "transformed", -38, -8, gfx.White)
	})
	return nil
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip vector paths", Width: 1024, Height: 720, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "vector:", err)
		os.Exit(1)
	}
}
