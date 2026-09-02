// Command solar shows the entity component system driving a scene: a
// sun with orbiting planets and moons as a parent-child hierarchy, an
// asteroid belt of hundreds of entities drawn as one instanced call,
// systems for orbits and spin, click-to-pick with a screen ray, a render
// texture used as a top-down minimap, and profile scopes in the debug
// overlay (F3).
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/rng"
)

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
