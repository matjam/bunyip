// Command physics2d shows the 2D rigid bodies: balls, boxes and a
// triangle fall into a pit, a kinematic paddle sweeps through them, a
// car on sprung wheel joints drives back and forth along the floor, a
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
	car     ecs.Entity
	wheels  [2]ecs.Entity
	axles   [2]ecs.Entity // the wheel joints, whose motors drive the car
	forward bool
	hit     string
	rayEnd  lin.Vec2

	shotDone bool
}

// Collision layers: the car's own parts pass through each other and
// through nothing else.
const (
	layerWorld  = 1
	layerFrame  = 2
	layerWheels = 4
)

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
	g.shapes = w.Query3[gfx.Transform2, phys.Collider2, look]()
	// Screen space: y grows downward, so gravity is positive, in pixels/s².
	w.SetResource(phys.Settings2{Gravity: lin.V2(0, 900), Substeps: 4, Iterations: 10})
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
	// The chassis rides an axle drop above the wheels, which rest on the
	// floor whose top is 40 below the bottom of the view.
	g.buildCar(w, lin.V2(140, H-66))
	for i := range 24 {
		g.spawn(lin.V2(W*0.3+float32(i%6)*50, 60+float32(i/6)*60))
	}
	w.AddSystem("physics", phys.System2)
	// Entities that enter the trigger light up.
	w.AddSystem("trigger", func(w *ecs.World, dt float64) {
		for _, ev := range w.Events[phys.Trigger2]() {
			if l, ok := w.Get[look](ev.Other); ok {
				l.Hot = 1
			}
		}
		g.shapes.Each(func(e ecs.Entity, _ *gfx.Transform2, _ *phys.Collider2, l *look) {
			l.Hot = max(0, l.Hot-float32(dt))
		})
	})
}

// buildCar puts a chassis on two wheels held by WheelJoint2 springs,
// with the axle motors driving the wheels. The wheel's centre is where
// the joint's AnchorA in the chassis frame points, so the suspension
// rests at the height the parts were spawned at.
func (g *game) buildCar(w *ecs.World, at lin.Vec2) {
	const (
		halfW  = 34.0 // chassis half width
		halfH  = 10.0 // chassis half height
		radius = 14.0 // wheel radius
		axleX  = 22.0 // wheel offset from the chassis centre
		axleY  = 12.0 // wheel drop below the chassis centre
	)
	frame := phys.Dynamic2(8)
	frame.Friction = 0.6
	g.car = w.SpawnWith(gfx.Transform2{Position: at}, frame,
		phys.Collider2{Shape: phys.Box2{HalfW: halfW, HalfH: halfH},
			Layers: phys.Layers{Layer: layerFrame, Mask: layerWorld}},
		look{Color: gfx.RGB(230, 110, 110)})
	for i, dx := range [2]float32{-axleX, axleX} {
		wheel := phys.Dynamic2(1)
		wheel.Friction = 0.9
		g.wheels[i] = w.SpawnWith(gfx.Transform2{Position: at.Add(lin.V2(dx, axleY))}, wheel,
			phys.Collider2{Shape: phys.Circle{Radius: radius},
				Layers: phys.Layers{Layer: layerWheels, Mask: layerWorld}},
			look{Color: gfx.RGB(24, 26, 34)})
		g.axles[i] = w.SpawnWith(phys.WheelJoint2{A: g.car, B: g.wheels[i],
			AnchorA: lin.V2(dx, axleY), Axis: lin.V2(0, 1),
			Frequency: 5, DampingRatio: 0.8, MaxMotorTorque: 60000})
	}
	g.forward = true
}

// drive turns the wheels, reversing when the car nears a wall. Screen
// coordinates grow downward, so a wheel turning the way the angle grows
// rolls to the right.
func (g *game) drive(ctx *bunyip.Context) {
	t, ok := g.world.Get[gfx.Transform2](g.car)
	if !ok {
		return
	}
	if t.Position.X > ctx.Width-120 {
		g.forward = false
	} else if t.Position.X < 120 {
		g.forward = true
	}
	speed := float32(16)
	if !g.forward {
		speed = -16
	}
	for _, e := range g.axles {
		if j, ok := g.world.Get[phys.WheelJoint2](e); ok {
			j.MotorSpeed = speed
		}
	}
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
	if b, ok := g.world.Get[phys.Body2](g.paddle); ok {
		b.Vel = lin.V2(220*float32(math.Sin(ctx.Time*0.8)), 0)
	}
	g.drive(ctx)
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
	// A spoke on each wheel, so the drive is visible.
	for _, e := range g.wheels {
		t, ok := g.world.Get[gfx.Transform2](e)
		c, ok2 := g.world.Get[phys.Collider2](e)
		if !ok || !ok2 {
			continue
		}
		r := c.Shape.(phys.Circle).Radius
		dir := lin.V2(float32(math.Cos(float64(t.Rotation))), float32(math.Sin(float64(t.Rotation))))
		g.segment(gr, t.Position.Sub(dir.Mul(r)), t.Position.Add(dir.Mul(r)), 3, gfx.RGB(200, 200, 215))
	}
	g.segment(gr, lin.V2(40, 40), g.rayEnd, 2, gfx.RGBA(255, 255, 120, 200))
	gr.DrawText(g.font, g.hit+"; click to drop shapes, R resets", 48, 30, gfx.RGB(230, 230, 240))
	gr.DrawText(g.font, fmt.Sprintf("%d bodies", g.world.Count[phys.Body2]()), 48, 52, gfx.RGB(170, 170, 190))
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
