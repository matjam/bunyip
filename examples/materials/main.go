// Command materials shows every material feature on a row of spheres and
// a few props: clearcoat, sheen, subsurface, iridescence, anisotropy, a
// specular tint, fur, alpha cutout, unlit, vertex colours, a scrolling
// texture transform, occlusion, an outline and an x-ray tint through a
// wall, a stencil mask, a projected decal, all under image-based
// lighting from a sky or a panorama (-env file.png, .hdr or .exr). Drag
// to orbit, scroll to zoom, Escape quits.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/ui"
)

type game struct {
	seconds float64
	shot    string
	envPath string

	font     *gfx.Font
	ui       *ui.Context
	sphere   *gfx.Mesh
	cube     *gfx.Mesh
	quad     *gfx.Mesh
	colored  *gfx.Mesh
	env      *gfx.Environment
	stripes  *gfx.Texture
	leaf     *gfx.Texture
	splat    *gfx.Texture
	thick    *gfx.Texture
	fur      *gfx.Texture
	yaw      float32
	pitch    float32
	dist     float32
	lastX    float32
	lastY    float32
	dragging bool
	shotDone bool
	xray     bool
	mask     bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 14, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	sv, si := gfx.SphereMesh(32, 64)
	if g.sphere, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	cv, ci := gfx.CubeMesh()
	if g.cube, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	qv, qi := quadMesh()
	if g.quad, err = ctx.Gfx.NewMesh(qv, qi); err != nil {
		return err
	}
	// A sphere with a colour per vertex, blending around the equator.
	for i := range sv {
		a := math.Atan2(float64(sv[i].Pos.Z), float64(sv[i].Pos.X))
		sv[i].Color = gfx.Color{R: 0.5 + 0.5*float32(math.Cos(a)), G: 0.5 + 0.5*float32(math.Sin(a)), B: 0.5 + 0.5*sv[i].Pos.Y, A: 1}
	}
	if g.colored, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	if g.stripes, err = ctx.Gfx.NewTexture(stripes(64), gfx.TextureOptions{Linear: true, Repeat: true}); err != nil {
		return err
	}
	if g.leaf, err = ctx.Gfx.NewTexture(leafShape(128), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	if g.splat, err = ctx.Gfx.NewTexture(splat(128), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	if g.thick, err = ctx.Gfx.NewTexture(thickness(64), gfx.TextureOptions{Linear: true, Data: true}); err != nil {
		return err
	}
	if g.fur, err = ctx.Gfx.NewTexture(strands(128), gfx.TextureOptions{Linear: true, Data: true, Repeat: true}); err != nil {
		return err
	}
	if g.envPath != "" {
		if g.env, err = loadEnvironment(ctx.Gfx, g.envPath); err != nil {
			return err
		}
	}
	g.yaw, g.pitch, g.dist = 0.4, 0.3, 14
	g.xray, g.mask = true, true
	return nil
}

// loadEnvironment reads a panorama in whichever format it is in.
// DecodePanorama keeps the full range of an OpenEXR or Radiance file, so
// a sun in it stays a sun, and converts a PNG or JPEG from sRGB, which
// needs an intensity to make up some of the range it cannot carry.
func loadEnvironment(gr *gfx.Graphics, path string) (*gfx.Environment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := gfx.DecodePanorama(data)
	if err != nil {
		return nil, err
	}
	opts := gfx.EnvironmentOptions{}
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".hdr" && ext != ".exr" {
		opts.Intensity = 1.5
	}
	return gr.NewEnvironmentHDR(img, opts)
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	if g.env != nil {
		g.env.Destroy()
	}
	for _, t := range []*gfx.Texture{g.fur, g.thick, g.splat, g.leaf, g.stripes} {
		t.Destroy()
	}
	for _, m := range []*gfx.Mesh{g.colored, g.quad, g.cube, g.sphere} {
		m.Destroy()
	}
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
	if _, dy := in.Scroll(); dy != 0 {
		g.dist = lin.Clamp(g.dist-float32(dy)*0.5, 4, 30)
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	t := float32(ctx.Time)
	gr.SetCamera(gfx.OrbitCamera(lin.V3(0, 0.8, 0), g.yaw, g.pitch, g.dist))
	// A procedural sky lights the scene unless a panorama was given.
	gr.SetLight(gfx.Light{Direction: lin.V3(-0.4, -1, -0.5), Color: gfx.Color{R: 2.2, G: 2.1, B: 2, A: 1},
		Sky:     gfx.Sky{Zenith: gfx.RGB(80, 130, 220), Horizon: gfx.RGB(210, 220, 235), Ground: gfx.RGB(90, 80, 70)},
		Shadows: true, ShadowDistance: 30, Environment: g.env, Background: true})
	// The floor, with a scrolling stripe texture and a decal on it.
	gr.DrawMesh(g.cube, gfx.Material{Texture: g.stripes, Roughness: 0.8, UVTransform: lin.Translate2(t*0.05, 0).Mul(lin.Scale2(6, 6))},
		lin.Translate(lin.V3(0, -0.5, 0)).Mul(lin.Scale(lin.V3(22, 0.2, 14))))
	gr.DrawDecal(g.splat, lin.Translate(lin.V3(-2.5, -0.3, 2.5)).Mul(lin.Rotate(t*0.3, lin.V3(0, 1, 0))).Mul(lin.Scale(lin.V3(2, 1, 2))), gfx.RGB(120, 20, 20))

	// The row of spheres: each shows one feature.
	row := []struct {
		name string
		mat  gfx.Material
		mesh *gfx.Mesh
	}{
		{"metal", gfx.Material{BaseColor: gfx.RGB(240, 200, 120), Metallic: 1, Roughness: 0.15}, g.sphere},
		{"clearcoat", gfx.Material{BaseColor: gfx.RGB(150, 20, 30), Roughness: 0.6, Clearcoat: 1, ClearcoatRoughness: 0.05}, g.sphere},
		{"sheen", gfx.Material{BaseColor: gfx.RGB(40, 30, 90), Roughness: 0.9, Sheen: gfx.RGB(200, 180, 255), SheenRoughness: 0.4}, g.sphere},
		{"subsurface", gfx.Material{BaseColor: gfx.RGB(240, 180, 150), Roughness: 0.7, Subsurface: 1, ThicknessTexture: g.thick}, g.sphere},
		{"vertex colours", gfx.Material{Roughness: 0.5}, g.colored},
		{"unlit", gfx.Material{BaseColor: gfx.RGB(255, 140, 40), Unlit: true}, g.sphere},
		{"glass", gfx.Material{Roughness: 0.05, Transmission: 1, IOR: 1.5, Thickness: 0.8,
			AttenuationColor: gfx.RGB(120, 200, 255), AttenuationDistance: 1}, g.sphere},
		{"iridescence", gfx.Material{BaseColor: gfx.RGB(140, 140, 150), Metallic: 1, Roughness: 0.2,
			Iridescence: 1, IridescenceThickness: 480}, g.sphere},
		{"anisotropy", gfx.Material{BaseColor: gfx.RGB(200, 200, 210), Metallic: 1, Roughness: 0.35, Anisotropy: 0.9}, g.sphere},
		{"specular tint", gfx.Material{BaseColor: gfx.RGB(20, 20, 22), Roughness: 0.2, SpecularColor: gfx.RGB(255, 170, 90)}, g.sphere},
		{"fur", gfx.Material{BaseColor: gfx.RGB(190, 140, 70), Roughness: 0.9, Shells: 16, ShellLength: 0.22,
			FurTexture: g.fur, UVTransform: lin.Scale2(8, 4)}, g.sphere},
	}
	for i, r := range row {
		x := float32(i)*1.8 - 9
		gr.DrawMesh(r.mesh, r.mat, lin.Translate(lin.V3(x, 0.4, -2)).Mul(lin.Scale(lin.V3(0.8, 0.8, 0.8))))
	}
	// Alpha cutout leaves, double-sided, casting cutout shadows.
	for i := range 3 {
		gr.DrawMesh(g.quad, gfx.Material{Texture: g.leaf, AlphaCutoff: 0.5, DoubleSided: true, Roughness: 0.8},
			lin.Translate(lin.V3(3+float32(i)*0.9, 0.9, 1.5)).Mul(lin.Rotate(t*0.5+float32(i), lin.V3(0, 1, 0))).Mul(lin.Scale(lin.V3(0.8, 1.2, 1))))
	}
	// An outlined sphere, and one behind a wall showing through with x-ray.
	gr.DrawMesh(g.sphere, gfx.Material{BaseColor: gfx.RGB(90, 200, 120), Roughness: 0.5, Outline: 3, OutlineColor: gfx.RGB(255, 255, 255)},
		lin.Translate(lin.V3(-4, 0.3, 1.5)).Mul(lin.Scale(lin.V3(0.7, 0.7, 0.7))))
	wall := gfx.Material{BaseColor: gfx.RGB(120, 120, 130), Roughness: 0.9}
	gr.DrawMesh(g.cube, wall, lin.Translate(lin.V3(-1, 0.5, 2.6)).Mul(lin.Scale(lin.V3(2.4, 1.6, 0.15))))
	hidden := gfx.Material{BaseColor: gfx.RGB(255, 80, 80), Roughness: 0.4}
	if g.xray {
		hidden.XRay = gfx.RGBA(255, 60, 60, 160)
	}
	gr.DrawMesh(g.sphere, hidden, lin.Translate(lin.V3(-1, 0.4, 1.2)).Mul(lin.Scale(lin.V3(0.5, 0.5, 0.5))))
	// A stencil mask: the pane marks the buffer where it draws and the
	// sphere in front of it is drawn only where the mark is, which is how
	// a portal or a cutaway is built. Materials that write the stencil
	// buffer draw first, so the two can be queued either way round.
	pane := gfx.Material{BaseColor: gfx.RGB(25, 35, 55), Roughness: 0.3}
	orb := gfx.Material{BaseColor: gfx.RGB(255, 120, 40), Roughness: 0.3, Emissive: 0.8}
	if g.mask {
		pane.StencilWrite, pane.StencilRef = gfx.StencilReplace, 1
		orb.Stencil, orb.StencilRef = gfx.StencilEqual, 1
	}
	gr.DrawMesh(g.sphere, orb, lin.Translate(lin.V3(-4.6, 1.2, 5.2)).Mul(lin.Scale(lin.V3(1.3, 1.3, 1.3))))
	gr.DrawMesh(g.quad, pane, lin.Translate(lin.V3(-4.6, 1.2, 4.2)).Mul(lin.Scale(lin.V3(1.8, 1.8, 1))))

	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Materials", ui.Rect{X: 12, Y: 12, W: 340, H: 190}, func() {
			u.Checkbox("X-ray the sphere behind the wall", &g.xray)
			u.Checkbox("Stencil mask on the pane", &g.mask)
			u.Label("Row: metal, clearcoat, sheen, subsurface, vertex colours, unlit, glass, iridescence, anisotropy, specular tint, fur. Leaves are alpha cutouts; the green sphere is outlined; the floor scrolls its texture and carries a decal.")
		})
	})
	return nil
}

// quadMesh is a unit square in the x-y plane facing +z.
func quadMesh() ([]gfx.Vertex, []uint32) {
	n := lin.V3(0, 0, 1)
	return []gfx.Vertex{
		{Pos: lin.V3(-0.5, -0.5, 0), Normal: n, UV: lin.V2(0, 1)},
		{Pos: lin.V3(0.5, -0.5, 0), Normal: n, UV: lin.V2(1, 1)},
		{Pos: lin.V3(0.5, 0.5, 0), Normal: n, UV: lin.V2(1, 0)},
		{Pos: lin.V3(-0.5, 0.5, 0), Normal: n, UV: lin.V2(0, 0)},
	}, []uint32{0, 1, 2, 0, 2, 3}
}

func stripes(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			c := color.RGBA{200, 200, 205, 255}
			if (x/8)%2 == 0 {
				c = color.RGBA{150, 150, 160, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// leafShape is a green leaf on a transparent background.
func leafShape(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			u, v := float64(x)/float64(size)*2-1, float64(y)/float64(size)*2-1
			inside := u*u/0.36+v*v <= 1 && math.Abs(u) > 0.03 || (math.Abs(u) <= 0.03 && v > -1)
			if inside {
				g := uint8(110 + 80*math.Abs(u))
				img.SetRGBA(x, y, color.RGBA{30, g, 40, 255})
			}
		}
	}
	return img
}

// splat is a soft-edged blob with alpha.
func splat(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			u, v := float64(x)/float64(size)*2-1, float64(y)/float64(size)*2-1
			r := math.Hypot(u, v) + 0.15*math.Sin(6*math.Atan2(v, u))
			a := math.Max(0, math.Min(1, (0.9-r)*4))
			img.SetRGBA(x, y, color.RGBA{uint8(255 * a), uint8(255 * a), uint8(255 * a), uint8(255 * a)})
		}
	}
	return img
}

// thickness is thin (dark) in bands so light shows through them.
func thickness(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			v := uint8(255)
			if (y/8)%2 == 0 {
				v = 40
			}
			img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	return img
}

// strands is the fur mask: each texel is how far out the strand at that
// point reaches, so a shell keeps the texels above its own height and
// the fur thins towards its tips. The pattern is a hash of the cell the
// texel falls in, which is enough to read as fur.
func strands(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	const cells = 40
	for y := range size {
		for x := range size {
			cx, cy := x*cells/size, y*cells/size
			h := uint32(cx*374761393 + cy*668265263)
			h = (h ^ (h >> 13)) * 1274126177
			v := uint8(h >> 24)
			img.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	return img
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	env := flag.String("env", "", "panorama for lighting: .exr, .hdr, .png or .jpg")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip materials", Width: 1100, Height: 680, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, envPath: *env})
	if err != nil {
		fmt.Fprintln(os.Stderr, "materials:", err)
		os.Exit(1)
	}
}
