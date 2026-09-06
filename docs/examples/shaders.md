---
title: Shaders
example: shaders
summary: game-written GLSL compiled to SPIR-V offline, as 2D sprite shaders and mesh surface and vertex hooks, with uniforms driven by sliders
---

This example is four shaders written by the game rather than the engine.
Two of them colour sprites in the 2D stream: a wave that ripples the
texture coordinates and a dissolve that burns a sprite away along a
noise image. The other two run on meshes under the engine's own
lighting: a lava surface that writes albedo, roughness and emissive
before the light is applied, and a flag whose vertex hook displaces the
cloth in the lit pass and the shadow pass alike. Sliders drive their
uniforms while the program runs.

Shaders are compiled offline. `bunyip-shader` composes each `.glsl` with
a prelude and a postlude and runs `glslangValidator` over the result, and
the `.spv` output is embedded in the binary, so a shipped game carries
no compiler. The engine side is `NewShader` and `NewMeshShader` in
[gfx](../pkg/gfx.html), `Shader.SetUniforms` and `Shader.SetImage`,
`Graphics.Shaded` for the 2D case and `Material.Shader` for the mesh
case. The guide is [Shaders](../guides/shaders.html).

Run it with:

```bash
go run ./examples/shaders -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`. The three sliders set
the wave amplitude, the lava heat and the wind strength; Escape quits.
After editing a `.glsl`, run `go generate ./examples/shaders/` to
rebuild the SPIR-V.

## Generate directives and embedded SPIR-V

The four `go:generate` lines are the build step. `-kind mesh` selects
the mesh prelude, which is what decides whether the file supplies
`fragment` or `surface`, `vertex` and `finish`. The default kind is the
2D one.

Each `.spv` is embedded with `go:embed`. A new shader needs its `.spv`
file to exist before `bunyip-shader` can be built at all, because the
tool imports the package that embeds them, so create an empty
placeholder first.

```go
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
```

## The game type and the cloth mesh

The game holds four shaders, two textures, two meshes and the three
slider values.

`clothMesh` builds a subdivided quad in the x-y plane with `u` running
along x, which is what lets the flag shader pin the edge at `u = 0` and
wave the rest. The subdivision matters: a vertex shader can only move
vertices that exist, so a quad of two triangles would not ripple.

```go
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
```

## Init: compiling nothing, loading everything

`NewShader` takes 2D SPIR-V and `NewMeshShader` takes mesh SPIR-V; the
two pipelines differ, so the constructor picks which one the module is
built for.

`SetImage(0, tex)` binds an extra texture the shader reads as `image0`.
A shader has four such slots, separate from the material's own textures.
The noise texture is created with `Data: true`, which uploads the bytes
as they are rather than treating them as sRGB colour: a shader reading a
noise value wants the number, not a colour conversion. `Repeat: true`
lets the shaders sample it with coordinates outside the unit square.

```go
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
```

A `Shader` is a GPU resource with `Destroy`, like a texture or a mesh.

## Update

```go
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
```

## Draw: the mesh shaders

`SetUniforms` packs exported struct fields into a std140 uniform block and
returns an error for unsupported types or oversized blocks. Padding and array
strides are automatic. The Go struct matches the GLSL `Params` block's field
order and types, which is why each call passes an anonymous struct written
next to the shader that consumes it.

A mesh shader is attached through `Material.Shader`. The lava slab is a
cube scaled flat with no base colour or texture, because the shader
writes those itself. The plain cubes around it use the standard material
path in the same frame, so both pipelines are in one scene.

The flag is drawn with `DoubleSided: true`, since a rippling cloth shows
both faces. Its vertex hook runs in the shadow pass as well, so the
shadow it casts ripples with it. Meshes whose shader has a vertex hook
are skipped by the frustum culling, because the bind-pose bounds no
longer describe where the geometry ends up.

```go
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
```

## Draw: the 2D shaders, blends and transforms

`Shaded(shader, body)` applies a 2D shader to everything the closure
draws, and restores the previous state at the end. It is the same
closure form as `Blended` and `Transformed`, which appear below it.
Sprites drawn under a shader still go into the ordinary 2D stream, so
compatible draws can batch together. Texture, blend and clip changes can
also break a batch, even when the shader stays the same.

The dissolve's progress is driven from `ctx.Time` through a cosine, so
it burns away and back without any state on the game.

```go
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
```

The sliders write straight into the game's fields through pointers, and
the next frame's `SetUniforms` picks the values up, which is the whole
loop between the interface and the shaders.

## wave.glsl

A 2D shader supplies `fragment(vec2 uv, vec4 color)`, returning the
colour to write. `UNIFORMS` marks the uniform block that `SetUniforms`
fills; `tex` is the sprite's own texture, `image0` is the first extra
image, and `time()` is the elapsed time supplied by the prelude.

This one samples the noise, scrolls it, uses it to modulate a sine
offset applied to the horizontal texture coordinate, and tints the
result. Rippling the coordinate rather than the colour is what makes the
image itself wobble.

<!-- file: wave.glsl -->
```glsl
// A 2D shader: ripples the texture coordinates over time and tints by
// the extra image, a noise texture.
UNIFORMS uniform Params {
    float amplitude;
    float frequency;
} u;

vec4 fragment(vec2 uv, vec4 color) {
    float n = texture(image0, uv * 2.0 + vec2(time() * 0.1, 0.0)).r;
    uv.x += sin(uv.y * u.frequency + time() * 3.0) * u.amplitude * n;
    vec4 c = texture(tex, uv) * color;
    return c * vec4(1.0, 0.85 + 0.15 * n, 0.7 + 0.3 * n, 1.0);
}
```

## dissolve.glsl

The dissolve compares the noise value at each texel with a threshold
that rises with `progress`. Below the threshold the fragment is
discarded by returning a fully transparent colour; just above it, a
glowing edge is added, whose width is the `edge` uniform. Multiplying
the glow by `c.a` keeps it inside the sprite's own shape.

<!-- file: dissolve.glsl -->
```glsl
// A 2D shader: burns the sprite away along a noise image, with a glowing
// edge, as progress goes from 0 to 1.
UNIFORMS uniform Params {
    float progress;
    float edge;
} u;

vec4 fragment(vec2 uv, vec4 color) {
    vec4 c = texture(tex, uv) * color;
    float n = texture(image0, uv).r;
    float cut = u.progress * (1.0 + u.edge);
    if (n < cut - u.edge) return vec4(0.0);
    float glow = 1.0 - clamp((n - (cut - u.edge)) / u.edge, 0.0, 1.0);
    vec3 fire = vec3(1.0, 0.5, 0.1) * glow * 2.0 * c.a;
    return vec4(c.rgb + fire, c.a);
}
```

## lava.glsl

A mesh shader supplies `surface(inout Surface s)`, which runs before the
lighting and writes the material properties the lighting then uses:
albedo, roughness, metallic, normal and emissive. Writing `s.albedo`
rather than returning a colour is what keeps the shader inside the
engine's shading model, so shadows, point lights and fog still apply.

`s.worldPos` positions the pattern in the world rather than on the
surface, so the cracks do not stretch with the cube's scale.
`s.emissive +=` adds to whatever the material set instead of replacing
it.

The optional `finish(vec4 lit, Surface s)` hook runs after the lighting
and can adjust the lit colour, which is used here to fade the slab's
edges towards black.

<!-- file: lava.glsl -->
```glsl
// A mesh shader: a cooled crust with glowing cracks that pulse, on top
// of the standard lighting.
UNIFORMS uniform Params {
    float heat;
} u;

void surface(inout Surface s) {
    vec2 p = s.worldPos.xz * 1.5 + vec2(time() * 0.05, 0.0);
    float n = texture(image0, p * 0.25).r;
    float crack = smoothstep(0.45, 0.55, n);
    float pulse = 0.6 + 0.4 * sin(time() * 2.0 + n * 12.0);
    s.albedo = mix(vec3(0.05, 0.04, 0.04), vec3(0.2, 0.1, 0.08), n);
    s.roughness = mix(0.95, 0.4, crack);
    s.emissive += vec3(1.0, 0.35, 0.05) * crack * pulse * u.heat;
}

vec4 finish(vec4 lit, Surface s) {
    // Fade the far edges to black so the slab reads as a pool.
    float rim = smoothstep(0.0, 0.5, 1.0 - abs(s.uv.x - 0.5) * 2.0) * smoothstep(0.0, 0.5, 1.0 - abs(s.uv.y - 0.5) * 2.0);
    return vec4(lit.rgb * mix(0.3, 1.0, rim), lit.a);
}
```

## flag.glsl

`vertex(inout VertexData v)` runs before the model matrix, in object
space, and in both the lit pass and the shadow pass, which is what makes
the flag's shadow match the flag. The displacement is scaled by `v.uv.x`
so the edge at `u = 0` stays pinned to the pole.

The normal is recomputed from the slope of the same wave, because moving
a vertex without moving its normal leaves the lighting flat. `surface`
then stripes the cloth from the vertical texture coordinate.

<!-- file: flag.glsl -->
```glsl
// A mesh shader with a vertex hook: a flag rippling in the wind. The
// vertex() hook displaces the cloth before the model matrix, in the lit
// and shadow passes alike; surface() stripes it.
UNIFORMS uniform Params {
    float strength;
} u;

void vertex(inout VertexData v) {
    // Fixed at the pole (u = 0), waving more towards the free edge.
    float free = v.uv.x;
    float wave = sin(v.uv.x * 6.0 - time() * 4.0) + 0.5 * sin(v.uv.y * 4.0 - time() * 6.0);
    v.position.z += wave * 0.15 * free * u.strength;
    // Tilt the normal with the slope of the wave so the lighting ripples.
    float slope = cos(v.uv.x * 6.0 - time() * 4.0) * 6.0 * 0.15 * free * u.strength;
    v.normal = normalize(vec3(-slope * 0.5, 0.0, 1.0));
}

void surface(inout Surface s) {
    float band = step(0.5, fract(s.uv.y * 3.0));
    s.albedo = mix(vec3(0.9, 0.2, 0.15), vec3(0.95, 0.95, 0.9), band);
    s.roughness = 0.8;
}
```

## The generated textures and main

`checker` is a two-tone board and `noise` is smooth value noise on an 8
by 8 grid, interpolated with a smoothstep and wrapped with a modulo so
it tiles. The noise is the input to three of the four shaders, which is
why it is worth generating rather than shipping.

```go
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
```

## What to try

- Change a constant in `lava.glsl`, run `go generate
  ./examples/shaders/`, and run the program again; that is the whole
  edit cycle.
- Add a `float speed` to the `Params` block in `wave.glsl` and to the
  struct passed to `SetUniforms` in `Draw`, and give it a slider.
- Write a `finish` hook in `flag.glsl` that darkens the cloth towards
  its free edge, and see it apply after the lighting.
- Bind a second image with `SetImage(1, ...)` in `Init` and sample
  `image1` from `dissolve.glsl` for a different burn pattern.
- Reduce `clothMesh(40, 24)` in `Init` to `clothMesh(4, 3)` to see how
  much the vertex hook depends on the subdivision.
