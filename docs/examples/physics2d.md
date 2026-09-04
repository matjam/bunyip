---
title: Physics 2D
example: physics2d
summary: balls, boxes and triangles falling into a pit with a ramp, a kinematic paddle, a trigger zone and a raycast
---

This program is the 2D half of [phys](../pkg/phys.html). Static walls
make a pit, a rotated static box makes a ramp, a kinematic paddle sweeps
back and forth, a circular trigger tints whatever passes through it, and
two dozen dynamic circles, boxes and triangles fall into the pile. A ray
is cast from the top left corner to the pointer every update and the
entity it hits is named.

Physics in Bunyip is a set of components on the
[entity component system](../pkg/ecs.html). A body is `phys.Body2`, a
shape is `phys.Collider2`, and the position they act on is
`gfx.Transform2`, the same component the drawing reads. The simulation is
a system registered on the world, and the game steps it by calling
`world.Update`. Names carry the dimension, so `Body2` and `Box2` are the
2D forms and `Circle` has no suffix because it exists in one dimension
only. [The physics guide](../guides/physics.html) covers the model, and
[the entities and systems guide](../guides/ecs.html) the world.

Run it:

```bash
go run ./examples/physics2d -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`. A left click drops a new
random shape at the pointer, R resets the scene and Escape quits.

## Package and state

`look` is the game's own component: the colour to draw a collider in, and
a value that fades after the entity has touched the trigger. Storing it
as a component rather than in a map keeps it in the same tables as the
transform, so one query reads all three.

`ecs.Query3` is a query over three component types, cached on the world.
It is created once and walked every frame.

```go
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
```

## Init: the textures

Two textures cover every shape here: a two by two white image stretched
into rectangles, and a soft-edged disc for circles. The disc asks for
`Linear: true` so it stays smooth when it is scaled up. `rng.New(9)`
seeds the shape generator so a screenshot of the pile is reproducible.

```go
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
```

## Building the scene

`reset` builds a whole world from nothing, which is why R can restart the
scene by calling it again.

`phys.Settings2` is a resource on the world rather than a component: one
value the simulation reads. Gravity is positive on Y because 2D view
units run downwards from the top left. `Substeps` splits each step into
smaller integrations and `Iterations` is the solver's passes per substep;
both trade time for a stiffer pile.

The three walls and the ramp are colliders with no body, which is what
makes them static: they collide and never move. The paddle is
`phys.Kinematic2`, a body that is moved by setting its velocity and is
not pushed back by what it hits. The trigger is a collider with
`Trigger: true`, so overlaps are reported as events and never resolved.

`w.SpawnWith` creates an entity with the components given. `gfx.At2` is
the short form of a `gfx.Transform2` at a position with no rotation.

Two systems are registered. `phys.System2` is the simulation. The second
is a closure over the game: it reads this step's `phys.Trigger2` events,
lights up the entity on the other side of each overlap, and fades every
`Hot` value towards zero. Events are drained per step, so a system reads
them by asking the world rather than by subscribing.

```go
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
```

`spawn` drops one random shape. `phys.Dynamic2(1)` is a body of mass one;
`Restitution` is bounciness from zero to one and `Friction` resists
sliding. The shape is one of `phys.Circle`, `phys.Box2` or
`phys.Polygon2`, all of which satisfy `phys.Shape2`. A polygon's points
are in the body's own frame and must be convex, in either winding order;
the triangle here is built around the origin so it spins about its
middle. Rotation is in radians.

```go
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
```

```go
func (g *game) Shutdown(ctx *bunyip.Context) {
	g.dot.Destroy()
	g.white.Destroy()
	g.font.Destroy()
}
```

## Update: input, the paddle and the ray

`Update` runs at the fixed step, which is what the simulation needs: a
fixed `ctx.Delta` gives the same result on every machine.

The paddle is driven by writing its velocity. `ecs.Get` returns a pointer
to the component in its table, so assigning through it changes the
world's copy. A kinematic body ignores forces, so a velocity written here
holds until it is written again, and bodies resting on the paddle are
carried along by the contact.

`g.world.Update(ctx.Delta)` runs the registered systems in order, which
is where the simulation actually steps.

The raycast comes after the step, so it sees the positions the frame will
draw. `phys.Ray2` is an origin and a direction whose length is the
distance to search; the final argument of `phys.Raycast2` is a layer
mask, and zero means every layer. Triggers are never hit by a ray. The
hit carries the entity, the point, the surface normal and the distance,
and the drawing uses the point to stop the line where it struck.

```go
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
```

## Draw: the shapes

One walk of the query draws everything. The `Each` callback is handed
pointers into the component tables, so no copying happens and the values
seen are the ones the simulation just wrote.

A collider's shape is an interface value, so a type switch decides how to
draw it. `t.Apply` takes a sprite and returns it positioned and rotated
by the transform, which keeps the drawing in step with the body without
recomputing the matrix. `UV1: lin.V2(1, 1)` uses the whole texture.
Triangles are drawn as three lines, rotating each point by the
transform's own angle, because there is no filled polygon call in the 2D
API.

```go
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
```

`segment` draws a line as a thin rectangle: the midpoint is the sprite's
position, the length its width and the angle between the endpoints its
rotation. It is the smallest way to draw a line with the sprite path.
[gfx](../pkg/gfx.html) also has a vector path API, which the vector
example uses.

```go
// segment draws a line as a thin rotated rectangle.
func (g *game) segment(gr *gfx.Graphics, a, b lin.Vec2, thick float32, col gfx.Color) {
	d := b.Sub(a)
	t := gfx.Transform2{Position: a.Add(b).Mul(0.5), Rotation: float32(math.Atan2(float64(d.Y), float64(d.X)))}
	gr.Draw(g.white, t.Apply(gfx.Sprite{Size: lin.V2(d.Len(), thick), UV1: lin.V2(1, 1), Color: col}))
}
```

`circle` builds the disc texture: white everywhere, with the alpha
falling off over the last unit of the radius, which is a cheap
antialiased edge. It returns an `image.NRGBA`, unpremultiplied, and
`NewTexture` premultiplies it in linear light on the way to the GPU.

```go
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
```

## main

```go
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
```

## What to try

- Raise `Substeps` and `Iterations` in `reset` and watch the pile settle
  harder and the frame cost rise.
- Set `Restitution` to 0.9 in `spawn` and drop a few shapes onto the
  ramp.
- Make the trigger a solid collider by removing `Trigger: true` in
  `reset`, and see the overlap resolved instead of reported.
- Give the paddle a vertical velocity as well in `Update` and watch
  bodies ride it.
- Cast the ray from the pointer in a direction in `Update` rather than
  towards it, and draw the surface normal the hit reports.
