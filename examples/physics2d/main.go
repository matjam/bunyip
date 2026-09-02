// Command physics2d shows the 2D rigid bodies: balls, boxes and a
// triangle fall into a pit, a kinematic paddle sweeps through them, a
// trigger zone tints whatever enters it, and a raycast from the corner
// to the pointer reports what it hits. Click to drop more shapes at the
// pointer; R resets; Escape quits.
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
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
	"github.com/matjam/bunyip/rng"
)

// look says how to draw an entity's collider.
type look struct {
	Color gfx.Color
	Hot   float32 // fades after touching the trigger
}

type game struct {
	seconds float64
	shot    string

	font    *gfx.Font
	white   *gfx.Texture
	dot     *gfx.Texture
	world   *ecs.World
	shapes  *ecs.Query3[gfx.Transform2, phys.Collider2, look]
	random  *rng.Rand
	paddle  ecs.Entity
	trigger ecs.Entity
	hit     string
	rayEnd  lin.Vec2

	shotDone bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	white := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range white.Pix {
		white.Pix[i] = 255
	}
	if g.white, err = ctx.Gfx.NewTexture(white, gfx.TextureOptions{}); err != nil {
		return err
	}
	if g.dot, err = ctx.Gfx.NewTexture(circle(64), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	g.random = rng.New(9)
	g.reset(ctx)
	return nil
}

func (g *game) reset(ctx *bunyip.Context) {
	w := ecs.NewWorld()
	g.world = w
	g.shapes = ecs.NewQuery3[gfx.Transform2, phys.Collider2, look](w)
	// Screen space: y grows downward, so gravity is positive, in pixels/s².
	ecs.SetResource(w, phys.Settings2{Gravity: lin.V2(0, 900), Substeps: 4, Iterations: 10})
	W, H := ctx.Width, ctx.Height
	wallLook := look{Color: gfx.RGB(70, 74, 90)}
	w.SpawnWith(gfx.At2(W/2, H-20), phys.Collider2{Shape: phys.Box2{HalfW: W / 2, HalfH: 20}}, wallLook)
	w.SpawnWith(gfx.At2(20, H/2), phys.Collider2{Shape: phys.Box2{HalfW: 20, HalfH: H / 2}}, wallLook)
	w.SpawnWith(gfx.At2(W-20, H/2), phys.Collider2{Shape: phys.Box2{HalfW: 20, HalfH: H / 2}}, wallLook)
	// A ramp: a static rotated box.
	w.SpawnWith(gfx.Transform2{Position: lin.V2(W*0.25, H*0.55), Rotation: 0.35}, phys.Collider2{Shape: phys.Box2{HalfW: 160, HalfH: 12}}, wallLook)
	// A kinematic paddle sweeps back and forth; bodies ride it.
	paddle := phys.Kinematic2()
	g.paddle = w.SpawnWith(gfx.At2(W*0.7, H*0.7), paddle, phys.Collider2{Shape: phys.Box2{HalfW: 90, HalfH: 10}}, look{Color: gfx.RGB(255, 200, 90)})
	// A trigger zone: overlaps are reported, never resolved.
	g.trigger = w.SpawnWith(gfx.At2(W*0.5, H*0.3), phys.Collider2{Shape: phys.Circle{Radius: 60}, Trigger: true}, look{Color: gfx.RGBA(120, 200, 255, 60)})
	for i := range 24 {
		g.spawn(lin.V2(W*0.3+float32(i%6)*50, 60+float32(i/6)*60))
	}
	w.AddSystem("physics", phys.System2)
	// Entities that enter the trigger light up.
	w.AddSystem("trigger", func(w *ecs.World, dt float64) {
		for _, ev := range ecs.Events[phys.Trigger2](w) {
			if l, ok := ecs.Get[look](w, ev.Other); ok {
				l.Hot = 1
			}
		}
		g.shapes.Each(func(e ecs.Entity, _ *gfx.Transform2, _ *phys.Collider2, l *look) {
			l.Hot = max(0, l.Hot-float32(dt))
		})
	})
}

// spawn drops a random shape at p.
func (g *game) spawn(p lin.Vec2) {
	body := phys.Dynamic2(1)
	body.Restitution = g.random.Between(0, 0.5)
	body.Friction = 0.4
	c := gfx.RGB(uint8(120+g.random.Intn(120)), uint8(120+g.random.Intn(120)), uint8(120+g.random.Intn(120)))
	var shape phys.Shape2
	switch g.random.Intn(3) {
	case 0:
		shape = phys.Circle{Radius: g.random.Between(12, 24)}
	case 1:
		shape = phys.Box2{HalfW: g.random.Between(12, 28), HalfH: g.random.Between(12, 28)}
	default:
		r := g.random.Between(18, 30)
		shape = phys.Polygon2{Points: []lin.Vec2{{X: 0, Y: -r}, {X: r * 0.87, Y: r * 0.5}, {X: -r * 0.87, Y: r * 0.5}}}
	}
	g.world.SpawnWith(gfx.Transform2{Position: p, Rotation: g.random.Float() * 6}, body, phys.Collider2{Shape: shape}, look{Color: c})
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.dot.Destroy()
	g.white.Destroy()
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
	if in.KeyPressed(input.KeyR) {
		g.reset(ctx)
	}
	if in.MousePressed(input.MouseLeft) {
		x, y := in.Mouse()
		g.spawn(lin.V2(float32(x), float32(y)))
	}
	if b, ok := ecs.Get[phys.Body2](g.world, g.paddle); ok {
		b.Vel = lin.V2(220*float32(math.Sin(ctx.Time*0.8)), 0)
	}
	g.world.Update(ctx.Delta)
	// A raycast from the corner to the pointer.
	x, y := in.Mouse()
	ray := phys.Ray2{Origin: lin.V2(40, 40), Dir: lin.V2(float32(x)-40, float32(y)-40)}
	if hit, ok := phys.Raycast2(g.world, ray, 0); ok {
		g.hit = fmt.Sprintf("ray hits %v at (%.0f, %.0f)", hit.Entity, hit.Point.X, hit.Point.Y)
		g.rayEnd = hit.Point
	} else {
		g.hit = "ray hits nothing"
		g.rayEnd = ray.Origin.Add(ray.Dir)
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	g.shapes.Each(func(e ecs.Entity, t *gfx.Transform2, c *phys.Collider2, l *look) {
		col := l.Color
		if l.Hot > 0 {
			col = gfx.Color{R: min(1, col.R+l.Hot), G: col.G, B: col.B, A: col.A}
		}
		switch s := c.Shape.(type) {
		case phys.Circle:
			gr.Draw(g.dot, t.Apply(gfx.Sprite{Size: lin.V2(2*s.Radius, 2*s.Radius), UV1: lin.V2(1, 1), Color: col}))
		case phys.Box2:
			gr.Draw(g.white, t.Apply(gfx.Sprite{Size: lin.V2(2*s.HalfW, 2*s.HalfH), UV1: lin.V2(1, 1), Color: col}))
		case phys.Polygon2:
			cs, sn := float32(math.Cos(float64(t.Rotation))), float32(math.Sin(float64(t.Rotation)))
			n := len(s.Points)
			for i := range n {
				a, b := s.Points[i], s.Points[(i+1)%n]
				wa := t.Position.Add(lin.V2(cs*a.X-sn*a.Y, sn*a.X+cs*a.Y))
				wb := t.Position.Add(lin.V2(cs*b.X-sn*b.Y, sn*b.X+cs*b.Y))
				g.segment(gr, wa, wb, 3, col)
			}
		}
	})
	g.segment(gr, lin.V2(40, 40), g.rayEnd, 2, gfx.RGBA(255, 255, 120, 200))
	gr.DrawText(g.font, g.hit+"; click to drop shapes, R resets", 48, 30, gfx.RGB(230, 230, 240))
	gr.DrawText(g.font, fmt.Sprintf("%d bodies", ecs.Count[phys.Body2](g.world)), 48, 52, gfx.RGB(170, 170, 190))
	return nil
}

// segment draws a line as a thin rotated rectangle.
func (g *game) segment(gr *gfx.Graphics, a, b lin.Vec2, thick float32, col gfx.Color) {
	d := b.Sub(a)
	t := gfx.Transform2{Position: a.Add(b).Mul(0.5), Rotation: float32(math.Atan2(float64(d.Y), float64(d.X)))}
	gr.Draw(g.white, t.Apply(gfx.Sprite{Size: lin.V2(d.Len(), thick), UV1: lin.V2(1, 1), Color: col}))
}

func circle(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	for y := range size {
		for x := range size {
			d := math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r)
			a := math.Max(0, math.Min(1, r-d))
			img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, uint8(255 * a)})
		}
	}
	return img
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip physics 2D", Width: 960, Height: 640, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "physics2d:", err)
		os.Exit(1)
	}
}
