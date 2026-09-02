// Command lighting is the renderer playground: a skinned mesh bent by
// joint matrices computed each frame, cascaded shadows, point lights, and
// every post-processing setting on a slider (exposure, bloom, vignette,
// saturation, contrast, ambient occlusion, FXAA). With -model it loads a
// glTF file and plays its animation clips (Space cycles them).
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/ui"
)

const (
	segments = 10  // joints along each tentacle
	segLen   = 0.4 // metres per joint
)

type game struct {
	seconds   float64
	shot      string
	modelPath string

	font     *gfx.Font
	ui       *ui.Context
	floor    *gfx.Mesh
	sphere   *gfx.Mesh
	tentacle *gfx.Mesh
	model    *gfx.Model
	player   *gfx.AnimPlayer
	clip     int
	post     gfx.PostSettings
	shadows  bool
	yaw      float32
	shotDone bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	cv, ci := gfx.CubeMesh()
	if g.floor, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	sv, si := gfx.SphereMesh(20, 40)
	if g.sphere, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	verts, idx := tentacleMesh()
	if g.tentacle, err = ctx.Gfx.NewSkinnedMesh(verts, idx); err != nil {
		return err
	}
	if g.modelPath != "" {
		doc, err := gltf.Load(g.modelPath)
		if err != nil {
			return err
		}
		if g.model, err = ctx.Gfx.LoadModel(doc); err != nil {
			return err
		}
		g.player = g.model.NewAnimPlayer()
		if clips := g.model.Clips(); len(clips) > 0 {
			g.player.PlayIndex(0, true)
			ctx.Log.Info("lighting: clips", "names", clips)
		}
	}
	g.post = gfx.DefaultPost()
	g.post.Vignette = 0.3
	g.shadows = true
	g.yaw = 0.7
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	if g.model != nil {
		g.model.Destroy()
	}
	g.tentacle.Destroy()
	g.sphere.Destroy()
	g.floor.Destroy()
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
	if g.player != nil {
		if in.KeyPressed(input.KeySpace) && len(g.model.Clips()) > 0 {
			g.clip = (g.clip + 1) % len(g.model.Clips())
			g.player.PlayIndex(g.clip, true)
		}
		g.player.Advance(ctx.Delta)
	}
	if in.MouseDown(input.MouseLeft) && !g.ui.WantsMouse() {
		dx, _ := in.MouseDelta()
		g.yaw += float32(dx) * 0.01
	} else {
		g.yaw += float32(ctx.Delta) * 0.15
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	gr.SetPost(g.post)
	gr.SetCamera(gfx.OrbitCamera(lin.V3(0, 1.5, 0), g.yaw, 0.35, 11))
	gr.SetLight(gfx.Light{Direction: lin.V3(-0.5, -1, -0.4), Color: gfx.Color{R: 2.5, G: 2.3, B: 2, A: 1},
		Sky: gfx.Color{R: 0.25, G: 0.3, B: 0.45, A: 1}, Ground: gfx.Color{R: 0.12, G: 0.1, B: 0.08, A: 1},
		Shadows: g.shadows, ShadowDistance: 30})
	t := float32(ctx.Time)
	for i := range 3 {
		a := t*0.7 + float32(i)*2.1
		gr.AddPointLight(lin.V3(4*float32(math.Cos(float64(a))), 1.2, 4*float32(math.Sin(float64(a)))),
			gfx.Color{R: 3 * float32(i%2), G: 2, B: 4 * float32((i+1)%2), A: 1}, 7)
	}
	gr.DrawMesh(g.floor, gfx.Material{BaseColor: gfx.RGB(150, 150, 160), Roughness: 0.9}, lin.Translate(lin.V3(0, -0.1, 0)).Mul(lin.Scale(lin.V3(12, 0.2, 12))))
	gr.DrawMesh(g.sphere, gfx.Material{BaseColor: gfx.RGB(230, 200, 120), Metallic: 1, Roughness: 0.2}, lin.Translate(lin.V3(3.5, 1, -2)))
	gr.DrawMesh(g.sphere, gfx.Material{BaseColor: gfx.RGB(90, 200, 255), Emissive: 2.5}, lin.Translate(lin.V3(-3.5, 0.6, 2)).Mul(lin.Scale(lin.V3(0.6, 0.6, 0.6))))
	if g.model != nil {
		gr.DrawModelAnimated(g.model, gfx.At(0, 0, 0), g.player)
	} else {
		// Five tentacles, each bent by joint matrices built on the CPU.
		for i := range 5 {
			base := lin.V3(2.2*float32(math.Cos(float64(i)*1.2566)), 0, 2.2*float32(math.Sin(float64(i)*1.2566)))
			joints := tentacleJoints(t + float32(i)*0.9)
			hue := gfx.RGB(uint8(120+25*i), uint8(90+30*(4-i)), 180)
			gr.DrawSkinned(g.tentacle, gfx.Material{BaseColor: hue, Roughness: 0.5}, lin.Translate(base), joints)
		}
	}

	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Post-processing", ui.Rect{X: 12, Y: 12, W: 260, H: 470}, func() {
			u.Slider("Exposure", &g.post.Exposure, 0.1, 4)
			u.Slider("Bloom", &g.post.Bloom, 0, 1)
			u.Slider("Bloom threshold", &g.post.BloomThreshold, 0.2, 3)
			u.Slider("Vignette", &g.post.Vignette, 0, 1)
			u.Slider("Saturation", &g.post.Saturation, 0, 2)
			u.Slider("Contrast", &g.post.Contrast, 0.5, 1.5)
			u.Slider("Ambient occlusion", &g.post.AmbientOcclusion, 0, 1)
			u.Slider("Occlusion radius", &g.post.OcclusionRadius, 0.2, 3)
			u.Checkbox("Show occlusion buffer", &g.post.ShowOcclusion)
			fxaa := !g.post.NoAntiAlias
			if u.Checkbox("Anti-alias (FXAA)", &fxaa) {
				g.post.NoAntiAlias = !fxaa
			}
			u.Checkbox("Shadows", &g.shadows)
			if g.model != nil {
				u.Label(fmt.Sprintf("Clip %d/%d, Space cycles", g.clip+1, len(g.model.Clips())))
			} else {
				u.Label("Tentacles are skinned meshes; pass -model file.glb to animate a glTF.")
			}
		})
	})
	return nil
}

// tentacleMesh builds a tapered tube whose rings belong to successive
// joints.
func tentacleMesh() ([]gfx.SkinVertex, []uint32) {
	const sides = 12
	var verts []gfx.SkinVertex
	var idx []uint32
	for k := 0; k <= segments; k++ {
		y := float32(k) * segLen
		radius := 0.35 * (1 - float32(k)/float32(segments+1))
		j0 := uint8(min(k, segments-1))
		for s := range sides {
			a := float64(s) / sides * 2 * math.Pi
			nx, nz := float32(math.Cos(a)), float32(math.Sin(a))
			verts = append(verts, gfx.SkinVertex{
				Pos: lin.V3(nx*radius, y, nz*radius), Normal: lin.V3(nx, 0, nz),
				UV:     lin.V2(float32(s)/sides, float32(k)/segments),
				Joints: [4]uint8{j0}, Weights: [4]float32{1},
			})
		}
	}
	for k := range segments {
		for s := range sides {
			a := uint32(k*sides + s)
			b := uint32(k*sides + (s+1)%sides)
			c, d := a+sides, b+sides
			idx = append(idx, a, c, b, b, c, d)
		}
	}
	return verts, idx
}

// tentacleJoints returns the skinning matrix for each joint: the joint's
// animated world transform times the inverse of its rest transform.
func tentacleJoints(t float32) []lin.Mat4 {
	joints := make([]lin.Mat4, segments)
	global := lin.Identity()
	for i := range segments {
		bend := 0.35 * float32(math.Sin(float64(t*1.5+float32(i)*0.5)))
		local := lin.Rotate(bend, lin.V3(0, 0, 1)).Mul(lin.Rotate(bend*0.6, lin.V3(1, 0, 0)))
		if i > 0 {
			local = lin.Translate(lin.V3(0, segLen, 0)).Mul(local)
		}
		global = global.Mul(local)
		inverseBind := lin.Translate(lin.V3(0, -float32(i)*segLen, 0))
		joints[i] = global.Mul(inverseBind)
	}
	return joints
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	model := flag.String("model", "", "glTF file with animation clips to play")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip lighting", Width: 1024, Height: 640, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, modelPath: *model})
	if err != nil {
		fmt.Fprintln(os.Stderr, "lighting:", err)
		os.Exit(1)
	}
}
