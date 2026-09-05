---
title: Soft bodies
example: softbody
summary: a cloth flag flapping on a pole, a jelly cube that drops and can be kicked beside a rigid crate, and a tank of 2D fluid breaking around a post
---

This program runs all three deformable simulations of
[phys/soft](../pkg/phys/soft.html) in one scene. A flag of cloth hangs
from a pole and flaps in a gust that swings around. A jelly cube falls
onto the floor, squashes and springs back, and can be kicked into the
air. A tank of two-dimensional fluid sits in the corner of the screen,
breaking around a post. A rigid crate from [phys](../pkg/phys.html)
drops beside the jelly, so the two simulations can be compared side by
side.

Soft bodies are particles held together by constraints, solved by
extended position-based dynamics. `Cloth`, `SoftBody3` and `Fluid2` are
components on the same [world](../pkg/ecs.html) as the rigid bodies, and
one system, `soft.System`, steps all three. Particles collide with the
static and kinematic colliders already in the world, through the
signed-distance queries in `phys`, and never push a rigid body back:
here the floor, the pole and the ball are colliders that both solvers
see. Read
[the physics guide](../guides/physics.html) for the model and the
tuning, and [the physics3d walkthrough](physics3d.html) for the rigid
half on its own.

Particle positions are world space, which shapes the drawing: a cloth or
a soft body carries no transform, and is drawn by keeping a `gfx.Mesh`
in step with its particles and drawing that mesh with an identity
matrix. The fluid has no mesh at all; the program draws one sprite per
particle.

Run it:

```bash
go run ./examples/softbody -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`. Dragging orbits the
camera, the wheel zooms, Space kicks the jelly, R drops everything again
and Escape quits.

## Package and constants

The flag's size is in particles rather than in metres: 26 across and 16
down, 0.14 metres apart. `crate` is a marker component with no fields,
so the one rigid body in the scene can be found by a query instead of
being kept as a field and cleared by hand.

```go
// Command softbody shows the phys/soft package: a flag of cloth
// flapping on a pole, a jelly cube dropping onto the floor beside a
// rigid crate, and a tank of two-dimensional fluid in the corner of the
// screen. Drag to orbit, scroll to zoom, Space kicks the jelly, R drops
// everything again, Escape quits.
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
	"github.com/matjam/bunyip/particle"
	"github.com/matjam/bunyip/phys"
	"github.com/matjam/bunyip/phys/soft"
	"github.com/matjam/bunyip/ui"
)

// The flag, in particles across and down, and how far apart they sit.
const (
	flagCols    = 26
	flagRows    = 16
	flagSpacing = 0.14
)

// crate marks the one rigid body in the scene, so it can be drawn from a
// query rather than kept as a field.
type crate struct{}
```

## The game type

The game holds four meshes, three textures, and the entities of the
cloth, the soft body and the fluid. `flag` and `jelly` are the meshes
that follow the two deformables; they are rebuilt whenever the bodies
are, because a mesh is shaped for the particle count it was made from.

```go
type game struct {
	seconds float64
	shot    string

	font  *gfx.Font
	ui    *ui.Context
	world *ecs.World

	cube  *gfx.Mesh
	ball  *gfx.Mesh
	flag  *gfx.Mesh
	jelly *gfx.Mesh
	white *gfx.Texture
	drop  *gfx.Texture
	disc  *gfx.Texture

	cloth  ecs.Entity
	body   ecs.Entity
	fluid  ecs.Entity
	crates *ecs.Query2[gfx.Transform, crate]

	tank     lin.Rect
	yaw      float32
	pitch    float32
	dist     float32
	lastX    float32
	lastY    float32
	dragging bool
	shotDone bool
	kicked   bool
}
```

## Init: the world, the colliders and the tank

Two resources tune the two solvers. `phys.Settings3` gives the rigid
bodies gravity, and `soft.Settings` gives the soft ones their substep
count. `Gravity3` is left zero on purpose: a zero takes the gravity from
`phys.Settings3`, so rigid and soft bodies fall together without the
number appearing twice. `Gravity2` has to be set, because the fluid
lives in view units on the screen rather than in metres in the world.

The floor, the pole and the ball are colliders with no body, which is
what makes them static. Both systems see them: `phys.System3` collides
rigid bodies against them and `soft.System` pushes particles out of
them. The two systems are registered in that order, so the soft solver
sees where the rigid bodies ended the update.

`soft.NewFluid2` takes a tank rectangle and the spacing between
particles at rest; `Fill` seeds a region of it with liquid, here the
left half, so it collapses into the rest of the tank as soon as the
simulation starts. The post in the tank is an ordinary
`phys.Collider2` on a `gfx.Transform2`, which is how a 2D obstacle
reaches the fluid.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	cv, ci := gfx.CubeMesh()
	if g.cube, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	sv, si := gfx.SphereMesh(16, 24)
	if g.ball, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	if g.drop, err = ctx.Gfx.NewTexture(particle.SoftCircle(32), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pixel.Set(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	if g.white, err = ctx.Gfx.NewTexture(pixel, gfx.TextureOptions{}); err != nil {
		return err
	}
	if g.disc, err = ctx.Gfx.NewTexture(discImage(64), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	g.yaw, g.pitch, g.dist = 0.9, 0.35, 12

	w := ecs.NewWorld()
	g.world = w
	g.crates = w.Query2[gfx.Transform, crate]()
	w.SetResource(phys.Settings3{Gravity: lin.V3(0, -9.8, 0)})
	// The soft solver takes its 3D gravity from the physics settings; the
	// fluid needs its own, in view units per second squared.
	w.SetResource(soft.Settings{Gravity2: lin.V2(0, 900), Substeps: 6})
	// The floor and the pole are static colliders, which both solvers see.
	w.SpawnWith(gfx.Transform{Position: lin.V3(0, -0.5, 0)}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(12, 0.5, 12)}})
	w.SpawnWith(gfx.Transform{Position: lin.V3(-2.2, 2, 0)}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(0.08, 2, 0.08)}})
	// A ball for the cloth to fall past on its way out.
	w.SpawnWith(gfx.Transform{Position: lin.V3(0.9, 0.75, 0.9)}, phys.Collider3{Shape: phys.Sphere{Radius: 0.75}})
	w.AddSystem("physics", phys.System3)
	w.AddSystem("soft", soft.System)

	g.tank = lin.Rect{X: 24, Y: 24, W: 280, H: 300}
	fluid := soft.NewFluid2(soft.Fluid2Spec{Bounds: g.tank, Spacing: 7})
	fluid.Fill(lin.Rect{X: g.tank.X + 8, Y: g.tank.Y + 8, W: g.tank.W/2 - 8, H: g.tank.H - 16})
	g.fluid = w.SpawnWith(fluid)
	// A post in the tank for the liquid to break around.
	w.SpawnWith(gfx.Transform2{Position: g.tank.Center()}, phys.Collider2{Shape: phys.Circle{Radius: 34}})

	if err := g.reset(ctx); err != nil {
		return err
	}
	return nil
}
```

## reset: building the cloth and the jelly

`reset` is called from `Init` and again on R, so everything it builds it
first tears down: the two soft entities, every entity carrying the
`crate` marker, and the two meshes.

`soft.NewCloth` builds a rectangular sheet: distance constraints along
the edges and diagonals, and a bending constraint across each pair of
edges in line. `Pinned` is the list of particle indices that are held,
computed here as the first particle of every row, which hangs the sheet
by its left edge. `Bend` is compliance in metres per newton, so zero is
rigid and larger is softer. XPBD accounts for the timestep in compliance,
though substeps and solver iterations still affect the result. `Wind`
pushes each cell by the air blowing through it, so a
sheet edge-on to the wind is barely moved.

`soft.NewSoftBody3` takes a closed triangle mesh, here the built-in
cube, welds the vertices that share a position into particles, and holds
them with edge constraints, one constraint on the enclosed volume and
`ShapeMatch`, which pulls the body back towards its original shape
rotated to where it is now. The crate beside it is a plain
`phys.Dynamic3` body with a box collider and the `crate` marker.

`c.NewMesh(ctx.Gfx)` uploads a mesh shaped like the cloth, and
`b.NewMesh` one shaped like the body. They are GPU resources like any
other, so the old pair is destroyed before the new one is made.

```go
// reset builds the flag and the jelly cube again, and drops the crate.
func (g *game) reset(ctx *bunyip.Context) error {
	w := g.world
	for _, e := range []ecs.Entity{g.cloth, g.body} {
		if e != ecs.None {
			w.Despawn(e)
		}
	}
	g.crates.Each(func(e ecs.Entity, _ *gfx.Transform, _ *crate) { w.Despawn(e) })
	g.kicked = false

	pinned := make([]int, 0, flagRows)
	for y := range flagRows {
		pinned = append(pinned, y*flagCols)
	}
	cloth := soft.NewCloth(soft.ClothSpec{
		Width: flagCols, Height: flagRows, Spacing: flagSpacing, Mass: 0.4,
		Origin: lin.V3(-2.1, 3.6, 0), Pinned: pinned,
		Bend: 0.05, Damping: 0.4, Wind: lin.V3(0, 0, 5),
	})
	g.cloth = w.SpawnWith(cloth)

	cv, ci := gfx.CubeMesh()
	body := soft.NewSoftBody3(soft.SoftBody3Spec{
		Vertices: cv, Indices: ci, Scale: 1.4, Position: lin.V3(1.6, 2.4, -1.4), Mass: 3,
		Compliance: 0.001, ShapeMatch: 0.04, Damping: 0.6,
	})
	g.body = w.SpawnWith(body)

	rigid := phys.Dynamic3(2)
	rigid.Friction, rigid.Restitution = 0.6, 0.1
	w.SpawnWith(gfx.Transform{Position: lin.V3(3.6, 2.4, -1.4)}, rigid,
		phys.Collider3{Shape: phys.Box3{Half: lin.V3(0.7, 0.7, 0.7)}}, crate{})

	// The meshes follow the new cloth and body.
	if g.flag != nil {
		g.flag.Destroy()
		g.jelly.Destroy()
	}
	var err error
	c, _ := w.Get[soft.Cloth](g.cloth)
	if g.flag, err = c.NewMesh(ctx.Gfx); err != nil {
		return err
	}
	b, _ := w.Get[soft.SoftBody3](g.body)
	if g.jelly, err = b.NewMesh(ctx.Gfx); err != nil {
		return err
	}
	return nil
}
```

```go
func (g *game) Shutdown(ctx *bunyip.Context) {
	g.cube.Destroy()
	g.ball.Destroy()
	g.flag.Destroy()
	g.jelly.Destroy()
	g.white.Destroy()
	g.drop.Destroy()
	g.disc.Destroy()
	g.font.Destroy()
}
```

## Update: the gust, the kick and the step

`ecs.World.Get` returns a pointer to the component in its table, so assigning
to `c.Wind` changes the value the solver will read. The gust is three
sines at different rates, which keeps the flag flapping rather than
standing out stiffly in a steady wind.

`b.AddImpulse` throws the whole body, which is how a soft body is pushed
without reaching for its particles. A timed run kicks it automatically
at three tenths of the run, so the screenshot catches the jelly in the
air.

`g.world.Update(ctx.Delta)` runs both systems at the fixed step, wrapped
in a profile scope named `soft` whose time the panel prints and the F3
overlay reports.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if in.KeyPressed(input.KeyR) {
		if err := g.reset(ctx); err != nil {
			return err
		}
	}
	// A gust that swings around, so the flag flaps rather than sitting out
	// stiffly in a steady wind.
	if c, ok := g.world.Get[soft.Cloth](g.cloth); ok {
		t := float32(ctx.Time)
		c.Wind = lin.V3(1.5*sin(t*1.7), 0.6*sin(t*2.3), 5+2.5*sin(t*1.1))
	}
	b, hasBody := g.world.Get[soft.SoftBody3](g.body)
	if hasBody && (in.KeyPressed(input.KeySpace) || (g.seconds > 0 && !g.kicked && ctx.Time >= g.seconds*0.3)) {
		b.AddImpulse(lin.V3(-9, 16, 0))
		g.kicked = true
	}
	x, y := in.Mouse()
	if in.MousePressed(input.MouseLeft) && !g.ui.WantsMouse() {
		g.dragging = true
	}
	if in.MouseReleased(input.MouseLeft) {
		g.dragging = false
	}
	if g.dragging {
		g.yaw += (x - g.lastX) * 0.01
		g.pitch = lin.Clamp(g.pitch+(y-g.lastY)*0.01, 0.05, 1.4)
	}
	g.lastX, g.lastY = x, y
	_, dy := in.Scroll()
	g.dist = lin.Clamp(g.dist-float32(dy)*0.8, 4, 40)

	step := ctx.Profile("soft")
	g.world.Update(ctx.Delta)
	step.End()
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	return nil
}
```

`sin` is a float32 convenience for the gust.

```go
func sin(v float32) float32 { return float32(math.Sin(float64(v))) }
```

`discImage` draws the post in the tank: white, with the alpha falling
off over the last unit of the radius, which is a cheap antialiased edge.

```go
// discImage is a filled circle with a soft edge, for the post in the
// tank.
func discImage(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			d := math.Sqrt(dx*dx + dy*dy)
			a := math.Max(0, math.Min(1, r-d))
			img.Set(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: uint8(a * 255)})
		}
	}
	return img
}
```

## Draw: the rigid scene

The camera, the light and the static geometry come first. The floor, the
pole and the ball are drawn as scaled meshes at the same places their
colliders were spawned; the drawing and the simulation are separate
values that happen to line up, which is worth remembering when one is
changed. The crate is drawn from the query, so the transform the
simulation wrote is the one drawn.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	w := g.world
	gr.SetCamera(gfx.OrbitCamera(lin.V3(0.6, 1.6, 0), g.yaw, g.pitch, g.dist))
	gr.SetLight(gfx.Light{
		Direction: lin.V3(-0.6, -1, -0.4), Color: gfx.Color{R: 2.6, G: 2.5, B: 2.3, A: 1},
		Sky:     gfx.Sky{Zenith: gfx.Color{R: 0.32, G: 0.38, B: 0.55, A: 1}, Ground: gfx.Color{R: 0.16, G: 0.13, B: 0.11, A: 1}},
		Shadows: true, ShadowDistance: 30,
	})
	// Floor, pole and the ball the cloth blows past.
	gr.DrawMesh(g.cube, gfx.Material{BaseColor: gfx.RGB(120, 124, 132), Roughness: 0.95},
		lin.Translate(lin.V3(0, -0.5, 0)).Mul(lin.Scale(lin.V3(24, 1, 24))))
	gr.DrawMesh(g.cube, gfx.Material{BaseColor: gfx.RGB(90, 80, 70), Roughness: 0.6},
		lin.Translate(lin.V3(-2.2, 2, 0)).Mul(lin.Scale(lin.V3(0.16, 4, 0.16))))
	gr.DrawMesh(g.ball, gfx.Material{BaseColor: gfx.RGB(200, 200, 210), Roughness: 0.3, Metallic: 0.6},
		lin.Translate(lin.V3(0.9, 0.75, 0.9)).Mul(lin.Scale(lin.V3(0.75, 0.75, 0.75))))
	g.crates.Each(func(_ ecs.Entity, t *gfx.Transform, _ *crate) {
		gr.DrawMeshAt(g.cube, gfx.Material{BaseColor: gfx.RGB(190, 140, 70), Roughness: 0.8},
			gfx.Transform{Position: t.Position, Rotation: t.Rotation, Scale: lin.V3(1.4, 1.4, 1.4)})
	})
```

## Draw: the deformables

Each frame `UpdateMesh` writes the particle positions into the mesh and
recomputes its normals. Because the particles are in world space, both
meshes are drawn with `lin.Identity()` rather than a transform. The
cloth's material is `DoubleSided`, because a sheet is seen from both
sides and its back faces would otherwise be culled. The jelly gets a
clearcoat, which reads as a wet surface.

Both calls are guarded by `ecs.World.Get`, so drawing only proceeds when the
expected component exists. R rebuilds the entities synchronously in
`Update`; `Draw` does not run halfway through that reset.

```go
	// The cloth and the jelly follow their particles. Both are drawn with
	// an identity matrix, because their particles are already in world
	// space, and the cloth is seen from both sides.
	if c, ok := w.Get[soft.Cloth](g.cloth); ok {
		if err := c.UpdateMesh(g.flag); err != nil {
			return err
		}
		gr.DrawMesh(g.flag, gfx.Material{BaseColor: gfx.RGB(220, 60, 70), Roughness: 0.85, DoubleSided: true}, lin.Identity())
	}
	if b, ok := w.Get[soft.SoftBody3](g.body); ok {
		if err := b.UpdateMesh(g.jelly); err != nil {
			return err
		}
		gr.DrawMesh(g.jelly, gfx.Material{BaseColor: gfx.RGB(80, 210, 140), Roughness: 0.25, Clearcoat: 1, ClearcoatRoughness: 0.1}, lin.Identity())
	}
	g.drawTank(ctx)
```

## Draw: the panel

The panel reports the profile scope's time and the live particle count,
and its button calls the same `AddImpulse` that Space does. `u.Button`
returns true on the frame it is clicked, which is the immediate-mode
form.

```go
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Soft bodies", ui.Rect{X: 12, Y: ctx.Height - 132, W: 380, H: 120}, func() {
			ms := 0.0
			if len(ctx.Stats.Scopes) > 0 {
				ms = ctx.Stats.Scopes[0].MS
			}
			n := 0
			if f, ok := g.world.Get[soft.Fluid2](g.fluid); ok {
				n = f.Count()
			}
			u.Label(fmt.Sprintf("soft %.2f ms/frame: a %dx%d cloth flag, a jelly cube and %d fluid particles",
				ms, flagCols, flagRows, n))
			if u.Button("Kick the jelly (Space)") {
				if b, ok := g.world.Get[soft.SoftBody3](g.body); ok {
					b.AddImpulse(lin.V3(-9, 16, 0))
				}
			}
		})
	})
	return nil
}
```

## Drawing the fluid

The fluid has no mesh: the game draws its particles itself. `Positions`
returns them, `Density(i)` and `RestDensity` say how packed each one is,
and the colour is mixed from that, so the compressed body of the liquid
is deep blue and the spray at the surface is pale.

The layers are what keeps the tank readable over the 3D scene.
`SetLayer` sets the sort key for the 2D calls that follow: 10 for the
tank's dark background, 11 for the particles, 12 for the post over them,
and back to 0 at the end, because the layer is graphics state rather
than a per-call argument. Sprites are drawn in view units with the
origin at the top left, which is the same space the tank rectangle and
the fluid's own coordinates are in.

```go
// drawTank draws the two-dimensional fluid over the scene: the tank, the
// post in it, and one soft circle per particle.
func (g *game) drawTank(ctx *bunyip.Context) {
	gr := ctx.Gfx
	f, ok := g.world.Get[soft.Fluid2](g.fluid)
	if !ok {
		return
	}
	gr.SetLayer(10)
	gr.Draw(g.white, gfx.Sprite{Pos: g.tank.Min(), Size: g.tank.Size(), Color: gfx.Color{R: 0.03, G: 0.05, B: 0.09, A: 0.85}})
	gr.SetLayer(11)
	size := f.Spacing() * 2.4
	half := lin.V2(size/2, size/2)
	for i, p := range f.Positions() {
		// Denser liquid is deeper blue; the spray at the surface is paler.
		d := lin.Clamp(f.Density(i)/f.RestDensity(), 0, 1)
		c := gfx.Color{R: 0.35 - 0.25*d, G: 0.6 - 0.2*d, B: 1, A: 0.9}
		gr.Draw(g.drop, gfx.Sprite{Pos: p.Sub(half), Size: lin.V2(size, size), Color: c})
	}
	gr.SetLayer(12)
	post := g.tank.Center()
	gr.Draw(g.disc, gfx.Sprite{Pos: post.Sub(lin.V2(34, 34)), Size: lin.V2(68, 68), Color: gfx.Color{R: 0.5, G: 0.5, B: 0.55, A: 1}})
	gr.SetLayer(0)
}
```

## main

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip soft bodies", Width: 1100, Height: 700, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "softbody:", err)
		os.Exit(1)
	}
}
```

## What to try

- Set `Pressure` above one on the body in `reset` and watch the jelly
  inflate.
- Raise the flag's `Bend` compliance in `reset` and see the sheet go
  limp, then set it to zero and see it go stiff.
- Give `soft.Settings` more `Substeps` in `Init` and watch the cloth
  stiffen and the profile scope in the panel grow.
- Add a second `phys.Collider2` to the tank in `Init` and watch the
  liquid break around both posts.
- Pin the flag's far corner as well in `reset`, by adding
  `flagCols-1` to `pinned`, and see the sheet stretch between two
  points.
