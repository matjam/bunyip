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

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.mesh.Destroy()
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
