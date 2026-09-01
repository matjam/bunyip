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

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
)

type viewer struct {
	modelPath string
	seconds   float64
	shot      string

	model    *gfx.Model
	cube     *gfx.Mesh
	sphere   *gfx.Mesh
	checker  *gfx.Texture
	yaw      float32
	pitch    float32
	distance float32
	center   lin.Vec3
	dragging bool
	lastX    float64
	lastY    float64
	shotDone bool
}

func (v *viewer) Init(ctx *bunyip.Context) error {
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

func (v *viewer) Shutdown(ctx *bunyip.Context) {
	if v.model != nil {
		v.model.Destroy()
	}
	v.cube.Destroy()
	v.sphere.Destroy()
	v.checker.Destroy()
}

func (v *viewer) Update(ctx *bunyip.Context) error {
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
		v.yaw += float32(x-v.lastX) * 0.01
		v.pitch = lin.Clamp(v.pitch+float32(y-v.lastY)*0.01, -1.5, 1.5)
	}
	v.lastX, v.lastY = x, y
	_, dy := in.Scroll()
	v.distance = lin.Clamp(v.distance*float32(math.Pow(0.9, dy)), 0.5, 500)
	if v.model == nil {
		v.yaw += float32(ctx.Delta) * 0.3 // idle spin so a screenshot shows motion
	}
	return nil
}

func (v *viewer) Draw(ctx *bunyip.Context) error {
	g := ctx.Gfx
	eye := v.center.Add(lin.V3(
		v.distance*float32(math.Cos(float64(v.pitch))*math.Sin(float64(v.yaw))),
		v.distance*float32(math.Sin(float64(v.pitch))),
		v.distance*float32(math.Cos(float64(v.pitch))*math.Cos(float64(v.yaw)))))
	g.SetCamera(gfx.Camera{Position: eye, Target: v.center})
	g.SetLight(gfx.Light{Direction: lin.V3(-0.4, -1, -0.6), Color: gfx.Color{R: 1, G: 0.97, B: 0.9, A: 1}, Ambient: gfx.Color{R: 0.18, G: 0.2, B: 0.25, A: 1}})
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
	g.DrawMesh(v.cube, gfx.Material{Texture: v.checker}, lin.Translate(lin.V3(0, -1, 0)).Mul(lin.Scale(lin.V3(10, 0.2, 10))))
	g.DrawMesh(v.sphere, gfx.Material{BaseColor: gfx.RGB(220, 60, 60)}, lin.Translate(lin.V3(0, 0.6, 0)).Mul(lin.Scale(lin.V3(1.5, 1.5, 1.5))))
	for i := range 6 {
		a := float32(i)*math.Pi/3 + t*0.5
		pos := lin.V3(3.5*float32(math.Cos(float64(a))), 0, 3.5*float32(math.Sin(float64(a))))
		rot := lin.AxisAngle(lin.V3(0, 1, 0), a).Mul(lin.AxisAngle(lin.V3(1, 0, 0), t))
		shade := uint8(90 + 25*i)
		g.DrawMesh(v.cube, gfx.Material{BaseColor: gfx.RGB(shade, 255-shade, 200)}, lin.TRS(pos, rot, lin.V3(1, 1, 1)))
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
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip viewer", Width: 960, Height: 640, Resizable: true, Validation: true},
		&viewer{modelPath: *modelPath, seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "viewer:", err)
		os.Exit(1)
	}
}
