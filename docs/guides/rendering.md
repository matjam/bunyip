---
title: Rendering
order: 8
summary: sprites, cameras and text in 2D; meshes, materials, lights and post-processing in 3D
---

The [gfx](../pkg/gfx.html) package is one drawing context for both 2D
and 3D. Calls made in `Draw` are queued and submitted at the end of the
frame: 3D meshes render first with lighting and shadows, then sprites and
text composite on top.

## 2D

**Textures and sprites.** `NewTexture` uploads an `image.Image`; the
options choose nearest or linear filtering, mipmaps and repeat.
`NewBlankTexture` makes an empty one and `Texture.Write` replaces a
region of pixels later, for painting, video and procedural maps;
`Texture.Read` copies pixels back. `Draw` places a `Sprite`: position,
size, a UV window, a tint, rotation and origin. `DrawTexture` and
`FillRect` cover the common cases. A `Region` names a rectangle of a
texture, from `NewRegion` or `Sheet.Region`, and `DrawRegion` draws it.
`DrawNineSlice` stretches a `NineSlice` over a rectangle with its corners
kept, or repeats its edges and centre with `Tile` set. Sprites flip with
`FlipX` and `FlipY` and pick nearest or linear sampling per draw with
`Filter`. `DrawIndexed` draws shared-vertex meshes. `DebugText` prints
in the engine's own font without loading one. Writes to a texture inside
a frame stream without waiting for the GPU, so video and painting can
update every frame.

**Colour and light.** `ColorMatrixed` (or `SetColorMatrix`) recolours
everything drawn inside it through a `ColorMatrix`: `Saturation`,
`HueRotate`, `Brightness`, `Contrast`, `Invert`, `Sepia` and `Tint`
compose with `Mul`, so a hit flash or a night tint is one line.
`SetLights2D` places up to eight point lights above the sprite plane and
`DrawLit` draws a sprite lit by them through a tangent-space normal map,
for torchlight across a dungeon wall.

**Sheets, animation and tilemaps.** A `Sheet` divides a texture into
frames; `DrawFrame` draws one. `Animation` and `AnimState` step through
frames at a rate. A `Tilemap` holds frame indices, with `TileFlipped`
bits for mirrored and turned tiles and `Animate` for tiles that cycle,
and draws only the tiles inside the camera's view. The `tiled` package
loads maps and tilesets saved by the Tiled editor into tilemaps and
object lists, and `ParseAtlas` reads TexturePacker and Aseprite atlases
into named regions and animation tags.

**Cameras and layers.** `SetCamera2D` makes later sprites world-space:
the camera has a position, zoom and rotation, and `ViewToWorld` maps the
mouse back into the world. `ScreenSpace` returns to view coordinates for
the HUD. `SetLayer` orders sprites across calls; within a layer,
submission order wins.

**Text.** `NewFont` parses an OpenType font and rasterises glyphs from
its outlines at one size into an atlas; `DrawText` draws a line and
`DrawTextBlock` wraps and aligns paragraphs. Text is shaped with
HarfBuzz, so kerning, ligatures, mark placement and Arabic joining are
right, right-to-left runs are reordered, and lines break by the Unicode
rules. `FontOptions` adds fallback fonts for scripts the main one lacks,
OpenType features (`"smcp"`, `"-liga"`) and variable-font axes;
`TextOptions` sets the size, a rotation angle, baseline placement,
letter spacing, justification (`AlignJustify`), hyphenation through a
`Hyphenator` (`EnglishHyphenator` is built in from the TeX patterns),
the direction (automatic, left to right, right to left, or vertical) and
language; `Font.Measure` takes the same options. `Font.Shape` returns
positioned glyphs for custom drawing or hit-testing. `NewSDFFont` builds
a signed-distance atlas that stays sharp at any size and angle, where a
bitmap font resamples. Colour emoji from a bitmap emoji font given as a
fallback draw in their own colours. `ParseRich` reads a small markup
(`[b]`, `[i]`, `[u]`, `[#ff8800]`, `[link=name]`) into a `RichText`
that `DrawRichText` lays out across regular, bold and italic faces with
per-run colours, underlines and links, returning each link's rectangle
for clicks.

**Paths.** A `Path` collects lines, Bézier curves and arcs, with `Rect`,
`RoundRect`, `Circle`, `Ellipse` and `Polygon` helpers. `FillPath` fills
it under the non-zero or even-odd rule, optionally with a texture;
`StrokePath` outlines it with a width, caps and joins. Both are
anti-aliased and go through the same stream as sprites, so they sort by
layer and clip like everything else. A `Gradient`, linear or radial,
colours a fill or stroke by position (`FillGradient` for a rectangle),
`StrokeOptions.Dash` makes dashed and dotted lines, and `DrawTextOnPath`
runs a line of text along a path. `FillCircle`, `StrokeRect` and
`StrokeLine` cover the quick cases, and `DrawTriangles` takes raw
geometry.

**Particles.** The `particle` package simulates emitters on the CPU and
draws them through the sprite stream: a rate or bursts, a shape to emit
from, speed, spread, gravity, damping, size and colour curves over
lifetime, a texture, region or sheet frames, a blend mode and a layer.
`Fire`, `Smoke`, `Sparks`, `Rain` and `Confetti` are ready-made
emitters; the `particles` example shows them with a tuning panel.

**Blending and transforms.** `Blended` (or `SetBlend`) picks a blend mode
for a stretch of drawing: additive glows, multiplied shadows, screen,
lighten, darken, replace or erase. `Transformed` (or `PushTransform`)
maps everything drawn inside it through a `lin.Affine`: translate,
rotate, scale and shear compose with `Mul`.

**Shaders.** Fragment shaders written by the game colour sprites and
shape mesh surfaces; the [Shaders](shaders.html) guide covers them.
`Shader.Reload` swaps in a recompiled program while the game runs, so an
`asset.Watcher` on the compiled files gives shader hot reload.

**Budget.** `ctx.Stats` and `Graphics.Stats` count 2D draw calls and
vertices and 3D draw calls and instances for the last frame; the F3
overlay shows them. A rising 2D draw count means state changes are
breaking batches.

**Clipping.** `Clip` (or `PushClip` and `PopClip`) limits drawing to a
`lin.Rect`, which is how scroll areas work. Every rectangle in the
engine is a `lin.Rect`: clips, the interface's widgets, a camera's
visible area, a texture region.

## 3D

**Meshes and models.** `NewMesh` uploads vertices and indices;
`CubeMesh` and `SphereMesh` generate shapes. `LoadModel` uploads a parsed
glTF document with its materials, textures, skins and animation clips.

**Materials.** `Material` is metallic-roughness PBR: a base colour or
albedo texture, metallic and roughness factors or a texture, a normal
map, emissive strength, and flags for blending and double-sided drawing,
plus clearcoat, sheen, subsurface and glass (`Transmission` with a
`TransmissionTexture` for panes in a frame, thickness and absorption),
outlines and x-ray. glTF models bring all of it in.

**Camera and lights.** `SetCamera` takes a `Camera` (position, target,
field of view, or `Ortho` for an orthographic view where distance does
not shrink things, as isometric strategy games want); `OrbitCamera`
builds one from yaw, pitch and distance.
`SetLight` sets the directional light with sky and ground ambient and
optional cascaded shadow maps; `AddPointLight` and `AddSpotLight` add
local lights for the frame.

**The sky.** `Light.Sky` is a procedural environment built from what
the game already knows: an `Up` axis, `Zenith`, `Horizon` and `Ground`
colours, and how much air there is. It costs nothing to change every
frame. Raise `Vacuum` towards 1 and the sky fades to black while the
`Stars` come out, so a ship can climb from a runway to orbit without a
seam; point `Up` away from a nearby planet and set `Ground` to its
colour and the planet lights the ship's night side. Rough surfaces take
the sky's tint from every direction, metals reflect its gradient, and
with `Light.Background` the sun's disc, its haze and the stars are
drawn behind the scene. Leaving the sky unset gives a uniform `Ambient`.

**Environments.** `NewEnvironment` turns an equirectangular panorama
into image-based lighting and `NewEnvironmentHDR` does the same for a
Radiance `.hdr` panorama read with `DecodeHDR` so bright skies keep
their range. Set it as `Light.Environment`, replacing the sky, and
metals reflect it, rough surfaces take its tint from every direction
(nine spherical harmonics for the diffuse part, a prefiltered cube map
for the specular part), and `Light.Background` draws it as the sky.

**Material features.** Beyond textures and factors, a `Material` has
`AlphaCutoff` for hard-edged cutouts that cut their shadows too,
`OcclusionTexture` for baked ambient occlusion (on the second UV set
with `OcclusionUV2`), `UVTransform` to scroll, tile or rotate its
textures, `Unlit`, `DoubleSided`, `NoDepthTest` and `NoDepthWrite`, and
a `Shader` hook; the [Shaders](shaders.html) guide covers the hook.
Vertices carry a colour and a second UV set, and glTF files bring in
COLOR_0, TEXCOORD_1 and KHR_texture_transform.

**Layered materials.** `Clearcoat` adds a varnish lobe with its own
`ClearcoatRoughness` for car paint and lacquer. `Sheen` adds soft
grazing light in a colour for velvet and cloth. `Subsurface` lets light
through thin parts shaped by a `ThicknessTexture`, for leaves, wax and
skin. `Transmission` makes glass, water and ice: the opaque scene shows
through, refracted by `IOR` across `Thickness` units of material,
blurred by the roughness and absorbed towards `AttenuationColor` over
`AttenuationDistance`. Transmissive meshes draw after the opaque scene
like blended ones. All of these load from the matching glTF extensions.

**Outlines, x-ray and decals.** `Outline` draws a silhouette line of
that many pixels in `OutlineColor` through the stencil buffer, for
selection rings and cartoon edges. `XRay` tints the parts of a mesh
hidden behind other geometry so a unit shows through walls. `DrawDecal`
projects a texture onto whatever lies inside a box, for bullet holes,
blood and road markings; it fades on surfaces facing away from the
box's axis. The `materials` example shows every one of these.

**Instancing and sorting.** Draws sharing a mesh and material are
batched into one instanced call automatically, so hundreds of asteroids
cost one draw. Blended materials are sorted back to front.

**Animation.** `DrawModelAnimated` plays a glTF clip through an
`AnimPlayer`. `NewSkinnedMesh` and `DrawSkinned` take joint matrices you
compute yourself, as the `lighting` example does.

**Post-processing.** `SetPost` controls exposure, bloom, vignette,
saturation, contrast, ambient occlusion and FXAA. `DrawTo` renders into a
`RenderTexture` that later draws like any texture: minimaps, portals,
reflections.

**Picking and labels.** `ScreenRay` turns a screen point into a world
ray, and `Mesh.Intersect` or `Model.Intersect` report where it hits.
`Project` goes the other way, from a world point to the view, which is
where a label or a health bar belongs.

**Debug drawing.** `DrawLine3D`, `DrawWireBox`, `DrawWireCube`,
`DrawWireSphere`, `DrawWireFrustum` and `DrawAxes` draw lines over the
scene that ignore depth, so colliders, paths, rays, bones and cameras can
be seen while a game is being written; `DebugText3D` puts the overlay
font at a world point.

**Fog.** `Light.Fog` fades distant geometry into a colour: linear
between `Start` and `End`, exponential by `Density`, and ground fog that
thins above `Height`. It hides the far plane, gives a scene depth and
costs nothing. The sky is not fogged, so pick a colour near the
horizon's outdoors.

**Point and spot lights.** `AddPointLight` shines in every direction
from a point; `AddSpotLight` in a cone along a direction with a soft
edge between its inner and outer angles. A frame keeps the first
`MaxLights` (32); when a scene has more, add the nearest to the camera
first. `AddSpot` takes a `SpotLight` value, and with `Shadows` set the
light casts shadows from its own depth map; the first `MaxSpotShadows`
(4) such lights a frame get one. The cascades and the spot maps share
one atlas, so shadows cost one texture binding however many lights.

**Colour grading.** `PostSettings.LUT` runs the finished frame through
a lookup table: `NeutralLUT` gives the identity strip, a screenshot
with it pasted in is graded in an image editor, the strip is cropped
back out and loaded with `NewLUT`, and every frame gets the grade.
`LUTStrength` blends it in.

**Debugging the camera.** `bunyip.FlyCamera` is a free-flying camera
for looking round a scene while it is being built: keys move, the
right mouse button turns, `Camera` hands the result to `SetCamera`.

**Culling.** Every draw is tested against the camera's frustum and
skipped when nothing of its bounds is in view; `Stats().Culled` counts
them. Culled draws still cast shadows. A game with many chunks or
regions should test them itself with `Frustum` before building their
draws at all: `ContainsBox` and `ContainsSphere` are the tests, and
`Camera.Frustum` or `Graphics.Frustum` gives the volume.

**Level of detail.** `NewLOD` takes meshes from finest to coarsest and
the distances at which each hands over; `DrawLOD` picks by the camera's
distance to the model, and a nil last mesh draws nothing far away.

**Billboards and labels.** `DrawBillboard` puts a textured quad in the
scene that turns to face the camera: health bars, name tags, trees in
the distance, smoke. `Upright` keeps it vertical, `Cutout` gives it
hard edges that cast shadows, `OnTop` draws it over everything.
`DrawText3D` draws a font's text the same way, one instanced draw per
label. Both go through the mesh path, so they are lit and fogged when
asked.

**Dynamic meshes.** `Mesh.Update` replaces a mesh's geometry: a voxel
chunk after a block is dug, a terrain edit, a procedural mesh that
grows. Draws already queued keep the old geometry until the frame ends.

**Shapes.** Besides `CubeMesh` and `SphereMesh` there are `QuadMesh`,
`PlaneMesh`, `CylinderMesh`, `ConeMesh`, `CapsuleMesh` and `TorusMesh`,
and `HeightfieldMesh` builds terrain from a grid of heights.
`ComputeNormals` smooths geometry built by hand, `FlatShaded` gives it
the faceted look, `TransformVertices` and `AppendMesh` place parts and
merge them into one draw.

## Verifying without eyes

The renderer's tests run on a headless surface and read pixels back, and
every example saves a screenshot with `-shot`. When you change a shader or
a material, take a screenshot before and after; the difference is the
review.
