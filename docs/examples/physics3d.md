---
title: Physics 3D
example: physics3d
summary: five hundred textured cubes of wood, plastic, metal, gold, car paint, velvet, glass and glowing material falling into a concrete pen
---

Five hundred cubes fall into a walled pen and settle into a pile. Each
one is an entity with a `gfx.Transform`, a `phys.Body3` and a
`phys.Collider3`, and one system steps them all. Wood grain, brushed
metal and molded plastic give the cubes visible surface detail; gold,
clearcoat paint, velvet, glass and glowing cubes retain the material
showcase. The ground and walls use tiled concrete.

Run it:

```bash
CGO_ENABLED=0 go run ./examples/physics3d -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`. Dragging orbits the
camera, the wheel zooms, hovering highlights the cube under the pointer,
R drops the cubes again and Escape quits.

## Textures and material response

The texture images are generated in Go once during initialization. No
download or external asset is needed. Cubes share textures by material
family, so resetting the pile creates entities without allocating another
set of GPU textures.

Each textured family has three maps:

- Albedo supplies the surface colour.
- A tangent-space normal map gives grain and small scratches a lighting response.
- A packed map stores roughness in green and metallic weight in blue.

Colour textures use the normal sRGB path. Normal and metallic/roughness
maps use `TextureOptions.Data` because their channels contain numeric
data. Linear filtering, mipmaps and repeating coordinates keep detail
stable as the camera moves; the pen tiles its textures across the larger
surfaces.

These are visual materials. The rigid bodies retain the same mass,
friction and restitution, so a wooden cube does not simulate a different
density from a metal one. `Metallic`, `Roughness`, `Clearcoat`,
`Sheen` and `Transmission` affect rendering. Hovering raises a copy of
the cube's emissive value without changing its stored material.

See [Materials](../pkg/gfx.html#Material) for the renderer's fields and
[3D graphics](../guides/graphics-3d.html) for lights and cameras.

## Simulation and controls

The ground and four walls have static box colliders. Each cube has a
dynamic body and a unit box collider. The physics system advances on the
engine's fixed timestep; a ray from the pointer highlights the first
collider hit. All movement and collision behaviour is handled by
[the physics package](../guides/physics.html).

The seeded spawn sequence is independent of texture generation. Reset
continues that sequence and reuses the same meshes and texture resources.
Textures are created through the graphics owner and released with the
example's other resources.

## Main program

```go
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

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/engine"
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

type game struct {
	seconds float64
	shot    string

	font      *gfx.Font
	ui        *ui.Context
	world     *ecs.World
	cubes     *ecs.Query2[gfx.Transform, cube]
	mesh      *gfx.Mesh
	materials materialLibrary
	random    *rng.Rand
	hover     ecs.Entity
	yaw       float32
	pitch     float32
	dist      float32
	lastX     float32
	lastY     float32
	dragging  bool
	shotDone  bool
}

func (g *game) Init(ctx *engine.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	cv, ci := gfx.CubeMesh()
	if g.mesh, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	if err := g.materials.init(ctx.Gfx); err != nil {
		return err
	}
	g.random = rng.New(3)
	g.yaw, g.pitch, g.dist = 0.7, 0.4, 40
	w := ecs.NewWorld()
	g.world = w
	g.cubes = w.Query2[gfx.Transform, cube]()
	w.SetResource(phys.Settings3{Gravity: lin.V3(0, -9.8, 0), Substeps: 4, Iterations: 8})
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
			cube{Material: g.materials.cubeMaterial(cubeKinds[g.random.Intn(len(cubeKinds))], c)},
		)
	}
}

func (g *game) Shutdown(ctx *engine.Context) {
	for _, texture := range g.materials.textures {
		texture.Destroy()
	}
	g.mesh.Destroy()
	g.font.Destroy()
}

func (g *game) Update(ctx *engine.Context) error {
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

func (g *game) Draw(ctx *engine.Context) error {
	gr := ctx.Gfx
	w := g.world
	gr.SetCamera(gfx.OrbitCamera(lin.V3(0, 3, 0), g.yaw, g.pitch, g.dist))
	gr.SetLight(gfx.Light{Direction: lin.V3(-0.5, -1, -0.3), Color: gfx.Color{R: 2.4, G: 2.3, B: 2.1, A: 1},
		Sky: gfx.Sky{Zenith: gfx.Color{R: 0.3, G: 0.35, B: 0.5, A: 1}, Ground: gfx.Color{R: 0.15, G: 0.12, B: 0.1, A: 1}}, Shadows: true, ShadowDistance: 60})
	ground := g.materials.concrete
	ground.UVTransform = lin.Scale2(20, 20)
	gr.DrawMesh(g.mesh, ground, lin.Translate(lin.V3(0, 0, 0)).Mul(lin.Scale(lin.V3(60, 1, 60))))
	wallMaterial := g.materials.concrete
	wallMaterial.BaseColor = gfx.RGB(190, 190, 200)
	wallMaterial.UVTransform = lin.Scale2(8, 1)
	for _, wall := range []struct{ pos, half lin.Vec3 }{{lin.V3(12, 1.5, 0), lin.V3(0.5, 1.5, 12)}, {lin.V3(-12, 1.5, 0), lin.V3(0.5, 1.5, 12)}, {lin.V3(0, 1.5, 12), lin.V3(12, 1.5, 0.5)}, {lin.V3(0, 1.5, -12), lin.V3(12, 1.5, 0.5)}} {
		gr.DrawMesh(g.mesh, wallMaterial, lin.Translate(wall.pos).Mul(lin.Scale(wall.half.Mul(2))))
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
			u.Label(fmt.Sprintf("physics %.2f ms/frame; drag orbits, scroll zooms; wood, brushed metal, plastic, gold, paint, velvet, glass and glow", ms))
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
	err := engine.Run(engine.Config{Title: "Bunyip physics: 500 cubes", Width: 1024, Height: 680, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "physics3d:", err)
		os.Exit(1)
	}
}
```

## Shared procedural materials

The periodic height fields also generate normals using wrapped neighbouring
samples. A local integer hash supplies repeatable noise without consuming
the simulation's random stream. The nine spawn slots retain the original
weights, with two former plastic slots now producing wood.

<!-- file: materials.go -->
```go
package main

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

type cubeKind uint8

const (
	brushedMetal cubeKind = iota
	gold
	carPaint
	velvet
	glass
	glowing
	wood
	plastic
)

// Nine weighted slots preserve the simulation's seeded random sequence.
// Wood takes two of the former plastic slots so grain is easy to find.
var cubeKinds = [...]cubeKind{brushedMetal, gold, carPaint, velvet, glass, glowing, wood, wood, plastic}

// materialLibrary shares twelve GPU images across all cubes and the pen.
// Images are generated once during Init; dropping cubes only copies materials.
type materialLibrary struct {
	wood, metal, plastic, concrete gfx.Material
	textures                       []*gfx.Texture
}

func (m *materialLibrary) init(gr *gfx.Graphics) error {
	for _, surface := range []struct {
		name     string
		material *gfx.Material
		pixel    func(float64, float64) (color.RGBA, float64, float64)
		metallic uint8
	}{
		{"wood", &m.wood, woodPixel, 0},
		{"brushed metal", &m.metal, metalPixel, 255},
		{"molded plastic", &m.plastic, plasticPixel, 0},
		{"concrete", &m.concrete, concretePixel, 0},
	} {
		albedo, normal, roughness := surfaceImages(surface.pixel, surface.metallic)
		for _, channel := range []struct {
			name string
			src  *image.RGBA
			dst  **gfx.Texture
			data bool
		}{
			{"albedo", albedo, &surface.material.Texture, false},
			{"normal", normal, &surface.material.NormalTexture, true},
			{"metal/roughness", roughness, &surface.material.MetalRoughTexture, true},
		} {
			texture, err := gr.NewTexture(channel.src, gfx.TextureOptions{Linear: true, Repeat: true, Data: channel.data})
			if err != nil {
				// Graphics owns prior uploads and also releases them if Init fails.
				return fmt.Errorf("physics3d: %s %s texture: %w", surface.name, channel.name, err)
			}
			*channel.dst = texture
			m.textures = append(m.textures, texture)
		}
		// The map carries the absolute roughness, so its factor must be one.
		surface.material.Roughness = 1
	}
	m.wood.Clearcoat, m.wood.ClearcoatRoughness = 0.15, 0.35
	return nil
}

// cubeMaterial retains the optical showcase alongside the textured surfaces.
func (m *materialLibrary) cubeMaterial(kind cubeKind, c gfx.Color) gfx.Material {
	switch kind {
	case brushedMetal:
		mat := m.metal
		mat.BaseColor = c
		return mat
	case gold:
		return gfx.Material{BaseColor: gfx.RGB(255, 200, 90), Metallic: 1, Roughness: 0.1}
	case carPaint:
		return gfx.Material{BaseColor: c, Roughness: 0.5, Clearcoat: 1, ClearcoatRoughness: 0.05}
	case velvet:
		return gfx.Material{BaseColor: c, Roughness: 0.95, Sheen: gfx.RGB(255, 255, 255), SheenRoughness: 0.5}
	case glass:
		return gfx.Material{Roughness: 0.05, Transmission: 1, IOR: 1.5, Thickness: 1, AttenuationColor: c, AttenuationDistance: 2}
	case glowing:
		return gfx.Material{BaseColor: c, Roughness: 0.6, Emissive: 1.2}
	case wood:
		return m.wood
	default:
		mat := m.plastic
		mat.BaseColor = c
		mat.UVTransform = lin.Scale2(2, 2)
		return mat
	}
}

// surfaceImages samples a periodic surface into albedo, tangent-space normals
// and glTF G-roughness/B-metallic maps. Wrapped height differences keep the
// normal map continuous at tile boundaries; linear filtering supplies mipmaps.
func surfaceImages(pixel func(float64, float64) (color.RGBA, float64, float64), metallic uint8) (*image.RGBA, *image.RGBA, *image.RGBA) {
	const size = 256
	bounds := image.Rect(0, 0, size, size)
	albedo, normal, roughness := image.NewRGBA(bounds), image.NewRGBA(bounds), image.NewRGBA(bounds)
	heights := make([]float64, size*size)
	for y := range size {
		for x := range size {
			c, h, r := pixel(float64(x)/size, float64(y)/size)
			albedo.SetRGBA(x, y, c)
			heights[y*size+x] = h
			roughness.SetRGBA(x, y, color.RGBA{R: 255, G: channelByte(r), B: metallic, A: 255})
		}
	}
	for y := range size {
		for x := range size {
			dx := (heights[y*size+(x+1)%size] - heights[y*size+(x+size-1)%size]) * 2
			dy := (heights[((y+1)%size)*size+x] - heights[((y+size-1)%size)*size+x]) * 2
			length := math.Sqrt(dx*dx + dy*dy + 1)
			normal.SetRGBA(x, y, color.RGBA{R: channelByte(0.5 - dx/length*0.5), G: channelByte(0.5 - dy/length*0.5), B: channelByte(0.5 + 0.5/length), A: 255})
		}
	}
	return albedo, normal, roughness
}

func woodPixel(u, v float64) (color.RGBA, float64, float64) {
	warp := 0.10*math.Sin(2*math.Pi*v) + 0.025*math.Sin(4*math.Pi*v)
	grain := 0.5 + 0.5*math.Sin(2*math.Pi*(u*7+warp))
	fiber := 0.5 + 0.5*math.Sin(2*math.Pi*(u*43+warp*5))
	pore := tileNoise(u, v, 96, 8)
	tone := 0.25 + 0.55*grain + 0.12*fiber + 0.08*pore
	return color.RGBA{R: uint8(92 + 108*tone), G: uint8(43 + 96*tone), B: uint8(18 + 56*tone), A: 255},
		0.25*grain + 0.05*fiber + 0.04*pore, 0.48 + 0.22*(1-grain)
}

func metalPixel(u, v float64) (color.RGBA, float64, float64) {
	brush := tileNoise(u, v, 128, 4)
	streak := tileNoise(u, v, 32, 2)
	scratch := math.Pow(0.5+0.5*math.Sin(2*math.Pi*(u*59+0.02*math.Sin(2*math.Pi*v))), 16)
	tone := 0.73 + 0.16*brush + 0.08*streak - 0.10*scratch
	b := channelByte(tone)
	return color.RGBA{R: b, G: b, B: b, A: 255}, 0.14*brush - 0.08*scratch, 0.23 + 0.26*brush + 0.10*scratch
}

func plasticPixel(u, v float64) (color.RGBA, float64, float64) {
	grain := tileNoise(u, v, 64, 64)
	b := channelByte(0.94 + 0.06*grain)
	return color.RGBA{R: b, G: b, B: b, A: 255}, 0.10 * grain, 0.57 + 0.17*grain
}

func concretePixel(u, v float64) (color.RGBA, float64, float64) {
	mottle := tileNoise(u, v, 5, 5)
	aggregate := tileNoise(u, v, 32, 32)
	pores := math.Max(0, tileNoise(u, v, 96, 96)-0.65) * 2
	b := channelByte(0.53 + 0.08*mottle + 0.05*aggregate - 0.06*pores)
	return color.RGBA{R: b, G: b, B: b + 5, A: 255}, 0.14*aggregate - 0.22*pores, 0.80 + 0.16*aggregate
}

// tileNoise interpolates a wrapping integer lattice. Its local hash leaves
// the physics RNG untouched, and smooth interpolation avoids pixel speckle.
func tileNoise(u, v float64, nx, ny int) float64 {
	x, y := u*float64(nx), v*float64(ny)
	ix, iy := int(math.Floor(x)), int(math.Floor(y))
	x, y = x-math.Floor(x), y-math.Floor(y)
	x, y = x*x*(3-2*x), y*y*(3-2*y)
	hash := func(a, b int) float64 {
		h := uint32((a%nx+nx)%nx)*0x8da6b343 ^ uint32((b%ny+ny)%ny)*0xd8163841 ^ 0xcb1ab31f
		h ^= h >> 13
		h *= 0x85ebca6b
		h ^= h >> 16
		return float64(h&0xffff) / 65535
	}
	a, b := hash(ix, iy), hash(ix+1, iy)
	c, d := hash(ix, iy+1), hash(ix+1, iy+1)
	return (a+(b-a)*x)*(1-y) + (c+(d-c)*x)*y
}

func channelByte(v float64) uint8 {
	return uint8(math.Max(0, math.Min(1, v))*255 + 0.5)
}
```
