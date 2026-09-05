---
title: Physics 3D
example: physics3d
summary: five hundred cubes of seven different materials dropped into a pile, with an orbit camera and a raycast under the pointer
---

Five hundred cubes fall into a walled pen and settle into a pile. Each
one is an entity with a `gfx.Transform`, a `phys.Body3` and a
`phys.Collider3`, and one system steps them all. The cubes are shaded
with seven kinds of surface, so the pile is also a look at what
[gfx.Material](../pkg/gfx.html#Material) can do: matte plastic, brushed
metal, gold, car paint with a clearcoat, velvet with sheen, refracting
glass and a glowing one.

The program is the 3D counterpart of the
[physics2d](physics2d.html) example. Names carry the dimension:
`Body3`, `Collider3` and `Box3` are the 3D forms, and 3D space is right
handed with positive Y upwards, which is why gravity is negative here and
positive in the 2D example. Read
[the physics guide](../guides/physics.html) for the simulation and
[the 3D graphics guide](../guides/graphics-3d.html) for the camera,
lights and materials.

Run it:

```bash
go run ./examples/physics3d -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`. Dragging orbits the
camera, the wheel zooms, hovering highlights the cube under the pointer,
R drops the cubes again and Escape quits.

## Materials

`cube` is the game's own component, holding the material to draw an
entity with. A material is a plain value stored with the component, which
the draw call takes directly; it is not a separately allocated GPU resource.

`cubeMaterial` builds seven surfaces from a colour. The fields are the
usual physically based ones: `Metallic` at one makes the base colour the
reflection tint rather than a diffuse colour, `Roughness` spreads the
reflection, `Clearcoat` adds a second smooth layer over the first,
`Sheen` adds the soft edge light of cloth, and `Transmission` with an
`IOR` and a `Thickness` refracts what is behind the surface, tinted by
`AttenuationColor` over `AttenuationDistance`. `Emissive` scales the
colour the surface gives off, which is what makes the last kind glow and
what the hover highlight raises.

Every field defaults through its zero value, so an empty `gfx.Material`
is a usable matte white and a material only sets what it changes. Note
that a zero `Roughness` is not zero; it is the default 0.6, which is why
the glass case sets 0.05 explicitly.

```go
// Command physics3d drops five hundred cubes into a pile. Each is an
// entity with a transform, a rigid body and a box collider; the physics
// system stacks them with friction and restitution. Drag to orbit, hover
// to highlight the cube under the pointer (a raycast), R drops them
// again, Escape quits.
package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/phys"
	"github.com/matjam/bunyip/rng"
	"github.com/matjam/bunyip/ui"
)

const count = 500

type cube struct{ Material gfx.Material }

// palette is the cube colours: strong hues so each surface reads.
var palette = []gfx.Color{
	gfx.RGB(220, 50, 50), gfx.RGB(240, 140, 30), gfx.RGB(240, 220, 60), gfx.RGB(60, 190, 80),
	gfx.RGB(40, 200, 200), gfx.RGB(50, 110, 240), gfx.RGB(150, 70, 230), gfx.RGB(240, 100, 180),
	gfx.RGB(240, 240, 240),
}

// cubeMaterial picks one of seven kinds of surface in a colour: matte
// plastic, brushed metal, gold, car paint, velvet, glass and a glowing one.
func cubeMaterial(kind int, c gfx.Color) gfx.Material {
	switch kind {
	case 0:
		return gfx.Material{BaseColor: c, Metallic: 1, Roughness: 0.25}
	case 1:
		return gfx.Material{BaseColor: gfx.RGB(255, 200, 90), Metallic: 1, Roughness: 0.1}
	case 2:
		return gfx.Material{BaseColor: c, Roughness: 0.5, Clearcoat: 1, ClearcoatRoughness: 0.05}
	case 3:
		return gfx.Material{BaseColor: c, Roughness: 0.95, Sheen: gfx.RGB(255, 255, 255), SheenRoughness: 0.5}
	case 4:
		return gfx.Material{Roughness: 0.05, Transmission: 1, IOR: 1.5, Thickness: 1, AttenuationColor: c, AttenuationDistance: 2}
	case 5:
		return gfx.Material{BaseColor: c, Roughness: 0.6, Emissive: 1.2}
	}
	return gfx.Material{BaseColor: c, Roughness: 0.7}
}
```

## The game type

The camera is three numbers, a yaw, a pitch and a distance, rather than a
matrix; `gfx.OrbitCamera` turns them into a camera in `Draw`. `hover` is
the entity the ray last struck, or `ecs.None`.

```go
type game struct {
	seconds float64
	shot    string

	font     *gfx.Font
	ui       *ui.Context
	world    *ecs.World
	cubes    *ecs.Query2[gfx.Transform, cube]
	mesh     *gfx.Mesh
	random   *rng.Rand
	hover    ecs.Entity
	yaw      float32
	pitch    float32
	dist     float32
	lastX    float32
	lastY    float32
	dragging bool
	shotDone bool
}
```

## Init: one mesh, many cubes

`gfx.CubeMesh` returns vertices and indices for a unit cube, which
`NewMesh` uploads once. All five hundred cubes are drawn from that one
mesh with different transforms and materials, which is what lets the
renderer batch them into an instance stream.

The pen is five static colliders: a wide flat box for the ground and four
walls. A collider with no body never moves and is not integrated.
`phys.Settings3` is a resource on the world: gravity of 9.8 downwards,
four substeps and eight solver iterations, which is enough to keep a pile
this deep from sinking into itself.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	cv, ci := gfx.CubeMesh()
	if g.mesh, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	g.random = rng.New(3)
	g.yaw, g.pitch, g.dist = 0.7, 0.4, 40
	w := ecs.NewWorld()
	g.world = w
	g.cubes = ecs.NewQuery2[gfx.Transform, cube](w)
	ecs.SetResource(w, phys.Settings3{Gravity: lin.V3(0, -9.8, 0), Substeps: 4, Iterations: 8})
	// The ground and four low walls are static colliders: no body.
	w.SpawnWith(gfx.Transform{}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(30, 0.5, 30)}})
	for _, wall := range []lin.Vec3{{X: 12, Y: 1.5, Z: 0}, {X: -12, Y: 1.5, Z: 0}} {
		w.SpawnWith(gfx.Transform{Position: wall}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(0.5, 1.5, 12)}})
	}
	for _, wall := range []lin.Vec3{{X: 0, Y: 1.5, Z: 12}, {X: 0, Y: 1.5, Z: -12}} {
		w.SpawnWith(gfx.Transform{Position: wall}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(12, 1.5, 0.5)}})
	}
	w.AddSystem("physics", phys.System3)
	g.drop()
	return nil
}
```

`drop` clears the cubes and spawns them again. Despawning from inside a
query walk is allowed, because a query walks its tables last to first, so
the entity the callback removes is one the walk has already passed.

Each cube gets a position in a loose column, a random orientation from
`lin.AxisAngle` around a normalised axis, a dynamic body of mass one with
friction and a little restitution, a half-unit box collider, and a
material. Rotations are quaternions and angles are radians.

```go
// drop respawns the cubes in a loose column above the ground.
func (g *game) drop() {
	w := g.world
	g.cubes.Each(func(e ecs.Entity, _ *gfx.Transform, _ *cube) { w.Despawn(e) })
	for i := range count {
		x := float32(i%8)*1.3 - 4.5 + g.random.Between(-0.2, 0.2)
		z := float32((i/8)%8)*1.3 - 4.5 + g.random.Between(-0.2, 0.2)
		y := 3 + float32(i/64)*2.5 + g.random.Between(0, 1)
		body := phys.Dynamic3(1)
		body.Friction, body.Restitution = 0.6, 0.05
		c := palette[g.random.Intn(len(palette))]
		w.SpawnWith(
			gfx.Transform{Position: lin.V3(x, y, z), Rotation: lin.AxisAngle(lin.V3(g.random.Float(), g.random.Float(), g.random.Float()).Norm(), g.random.Float()*3)},
			body,
			phys.Collider3{Shape: phys.Box3{Half: lin.V3(0.5, 0.5, 0.5)}},
			cube{Material: cubeMaterial(g.random.Intn(9), c)},
		)
	}
}
```

```go
func (g *game) Shutdown(ctx *bunyip.Context) {
	g.mesh.Destroy()
	g.font.Destroy()
}
```

## Update: the camera and the step

The orbit camera is driven from the pointer's motion between updates.
`g.ui.WantsMouse` keeps a drag that started on the panel from turning
into a camera drag. `lin.Clamp` holds the pitch away from the poles and
the distance inside a sensible range.

`ctx.Profile` opens a named scope that ends with `step.End()`; the time
it measures turns up in `ctx.Stats.Scopes` and in the F3 overlay, and the
panel prints it. `g.world.Update(ctx.Delta)` runs the systems, which here
means the whole simulation, at the fixed step.

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
		g.drop()
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
		g.pitch = lin.Clamp(g.pitch+(y-g.lastY)*0.01, 0.05, 1.5)
	}
	g.lastX, g.lastY = x, y
	_, dy := in.Scroll()
	g.dist = lin.Clamp(g.dist-float32(dy)*2, 8, 120)
	step := ctx.Profile("physics")
	g.world.Update(ctx.Delta)
	step.End()
	return nil
}
```

## Draw: the scene

`SetCamera` and `SetLight` are frame state rather than per-draw
arguments. `gfx.OrbitCamera` takes a target, a yaw, a pitch and a
distance and returns a camera looking at the target. The light is a
directional one with a colour brighter than white, because the values are
linear and the renderer tone maps the result; `gfx.Sky` gives the ambient
term two colours, one from above and one from below, and `Shadows: true`
with a `ShadowDistance` puts the pen inside the shadow cascades.

The ground and walls are drawn as scaled cubes with
`lin.Translate(...).Mul(lin.Scale(...))`, which is the matrix form of
`DrawMesh`. These are drawing only; the colliders that stop the cubes
were spawned in `Init` and are separate values that happen to line up.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	w := g.world
	gr.SetCamera(gfx.OrbitCamera(lin.V3(0, 3, 0), g.yaw, g.pitch, g.dist))
	gr.SetLight(gfx.Light{Direction: lin.V3(-0.5, -1, -0.3), Color: gfx.Color{R: 2.4, G: 2.3, B: 2.1, A: 1},
		Sky: gfx.Sky{Zenith: gfx.Color{R: 0.3, G: 0.35, B: 0.5, A: 1}, Ground: gfx.Color{R: 0.15, G: 0.12, B: 0.1, A: 1}}, Shadows: true, ShadowDistance: 60})
	gr.DrawMesh(g.mesh, gfx.Material{BaseColor: gfx.RGB(140, 140, 150), Roughness: 0.9}, lin.Translate(lin.V3(0, 0, 0)).Mul(lin.Scale(lin.V3(60, 1, 60))))
	for _, wall := range []struct{ pos, half lin.Vec3 }{{lin.V3(12, 1.5, 0), lin.V3(0.5, 1.5, 12)}, {lin.V3(-12, 1.5, 0), lin.V3(0.5, 1.5, 12)}, {lin.V3(0, 1.5, 12), lin.V3(12, 1.5, 0.5)}, {lin.V3(0, 1.5, -12), lin.V3(12, 1.5, 0.5)}} {
		gr.DrawMesh(g.mesh, gfx.Material{BaseColor: gfx.RGB(100, 100, 110), Roughness: 0.9}, lin.Translate(wall.pos).Mul(lin.Scale(wall.half.Mul(2))))
	}
```

## Draw: picking and the cubes

`gr.ScreenRay` turns a point on the screen into a ray in the world using
the camera set this frame, which is why the picking happens in `Draw`.
The ray's direction is scaled to 200 units, because a `phys.Ray3`'s
direction carries the distance to search.

The query walk draws every cube with `DrawMeshAt`, which takes a
transform rather than a matrix. The hovered cube is drawn with a raised
`Emissive`, a copy of its material rather than a change to the component,
so the highlight lasts exactly one frame.

```go
	// The cube under the pointer, found by a raycast into the physics world.
	mx, my := ctx.Input.Mouse()
	ray := gr.ScreenRay(float32(mx), float32(my))
	g.hover = ecs.None
	if hit, ok := phys.Raycast3(w, phys.Ray3{Origin: ray.Origin, Dir: ray.Dir.Mul(200)}, 0); ok {
		g.hover = hit.Entity
	}
	g.cubes.Each(func(e ecs.Entity, t *gfx.Transform, c *cube) {
		mat := c.Material
		if e == g.hover {
			mat.Emissive = 1.5
		}
		gr.DrawMeshAt(g.mesh, mat, *t)
	})
```

## Draw: the panel

The panel reports the physics scope's time from `ctx.Stats.Scopes`, and
its button calls the same `drop` the R key does. `u.Button` returns true
on the frame it is clicked, which is the immediate-mode form: there is no
callback to register and nothing to unregister.

```go
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("500 cubes", ui.Rect{X: 12, Y: ctx.Height - 140, W: 340, H: 128}, func() {
			ms := 0.0
			if len(ctx.Stats.Scopes) > 0 {
				ms = ctx.Stats.Scopes[0].MS
			}
			u.Label(fmt.Sprintf("physics %.2f ms/frame; drag orbits, scroll zooms; plastic, metal, gold, car paint, velvet, glass and glowing cubes", ms))
			if u.Button("Drop again (R)") {
				g.drop()
			}
		})
	})
	return nil
}
```

## main

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip physics: 500 cubes", Width: 1024, Height: 680, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "physics3d:", err)
		os.Exit(1)
	}
}
```

## What to try

- Change `count` to 2000 and read the physics milliseconds in the panel.
- Drop `Substeps` to 1 in `Init` and watch the pile sink into itself.
- Give the cubes a `phys.Sphere` collider in `drop` while still drawing
  boxes, and see the drawing and the simulation disagree.
- Add a material kind to `cubeMaterial` with `Subsurface` set, or with
  `Unlit`, and extend the `Intn` range in `drop` to include it.
- Raycast from the camera in `Draw` and apply an impulse to the body it
  hits, turning the pointer into a stick.
