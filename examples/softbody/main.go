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

func sin(v float32) float32 { return float32(math.Sin(float64(v))) }

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
