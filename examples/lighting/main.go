// Command lighting is the renderer playground: a skinned mesh bent by
// joint matrices computed each frame, cascaded shadows, two lamps that
// cast their own shadows over a field of 160 small point lights, and
// every post-processing setting on a slider (exposure, bloom, vignette,
// saturation, contrast, ambient occlusion, FXAA, temporal anti-aliasing,
// depth of field, motion blur, god rays and the lens effects). A sphere
// orbits the scene and reports where it was last frame, so the velocity
// buffer has something in it. With -model it loads a glTF file and plays
// its animation clips (Space cycles them).
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
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
	// fieldLights is how many small point lights cover the floor. They
	// are far past what a fixed array would hold: the renderer sorts a
	// frame's lights into clusters over the view, so each one costs the
	// part of the view its range reaches.
	fieldLights = 160
	fieldCols   = 16 // lights across the field
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
	field    bool // draw the field of small point lights
	env      *gfx.Environment
	vacuum   float32
	useEnv   bool
	envPath  string
	yaw      float32
	prevT    float32 // the time the last frame drew at, for the moving sphere
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
	// An environment for image-based lighting: a panorama when given,
	// otherwise a graded sky.
	if g.envPath != "" {
		f, err := os.Open(g.envPath)
		if err != nil {
			return err
		}
		pano, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("decode %s: %w", g.envPath, err)
		}
		if g.env, err = ctx.Gfx.NewEnvironment(pano, gfx.EnvironmentOptions{Intensity: 1.5}); err != nil {
			return err
		}
	}
	g.useEnv = g.env != nil
	g.post = gfx.DefaultPost()
	g.post.Vignette = 0.3
	g.shadows = true
	g.field = true
	g.yaw = 0.7
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	if g.env != nil {
		g.env.Destroy()
	}
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
	// The procedural sky: its air thins as the vacuum slider climbs to
	// orbit, the sky goes black and the stars come out, and the sun and
	// ground light stay.
	light := gfx.Light{Direction: lin.V3(-0.5, -1, -0.4), Color: gfx.Color{R: 2.5, G: 2.3, B: 2, A: 1},
		Sky:        gfx.Sky{Zenith: gfx.RGB(70, 120, 210), Horizon: gfx.RGB(200, 215, 235), Ground: gfx.RGB(80, 70, 60), Vacuum: g.vacuum, Stars: 1},
		Background: true, Shadows: g.shadows, ShadowDistance: 30}
	if g.useEnv && g.env != nil {
		// Image-based lighting: the panorama replaces the sky and is
		// drawn behind the scene.
		light.Environment = g.env
	}
	gr.SetLight(light)
	t := float32(ctx.Time)
	// Two lamps orbiting over the floor. A point light with Shadows set
	// renders the six faces of a cube map, so it throws the tentacles'
	// shadows in every direction; a frame gets four of them.
	for i := range 2 {
		a := t*0.7 + float32(i)*3.1
		gr.AddPoint(gfx.PointLight{
			Position: lin.V3(4*float32(math.Cos(float64(a))), 2.2, 4*float32(math.Sin(float64(a)))),
			Color:    gfx.Color{R: 4 * float32(i%2), G: 2.5, B: 5 * float32((i+1)%2), A: 1},
			Range:    9, Shadows: g.shadows,
		})
	}
	// A field of small lights over the floor, more than any fixed array
	// would hold. Each one has a short range, so the cluster grid gives
	// it only the handful of clusters it reaches.
	if g.field {
		for i := range fieldLights {
			x := float32(i%fieldCols)*0.85 - 6.4
			z := float32(i/fieldCols)*0.85 - 3.8
			pulse := 0.5 + 0.5*float32(math.Sin(float64(t*2+float32(i)*0.7)))
			hue := float32(i) * 0.21
			gr.AddPointLight(lin.V3(x, 0.35, z), gfx.Color{
				R: 2 * pulse * (0.5 + 0.5*float32(math.Sin(float64(hue)))),
				G: 2 * pulse * (0.5 + 0.5*float32(math.Sin(float64(hue+2.1)))),
				B: 2 * pulse * (0.5 + 0.5*float32(math.Sin(float64(hue+4.2)))),
				A: 1}, 1.1)
		}
	}
	gr.DrawMesh(g.floor, gfx.Material{BaseColor: gfx.RGB(150, 150, 160), Roughness: 0.9}, lin.Translate(lin.V3(0, -0.1, 0)).Mul(lin.Scale(lin.V3(12, 0.2, 12))))
	gr.DrawMesh(g.sphere, gfx.Material{BaseColor: gfx.RGB(230, 200, 120), Metallic: 1, Roughness: 0.2}, lin.Translate(lin.V3(3.5, 1, -2)))
	gr.DrawMesh(g.sphere, gfx.Material{BaseColor: gfx.RGB(90, 200, 255), Emissive: 2.5}, lin.Translate(lin.V3(-3.5, 0.6, 2)).Mul(lin.Scale(lin.V3(0.6, 0.6, 0.6))))
	// A sphere on a fast orbit, drawn with the transform it had last
	// frame as well as this one. That difference is what goes into the
	// velocity buffer, so temporal anti-aliasing reprojects the sphere
	// instead of smearing it and motion blur streaks it the right way.
	gr.DrawMeshMoved(g.sphere, gfx.Material{BaseColor: gfx.RGB(255, 130, 90), Roughness: 0.35},
		orbitAt(t), orbitAt(g.prevT))
	g.prevT = t
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
		u.Panel("Post-processing", ui.Rect{X: 12, Y: 12, W: 260, H: 696}, func() {
			// Lights counts the point and spot lights the frame kept and
			// LightsDropped what it refused past gfx.MaxLights.
			s := gr.Stats()
			u.Label(fmt.Sprintf("%d lights, %d dropped, %d shadow draws", s.Lights, s.LightsDropped, s.ShadowDraws))
			u.Slider("Vacuum (up to orbit)", &g.vacuum, 0, 1)
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
			u.Checkbox("Light field (160 point lights)", &g.field)
			u.Checkbox("Environment (image-based lighting)", &g.useEnv)
			if g.model != nil {
				u.Label(fmt.Sprintf("Clip %d/%d, Space cycles", g.clip+1, len(g.model.Clips())))
			} else {
				u.Label("Tentacles are skinned meshes; pass -model file.glb to animate a glTF.")
			}
		})
		// The second panel is everything that needs the depth buffer or
		// the velocity buffer, plus the lens. Each slider's zero is off.
		u.Panel("Camera and lens", ui.Rect{X: 752, Y: 12, W: 260, H: 640}, func() {
			u.Checkbox("Temporal anti-alias", &g.post.TemporalAA)
			u.Slider("Temporal blend", &g.post.TemporalBlend, 0.02, 0.5)
			u.Slider("Focus distance (0 off)", &g.post.FocusDistance, 0, 25)
			u.Slider("Focus range", &g.post.FocusRange, 0.2, 10)
			u.Slider("Bokeh radius", &g.post.BokehRadius, 1, 40)
			u.Slider("Motion blur", &g.post.MotionBlur, 0, 1)
			u.Slider("God rays", &g.post.GodRays, 0, 2)
			u.Slider("Chromatic aberration", &g.post.Aberration, 0, 6)
			u.Slider("Lens distortion", &g.post.Distortion, -1, 1)
			u.Slider("Lens ghosts", &g.post.Ghosts, 0, 1)
			u.Slider("Film grain", &g.post.Grain, 0, 0.2)
		})
	})
	return nil
}

// orbitAt is the moving sphere's transform at a moment in time. Draw
// calls it twice, for now and for the previous frame.
func orbitAt(t float32) lin.Mat4 {
	a := float64(t) * 1.3
	return lin.Translate(lin.V3(3.2*float32(math.Cos(a)), 2.2, 3.2*float32(math.Sin(a)))).
		Mul(lin.Scale(lin.V3(0.55, 0.55, 0.55)))
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
	env := flag.String("env", "", "equirectangular panorama (PNG or JPEG) to light the scene with")
	vacuum := flag.Float64("vacuum", 0, "how thin the air starts: 0 on the ground, 1 in orbit")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip lighting", Width: 1024, Height: 720, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, modelPath: *model, envPath: *env, vacuum: float32(*vacuum)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "lighting:", err)
		os.Exit(1)
	}
}
