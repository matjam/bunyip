// Command viewer shows a 3D scene with an orbit camera. Without -model it
// builds a scene from generated shapes; with -model it loads a glTF file.
// Drag with the left mouse button to orbit, scroll to zoom, Escape to quit.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"

	"github.com/matjam/bunyip/engine"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

type viewer struct {
	modelPath string
	seconds   float64
	shot      string
	noAO      bool
	noShadow  bool
	showAO    bool
	sorted    bool

	model    *gfx.Model
	cube     *gfx.Mesh
	sphere   *gfx.Mesh
	pane     *gfx.Mesh
	checker  *gfx.Texture
	yaw      float32
	pitch    float32
	distance float32
	center   lin.Vec3
	dragging bool
	lastX    float32
	lastY    float32
	shotDone bool
}

func (v *viewer) Init(ctx *engine.Context) error {
	var err error
	if v.checker, err = ctx.Gfx.NewTexture(checkerImage(64, 8), gfx.TextureOptions{}); err != nil {
		return err
	}
	cv, ci := gfx.CubeMesh()
	if v.cube, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	sv, si := gfx.SphereMesh(24, 48)
	if v.sphere, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	pv, pi := gfx.PlaneMesh(1)
	if v.pane, err = ctx.Gfx.NewMesh(pv, pi); err != nil {
		return err
	}
	v.yaw, v.pitch, v.distance = 0.6, 0.5, 8
	if v.modelPath != "" {
		doc, err := gltf.Load(v.modelPath)
		if err != nil {
			return err
		}
		if v.model, err = ctx.Gfx.LoadModel(doc); err != nil {
			return err
		}
		v.center = v.model.Min.Add(v.model.Max).Mul(0.5)
		v.distance = v.model.Max.Sub(v.model.Min).Len() * 1.2
		ctx.Log.Info("viewer: model loaded", "parts", len(v.model.Parts), "min", v.model.Min, "max", v.model.Max)
	}
	return nil
}

func (v *viewer) Shutdown(ctx *engine.Context) {
	if v.model != nil {
		v.model.Destroy()
	}
	v.cube.Destroy()
	v.sphere.Destroy()
	v.pane.Destroy()
	v.checker.Destroy()
}

func (v *viewer) Update(ctx *engine.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) || (v.seconds > 0 && ctx.Time >= v.seconds) {
		ctx.Quit()
	}
	if v.shot != "" && !v.shotDone && (v.seconds == 0 || ctx.Time >= v.seconds/2) {
		ctx.Screenshot(v.shot)
		v.shotDone = true
	}
	x, y := in.Mouse()
	if in.MousePressed(input.MouseLeft) {
		v.dragging = true
	}
	if in.MouseReleased(input.MouseLeft) {
		v.dragging = false
	}
	if v.dragging && in.MouseDown(input.MouseLeft) {
		v.yaw += (x - v.lastX) * 0.01
		v.pitch = lin.Clamp(v.pitch+(y-v.lastY)*0.01, -1.5, 1.5)
	}
	v.lastX, v.lastY = x, y
	_, dy := in.Scroll()
	v.distance = lin.Clamp(v.distance*float32(math.Pow(0.9, float64(dy))), 0.5, 500)
	if v.model == nil {
		v.yaw += float32(ctx.Delta) * 0.3 // idle spin so a screenshot shows motion
	}
	return nil
}

func (v *viewer) Draw(ctx *engine.Context) error {
	g := ctx.Gfx
	p := g.Post()
	// Translucent surfaces are composited without sorting unless -sorted
	// asks for the old path, which is what the crossed panes below show.
	p.OrderIndependent = !v.sorted
	if v.noAO || v.showAO {
		p.AmbientOcclusion = 0
		if v.showAO {
			p.AmbientOcclusion, p.ShowOcclusion = 1, true
		}
	}
	g.SetPost(p)
	eye := v.center.Add(lin.V3(
		v.distance*float32(math.Cos(float64(v.pitch))*math.Sin(float64(v.yaw))),
		v.distance*float32(math.Sin(float64(v.pitch))),
		v.distance*float32(math.Cos(float64(v.pitch))*math.Cos(float64(v.yaw)))))
	g.SetCamera(gfx.Camera{Position: eye, Target: v.center})
	g.SetLight(gfx.Light{Direction: lin.V3(-0.4, -1, -0.6), Color: gfx.Color{R: 2.2, G: 2.1, B: 1.9, A: 1},
		Ambient: gfx.Color{R: 0.18, G: 0.2, B: 0.25, A: 1}, Shadows: !v.noShadow, ShadowDistance: 40})
	g.AddPointLight(lin.V3(4*float32(math.Sin(float64(ctx.Time))), 1.5, 4*float32(math.Cos(float64(ctx.Time)))), gfx.Color{R: 4, G: 1.5, B: 0.5, A: 1}, 8)
	if v.model != nil {
		g.DrawModel(v.model, lin.Identity())
	} else {
		v.drawScene(g, float32(ctx.Time))
	}
	// A 2D overlay proves sprites composite over the 3D scene.
	g.FillRect(10, 10, 220, 28, gfx.RGBA(0, 0, 0, 160))
	g.FillRect(14, 14, 20, 20, gfx.RGB(255, 200, 40))
	return nil
}

func (v *viewer) drawScene(g *gfx.Graphics, t float32) {
	g.DrawMesh(v.cube, gfx.Material{Texture: v.checker, Roughness: 0.8}, lin.Translate(lin.V3(0, -1, 0)).Mul(lin.Scale(lin.V3(10, 0.2, 10))))
	// A polished metal sphere, a rough dielectric one, and an emissive cube that blooms.
	g.DrawMesh(v.sphere, gfx.Material{BaseColor: gfx.RGB(230, 200, 120), Metallic: 1, Roughness: 0.15}, lin.Translate(lin.V3(0, 0.6, 0)).Mul(lin.Scale(lin.V3(1.5, 1.5, 1.5))))
	g.DrawMesh(v.sphere, gfx.Material{BaseColor: gfx.RGB(220, 60, 60), Roughness: 0.7}, lin.Translate(lin.V3(-3, 0, 2)).Mul(lin.Scale(lin.V3(0.9, 0.9, 0.9))))
	g.DrawMesh(v.cube, gfx.Material{BaseColor: gfx.RGB(120, 200, 255), Emissive: 3}, lin.Translate(lin.V3(3, 0, 2)).Mul(lin.Scale(lin.V3(0.8, 0.8, 0.8))))
	// Two translucent panes crossing above the sphere, the pair turned
	// with the camera so both stay side-on to it. Each is nearer than the
	// other on its own side of the crossing, which one order for the whole
	// draw cannot show: with PostSettings.OrderIndependent each side shows
	// the pane in front of it, and with -sorted one pane covers both.
	upright := lin.Rotate(-math.Pi/2, lin.V3(1, 0, 0))
	for i, c := range []gfx.Color{{R: 0.15, G: 0.6, B: 1, A: 0.5}, {R: 1, G: 0.4, B: 0.35, A: 0.5}} {
		turn := lin.Rotate(v.yaw+lin.Radians(35-70*float32(i)), lin.V3(0, 1, 0))
		place := lin.Translate(lin.V3(0, 2.6, 0)).Mul(turn).Mul(upright).Mul(lin.Scale(lin.V3(3.4, 1, 2.4)))
		g.DrawMesh(v.pane, gfx.Material{BaseColor: c, Blend: true, DoubleSided: true, Roughness: 0.25}, place)
	}
	for i := range 6 {
		a := float32(i)*math.Pi/3 + t*0.5
		pos := lin.V3(3.5*float32(math.Cos(float64(a))), 0.5+0.5*float32(math.Sin(float64(t+float32(i)))), 3.5*float32(math.Sin(float64(a))))
		rot := lin.AxisAngle(lin.V3(0, 1, 0), a).Mul(lin.AxisAngle(lin.V3(1, 0, 0), t))
		shade := uint8(90 + 25*i)
		g.DrawMesh(v.cube, gfx.Material{BaseColor: gfx.RGB(shade, 255-shade, 200), Metallic: float32(i) / 5, Roughness: 0.3 + 0.1*float32(i)}, lin.TRS(pos, rot, lin.V3(0.7, 0.7, 0.7)))
	}
}

func checkerImage(size, cell int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			c := color.RGBA{200, 200, 210, 255}
			if (x/cell+y/cell)%2 == 1 {
				c = color.RGBA{90, 95, 110, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func main() {
	modelPath := flag.String("model", "", "glTF (.gltf or .glb) file to show")
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	noAO := flag.Bool("noao", false, "disable ambient occlusion")
	noShadow := flag.Bool("noshadow", false, "disable shadows")
	showAO := flag.Bool("showao", false, "display the ambient occlusion buffer")
	sorted := flag.Bool("sorted", false, "composite translucent draws by sorting them, not order-independently")
	flag.Parse()
	err := engine.Run(engine.Config{Title: "Bunyip viewer", Width: 960, Height: 640, Resizable: true, Validation: true},
		&viewer{modelPath: *modelPath, seconds: *seconds, shot: *shot, noAO: *noAO, noShadow: *noShadow, showAO: *showAO, sorted: *sorted})
	if err != nil {
		fmt.Fprintln(os.Stderr, "viewer:", err)
		os.Exit(1)
	}
}
