---
title: Materials
example: materials
summary: every material feature on a row of spheres and a few props: clearcoat, sheen, subsurface, glass, alpha cutout, vertex colours, a scrolling texture, a decal, an outline and an x-ray tint
---

Seven spheres in a row show seven surfaces: brushed metal, car paint with
a clearcoat, velvet with sheen, skin with subsurface scattering shaped by
a thickness map, a mesh coloured per vertex, an unlit sphere, and
refracting glass that absorbs colour over distance. Around them are
alpha-cutout leaves casting cutout shadows, a floor scrolling its texture
through a UV transform with a decal projected onto it, an outlined
sphere, and a sphere behind a wall showing through with an x-ray tint.

Everything here is [gfx.Material](../pkg/gfx.html#Material), one plain
value passed to a draw call. There is no material object to create or
destroy, so a program can build one per frame, which is what the row does.
The textures a material refers to are GPU resources and are created once.

Lighting is a procedural sky, or a panorama with `-env`, which also shows
how a Radiance `.hdr` file differs from a PNG. Read
[the 3D graphics guide](../guides/graphics-3d.html) for the renderer and
[the physics3d walkthrough](physics3d.html) for the same material fields
used on five hundred cubes.

Run it:

```bash
go run ./examples/materials -seconds 3 -shot out.png
```

The flags are `-seconds N`, `-shot file.png` and `-env file.hdr` for a
panorama, which may also be a PNG or a JPEG. Dragging orbits, the wheel
zooms and Escape quits.

## Package and state

The game holds four meshes, four textures, the optional environment and
the camera's three numbers.

```go
// Command materials shows every material feature on a row of spheres and
// a few props: clearcoat, sheen, subsurface, alpha cutout, unlit, vertex
// colours, a scrolling texture transform, occlusion, an outline and an
// x-ray tint through a wall, a projected decal, all under image-based
// lighting from a sky or a panorama (-env file.png or file.hdr). Drag to
// orbit, scroll to zoom, Escape quits.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
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
	yaw      float32
	pitch    float32
	dist     float32
	lastX    float32
	lastY    float32
	dragging bool
	shotDone bool
	xray     bool
}
```

## Init: meshes and textures

The spheres and the cube come from the built-in generators. The vertex
coloured sphere is the same vertex slice with a `Color` written into each
vertex before a second upload: `gfx.Vertex` carries an optional colour
that the shader multiplies into the base colour, which is why the
material for it sets only a roughness.

The texture options matter here. `Repeat: true` makes the floor's stripes
tile under a UV transform that scales past one. `Linear: true` asks for
smooth sampling. `Data: true` on the thickness map says the texels are
numbers rather than colours, so they are uploaded without the sRGB
conversion a colour texture gets.

```go
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
	if g.envPath != "" {
		if g.env, err = loadEnvironment(ctx.Gfx, g.envPath); err != nil {
			return err
		}
	}
	g.yaw, g.pitch, g.dist = 0.4, 0.3, 11
	g.xray = true
	return nil
}
```

`loadEnvironment` shows the two paths into image-based lighting. A
Radiance `.hdr` file is decoded by `gfx.DecodeHDR` and passed to
`NewEnvironmentHDR`, which keeps values above one, so a sun in the
panorama stays a sun. A PNG or JPEG is an ordinary sRGB image, so it goes
through `NewEnvironment` with an `Intensity` to make up some of the range
it cannot carry.

```go
// loadEnvironment reads a panorama: Radiance .hdr keeps its full range,
// PNG and JPEG are treated as sRGB.
func loadEnvironment(gr *gfx.Graphics, path string) (*gfx.Environment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if strings.HasSuffix(strings.ToLower(path), ".hdr") {
		data, err := io.ReadAll(f)
		if err != nil {
			return nil, err
		}
		img, err := gfx.DecodeHDR(data)
		if err != nil {
			return nil, err
		}
		return gr.NewEnvironmentHDR(img, gfx.EnvironmentOptions{})
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return gr.NewEnvironment(img, gfx.EnvironmentOptions{Intensity: 1.5})
}
```

## Shutdown

The loops destroy each texture and mesh in turn. The environment is
optional, so it is checked first.

```go
func (g *game) Shutdown(ctx *bunyip.Context) {
	if g.env != nil {
		g.env.Destroy()
	}
	for _, t := range []*gfx.Texture{g.thick, g.splat, g.leaf, g.stripes} {
		t.Destroy()
	}
	for _, m := range []*gfx.Mesh{g.colored, g.quad, g.cube, g.sphere} {
		m.Destroy()
	}
	g.font.Destroy()
}
```

## Update: the orbit camera

The camera is driven by the pointer's motion between updates, clamped so
the pitch stays away from the poles and the distance inside a range.
`g.ui.WantsMouse` keeps a drag on the panel from turning the camera.

```go
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
```

## Draw: the light, the floor and the decal

The light carries the sky, the shadow settings and the environment
together. `Environment: g.env` is nil unless `-env` was given, and a nil
environment leaves the procedural sky in place, which is the usual
zero-value rule.

The floor is a flattened cube with a `UVTransform`, an affine transform
applied to texture coordinates before sampling:
`lin.Translate2(t*0.05, 0).Mul(lin.Scale2(6, 6))` tiles the stripes six
times and scrolls them sideways with time. The transform is part of the
material, so nothing about the mesh changes.

`DrawDecal` projects a texture onto whatever geometry lies inside a box.
The matrix maps the unit cube into the world, the texture is projected
along the box's Y axis with its X and Z spanning the image, and it fades
on surfaces facing away. It is how a game puts a scorch mark or a
footprint on geometry it does not own.

```go
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
		lin.Translate(lin.V3(0, -0.5, 0)).Mul(lin.Scale(lin.V3(14, 0.2, 14))))
	gr.DrawDecal(g.splat, lin.Translate(lin.V3(-2.5, -0.3, 2.5)).Mul(lin.Rotate(t*0.3, lin.V3(0, 1, 0))).Mul(lin.Scale(lin.V3(2, 1, 2))), gfx.RGB(120, 20, 20))
```

## Draw: the row of spheres

Each entry is a name and a material. `Metallic: 1` makes the base colour
a reflection tint. `Clearcoat` adds a smooth layer over a rough one.
`Sheen` is the soft edge light of cloth. `Subsurface` lets light through
thin parts, shaped by `ThicknessTexture`, whose bands make the light show
through in stripes. The vertex coloured sphere sets no base colour, so
white multiplies the per-vertex colours. `Unlit: true` draws the base
colour as it is, ignoring every light, which is what a user interface
element or a stylised object wants. The glass sets `Transmission` with an
`IOR` and a `Thickness`, and `AttenuationColor` over
`AttenuationDistance` is what white light becomes after that far inside
the volume.

The loop draws them along X with one matrix each.

```go
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
	}
	for i, r := range row {
		x := float32(i)*2.2 - 6.6
		gr.DrawMesh(r.mesh, r.mat, lin.Translate(lin.V3(x, 0.4, -2)).Mul(lin.Scale(lin.V3(0.8, 0.8, 0.8))))
	}
```

## Draw: cutouts, outlines and x-ray

`AlphaCutoff: 0.5` discards fragments below that alpha in both the lit
pass and the shadow pass, so a leaf casts a leaf-shaped shadow rather
than a rectangle. `DoubleSided: true` keeps the back faces, lit with a
flipped normal, because a flat quad seen from behind would otherwise
disappear.

`Outline` with an `OutlineColor` draws a line that many pixels wide
around the mesh's silhouette, for selection rings and cartoon edges.
`XRay` tints the parts of the mesh hidden behind other geometry, which is
the usual way to show a selected unit through a wall. Setting it on a
copy of the material leaves the original alone.

```go
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
```

## Draw: the panel

One checkbox and a caption. `u.Checkbox` takes a pointer to the bool it
edits, which is the immediate-mode form throughout
[ui](../pkg/ui.html).

```go
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Materials", ui.Rect{X: 12, Y: 12, W: 320, H: 150}, func() {
			u.Checkbox("X-ray the sphere behind the wall", &g.xray)
			u.Label("Row: metal, clearcoat, sheen, subsurface, vertex colours, unlit, glass. Leaves are alpha cutouts; the green sphere is outlined; the floor scrolls its texture and carries a decal.")
		})
	})
	return nil
}
```

## The generated meshes and textures

`quadMesh` is a unit square facing positive Z, with texture coordinates
whose V runs downwards, which is the convention the image loaders use.

```go
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
```

The four textures are generated so the example needs no asset files.
`stripes` is a tiling pattern for the floor, `leafShape` a leaf on a
transparent background for the cutouts, `splat` a soft-edged blob with
alpha for the decal, and `thickness` a banded greyscale read as data
rather than colour.

```go
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
```

```go
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
```

```go
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
```

```go
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
```

## main

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	env := flag.String("env", "", "panorama for lighting: .hdr, .png or .jpg")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip materials", Width: 1100, Height: 680, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, envPath: *env})
	if err != nil {
		fmt.Fprintln(os.Stderr, "materials:", err)
		os.Exit(1)
	}
}
```

## What to try

- Set `AttenuationDistance` on the glass sphere in `Draw` to 0.1 and
  watch the absorption swallow the colour.
- Give the leaves `AlphaCutoff` of 0 in `Draw` and see the shadows become
  rectangles.
- Change the `UVTransform` on the floor in `Draw` to rotate instead of
  scroll, with `lin.Rotate2`.
- Put a `NormalTexture` on the metal sphere in `Draw` and see roughness
  and normals interact.
- Pass `-env` a Radiance file and compare it with the same panorama
  exported as a PNG, which `loadEnvironment` treats differently.
