---
title: Solar system
example: solar
summary: the entity component system driving a 3D scene, with a parent-child hierarchy, an instanced asteroid belt, picking and a render-texture minimap
---

This example is the entity component system used as a scene graph. A sun
with four planets and their moons is a parent-child hierarchy, so a moon
follows its planet without any code saying so. Four hundred asteroids
are separate entities that share one mesh and one material, and draw as
a single instanced call. Two systems move everything, a click picks a
body with a screen ray, the same scene is drawn a second time from above
into a render texture for the minimap, and the work is bracketed by
profile scopes that the F3 overlay reports.

The packages are [ecs](../pkg/ecs.html) for the world, the components,
the queries, the resources, the hierarchy and the systems, and
[gfx](../pkg/gfx.html) for the meshes, the camera, the lights, the
render texture and picking. The guides are
[Entities and systems](../guides/ecs.html) and
[3D graphics](../guides/graphics-3d.html).

Run it with:

```bash
go run ./examples/solar -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`. Click a body to select
it, press F3 for the debug overlay, Escape quits. `Config.Debug` is
true, which is what makes the overlay and its profile scopes available.

## Components and resources

A component is any Go type. `body` is what a thing looks like, `orbit`
is a circular path with an angle, `spin` is a rotation rate, and
`asteroid` is an empty marker type used only to tell belt members apart
in a query. `clock` is a resource: the world has exactly one, so it is
not attached to an entity.

`game` holds the meshes, the world, one cached query, the render texture
and the selected entity. `ecs.Entity` is a generational handle, so
keeping one across frames is safe: it does not dangle if the entity is
despawned, the lookup simply fails.

```go
// Components.
type body struct {
	Name     string
	Radius   float32
	Color    gfx.Color
	Emissive float32
}

type orbit struct {
	Radius float32
	Speed  float32 // radians per second
	Angle  float32
}

type spin struct{ Speed float32 }

// asteroid marks belt members, which draw as cubes.
type asteroid struct{}

// clock is a resource: elapsed time for the spin system.
type clock struct{ Time float32 }

type game struct {
	seconds float64
	shot    string

	font     *gfx.Font
	sphere   *gfx.Mesh
	cube     *gfx.Mesh
	world    *ecs.World
	bodies   *ecs.Query1[body]
	minimap  *gfx.RenderTexture
	selected ecs.Entity
	yaw      float32
	shotDone bool
}
```

## Init: the world, the hierarchy and the belt

`NewRenderTexture(220, 220)` allocates an offscreen target and
`SetView(220, 220)` gives it its own view size, so drawing into it uses
those coordinates rather than the window's.

`ecs.NewWorld` creates the world and `ecs.SetResource` installs the
clock. `w.SpawnWith(...)` spawns an entity with a list of components,
which is how an archetype is chosen: entities with the same set of
component types share a table, and their components sit in dense typed
columns.

`gfx.Transform{}` is given to everything that is drawn. Its zero value
is the identity: position at the origin, no rotation, unit scale.

`ecs.SetParent(w, child, parent)` builds the hierarchy. A planet is a
child of the sun and a moon is a child of its planet, so a moon's
`orbit` places it relative to its planet and `ecs.WorldMatrix` composes
the chain when it is drawn.

The belt is four hundred entities with the same components. Because they
share a mesh and a material, the renderer merges them into one instanced
draw, which is why the count in the corner of the terrain example
matters: entities are cheap, draws are not.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 16, gfx.FontOptions{}); err != nil {
		return err
	}
	sv, si := gfx.SphereMesh(20, 40)
	if g.sphere, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	cv, ci := gfx.CubeMesh()
	if g.cube, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	if g.minimap, err = ctx.Gfx.NewRenderTexture(220, 220); err != nil {
		return err
	}
	g.minimap.SetView(220, 220)

	w := ecs.NewWorld()
	ecs.SetResource(w, clock{})
	sun := w.SpawnWith(body{Name: "Sun", Radius: 1.4, Color: gfx.RGB(255, 200, 90), Emissive: 3}, spin{0.2}, gfx.Transform{})
	planets := []struct {
		name  string
		r, d  float32
		speed float32
		col   gfx.Color
		moons int
	}{
		{"Ember", 0.35, 3.2, 0.9, gfx.RGB(200, 120, 80), 0},
		{"Verdis", 0.55, 5.4, 0.55, gfx.RGB(90, 170, 110), 1},
		{"Halcyon", 0.8, 8.2, 0.32, gfx.RGB(120, 150, 230), 2},
		{"Umber", 0.45, 11, 0.2, gfx.RGB(180, 140, 100), 1},
	}
	random := rng.New(11)
	for _, p := range planets {
		e := w.SpawnWith(body{Name: p.name, Radius: p.r, Color: p.col},
			orbit{Radius: p.d, Speed: p.speed, Angle: random.Float() * 6.28}, spin{1}, gfx.Transform{})
		ecs.SetParent(w, e, sun)
		for m := range p.moons {
			moon := w.SpawnWith(body{Name: p.name + " moon", Radius: 0.14, Color: gfx.RGB(200, 200, 210)},
				orbit{Radius: p.r + 0.6 + 0.5*float32(m), Speed: 2 + float32(m), Angle: random.Float() * 6.28}, gfx.Transform{})
			ecs.SetParent(w, moon, e) // moons follow their planet through the hierarchy
		}
	}
	// The belt: many small entities with the same mesh and material draw
	// as one instanced call.
	for range 400 {
		a := w.SpawnWith(body{Name: "asteroid", Radius: 0.05 + random.Float()*0.06, Color: gfx.RGB(150, 140, 130)}, asteroid{},
			orbit{Radius: 13.5 + random.Float()*2.5, Speed: 0.1 + random.Float()*0.05, Angle: random.Float() * 6.28},
			gfx.Transform{Position: lin.V3(0, random.Between(-0.4, 0.4), 0)})
		ecs.SetParent(w, a, sun)
	}
```

## The systems

A system is a `func(w *ecs.World, dt float64)` registered by name and
run in registration order on every `world.Update`. The queries are
created once, outside the system, and captured by the closure: a query
caches the archetype tables it matches, so making one per frame would
throw that away.

The orbit system writes each entity's local position from its angle,
which is why a moon's orbit radius is small: it is relative to its
planet. The spin system reads the `clock` resource, advances it, and
sets each rotation from the elapsed time, so the rotation is exact
rather than accumulated.

`ecs.NewQuery1[body](w)` is stored on the game because `Draw` uses it
every frame.

```go
	// Systems: orbits place bodies on their circles, spin turns them.
	orbits := ecs.NewQuery2[orbit, gfx.Transform](w)
	w.AddSystem("orbits", func(w *ecs.World, dt float64) {
		orbits.Each(func(e ecs.Entity, o *orbit, t *gfx.Transform) {
			o.Angle += o.Speed * float32(dt)
			t.Position.X = o.Radius * float32(math.Cos(float64(o.Angle)))
			t.Position.Z = o.Radius * float32(math.Sin(float64(o.Angle)))
		})
	})
	spins := ecs.NewQuery2[spin, gfx.Transform](w)
	w.AddSystem("spin", func(w *ecs.World, dt float64) {
		c := ecs.Resource[clock](w)
		c.Time += float32(dt)
		spins.Each(func(e ecs.Entity, s *spin, t *gfx.Transform) {
			t.Rotation = lin.AxisAngle(lin.V3(0, 1, 0), c.Time*s.Speed)
		})
	})
	g.world = w
	g.bodies = ecs.NewQuery1[body](w)
	g.selected = sun
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.minimap.Destroy()
	g.sphere.Destroy()
	g.cube.Destroy()
	g.font.Destroy()
}
```

The query callbacks take pointers to the components, so writing through
them writes into the table.

## Update: running the world under a profile scope

`world.Update(ctx.Delta)` runs every registered system once with the
fixed step. `ctx.Profile("systems")` opens a named scope and `End()`
closes it; the pair is timed and reported in the F3 overlay, which is
how the cost of the simulation is separated from the cost of drawing.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	systems := ctx.Profile("systems")
	g.world.Update(ctx.Delta)
	systems.End()
	g.yaw += float32(ctx.Delta) * 0.05
	return nil
}

func (g *game) camera() gfx.Camera {
	return gfx.OrbitCamera(lin.V3(0, 0, 0), g.yaw, 0.55, 26)
}
```

## Draw: one scene, two cameras

The bodies are drawn by a closure so the same code can run twice, once
into the minimap and once into the window. It walks the `body` query and
picks a mesh per entity: `ecs.Has[asteroid](w, e)` tests for the marker
component, so belt members draw as cubes.

`ecs.WorldMatrix(w, e)` composes an entity's transform with those of its
parents, which is where the hierarchy pays off: the moon's matrix
includes its planet's position without the moon knowing about it. The
scale is applied on the right, so the sphere is scaled about its own
centre before being placed.

`DrawTo(target, clear, body)` redirects everything the closure draws
into a render texture, clearing it first. Inside it the camera and light
are set again, because those are per-output state. The minimap camera
looks straight down from 40 units, offset by 0.01 in z so the view
direction and the up vector are not parallel.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	w := g.world
	light := gfx.Light{Direction: lin.V3(0.3, -1, 0.2), Color: gfx.Color{R: 0.6, G: 0.6, B: 0.7, A: 1},
		Ambient: gfx.Color{R: 0.08, G: 0.08, B: 0.12, A: 1}}
	drawBodies := func(withSelection bool) {
		gr.AddPointLight(lin.V3(0, 0, 0), gfx.Color{R: 40, G: 32, B: 20, A: 1}, 60)
		g.bodies.Each(func(e ecs.Entity, b *body) {
			mat := gfx.Material{BaseColor: b.Color, Emissive: b.Emissive, Roughness: 0.8}
			if withSelection && e == g.selected && b.Emissive == 0 {
				mat.Emissive = 1.5
			}
			mesh := g.sphere
			if ecs.Has[asteroid](w, e) {
				mesh = g.cube
			}
			gr.DrawMesh(mesh, mat, ecs.WorldMatrix(w, e).Mul(lin.Scale(lin.V3(b.Radius, b.Radius, b.Radius))))
		})
	}
	// The minimap: the same scene from straight above into a render texture.
	minimap := ctx.Profile("minimap")
	gr.DrawTo(g.minimap, gfx.RGB(5, 5, 12), func() {
		gr.SetCamera(gfx.Camera{Position: lin.V3(0, 40, 0.01), Target: lin.V3(0, 0, 0), FovY: lin.Radians(50)})
		gr.SetLight(light)
		drawBodies(false)
	})
	minimap.End()

	scene := ctx.Profile("scene")
	gr.SetCamera(g.camera())
	gr.SetLight(light)
	drawBodies(true)
	scene.End()
```

## Picking

The pick is done in `Draw`, after `SetCamera`, and the comment says why:
`ScreenRay` needs the camera that the ray should be cast through, and
the scene camera is only set here. Each body is tested with
`Mesh.Intersect` under the same matrix it was drawn with, and the
nearest hit wins. Testing the sphere mesh for every body, including the
asteroids drawn as cubes, is an approximation this example accepts.

Selection then feeds back into the next frame's draw: the selected
body's material gets an emissive term, unless it already has one.

```go
	// Picking happens here because the ray needs the camera just set.
	if ctx.Input.MousePressed(input.MouseLeft) {
		mx, my := ctx.Input.Mouse()
		ray := gr.ScreenRay(float32(mx), float32(my))
		best := float32(math.MaxFloat32)
		g.bodies.Each(func(e ecs.Entity, b *body) {
			m := ecs.WorldMatrix(w, e).Mul(lin.Scale(lin.V3(b.Radius, b.Radius, b.Radius)))
			if hit, ok := g.sphere.Intersect(m, ray); ok && hit.Distance < best {
				best, g.selected = hit.Distance, e
			}
		})
	}
```

## The overlay

`ScreenSpace()` leaves the 3D camera and draws in view units.
`g.minimap.Texture()` is the render texture's colour image, drawn as an
ordinary sprite; `UV1: lin.V2(1, 1)` uses the whole image.
`ecs.Get[body](w, g.selected)` returns the component and whether the
entity still exists, which is the check a stored handle needs.
`w.Count()` is the number of live entities.

```go
	gr.ScreenSpace()
	gr.Draw(g.minimap.Texture(), gfx.Sprite{Pos: lin.V2(ctx.Width-232, 12), Size: lin.V2(220, 220), UV1: lin.V2(1, 1), Color: gfx.White})
	name := "nothing"
	if b, ok := ecs.Get[body](w, g.selected); ok {
		name = b.Name
	}
	y := ctx.Height - 64
	gr.FillRect(12, y, 560, 52, gfx.RGBA(0, 0, 0, 150))
	gr.DrawText(g.font, fmt.Sprintf("%d entities; click a body to select. Selected: %s", w.Count(), name), 20, y+6, gfx.RGB(230, 230, 240))
	gr.DrawText(g.font, "Minimap top right is a render texture; the overlay top left shows profile scopes (F3).", 20, y+28, gfx.RGB(170, 170, 190))
	return nil
}
```

## main

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip solar", Width: 1024, Height: 640, Resizable: true, Validation: true, Debug: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "solar:", err)
		os.Exit(1)
	}
}
```

## What to try

- Raise the belt in `Init` from 400 to 20000 entities and watch the
  entity count, the frame time and the draw count; the belt stays one
  draw.
- Add a `trail` component and a third system in `Init` that records past
  positions, then draw it in `Draw` with `DrawLine3D`.
- Despawn the selected body on a keypress in `Update` with
  `w.Despawn`, and confirm its moons go with it or are left behind,
  depending on how the hierarchy handles it.
- Give the minimap in `Draw` its own light and a different camera height
  so it reads as a map rather than a small copy of the scene.
- Replace the sphere test in the picking loop with a distance test
  against each body's screen position, and compare which one feels
  better to click.
