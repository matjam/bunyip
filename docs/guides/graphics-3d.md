---
title: 3D graphics
group: Graphics
order: 2
summary: cameras, meshes and models, materials, lights and shadows, sky and fog, culling and LOD, post-processing and render textures for a 3D game
---

The [gfx](../pkg/gfx.html) package draws 2D and 3D through one context.
This guide is the 3D half, in the order a game needs it: put a camera in
the world, get geometry into it, shade it, light it, give it a sky, then
cull, grade and profile what you have. The examples are the fastest
reference: `viewer` orbits a scene or a glTF file, `lighting` puts every
post-processing setting on a slider, `materials` shows one sphere per
feature, `terrain` is an outdoor scene with billboards, levels of detail
and fog, and `space` and `solar` are vacuum scenes.

## The frame

There is no scene object and no begin call. Everything queued during
`Draw` is submitted when `Draw` returns, in a fixed order: render
textures first, then the shadow atlas, then the scene into a high dynamic
range image (sky, opaque meshes, decals, blended and transmissive meshes
back to front, debug lines), then the post pass, then 2D over the
tone-mapped result in layer and call order.

So 2D always draws over 3D. `ctx.Clear` is the colour behind everything,
though when `Light.Background` is set the sky or environment map is drawn
instead and the clear colour never shows. A HUD or a name plate is 2D
drawing after the mesh calls, and `Project` turns a world point into the
view coordinates 2D uses. A 3D hero on a 2D field is the other way round
and needs one step more: draw the 3D into a `RenderTexture` and draw that
as a sprite between the background and foreground layers.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	ctx.Clear = gfx.RGB(10, 12, 18)
	gr.SetCamera(gfx.OrbitCamera(lin.V3(0, 1, 0), g.yaw, g.pitch, 12))
	gr.SetLight(gfx.Light{Direction: lin.V3(-0.4, -1, -0.6),
		Color:   gfx.Color{R: 2.2, G: 2.1, B: 1.9, A: 1},
		Ambient: gfx.Color{R: 0.18, G: 0.2, B: 0.25, A: 1},
		Shadows: true, ShadowDistance: 40})
	gr.DrawMeshAt(g.ship, gfx.Material{Metallic: 1, Roughness: 0.3}, g.shipAt)
	gr.DebugText(10, 10, "hull 100%")
	return nil
}
```

`SetCamera`, `SetLight` and `SetPost` apply to the whole output for the
frame, not to the calls after them, so where you put them does not
matter. Call each once.

## Cameras

`Camera` looks from `Position` at `Target`. `FovY` is the vertical field
of view in radians (zero means 60 degrees), `Near` and `Far` default to
0.1 and 1000, `Up` defaults to +Y. Setting `Ortho` to half the view's
height in world units makes the camera orthographic, which is what an
isometric strategy view wants: distance no longer shrinks things.

```go
// Third person, then the same focus seen isometrically.
gr.SetCamera(gfx.Camera{Position: player.Add(lin.V3(0, 3, 8)),
	Target: player.Add(lin.V3(0, 1.6, 0)), FovY: lin.Radians(55), Far: 500})
gr.SetCamera(gfx.Camera{Position: focus.Add(lin.V3(30, 30, 30)), Target: focus, Ortho: 20})
```

`OrbitCamera(target, yaw, pitch, distance)` builds the inspector or
strategy camera from three numbers the mouse can drive.
`bunyip.FlyCamera` is a free-flying camera for looking around a scene
while it is being built: W, A, S and D move, Q and E go down and up,
Shift goes faster, and the view turns while the right mouse button is
held, or with every movement when `AlwaysLook` is set and the cursor is
captured. `LookAt` points it at something, `Forward` gives its direction.

```go
g.fly = &bunyip.FlyCamera{Position: lin.V3(0, 5, 20), Speed: 25} // in Init
func (g *game) Update(ctx *bunyip.Context) error { g.fly.Update(ctx); return nil }
func (g *game) Draw(ctx *bunyip.Context) error   { ctx.Gfx.SetCamera(g.fly.Camera()); return nil }
```

`Project(p)` maps a world point into the 2D view and returns `ok` false
when the point is behind the camera. `ScreenRay(x, y)` turns a point in
the view (the units the mouse reports) into a world ray, and
`Mesh.Intersect` or `Model.Intersect` report where it hits. For a scene
backed by physics, `phys.Raycast3` casts against colliders instead, which
is cheaper and gives you the entity.

```go
ray := gr.ScreenRay(ctx.Input.Mouse())
for _, u := range g.units {
	if hit, ok := u.model.Intersect(u.at.Matrix(), ray); ok && hit.Distance < best {
		best, g.selected = hit.Distance, u
	}
	if x, y, ok := gr.Project(u.at.Position.Add(lin.V3(0, 2.2, 0))); ok {
		gr.DrawText(g.font, u.name, x, y, gfx.White) // name plate in 2D
	}
}
```

`Camera.Frustum(aspect)` and `Graphics.Frustum()` return the volume the
camera sees; the culling section uses them.

## Meshes

A `Mesh` is indexed triangle geometry in device memory. `NewMesh` uploads
a slice of `Vertex` (position, normal, UV, an optional second UV set and
an optional colour that multiplies the material's base colour) and a
slice of indices.

The shape functions return those two slices without touching the GPU, so
you can transform and merge them first: `CubeMesh`, `SphereMesh(rings,
segments)`, `PlaneMesh(segments)`, `QuadMesh`, `CylinderMesh(segments)`,
`ConeMesh(segments)`, `CapsuleMesh(rings, segments, halfHeight)`,
`TorusMesh(tube, rings, segments)` and `HeightfieldMesh(heights, cols,
rows, cell)`. `HeightfieldMesh` is the terrain case: a grid of heights,
its size in samples, and the world units between samples.

```go
verts, idx := gfx.HeightfieldMesh(g.heights, cols, rows, 1.0)
for i := range verts { // colour by height and slope: no textures needed
	v := &verts[i]
	v.Color = gfx.RGB(86, 125, 50)
	if v.Normal.Y < 0.75 {
		v.Color = gfx.RGB(110, 105, 100) // cliff
	} else if v.Pos.Y > 6 {
		v.Color = gfx.RGB(235, 240, 245) // snow
	}
}
terrain, err := ctx.Gfx.NewMesh(verts, idx)
```

Building geometry yourself takes three helpers. `TransformVertices`
returns a copy of a shape moved by a matrix, `AppendMesh` merges two
shapes into one, and `ComputeNormals` fills in smooth normals for
vertices written by hand. `FlatShaded` does the opposite: it splits
shared vertices so every triangle keeps its own normal, for the faceted
look and for coarse levels of detail.

```go
// A voxel chunk: one mesh for the visible blocks, one draw call.
cube, cubeIdx := gfx.CubeMesh()
var verts []gfx.Vertex
var idx []uint32
for _, b := range chunk.Visible() {
	placed := gfx.TransformVertices(cube, lin.Translate(b.Pos))
	for i := range placed {
		placed[i].Color = b.Color
	}
	verts, idx = gfx.AppendMesh(verts, idx, placed, cubeIdx)
}
chunk.mesh, err = ctx.Gfx.NewMesh(verts, idx)
```

`Mesh.Update(verts, indices)` replaces the geometry of a mesh already on
the GPU: a voxel chunk after a block is dug, a terrain edit, a mesh that
grows. Draws already queued this frame keep the old geometry until the
frame ends, so an update is safe at any point in `Update` or `Draw`. Use
it instead of destroying and recreating a mesh. `Mesh.Min` and `Mesh.Max`
are the bounds in mesh space, `Vertices` and `Indices` read the geometry
back, and `Destroy` frees it.

Draw with `DrawMesh(mesh, material, model)` where `model` is a
`lin.Mat4`, or `DrawMeshAt(mesh, material, transform)` when a
`gfx.Transform` is what you have. Draws sharing a mesh and a material are
collected into one instanced call, so a thousand asteroids or a forest of
identical trunks cost one draw. That happens on its own, and keeping
materials identical is what makes it work, so prefer vertex colours or
one atlas over a material per object.

`NewSkinnedMesh` takes `SkinVertex` values with four joint indices and
weights, and `DrawSkinned(mesh, material, model, joints)` draws it with
joint matrices the game computed itself, as the `lighting` example does.

## Models

`gltf.Load` reads a `.gltf` or `.glb` file into a `Document` of plain Go
slices with no GPU involved, and `Graphics.LoadModel` uploads it: one
`Mesh` per primitive, one `Texture` per image, materials with the
extensions the renderer supports, skins, clips and morph targets.
`asset.Model` does both through the [asset](../pkg/asset.html) package,
from a loose directory, a pack file or an embedded FS.

```go
doc, err := gltf.Load("assets/ship.glb")
if err != nil {
	return err
}
g.ship, err = ctx.Gfx.LoadModel(doc) // or asset.Model(ctx.Gfx, g.fs, "ship.glb")
```

`DrawModel(m, world)` queues every part under a world matrix and
`DrawModelAt(m, transform)` takes a `Transform` instead. `Model.Min` and
`Model.Max` bound the whole thing, which is what to size a camera's
distance from when the file is a surprise. `Model.Parts` is the placed
primitives, each a `ModelPart` with a `Mesh`, a `Material` and a `World`
matrix, for drawing one part with its own material. `NodeCount`,
`NodeName`, `NodeIndex` and `NodeParent` walk the node hierarchy by name,
which is how you find the muzzle to spawn a flash at. `Model.Destroy`
frees the meshes and textures together.

Skinning: `Model.Clips` lists the animation clips, `Model.NewAnimPlayer`
makes an `AnimPlayer` holding one instance's pose, `Play` or `CrossFade`
chooses a clip, `Advance(dt)` runs it, and `DrawModelAnimated(model,
transform, player)` draws the result. Layers, masks, events, root motion,
morph targets and node overrides for inverse kinematics all hang off the
player; the [animation guide](animation.html) covers them.

## Transforms

The [lin](../pkg/lin.html) package is the maths: `Vec2`, `Vec3`, `Vec4`,
`Mat4` and `Quat` in float32, column-major, right-handed, +Y up. Values
pass by value and every operation returns a new one. `lin.Translate`,
`lin.Scale`, `lin.Rotate(angle, axis)` and `lin.TRS(t, r, s)` build
matrices and `Mul` composes them left to right, so the last applied is
written last. Quaternions avoid the gimbal trouble of stacked Euler
angles: `lin.AxisAngle`, `lin.FromEuler(yaw, pitch, roll)`,
`lin.QuatLookAt`, `lin.QuatIdentity`. `lin.Radians` converts degrees.

`gfx.Transform` is the friendlier form: a `Position`, a `Rotation`
quaternion and a `Scale`, where a zero rotation means none and a zero
scale means 1. `gfx.At(x, y, z)` makes one, `Moved`, `Rotated` and
`Scaled` return adjusted copies, `Matrix` gives the `lin.Mat4` and
`Forward` the direction it faces. It is the component physics, animation
and the ECS use, so a body's transform is the one you draw with.

```go
gr.DrawMesh(g.rock, rockMat, lin.Translate(pos).Mul(lin.Rotate(yaw, lin.V3(0, 1, 0))))
gr.DrawMeshAt(g.rock, rockMat, gfx.At(0, 1, 0).Rotated(lin.V3(0, 1, 0), lin.Radians(30)).Scaled(0.5))
```

For hierarchies, put a `gfx.Transform` on each entity, parent them with
`ecs.SetParent(w, child, parent)`, and `ecs.WorldMatrix(w, e)` composes
the chain from the root down. A turret on a hull on a chassis is three
entities and one call at draw time.

## Materials

`Material` is metallic-roughness PBR. `Texture` is the albedo in sRGB,
`BaseColor` multiplies it (zero means white), `Metallic` runs 0 for a
dielectric to 1 for a metal, and `Roughness` runs from mirror to matte
with a zero meaning 0.6. `MetalRoughTexture` carries roughness in green
and metallic in blue as glTF does, `NormalTexture` is a tangent-space
normal map, `EmissiveTexture` and `Emissive` make something glow, and
`OcclusionTexture` with `OcclusionStrength` darkens ambient light in
creases (`OcclusionUV2` samples it on the second UV set, the lightmap
convention). Every texture is optional; the factors alone are a complete
material: gold is `{BaseColor: gfx.RGB(240, 200, 120), Metallic: 1,
Roughness: 0.15}` and a glowing lamp is `{BaseColor: gfx.RGB(255, 180,
60), Emissive: 3}`.

Transparency has three modes and they are not interchangeable.
`AlphaCutoff` discards fragments below a threshold in both the lit and
the shadow pass, which is what leaves, fences and grass want: hard edges
and real shadows. `Blend` draws the material after the opaque scene,
sorted back to front, for smoke and water. `Transmission` is refractive
glass, below. Alongside them, `DoubleSided` turns off back-face culling
and lights back faces with a flipped normal, which a single-quad leaf
needs; `Unlit` shows the base colour and emissive as they are, for
holograms and map markers; `NoDepthTest` draws over everything already
drawn and `NoDepthWrite` leaves the depth buffer alone, for ghosts and
additive effects; and `UVTransform`, a `lin.Affine` on the texture
coordinates, scrolls a conveyor or tiles a floor in one field.

The layered features come from the matching glTF extensions and cost
nothing left at zero. `Clearcoat` with `ClearcoatRoughness` adds a
varnish lobe for car paint and wet surfaces, `Sheen` with
`SheenRoughness` adds soft light at grazing angles for velvet and cloth,
and `Subsurface`, shaped by a `ThicknessTexture`, lets light through thin
parts for leaves, wax and skin. `Transmission` makes glass and ice: the
opaque scene shows through, refracted by `IOR` (zero means 1.5) across
`Thickness` world units, blurred by the roughness and absorbed towards
`AttenuationColor` over `AttenuationDistance`, with a
`TransmissionTexture` letting one material hold an opaque frame and glass
panes. Transmissive meshes draw after the opaque ones, like blended ones.

```go
floor := gfx.Material{Texture: g.stripes, Roughness: 0.8,
	UVTransform: lin.Translate2(t*0.05, 0).Mul(lin.Scale2(6, 6))}
glass := gfx.Material{Roughness: 0.05, Transmission: 1, IOR: 1.5, Thickness: 0.8,
	AttenuationColor: gfx.RGB(120, 200, 255), AttenuationDistance: 1}
velvet := gfx.Material{BaseColor: gfx.RGB(40, 30, 90), Roughness: 0.9,
	Sheen: gfx.RGB(200, 180, 255), SheenRoughness: 0.4}
```

Vertex colours multiply the base colour and need no field at all; glTF
files fill them from `COLOR_0` and the terrain snippet above writes them
by hand. The second UV set comes from `TEXCOORD_1`. A `Shader` from
`NewMeshShader` replaces the surface calculation before lighting, for
water, dissolves, triplanar mapping and vertex displacement; the
[shaders guide](shaders.html) covers writing one.

## Lighting

`SetLight` takes one `Light` for the frame: the directional light
(`Direction` is the direction the light travels, so a sun overhead points
down), its `Color`, and an `Ambient` term that lights everything evenly
when no sky or environment is set. Light colours are linear and are not
clamped to 1; a sun is usually above 2.

`Shadows` renders cascaded shadow maps for the directional light,
reaching `ShadowDistance` world units from the camera (default 60) at
`ShadowStrength` (zero means fully dark). The cascades pack more
resolution near the camera, so the tuning knob is `ShadowDistance`: keep
it as small as the game can stand. Forty over a character scene is crisp,
two hundred over a whole valley will alias. Cutout materials cast cutout
shadows, so a leaf texture throws a leaf-shaped shadow.

`AddPointLight(pos, color, range)` shines in every direction from a
point, fading to nothing at `range`: torches, muzzle flashes, glowing
ore. `AddSpotLight(pos, dir, color, range, inner, outer)` shines in a
cone, full inside the inner angle and fading to nothing at the outer.
`AddSpot` takes a `SpotLight` value instead, and with `Shadows` set that
light casts shadows from its own depth map.

```go
gr.SetLight(gfx.Light{Direction: lin.V3(-0.4, -0.7, -0.35),
	Color:   gfx.Color{R: 3, G: 2.85, B: 2.5, A: 1},
	Shadows: true, ShadowDistance: 60, Background: true,
	Sky:     gfx.Sky{Zenith: gfx.RGB(50, 105, 215), Horizon: gfx.RGB(184, 199, 224)}})
for i, f := range g.fires {
	flick := 0.8 + 0.2*float32(math.Sin(float64(t)*9+float64(i)))
	gr.AddPointLight(f, gfx.Color{R: 4 * flick, G: 2.2 * flick, B: 0.8 * flick, A: 1}, 12)
}
gr.AddSpot(gfx.SpotLight{Position: towerTop, Direction: beam, Range: 60,
	Color:      gfx.Color{R: 9, G: 8.5, B: 6, A: 1},
	InnerAngle: lin.Radians(14), OuterAngle: lin.Radians(28), Shadows: true})
```

The limits are worth planning around. A frame keeps its first
`MaxLights` (32) point and spot lights and gives shadow maps to the first
`MaxSpotShadows` (4) that ask; the rest shine without. Sort by distance
to the camera and add the nearest first. Point lights never cast shadows.
All the cascades and spot maps share one depth atlas, so shadows cost one
texture binding however many lights cast them.

For image-based lighting, `NewEnvironment` turns an equirectangular
panorama into a light probe and `NewEnvironmentHDR` does the same for a
Radiance `.hdr` panorama read with `DecodeHDR`, keeping its range.
`EnvironmentOptions.Intensity` scales it and `Size` sets the cube map's
side in texels (default 128). Set it as `Light.Environment` and it
replaces the ambient and the sky: metals reflect it, rough surfaces take
its tint from every direction, and `Light.Background` draws it behind the
scene. Environments hold GPU memory; `Destroy` them in `Shutdown`.

```go
hdr, err := gfx.DecodeHDR(data)
if err != nil {
	return err
}
g.env, err = ctx.Gfx.NewEnvironmentHDR(hdr, gfx.EnvironmentOptions{Intensity: 1.5, Size: 256})
light.Environment, light.Background = g.env, true
```

## Sky, fog and atmosphere

`Light.Sky` is a procedural environment built from what the game already
knows: an `Up` axis, `Zenith`, `Horizon` and `Ground` colours, and how
much air there is. It needs no image, costs nothing to change every
frame, and lights the scene the way an environment map does. With
`Light.Background` the sun's disc, sized by `SunSize` and coloured by
`Sun`, its haze and the stars are drawn behind the scene.

`Vacuum` is what makes it useful to a space game. At 0 the air is full;
raising it towards 1 fades the sky to black while `Stars` come out, and
the ground half stays, so a ship can climb from a runway to orbit with no
seam. Pointing `Up` away from a nearby planet and setting `Ground` to
that planet's colour lights the ship's night side with planet-shine.

`Light.Fog` fades geometry into a colour with distance, the cheapest way
to give a scene depth and hide the far plane. Linear fog ramps from
`Start` to full at `End`; exponential fog thickens with `Density`; when
both are set the denser wins. `Height` and `HeightFalloff` add ground
fog, full at and below `Height` and thinning above it along the world's y
axis, which puts mist in a valley without touching the hilltops. The sky
is not fogged, so outdoors pick a fog colour close to the horizon's or
the join will show.

```go
// In orbit, the night side lit by the planet below.
sky := gfx.Sky{Up: planetUp.Mul(-1), Vacuum: 1, Stars: 1,
	Ground: gfx.Color{R: 0.2 * k, G: 0.3 * k, B: 0.45 * k, A: 1}}
gr.SetLight(gfx.Light{Direction: sunDir, Sky: sky, Background: true,
	Color: gfx.Color{R: 2.4, G: 2.2, B: 1.9, A: 1}})

// On the ground: haze on the horizon, mist in the valley.
haze := gfx.Color{R: 0.72, G: 0.78, B: 0.88, A: 1}
light.Fog = gfx.Fog{Color: haze, Start: 45, End: 170, Height: 0.6, HeightFalloff: 0.4}
```

## Billboards, labels and marks on the world

`DrawBillboard` puts a textured quad in the scene that turns to face the
camera. `Upright` turns it about the world's up axis only, so a tree
stays vertical when the camera looks down; `Offset` moves the quad in its
own plane in units of its size, so `(0, 0.5)` stands the sprite on the
ground; `Lit` shades it with the scene's lights; `Cutout` gives it hard
edges that write depth and cast shadows; `OnTop` draws it over
everything; `Region` takes a rectangle of an atlas. Billboards go through
the mesh path, so many with one texture become one instanced draw: five
hundred trees are one call. `DrawText3D(font, text, pos, scale, color,
onTop, opts)` draws a line of text the same way, centred on `pos`, with
`scale` in world units per view unit of the font.

`DrawDecal(tex, box, tint)` projects a texture onto whatever geometry
lies inside a box: bullet holes, blood, footprints, road markings. The
box matrix maps the unit cube to the world, the texture projects along
the box's y axis with x and z spanning the image, and it fades on
surfaces facing away. Two material fields mark a mesh out rather than the
world: `Outline` draws a silhouette line of that many pixels in
`OutlineColor` through the stencil buffer, and `XRay` tints the parts of
a mesh hidden behind other geometry, so a selected unit shows through a
wall.

```go
gr.DrawBillboard(gfx.Billboard{Texture: g.tree, Position: p, Size: lin.V2(2, 3),
	Offset: lin.V2(0, 0.5), Upright: true, Lit: true, Cutout: true})
gr.DrawText3D(g.font, "Watchtower", top, 0.05, gfx.White, false, gfx.TextOptions{})
gr.DrawDecal(g.splat, lin.Translate(hit.Point).Mul(lin.Scale(lin.V3(2, 1, 2))), gfx.RGB(120, 20, 20))
sel := gfx.Material{BaseColor: gfx.RGB(90, 200, 120), Roughness: 0.5,
	Outline: 3, OutlineColor: gfx.White, XRay: gfx.RGBA(255, 60, 60, 160)}
```

## Culling and levels of detail

Every mesh draw is tested against the camera's frustum and skipped when
its bounds are outside; culled draws still cast shadows, which is why a
tree behind the camera can still darken the road. Two caveats: culling
uses each mesh's bind-pose bounds, so a wildly animated model may be
culled while a limb is still visible, and meshes whose material has a
vertex-moving shader are never culled, because the engine cannot know
where their vertices went.

Engine culling still costs the work of building the draw, so a game with
chunks, regions or a crowd should test them itself first.
`Graphics.Frustum()` gives the current camera's frustum for the current
aspect, `Camera.Frustum(aspect)` gives one for any camera, and
`gfx.FrustumOf(viewProj)` builds one from a matrix; `ContainsPoint`,
`ContainsSphere(centre, radius)` and `ContainsBox(min, max)` are the
tests.

`NewLOD(meshes, distances)` takes meshes from finest to coarsest and the
camera distances at which each hands over to the next, so three meshes
take two distances. A nil last mesh draws nothing beyond the last
distance, which is how scenery vanishes instead of shimmering.
`DrawLOD(lod, material, model)` and `DrawLODAt(lod, material, transform)`
pick by the camera's distance to the model's origin; `LOD.Pick` chooses
yourself. Walk `LOD.Levels` in `Shutdown` to destroy the meshes.

```go
// A fine rock near, a faceted one far, nothing beyond seventy units.
fine, _ := ctx.Gfx.NewMesh(gfx.SphereMesh(16, 32))
coarse, _ := ctx.Gfx.NewMesh(gfx.FlatShaded(gfx.SphereMesh(5, 8)))
g.rocks = gfx.NewLOD([]*gfx.Mesh{fine, coarse, nil}, []float32{25, 70})

fr := gr.Frustum()
for _, c := range g.chunks {
	if fr.ContainsBox(c.Min, c.Max) {
		gr.DrawMesh(c.mesh, chunkMat, lin.Translate(c.Origin))
	}
}
```

## Post-processing

`SetPost` replaces the settings the post pass uses on the 3D scene.
`DefaultPost` returns the defaults and `Post` reads back the current
ones, which is how to change one field without restating the rest.

`Exposure` multiplies the scene before tone mapping, the control for a
scene that is too dark or blown out. `Bloom` is the strength of the glow
around bright pixels and `BloomThreshold` the luminance where it starts;
zero bloom skips the passes. `AmbientOcclusion` is screen-space occlusion
darkening creases and contact points, 0 to 1 with a default of 0.6, over
`OcclusionRadius` world units, and `ShowOcclusion` displays the occlusion
buffer instead of the scene while you tune it. `Vignette`, `Saturation`
and `Contrast` are the grade, and `NoAntiAlias` skips the FXAA pass.

`PostSettings.LUT` grades the finished colours through a lookup table.
`NeutralLUT(n)` returns the identity strip of n slices (16 or 32 are
usual); paste it into a corner of a screenshot, grade that in an image
editor, crop the strip back out and load it with `NewLUT`, and every
frame gets the same grade, blended in by `LUTStrength`.

```go
p := gfx.DefaultPost()
p.Exposure, p.Vignette = 1.2, 0.25
p.Bloom, p.BloomThreshold = 0.3, 1.1
p.AmbientOcclusion, p.OcclusionRadius = 0.7, 0.8
p.LUT, p.LUTStrength = g.coldGrade, 0.8
gr.SetPost(p)
```

Post applies to the 3D scene, not to the 2D drawn over it: a HUD is not
bloomed, tone-mapped or graded. That is what you want for readable text,
and what to remember when a sprite over the scene looks brighter than the
scene does. The `lighting` example puts all of these on sliders.

## Render textures

`NewRenderTexture(w, h)` makes an offscreen surface;
`NewRenderTextureOptions` adds `Nearest` for a low-resolution scene that
should stay sharp when scaled up and `Repeat` for one that tiles.
`DrawTo(rt, clear, draw)` runs the closure with that surface as the
output: every `Draw*`, `SetCamera` and `SetLight` call inside it lands on
the texture, with its own camera and its own lighting. It renders before
the main frame, so the result can be drawn in the same frame.
`RenderTexture.Texture()` is the texture to draw with, `SetView` sets its
2D coordinate space, and `Read` copies the pixels back.

```go
// A minimap: the same world from straight above, drawn as a sprite.
gr.DrawTo(g.minimap, gfx.RGB(5, 5, 12), func() {
	gr.SetCamera(gfx.Camera{Position: lin.V3(0, 400, 0.01), Target: lin.V3(0, 0, 0), Ortho: 250})
	gr.SetLight(light)
	g.drawWorld(gr)
})
// ... the main scene ...
gr.DrawTexture(g.minimap.Texture(), ctx.Width-236, 16)
```

The same call is a portal (a camera at the far end of the pair, the
texture as the portal surface's albedo), a mirror, a character portrait,
and the way to put a 3D hero on a 2D field: render the character with a
transparent clear, then draw that texture as a sprite on its layer.

## Debug drawing

`DrawLine3D(a, b, c)` draws a line in the world, `DrawWireBox(min, max,
c)` outlines an axis-aligned box, `DrawWireCube(m, c)` outlines the unit
cube under a matrix (the shape of a `phys.Box3` collider),
`DrawWireSphere(centre, radius, c)` a sphere, `DrawWireFrustum(cam,
aspect, c)` another camera's view volume, and `DrawAxes(m, size)` a
transform's three axes in red, green and blue. All of them ignore depth,
so they show through geometry. `DebugText(x, y, text)` and `Debugf` print
in the engine's own font with no font to load, and `DebugText3D(p, text)`
puts that text at a world point. `gfx.FrustumCorners(viewProj)` returns a
view volume's eight world-space corners.

```go
gr.DrawWireBox(body.Min, body.Max, gfx.RGB(0, 255, 0))
gr.DrawLine3D(muzzle, muzzle.Add(dir.Mul(50)), gfx.RGB(255, 80, 0))
gr.DrawAxes(t.Matrix(), 1)
scout := gfx.Camera{Position: lin.V3(30, 10, 25), Target: lin.V3(10, 0, 5), Far: 30}
gr.DrawWireFrustum(scout, 16.0/9, gfx.RGB(255, 230, 50))
gr.DebugText3D(scout.Position, "scout")
```

## Performance

`ctx.Stats` and `Graphics.Stats()` report the last frame as a
`FrameStats`: `Draws3D` is mesh draw calls after instancing across all
passes, `Instances` is mesh instances in the main pass, `Culled` is the
draws skipped as out of view, and `Draws2D` and `Vertices2D` cover the
sprite stream. The F3 overlay shows them and `Config.DrawBudget` warns
when a frame goes over a number you set.

```go
s := gr.Stats()
gr.Debugf(10, 10, "draws %d  instances %d  culled %d", s.Draws3D, s.Instances, s.Culled)
```

Draw calls cost more than triangles: a high `Draws3D` next to a low
`Instances` means batching is breaking, and merging static geometry with
`AppendMesh` or sharing a material across a crowd collapses the calls.
After that, in order: shadows, where halving `ShadowDistance` doubles the
effective resolution for free and every shadowed spot light is another
pass; lights, which are per-fragment work over the meshes they reach;
transmission, which copies the scene before drawing transmissive meshes;
post, where ambient occlusion and bloom are full-screen passes a zero
turns off; and render textures, which are whole extra frames.

Transparency sorts by distance to the camera per draw, not per triangle,
so two blended meshes that interpenetrate pick an order and keep it, and
a large one sorted by its origin can land on the wrong side of a small
one. Prefer `AlphaCutoff` where hard edges are acceptable, keep blended
geometry convex and small, and use `NoDepthWrite` for additive effects
where order does not matter.

Meshes, models, textures, environments, render textures, shaders and
fonts all hold GPU memory and all have `Destroy`. Call it from `Init`,
`Update`, `Draw` or `Shutdown` on the same goroutine, never from another.
