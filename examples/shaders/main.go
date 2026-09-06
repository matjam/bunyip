// Command shaders shows fragment shaders written by the game: a 2D wave
// shader and a dissolve on sprites, and a lava surface shader on a mesh
// under the engine's lighting. The GLSL sources sit beside this file;
// bunyip-shader compiles them to SPIR-V (go generate) and they are
// embedded. Blend modes and the transform stack are shown too. Escape
// quits.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/rng"
	"github.com/matjam/bunyip/ui"
)

//go:generate go run ../../cmd/bunyip-shader -o wave.spv wave.glsl
//go:generate go run ../../cmd/bunyip-shader -o dissolve.spv dissolve.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -o lava.spv lava.glsl
//go:generate go run ../../cmd/bunyip-shader -kind mesh -o flag.spv flag.glsl

var (
	//go:embed wave.spv
	waveSPV []byte
	//go:embed dissolve.spv
	dissolveSPV []byte
	//go:embed lava.spv
	lavaSPV []byte
	//go:embed flag.spv
	flagSPV []byte
)

type game struct {
	seconds float64
	shot    string

	font      *gfx.Font
	ui        *ui.Context
	checker   *gfx.Texture
	noise     *gfx.Texture
	wave      *gfx.Shader
	dissolve  *gfx.Shader
	lava      *gfx.Shader
	flag      *gfx.Shader
	cube      *gfx.Mesh
	cloth     *gfx.Mesh
	amplitude float32
	heat      float32
	wind      float32
	shotDone  bool
}

// clothMesh is a subdivided quad in the x-y plane, 2 by 1.2 units, with
// u running along x so the flag's vertex hook can pin one edge.
func clothMesh(nx, ny int) ([]gfx.Vertex, []uint32) {
	var verts []gfx.Vertex
	var idx []uint32
	for j := 0; j <= ny; j++ {
		for i := 0; i <= nx; i++ {
			u, v := float32(i)/float32(nx), float32(j)/float32(ny)
			verts = append(verts, gfx.Vertex{Pos: lin.V3(u*2, 1.2-v*1.2, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(u, v)})
		}
	}
	stride := uint32(nx + 1)
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			a := uint32(j)*stride + uint32(i)
			idx = append(idx, a, a+stride, a+1, a+1, a+stride, a+stride+1)
		}
	}
	return verts, idx
}

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	if g.checker, err = ctx.Gfx.NewTexture(checker(128), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	if g.noise, err = ctx.Gfx.NewTexture(noise(256, 7), gfx.TextureOptions{Linear: true, Data: true, Repeat: true}); err != nil {
		return err
	}
	if g.wave, err = ctx.Gfx.NewShader(waveSPV); err != nil {
		return err
	}
	if g.dissolve, err = ctx.Gfx.NewShader(dissolveSPV); err != nil {
		return err
	}
	if g.lava, err = ctx.Gfx.NewMeshShader(lavaSPV); err != nil {
		return err
	}
	if g.flag, err = ctx.Gfx.NewMeshShader(flagSPV); err != nil {
		return err
	}
	g.wave.SetImage(0, g.noise)
	g.dissolve.SetImage(0, g.noise)
	g.lava.SetImage(0, g.noise)
	cv, ci := gfx.CubeMesh()
	if g.cube, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	fv, fi := clothMesh(40, 24)
	if g.cloth, err = ctx.Gfx.NewMesh(fv, fi); err != nil {
		return err
	}
	g.amplitude, g.heat, g.wind = 0.03, 1, 1
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.cloth.Destroy()
	g.flag.Destroy()
	g.cube.Destroy()
	g.lava.Destroy()
	g.dissolve.Destroy()
	g.wave.Destroy()
	g.noise.Destroy()
	g.checker.Destroy()
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
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	t := float32(ctx.Time)
	// The 3D scene: a lava slab with plain cubes on it.
	gr.SetCamera(gfx.Camera{Position: lin.V3(6*float32(math.Sin(float64(t)*0.2)), 4.5, 6*float32(math.Cos(float64(t)*0.2))), Target: lin.V3(0, 0, 0)})
	gr.SetLight(gfx.Light{Direction: lin.V3(-0.4, -1, -0.3), Color: gfx.Color{R: 1.5, G: 1.4, B: 1.3, A: 1},
		Sky: gfx.Sky{Zenith: gfx.Color{R: 0.25, G: 0.3, B: 0.4, A: 1}, Ground: gfx.Color{R: 0.1, G: 0.05, B: 0.03, A: 1}}, Shadows: true, ShadowDistance: 20})
	if err := g.lava.SetUniforms(struct{ Heat float32 }{g.heat}); err != nil {
		return err
	}
	gr.DrawMesh(g.cube, gfx.Material{Shader: g.lava}, lin.Translate(lin.V3(0, -0.5, 0)).Mul(lin.Scale(lin.V3(8, 0.4, 8))))
	for i := range 5 {
		a := float64(i) * 2 * math.Pi / 5
		gr.DrawMesh(g.cube, gfx.Material{BaseColor: gfx.RGB(200, 200, 210), Roughness: 0.4, Metallic: 0.6},
			lin.Translate(lin.V3(2.5*float32(math.Cos(a)), 0.2, 2.5*float32(math.Sin(a)))).Mul(lin.Rotate(t+float32(i), lin.V3(0, 1, 0))).Mul(lin.Scale(lin.V3(0.8, 0.8, 0.8))))
	}
	// A flag on a pole: the vertex hook ripples the cloth and its shadow.
	if err := g.flag.SetUniforms(struct{ Strength float32 }{g.wind}); err != nil {
		return err
	}
	gr.DrawMesh(g.cube, gfx.Material{BaseColor: gfx.RGB(90, 90, 100), Roughness: 0.5}, lin.Translate(lin.V3(0, 1.2, 0)).Mul(lin.Scale(lin.V3(0.06, 3.2, 0.06))))
	gr.DrawMesh(g.cloth, gfx.Material{Shader: g.flag, DoubleSided: true}, lin.Translate(lin.V3(0.05, 1.6, 0)).Mul(lin.Rotate(t*0.2, lin.V3(0, 1, 0))))

	// 2D: the wave shader over a checker, then the dissolve.
	if err := g.wave.SetUniforms(struct{ Amplitude, Frequency float32 }{g.amplitude, 24}); err != nil {
		return err
	}
	gr.Shaded(g.wave, func() {
		gr.Draw(g.checker, gfx.Sprite{Pos: lin.V2(ctx.Width-300, 20), Size: lin.V2(260, 180)})
	})
	progress := float32(0.5 - 0.5*math.Cos(float64(t)*0.8))
	if err := g.dissolve.SetUniforms(struct{ Progress, Edge float32 }{progress, 0.08}); err != nil {
		return err
	}
	gr.Shaded(g.dissolve, func() {
		gr.Draw(g.checker, gfx.Sprite{Pos: lin.V2(ctx.Width-300, 220), Size: lin.V2(260, 180), Color: gfx.RGB(120, 200, 255)})
	})
	// Blend modes: additive glows and a multiplied shadow over the checker.
	gr.Draw(g.checker, gfx.Sprite{Pos: lin.V2(ctx.Width-300, 420), Size: lin.V2(260, 120)})
	gr.Blended(gfx.BlendAdd, func() {
		for i := range 3 {
			x := ctx.Width - 260 + float32(i)*90 + 30*float32(math.Sin(float64(t)*2+float64(i)))
			gr.FillCircle(x, 480, 40, gfx.RGBA(255, 90, 30, 160))
		}
	})
	gr.Blended(gfx.BlendMultiply, func() {
		gr.FillRect(ctx.Width-300, 500, 260, 40, gfx.RGB(90, 110, 160))
	})
	// The transform stack: a sheared, rotating sprite.
	gr.Transformed(lin.Translate2(ctx.Width-170, 620).Mul(lin.Rotate2(t*0.5)).Mul(lin.Shear2(0.4, 0)), func() {
		gr.Draw(g.checker, gfx.Sprite{Pos: lin.V2(-40, -40), Size: lin.V2(80, 80), Color: gfx.RGB(255, 230, 150)})
	})

	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Shaders", ui.Rect{X: 12, Y: 12, W: 320, H: 250}, func() {
			u.Slider("Wave amplitude", &g.amplitude, 0, 0.1)
			u.Slider("Lava heat", &g.heat, 0, 3)
			u.Slider("Wind", &g.wind, 0, 2)
			u.Label("wave.glsl and dissolve.glsl colour sprites; lava.glsl shapes a surface before lighting; flag.glsl moves vertices. Additive glows, a multiplied shadow, and a sheared sprite below.")
		})
	})
	return nil
}

// checker makes a two-tone checkerboard.
func checker(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			c := color.RGBA{60, 70, 90, 255}
			if (x/16+y/16)%2 == 0 {
				c = color.RGBA{220, 210, 190, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// noise makes smooth value noise, tiling.
func noise(size int, seed uint64) image.Image {
	const cells = 8
	random := rng.New(seed)
	grid := make([]float64, cells*cells)
	for i := range grid {
		grid[i] = float64(random.Float())
	}
	at := func(x, y int) float64 { return grid[(y%cells)*cells+x%cells] }
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			fx, fy := float64(x)/float64(size)*cells, float64(y)/float64(size)*cells
			ix, iy := int(fx), int(fy)
			tx, ty := fx-float64(ix), fy-float64(iy)
			tx, ty = tx*tx*(3-2*tx), ty*ty*(3-2*ty)
			v := (at(ix, iy)*(1-tx)+at(ix+1, iy)*tx)*(1-ty) + (at(ix, iy+1)*(1-tx)+at(ix+1, iy+1)*tx)*ty
			b := uint8(v * 255)
			img.SetRGBA(x, y, color.RGBA{b, b, b, 255})
		}
	}
	return img
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip shaders", Width: 1024, Height: 720, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "shaders:", err)
		os.Exit(1)
	}
}
