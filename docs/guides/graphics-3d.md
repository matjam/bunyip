---
title: 3D graphics
group: Graphics
order: 2
summary: cameras, meshes and models, materials, lights and shadows, sky and fog, culling and LOD, post-processing and render textures for a 3D game
---

The [gfx](../pkg/gfx.html) package draws 2D and 3D through one context.
This guide covers the 3D half. It works through cameras, geometry,
materials, lighting, sky, then culling, grading and profiling. The
examples are the fastest reference: `viewer` orbits a scene or a glTF
file, `lighting` puts every post-processing setting on a slider,
`materials` shows one sphere per feature, `terrain` is an outdoor scene
with billboards, levels of detail and fog, and `space` and `solar` are
vacuum scenes.

## The frame

There is no scene object and no begin call. Everything queued during
`Draw` is submitted when `Draw` returns, in a fixed order: render
textures first, then the shadow atlas, then the scene into a high dynamic
range image (sky, opaque meshes, decals, blended and transmissive meshes
back to front, debug lines), then the post pass, then 2D over the
tone-mapped result in layer and call order.

So 2D always draws over 3D. `ctx.Clear` is the colour behind everything,
though when `Light.Background` is set the sky or environment map is drawn
instead and the clear colour never shows. To draw a HUD or a name plate,
make the 2D calls after the mesh calls, and use `Project` to turn a world
point into the view coordinates 2D uses. To put a 3D character on a 2D
field, draw the 3D into a `RenderTexture` and draw that texture as a
sprite between the background and foreground layers.

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
height in world units makes the camera orthographic, so distance no
longer shrinks things. That is what an isometric strategy view uses.

```go
// Third person, then the same focus seen isometrically.
gr.SetCamera(gfx.Camera{Position: player.Add(lin.V3(0, 3, 8)),
	Target: player.Add(lin.V3(0, 1.6, 0)), FovY: lin.Radians(55), Far: 500})
gr.SetCamera(gfx.Camera{Position: focus.Add(lin.V3(30, 30, 30)), Target: focus, Ortho: 20})
```

`OrbitCamera(target, yaw, pitch, distance)` builds an inspector or
strategy camera from three numbers you can drive with the mouse.
`bunyip.FlyCamera` is a free-flying camera for looking around a scene
while it is being built. W, A, S and D move, Q and E go down and up,
Shift goes faster, and the view turns while the right mouse button is
held, or with every movement when `AlwaysLook` is set and the cursor is
captured. `LookAt` points it at a position, `Forward` gives its
direction.

```go
g.fly = &bunyip.FlyCamera{Position: lin.V3(0, 5, 20), Speed: 25} // in Init
func (g *game) Update(ctx *bunyip.Context) error { g.fly.Update(ctx); return nil }
func (g *game) Draw(ctx *bunyip.Context) error   { ctx.Gfx.SetCamera(g.fly.Camera()); return nil }
```

`Project(p)` maps a world point into the 2D view and returns `ok` false
when the point is behind the camera. `ScreenRay(x, y)` turns a point in
the view (the units the mouse reports) into a world ray, and
`Mesh.Intersect` or `Model.Intersect` report where it hits. Both are on
`Graphics` for use while drawing and on `Camera` itself, taking the view
size, for picking from `Update`: `g.cam.ScreenRay(x, y, ctx.Width,
ctx.Height)`. For a scene backed by physics, `phys.Raycast3` casts
against colliders instead, which is cheaper and gives you the entity.

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
rows, cell)`. Use `HeightfieldMesh` for terrain. It takes a grid of
heights, its size in samples, and the world units between samples.

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
vertices written by hand. `FlatShaded` splits shared vertices so every
triangle keeps its own normal, for the faceted look and for coarse
levels of detail.

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
the GPU, for a voxel chunk after a block is dug, a terrain edit, or a
mesh that grows. Draws already queued this frame keep the old geometry
until the frame ends, so an update is safe at any point in `Update` or
`Draw`. Use it instead of destroying and recreating a mesh. `Mesh.Min`
and `Mesh.Max` are the bounds in mesh space, `Vertices` and `Indices`
read the geometry back, and `Destroy` frees it.

Draw with `DrawMesh(mesh, material, model)` where `model` is a
`lin.Mat4`, or `DrawMeshAt(mesh, material, transform)` when a
`gfx.Transform` is what you have. Draws sharing a mesh and a material are
collected into one instanced call, so a thousand asteroids or a forest of
identical trunks cost one draw. This happens automatically, but only
while the materials stay identical, so prefer vertex colours or one
atlas over a material per object.

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
`Model.Max` bound the whole model. Use them to set a camera's distance
when you do not know the file in advance. `Model.Parts` is the placed
primitives, each a `ModelPart` with a `Mesh`, a `Material` and a `World`
matrix, for drawing one part with its own material. `NodeCount`,
`NodeName`, `NodeIndex` and `NodeParent` walk the node hierarchy by name,
so you can find a node such as a gun muzzle and spawn an effect there;
`Model.NodeMatrix` and `NodePosition` give a node's rest-pose place in
model space for a model that is not animated, and `AnimPlayer.NodeMatrix`
its current place for one that is. `Model.Destroy` frees the meshes and
textures together.

For skinning, `Model.Clips` lists the animation clips,
`Model.NewAnimPlayer` makes an `AnimPlayer` holding one instance's pose,
`Play` or `CrossFade` chooses a clip, `Advance(dt)` runs it, and
`DrawModelAnimated(model, transform, player)` draws the result. Layers,
masks, events, root motion, morph targets and node overrides for
inverse kinematics are all on the player; the
[animation guide](animation.html) covers them.

## Transforms

The [lin](../pkg/lin.html) package holds the maths types: `Vec2`,
`Vec3`, `Vec4`, `Mat4` and `Quat` in float32, column-major,
right-handed, +Y up. Values pass by value and every operation returns a
new one. `lin.Translate`, `lin.Scale`, `lin.Rotate(angle, axis)` and
`lin.TRS(t, r, s)` build matrices and `Mul` composes them left to right,
so the last applied is written last. Quaternions avoid the gimbal
problems of stacked Euler angles. Build them with `lin.AxisAngle`,
`lin.FromEuler(yaw, pitch, roll)`, `lin.QuatLookAt` and
`lin.QuatIdentity`. `lin.Radians` converts degrees.

`gfx.Transform` is a simpler form. It holds a `Position`, a `Rotation`
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
material. Gold is `{BaseColor: gfx.RGB(240, 200, 120), Metallic: 1,
Roughness: 0.15}` and a glowing lamp is `{BaseColor: gfx.RGB(255, 180,
60), Emissive: 3}`.

Each map keeps the filtering and edge handling its `TextureOptions` gave
it, whatever else the material binds: the engine's four samplers (linear
or nearest, repeating or clamped) are shared by every material and the
map only says which one to read it through. So a nearest-filtered sprite
sheet as albedo and a linear, repeating detail map on the same mesh both
sample the way they were made.

Transparency has three modes, and they do different things.
`AlphaCutoff` discards fragments below a threshold in both the lit and
the shadow pass, giving hard edges and real shadows, which is what
leaves, fences and grass need. `Blend` draws the material after the
opaque scene, sorted back to front, for smoke and water. `Transmission`
is refractive glass, below. Alongside them, `DoubleSided` turns off
back-face culling and lights back faces with a flipped normal, which a
single-quad leaf needs; `Unlit` shows the base colour and emissive as
they are, for holograms and map markers; `NoDepthTest` draws over
everything already drawn and `NoDepthWrite` leaves the depth buffer
alone, for ghosts and additive effects; and `UVTransform`, a
`lin.Affine` on the texture coordinates, scrolls a conveyor or tiles a
floor in one field.

The layered features come from the matching glTF extensions and cost
nothing left at zero. `Clearcoat` with `ClearcoatRoughness` adds a
varnish lobe for car paint and wet surfaces, `Sheen` with
`SheenRoughness` adds soft light at grazing angles for velvet and cloth,
and `Subsurface`, shaped by a `ThicknessTexture`, lets light through thin
parts for leaves, wax and skin. `Transmission` makes glass and ice. The
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
resolution near the camera, so `ShadowDistance` is the setting to tune.
Keep it as small as the game allows. Forty over a character scene is
crisp; two hundred over a whole valley will alias. Cutout materials cast
cutout shadows, so a leaf texture throws a leaf-shaped shadow. A caster
far above the cascades, a bridge over a street or a cloud over a field,
still casts into them: the shadow pipelines clamp depth rather than clip
at the cascade's near plane.

Each caster is recorded only into the cascades and spot maps its bounds
reach, so a scene spread over a large map pays for the maps a mesh can
land in rather than for all seven. `FrameStats.ShadowDraws` counts the
instances that went into the shadow atlas this frame.

`AddPointLight(pos, color, range)` shines in every direction from a
point, fading to nothing at `range`, for torches, muzzle flashes and
glowing ore. `AddSpotLight(pos, dir, color, range, inner, outer)` shines
in a cone, full inside the inner angle and fading to nothing at the
outer. `AddSpot` takes a `SpotLight` value instead, and with `Shadows`
set that light casts shadows from its own depth map.

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

Plan around the limits. A frame keeps its first `MaxLights` (32) point
and spot lights and gives shadow maps to the first
`MaxSpotShadows` (4) that ask; the rest shine without. Sort by distance
to the camera and add the nearest first; `FrameStats.LightsDropped`
counts the lights a frame refused, so a scene can tell when it went
over. Point lights never cast shadows.
All the cascades and spot maps share one depth atlas, so shadows cost one
texture binding however many lights cast them.

For image-based lighting, `NewEnvironment` turns an equirectangular
panorama into a light probe and `NewEnvironmentHDR` does the same for a
Radiance `.hdr` panorama read with `DecodeHDR`, keeping its range.
`EnvironmentOptions.Intensity` scales it and `Size` sets the cube map's
side in texels (default 128). Set it as `Light.Environment` and it
replaces the ambient and the sky. Metals reflect it, rough surfaces take
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

`Light.Sky` is a procedural environment. It takes an `Up` axis,
`Zenith`, `Horizon` and `Ground` colours, and how much air there is. It
needs no image, costs nothing to change every frame, and lights the
scene the way an environment map does. With `Light.Background` the sun's
disc, sized by `SunSize` and coloured by `Sun`, its haze and the stars
are drawn behind the scene.

`Vacuum` sets how thin the air is, which is what a space game needs. At
0 the air is full. Raising it towards 1 fades the sky to black while
`Stars` come out, and the ground half stays, so a ship can climb from a
runway to orbit with no seam. Point `Up` away from a nearby planet and
set `Ground` to that planet's colour to light the ship's night side with
planet-shine.

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
the mesh path, so many with one texture become one instanced draw, and
five hundred trees are one call. `DrawText3D(font, text, pos, scale,
color, onTop, opts)` draws a line of text the same way, centred on
`pos`, with `scale` in world units per view unit of the font.

`DrawDecal(tex, box, tint)` projects a texture onto whatever geometry
lies inside a box, for bullet holes, blood, footprints and road
markings. The box matrix maps the unit cube to the world, the texture
projects along the box's y axis with x and z spanning the image, and it
fades on surfaces facing away. Two material fields mark a mesh rather
than the world. `Outline` draws a silhouette line of that many pixels in
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
tree behind the camera can still darken the road. A static mesh is
bounded by the box its vertices fill. A skinned mesh keeps a box per
joint over the vertices weighted to it, and each frame the pose's joint
matrices move those boxes and the union bounds the draw, so a limb that
swings clear of the bind pose is still drawn.

Two cases need the game's help. A mesh whose drawn shape leaves its
geometry takes `Mesh.SetBounds(min, max)` to say the box it stays
inside, in mesh space; `Mesh.Bounds()` reads the bounds back, and
`Update` leaves bounds given by hand alone. A material shader with a
vertex program can put a vertex anywhere, so draws using it are never
culled until `Shader.VertexBounds` says how far the program moves one,
as a multiple of the mesh's bounding radius; culling then grows the
radius by 1 + `VertexBounds`.

```go
// A flag whose shader ripples it by a quarter of its own size.
g.flagShader.VertexBounds = 0.25
// A billboard grass mesh the shader bends and scatters over its cell.
grass.SetBounds(lin.V3(-2, 0, -2), lin.V3(2, 3, 2))
```

Engine culling still costs the work of building the draw, so a game with
chunks, regions or a crowd should test them itself first.
`Graphics.Frustum()` gives the current camera's frustum for the current
aspect, `Camera.Frustum(aspect)` gives one for any camera, and
`gfx.FrustumOf(viewProj)` builds one from a matrix; `ContainsPoint`,
`ContainsSphere(centre, radius)` and `ContainsBox(min, max)` are the
tests.

The frustum only knows what is outside the view. To skip what is inside
it but hidden, mark the geometry that blocks the view with
`AddOccluder3D(mesh, model)` or `AddOccluder3DAt(mesh, transform)`: a
wall, a hill, a building's shell. Each frame the engine rasterises the
occluders into a small depth buffer on the CPU and culls every draw
whose bounding sphere lies entirely behind it.
`FrameStats.Occluded` counts them, and they still cast shadows like any
culled draw. Adding an occluder does not draw it, so draw the mesh too,
or add a coarse box in place of geometry drawn in detail.

Occluders must be opaque and closed enough that nothing shows through
their triangles, so a fence whose gaps are a cutout texture is a bad
one. Keep them few and low-poly, since every triangle is rasterised on
the CPU and a mesh with more than `MaxOccluderTriangles` is ignored.
`SetOcclusionSize(width, height)` sizes the buffer, 256 by 144 by
default: a gap narrower than one of its pixels counts as covered, so
raise it when a real gap is being missed and lower it when the test
costs more than it saves. Fifty box occluders and a thousand draws cost
around 170 microseconds a frame at the default size.

```go
// The castle wall hides most of the town behind it.
gr.AddOccluder3DAt(g.wallBox, g.wallAt)   // a coarse box, not the wall's own mesh
gr.DrawMeshAt(g.wall, stone, g.wallAt)
```

Both tests are per draw, so a level of ten thousand rocks, crates and
lamp posts still costs ten thousand of them. `NewStaticBatch(items)`
takes a slice of `BatchItem` (a mesh, a material and a model matrix, as
`DrawMesh` takes) and builds a bounding volume hierarchy over them once;
`DrawBatch(batch)` then tests the hierarchy rather than the items and
queues only what survives. A subtree behind the camera or behind an
occluder is rejected at one node, so the ten thousand cost a few dozen
box tests, and `FrameStats.CullTests` counts them. The items that come
through are ordinary draws, instanced, sorted, lit and shadowed like any
others. Ten thousand cubes along a strip most of which is behind the
camera fall from 220 microseconds of culling a frame to under two.

A batch is for geometry that never moves: the hierarchy is built from
the models given and is not rebuilt, so anything that moves belongs in
`DrawMesh`. It does not own its meshes or textures, which are destroyed
as usual, and `Len` and `Bounds` report what it holds.

```go
var items []gfx.BatchItem
for _, p := range level.Props {
	items = append(items, gfx.BatchItem{Mesh: p.Mesh, Material: p.Mat, Model: p.At.Matrix()})
}
g.props = ctx.Gfx.NewStaticBatch(items) // once, at load

gr.DrawBatch(g.props) // every frame
```

`NewLOD(meshes, distances)` takes meshes from finest to coarsest and the
camera distances at which each hands over to the next, so three meshes
take two distances. A nil last mesh draws nothing beyond the last
distance, so distant scenery disappears instead of shimmering.
`DrawLOD(lod, material, model)` and `DrawLODAt(lod, material, transform)`
pick by the camera's distance to the model's origin; `LOD.Pick` lets you
choose the level yourself. Walk `LOD.Levels` in `Shutdown` to destroy
the meshes.

The coarsest level of all is an impostor: the model baked into pictures
of itself. `BakeImpostor(model, opts)` renders the model from a ring of
directions around it into one atlas texture, and `DrawImpostor(impostor,
pos, yaw, tint)` draws the view nearest the camera as a cutout
billboard, so a distant tree costs one quad and no vertex work. Set
`Impostor.Distance` and call `DrawModelImpostor(model, impostor,
transform)` to draw the model up close and the impostor beyond.
Impostors of one model share an atlas, so a forest of them is one
instanced draw.

`ImpostorOptions` chooses `Views` (8 by default, at most
`MaxImpostorViews`), `Resolution` in pixels per view (128), the `Pitch`
each view looks down from (15 degrees, so match it to the camera's usual
elevation) and the `Light` to bake under. The bake fixes its lighting
into the atlas the same way for every view, so an impostor does not turn
its shading as the sun moves; keep them far enough away that this does
not read. It runs a frame of its own and reads the views back, so call
it from `Init` or `Update`, never from `Draw`.

```go
// In Init: pines beyond forty units become one quad each.
g.pineFar, err = ctx.Gfx.BakeImpostor(g.pine, gfx.ImpostorOptions{Views: 12, Resolution: 96})
g.pineFar.Distance = 40

// In Draw:
for _, t := range g.forest {
	gr.DrawModelImpostor(g.pine, g.pineFar, t)
}
```

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
ones, so you can change one field without restating the rest.

`Exposure` multiplies the scene before tone mapping. Use it when a scene
is too dark or blown out. `Bloom` is the strength of the glow around
bright pixels and `BloomThreshold` the luminance where it starts; zero
bloom skips the passes. `AmbientOcclusion` is screen-space occlusion
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

Post applies to the 3D scene, not to the 2D drawn over it. A HUD is not
bloomed, tone-mapped or graded. That keeps text readable, and it
explains why a sprite over the scene can look brighter than the scene
does. The `lighting` example puts all of these on sliders.

## Render textures

`NewRenderTexture(w, h)` makes an offscreen surface;
`NewRenderTextureOptions` adds `Nearest` for a low-resolution scene that
should stay sharp when scaled up and `Repeat` for one that tiles.
`DrawTo(rt, clear, draw)` runs the closure with that surface as the
output. Every `Draw*`, `SetCamera` and `SetLight` call inside it lands on
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

The same call makes a portal (a camera at the far end of the pair, the
texture as the portal surface's albedo), a mirror, or a character
portrait. To put a 3D character on a 2D field, render the character with
a transparent clear, then draw that texture as a sprite on its layer.

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
passes, `Instances` is mesh instances in the main pass, `ShadowDraws` is
the instances recorded into the shadow maps, `Culled` is the draws
skipped as out of view, `Occluded` how many of those an occluder hid
rather than the frustum and `CullTests` how many bounding volumes were
tested to decide, `Lights` and `LightsDropped` are the point and
spot lights the frame kept and threw away, `Draws2D` and `Vertices2D`
cover the sprite stream, and `Waits` counts the times the frame stopped
for the GPU to go idle, which a running game keeps at zero. The F3
overlay shows them and `Config.DrawBudget` warns when a frame goes over
a number you set.

```go
s := gr.Stats()
gr.Debugf(10, 10, "draws %d  instances %d  shadow %d  culled %d",
	s.Draws3D, s.Instances, s.ShadowDraws, s.Culled)
```

`Graphics.Resources()` lists every texture, mesh, model, font, render
texture and environment the context has made and not destroyed, with
sizes and an estimate of the GPU memory each holds: what to print when a
scene is using more memory than it should, or to check that a level
teardown freed what it loaded. The [debug console](console.html) shows
the same list with a running total.

Draw calls cost more than triangles. A high `Draws3D` next to a low
`Instances` means batching is breaking. Merge static geometry with
`AppendMesh` or share a material across a crowd to collapse the calls.
After that, look at the costs in order: shadows, where halving
`ShadowDistance` doubles the effective resolution at no cost and every
shadowed spot light is another pass; lights, which are per-fragment work
over the meshes they reach; transmission, which copies the scene before
drawing transmissive meshes; post, where ambient occlusion and bloom are
full-screen passes a zero turns off; and render textures, which are
whole extra frames.

Transparency sorts by distance to the camera per draw, not per triangle,
so two blended meshes that interpenetrate pick an order and keep it, and
a large one sorted by its origin can land on the wrong side of a small
one. Prefer `AlphaCutoff` where hard edges are acceptable, keep blended
geometry convex and small, and use `NoDepthWrite` for additive effects
where order does not matter.

Meshes, models, textures, environments, render textures, shaders and
fonts all hold GPU memory and all have `Destroy`. Call it from `Init`,
`Update`, `Draw` or `Shutdown` on the same goroutine, never from another.
Destroying inside a frame costs no wait: the object goes on that frame
slot's retire list and is freed a couple of frames later, once the GPU
has finished with it, so what was already queued still draws. Uploads
inside a frame are the same shape: `NewMesh`, `Mesh.Update`,
`NewTexture`, `Texture.Write` and `NewEnvironment` copy through a
staging arena into the frame's own command buffer, and what a frame
uploads is what that frame draws.
