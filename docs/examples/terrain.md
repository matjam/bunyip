---
title: Terrain
example: terrain
summary: an outdoor scene with a heightfield, billboards, levels of detail, point and spot lights, fog, world text, picking and live mesh edits
---

This is the outdoor scene a strategy or survival game draws. A
heightfield mesh with a lake and a ridge, five hundred billboard trees,
eighty rocks drawn at whichever level of detail their distance calls
for, four campfires as flickering point lights, a watchtower whose
searchlight is a spot light, distance and height fog, labels standing in
the world, a second camera's view volume drawn as lines, and terrain the
player digs into with a click, which rebuilds the mesh and uploads it
while the scene is running.

It uses the 3D half of [gfx](../pkg/gfx.html) broadly:
`HeightfieldMesh`, `Mesh.Update`, `Mesh.Intersect` with `ScreenRay` for
picking, `LOD` and `DrawLODAt`, `DrawBillboard`, `AddPointLight` and
`AddSpotLight`, `Sky` and `Fog` on the light, `DrawText3D`,
`DrawWireFrustum`, `Frustum().ContainsSphere` for culling by hand,
`PostSettings`, and `Stats` for the counts in the corner. The guide for
this material is [3D graphics](../guides/graphics-3d.html).

Run it with:

```bash
go run ./examples/terrain -seconds 3 -shot out.png
```

The flags are `-seconds N` and `-shot file.png`. Right-drag orbits the
camera, the scroll wheel zooms, a left click digs a crater where the ray
meets the ground, and Escape quits. With no drag the camera turns slowly
on its own.

## Constants and the game type

The heightfield is 97 by 97 samples at one world unit apart, so the
terrain is 96 units square. An odd count puts a sample exactly at the
origin, which keeps the arithmetic in `heightAt` and `dig` symmetric.

The game keeps the heights as a flat `[]float32` as well as the mesh,
because digging edits the heights and rebuilds the mesh from them. The
rest is the meshes, the tree texture, the scattered positions and the
camera's three numbers.

```go
const (
	cols, rows = 97, 97 // height samples across and deep
	cell       = 1.0    // world units per sample
)

type game struct {
	seconds  float64
	shot     string
	shotDone bool

	font    *gfx.Font
	terrain *gfx.Mesh
	heights []float32
	water   *gfx.Mesh
	tower   *gfx.Mesh
	roof    *gfx.Mesh
	ember   *gfx.Mesh
	rocks   *gfx.LOD
	rockAt  []gfx.Transform
	tree    *gfx.Texture
	trees   []lin.Vec3
	fires   []lin.Vec3
	towerAt lin.Vec3

	yaw, pitch, dist float32
	dug              bool
	skipped          int
}
```

## The shape of the ground

`height` is the terrain's generator: three sine and cosine products at
falling amplitudes for rolling hills, a Gaussian dip that carves the
lake basin, and a Gaussian ridge to the north. Anything smooth works
here; the point is that the terrain is a function, so it can be sampled
anywhere without a lookup table.

`heightAt` reads the current heights, which is a different thing: after
a dig the mesh no longer matches `height`, and everything placed on the
ground must follow what the heights say. It is a bilinear sample of the
four surrounding grid points, returning -10 outside the map so callers
reject the position.

```go
// height is the terrain's shape: rolling hills with a lake basin in the
// middle and a ridge to the north.
func height(x, z float32) float32 {
	h := 3*float32(math.Sin(float64(x)*0.08)*math.Cos(float64(z)*0.06)) +
		1.5*float32(math.Sin(float64(x)*0.21+1.3)*math.Sin(float64(z)*0.17)) +
		0.4*float32(math.Sin(float64(x)*0.9)*math.Cos(float64(z)*0.7))
	r := float32(math.Hypot(float64(x)+8, float64(z)-6))
	h -= 4 * float32(math.Exp(-float64(r*r)/300))
	h += 3 * float32(math.Exp(-float64((z+30)*(z+30))/200))
	return h + 1
}

// heightAt reads the current heights at a world point, bilinearly.
func (g *game) heightAt(x, z float32) float32 {
	fx, fz := x/cell+float32(cols-1)/2, z/cell+float32(rows-1)/2
	ix, iz := int(fx), int(fz)
	if ix < 0 || iz < 0 || ix >= cols-1 || iz >= rows-1 {
		return -10
	}
	tx, tz := fx-float32(ix), fz-float32(iz)
	h00, h10 := g.heights[iz*cols+ix], g.heights[iz*cols+ix+1]
	h01, h11 := g.heights[(iz+1)*cols+ix], g.heights[(iz+1)*cols+ix+1]
	return lerp(lerp(h00, h10, tx), lerp(h01, h11, tx), tz)
}

func lerp(a, b, t float32) float32 { return a + (b-a)*t }
```

## Building the terrain mesh

`gfx.HeightfieldMesh` turns a grid of heights into vertices and indices
with the normals already computed. The colours are then written per
vertex from the height and the slope: sand below the waterline, snow
above six units, and rock wherever the normal has tilted far enough from
vertical. Vertex colour multiplies the material's base colour, so a
plain material with no texture gives a terrain that reads correctly.

The function returns the two slices rather than a mesh, because it is
called twice: once in `Init` to create the mesh and once per dig to
update it.

```go
// buildTerrain makes the terrain mesh from the heights, coloured by
// height and slope: sand by the water, grass, rock on steep faces, snow
// on the peaks.
func (g *game) buildTerrain() ([]gfx.Vertex, []uint32) {
	verts, idx := gfx.HeightfieldMesh(g.heights, cols, rows, cell)
	sand, grass, rock, snow := gfx.RGB(194, 178, 128), gfx.RGB(86, 125, 50), gfx.RGB(110, 105, 100), gfx.RGB(235, 240, 245)
	for i := range verts {
		v := &verts[i]
		c := grass
		switch {
		case v.Pos.Y < 0.5:
			c = sand
		case v.Pos.Y > 6:
			c = snow
		}
		if v.Normal.Y < 0.75 {
			c = rock
		}
		v.Color = c
	}
	return verts, idx
}

// treeImage draws a cutout tree: a green canopy over a brown trunk.
func treeImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 64, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 64; x++ {
			var c color.RGBA
			switch {
			case y >= 62 && x >= 28 && x < 36:
				c = color.RGBA{92, 64, 40, 255}
			case y < 70 && math.Abs(float64(x)-32) < float64(y)*0.42+2:
				shade := uint8(90 + (x%7)*8)
				c = color.RGBA{30, shade, 40, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}
```

The tree image leaves everything outside the canopy and trunk fully
transparent, which is what makes it a cutout: the billboard is drawn
with `Cutout: true`, so those texels are discarded rather than blended,
and the tree casts a shaped shadow.

## Init: meshes, levels of detail and scattering

The heights are filled from `height` and the terrain mesh is built. Then
a plane for the water, a cylinder for the tower, a cone for its roof and
a small sphere for the campfire embers.

The rocks are a `gfx.LOD`: a list of meshes and the distances at which
each takes over. Here a 16 by 32 sphere is used up to 25 units, a
flat-shaded 5 by 8 sphere from there to 70, and nothing beyond, because
the third entry is `nil`. `gfx.FlatShaded` splits the shared vertices so
each face gets its own normal, which is what makes the far rocks read as
faceted stone rather than a smooth ball.

Trees and rocks are scattered with a seeded `rand` and rejected until
they land somewhere sensible: above the waterline, below the snow, and
on ground that is not too steep, tested by sampling the height one unit
away. Rocks are given a random scale and a random rotation about the
vertical, and `gfx.Transform.Rotated` returns a copy with that rotation
applied.

`SetPost` sets the post-processing for the whole run: exposure 1, a
touch of extra saturation, and bloom at 0.15 so the fires and the
searchlight glow.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 28, gfx.FontOptions{}); err != nil {
		return err
	}
	g.heights = make([]float32, cols*rows)
	for z := range rows {
		for x := range cols {
			g.heights[z*cols+x] = height(float32(x-cols/2)*cell, float32(z-rows/2)*cell)
		}
	}
	tv, ti := g.buildTerrain()
	if g.terrain, err = ctx.Gfx.NewMesh(tv, ti); err != nil {
		return err
	}
	pv, pi := gfx.PlaneMesh(1)
	if g.water, err = ctx.Gfx.NewMesh(pv, pi); err != nil {
		return err
	}
	cv, ci := gfx.CylinderMesh(16)
	if g.tower, err = ctx.Gfx.NewMesh(cv, ci); err != nil {
		return err
	}
	kv, ki := gfx.ConeMesh(16)
	if g.roof, err = ctx.Gfx.NewMesh(kv, ki); err != nil {
		return err
	}
	sv, si := gfx.SphereMesh(8, 12)
	if g.ember, err = ctx.Gfx.NewMesh(sv, si); err != nil {
		return err
	}
	// Rocks: a fine sphere near, a coarse one far, nothing beyond.
	fine, fineIdx := gfx.SphereMesh(16, 32)
	coarse, coarseIdx := gfx.FlatShaded(gfx.SphereMesh(5, 8))
	fineMesh, err := ctx.Gfx.NewMesh(fine, fineIdx)
	if err != nil {
		return err
	}
	coarseMesh, err := ctx.Gfx.NewMesh(coarse, coarseIdx)
	if err != nil {
		return err
	}
	g.rocks = gfx.NewLOD([]*gfx.Mesh{fineMesh, coarseMesh, nil}, []float32{25, 70})
	if g.tree, err = ctx.Gfx.NewTexture(treeImage(), gfx.TextureOptions{}); err != nil {
		return err
	}
	// Scatter trees and rocks on gentle land above the water.
	r := rand.New(rand.NewSource(7))
	for len(g.trees) < 500 {
		x, z := (r.Float32()-0.5)*90, (r.Float32()-0.5)*90
		h := g.heightAt(x, z)
		if h > 0.8 && h < 5.5 && g.heightAt(x+1, z)-h < 0.6 {
			g.trees = append(g.trees, lin.V3(x, h-0.1, z))
		}
	}
	for len(g.rockAt) < 80 {
		x, z := (r.Float32()-0.5)*92, (r.Float32()-0.5)*92
		h := g.heightAt(x, z)
		if h > 0.3 {
			s := 0.4 + r.Float32()*1.2
			t := gfx.Transform{Position: lin.V3(x, h-s*0.3, z), Scale: lin.V3(s, s*0.6, s*(0.7+r.Float32()*0.6))}
			g.rockAt = append(g.rockAt, t.Rotated(lin.V3(0, 1, 0), r.Float32()*6.28))
		}
	}
	for _, p := range [][2]float32{{-30, 20}, {25, -12}, {12, 30}, {-25, -25}} {
		g.fires = append(g.fires, lin.V3(p[0], g.heightAt(p[0], p[1])+0.3, p[1]))
	}
	g.towerAt = lin.V3(0, g.heightAt(0, -30), -30)
	g.yaw, g.pitch, g.dist = 0.6, 0.42, 48
	ctx.Gfx.SetPost(gfx.PostSettings{Exposure: 1, Saturation: 1.05, Contrast: 1, Bloom: 0.15})
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.font.Destroy()
	for _, m := range []*gfx.Mesh{g.terrain, g.water, g.tower, g.roof, g.ember} {
		m.Destroy()
	}
	for _, l := range g.rocks.Levels {
		if l.Mesh != nil {
			l.Mesh.Destroy()
		}
	}
	g.tree.Destroy()
}
```

`Shutdown` walks the LOD's levels and skips the nil one, since the LOD
does not own its meshes.

## Digging

`dig` lowers every height within a radius of a point by a smooth falloff
and calls `Mesh.Update` with a freshly built mesh. `Update` replaces the
geometry in place, keeping the same `*gfx.Mesh` so nothing that
references it has to change, and retires the old GPU buffers until the
frames that might still use them have been submitted.

```go
// dig lowers the terrain around a point and uploads the new geometry.
func (g *game) dig(at lin.Vec3, radius, depth float32) error {
	for z := range rows {
		for x := range cols {
			wx, wz := float32(x-cols/2)*cell, float32(z-rows/2)*cell
			d := float32(math.Hypot(float64(wx-at.X), float64(wz-at.Z)))
			if d < radius {
				t := 1 - d/radius
				g.heights[z*cols+x] -= depth * t * t
			}
		}
	}
	return g.terrain.Update(g.buildTerrain())
}
```

## Update: camera and picking

The right button orbits and the scroll wheel zooms, both clamped. With
no drag the yaw advances slowly, so a screenshot from an unattended run
shows the scene from a slightly different angle each time.

Picking is two calls. `ScreenRay(mx, my)` turns a pointer position into
a world ray using the camera the last frame set, and `Mesh.Intersect`
tests the ray against the mesh under a model matrix and returns the hit
point. Picking against the terrain mesh rather than against the height
function means the ray meets what is actually drawn, including previous
craters.

The second dig, one second in, is there so a headless screenshot shows a
crater without anyone clicking.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if in.MouseDown(input.MouseRight) {
		dx, dy := in.MouseDelta()
		g.yaw -= dx * 0.005
		g.pitch = lin.Clamp(g.pitch-dy*0.005, 0.1, 1.4)
	} else {
		g.yaw += float32(ctx.Delta) * 0.05
	}
	if _, dy := in.Scroll(); dy != 0 {
		g.dist = lin.Clamp(g.dist-dy*2, 10, 120)
	}
	if in.MousePressed(input.MouseLeft) {
		mx, my := in.Mouse()
		if hit, ok := g.terrain.Intersect(lin.Identity(), ctx.Gfx.ScreenRay(mx, my)); ok {
			if err := g.dig(hit.Point, 5, 2); err != nil {
				return err
			}
		}
	}
	if !g.dug && ctx.Time >= 1 {
		g.dug = true
		if err := g.dig(lin.V3(18, 0, 8), 7, 3); err != nil {
			return err
		}
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	return nil
}
```

## Draw: sky, fog and the lights

`gfx.OrbitCamera(target, yaw, pitch, distance)` builds the camera from
the three numbers, which saves writing the spherical coordinates by
hand.

The `Light` carries more than a direction here. `Sky` gives the ambient
term a gradient with a zenith, a horizon and a ground colour, so
surfaces facing up pick up blue and surfaces facing down pick up brown.
`Background: true` draws that sky behind the scene. `Fog` has a colour,
a start and an end distance, and a height falloff, so the fog thickens
in the valleys as well as with distance. Using the same haze colour for
the horizon and the fog is what makes the far hills meet the sky.

The campfires add a point light each frame with a flicker, plus a small
emissive sphere so the source is visible. The searchlight is
`AddSpotLight` with a position, a direction, a colour, a range and two
cone angles: the inner angle where it is at full strength and the outer
where it has fallen to nothing. Both are given in radians through
`lin.Radians`.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	t := float32(ctx.Time)
	gr.SetCamera(gfx.OrbitCamera(lin.V3(0, 2, 0), g.yaw, g.pitch, g.dist))
	haze := gfx.Color{R: 0.72, G: 0.78, B: 0.88, A: 1}
	gr.SetLight(gfx.Light{
		Direction:      lin.V3(-0.4, -0.7, -0.35),
		Color:          gfx.Color{R: 1, G: 0.95, B: 0.85, A: 1},
		Sky:            gfx.Sky{Zenith: gfx.Color{R: 0.2, G: 0.42, B: 0.85, A: 1}, Horizon: haze, Ground: gfx.Color{R: 0.3, G: 0.32, B: 0.25, A: 1}},
		Shadows:        true,
		ShadowDistance: 90,
		Background:     true,
		Fog:            gfx.Fog{Color: haze, Start: 45, End: 170, Height: 0.6, HeightFalloff: 0.4},
	})
	// Campfires flicker; the tower's searchlight sweeps.
	for i, f := range g.fires {
		flick := 0.8 + 0.2*float32(math.Sin(float64(t)*9+float64(i)))
		gr.AddPointLight(f.Add(lin.V3(0, 0.6, 0)), gfx.Color{R: 4 * flick, G: 2.2 * flick, B: 0.8 * flick, A: 1}, 12)
		gr.DrawMeshAt(g.ember, gfx.Material{BaseColor: gfx.Color{R: 1, G: 0.5, B: 0.15, A: 1}, Emissive: 3 * flick}, gfx.Transform{Position: f, Scale: lin.V3(0.35, 0.25, 0.35)})
	}
	beam := lin.V3(float32(math.Cos(float64(t)*0.6)), -0.45, float32(math.Sin(float64(t)*0.6)))
	top := g.towerAt.Add(lin.V3(0, 6.2, 0))
	gr.AddSpotLight(top, beam, gfx.Color{R: 9, G: 8.5, B: 6, A: 1}, 60, lin.Radians(14), lin.Radians(28))
```

## The scene, the billboards and the debug overlay

`DrawMesh` takes a matrix and `DrawMeshAt` takes a `gfx.Transform`,
which is the same thing written as a position, a rotation and a scale.
The water is a plane scaled to 200 units with a translucent base colour
and `Blend: true`, which puts it in the blended pass after the opaque
one.

`DrawText3D` draws shaped text standing in the world at a scale in world
units per view unit. The fourth argument says whether the text always
faces the camera; here it is false, so the labels stay fixed to the
scene.

The rocks are drawn with `DrawLODAt`, which picks a level from the
distance to the camera. The trees are culled by hand: `gr.Frustum()`
returns the camera's view volume and `ContainsSphere` rejects a tree
whose bounding sphere is outside it, so those are never queued at all.
The engine culls meshes itself; this loop shows the same test being done
before the call, which is what a game does when it has more instances
than it wants to submit.

`DrawWireFrustum` draws a second camera's view volume as lines, with the
aspect ratio it would be rendered at, and `DebugText3D` labels a point
in the world. `Stats()` returns the frame's counts, and `DebugText`
prints them in the corner.

```go
	gr.DrawMesh(g.terrain, gfx.Material{Roughness: 0.95}, lin.Identity())
	gr.DrawMesh(g.water, gfx.Material{BaseColor: gfx.Color{R: 0.1, G: 0.32, B: 0.6, A: 0.65}, Blend: true, Roughness: 0.12}, lin.Translate(lin.V3(0, 0, 0)).Mul(lin.Scale(lin.V3(200, 1, 200))))
	gr.DrawMeshAt(g.tower, gfx.Material{BaseColor: gfx.RGB(120, 100, 80), Roughness: 0.8}, gfx.Transform{Position: g.towerAt.Add(lin.V3(0, 3, 0)), Scale: lin.V3(0.9, 3, 0.9)})
	gr.DrawMeshAt(g.roof, gfx.Material{BaseColor: gfx.RGB(150, 50, 40), Roughness: 0.7}, gfx.Transform{Position: g.towerAt.Add(lin.V3(0, 7, 0)), Scale: lin.V3(1.5, 1, 1.5)})
	gr.DrawText3D(g.font, "Watchtower", g.towerAt.Add(lin.V3(0, 9, 0)), 0.05, gfx.White, false, gfx.TextOptions{})
	gr.DrawText3D(g.font, "Lake", lin.V3(-8, 1.2, 6), 0.06, gfx.Color{R: 0.8, G: 0.9, B: 1, A: 1}, false, gfx.TextOptions{})

	rock := gfx.Material{BaseColor: gfx.RGB(115, 110, 105), Roughness: 0.9}
	for _, at := range g.rockAt {
		gr.DrawLODAt(g.rocks, rock, at)
	}
	// Trees the camera cannot see are not even queued.
	fr := gr.Frustum()
	g.skipped = 0
	for _, p := range g.trees {
		if !fr.ContainsSphere(p.Add(lin.V3(0, 1.5, 0)), 2) {
			g.skipped++
			continue
		}
		gr.DrawBillboard(gfx.Billboard{Texture: g.tree, Position: p, Size: lin.V2(2, 3), Offset: lin.V2(0, 0.5), Upright: true, Lit: true, Cutout: true})
	}
	// A scout camera's view volume, drawn as lines.
	scout := gfx.Camera{Position: lin.V3(30, 10, 25), Target: lin.V3(10, 0, 5), FovY: lin.Radians(40), Far: 30}
	gr.DrawWireFrustum(scout, 16.0/9, gfx.Color{R: 1, G: 0.9, B: 0.2, A: 1})
	gr.DebugText3D(scout.Position, "scout")

	s := gr.Stats()
	gr.DebugText(10, 10, fmt.Sprintf("draws %d  instances %d  culled %d  trees skipped %d", s.Draws3D, s.Instances, s.Culled, g.skipped))
	gr.DebugText(10, 28, "right-drag orbits, scroll zooms, click digs")
	return nil
}
```

A billboard with `Upright: true` turns about the vertical axis only, so
a tree stays standing rather than tipping to face a camera looking down.
`Lit: true` puts it through the lighting instead of drawing it at full
brightness, and `Offset` moves its anchor so the position is the foot of
the trunk.

## main

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip terrain", Width: 1024, Height: 640, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

## What to try

- Change the LOD distances in `Init` to `[]float32{5, 12}` and watch the
  rocks switch level close to the camera, then read the culled count in
  the corner.
- Raise the fog `Start` and `End` in `Draw`, or set `HeightFalloff` to
  0, and see how much of the depth in the scene comes from the fog.
- Give the dig in `Update` a negative depth so a click raises the ground
  instead, which turns the same code into a terrain sculptor.
- Add a second `AddSpotLight` in `Draw` pointing along the beam from the
  camera, so the scene has a torch.
- Remove the frustum test in `Draw` and compare the frame time and the
  culled count with five hundred trees always queued.
