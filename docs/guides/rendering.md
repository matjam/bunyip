---
title: Rendering
order: 8
summary: sprites, text and paths in 2D; meshes, materials, lights, shadows and post-processing in 3D
---

The [gfx](../pkg/gfx.html) package is one drawing context for both 2D
and 3D. Calls made in `Draw` are queued and submitted at the end of the
frame. The 3D scene renders first, with lighting, shadows and
post-processing; sprites, text and paths then draw over it in the order
they were queued.

## 2D

### Textures and sprites

`NewTexture` uploads an `image.Image`; its options choose nearest or
linear filtering, mipmaps and repeating. `NewBlankTexture` makes an
empty texture and `Texture.Write` replaces a region of its pixels
later, for painting, video and procedural maps. Writes made during a
frame are streamed without waiting for the GPU, so a texture can change
every frame. `Texture.Read` copies pixels back.

`Draw` places a `Sprite`: a position, a size, a window into the
texture, a tint, a rotation and an origin, with `FlipX` and `FlipY` to
mirror it and `Filter` to choose the sampling for that draw. `DrawTexture`
and `FillRect` cover the simple cases. A `Region` names a rectangle of a
texture, from `NewRegion` or `Sheet.Region`, and `DrawRegion` draws it.
`DrawNineSlice` stretches a `NineSlice` over a rectangle with its
corners kept, or repeats the edges and centre when `Tile` is set.
`DrawIndexed` and `DrawTriangles` take raw geometry.

### Sheets and tilemaps

A `Sheet` divides a texture into frames and `DrawFrame` draws one.
`Animation` and `AnimState` step through frames at a rate. A `Tilemap`
holds a frame index per cell, with `TileFlipped` bits for mirrored and
turned tiles and `Animate` for tiles that cycle, and draws only the
cells inside the camera's view. The [tiled](../pkg/tiled.html) package
loads maps and tilesets saved by the Tiled editor, in JSON or XML form,
into tilemaps and object lists, and `ParseAtlas` reads TexturePacker
and Aseprite atlases into named regions and animation tags.

### Cameras and layers

`SetCamera2D` makes later sprites world-space: the camera has a
position, zoom and rotation, and `Camera2D.ViewToWorld` maps the
pointer back into the world. `ScreenSpace` returns to view coordinates
for the HUD. `SetLayer` orders drawing across calls; within a layer,
submission order wins.

### Text

`NewFont` parses an OpenType font and rasterises glyphs from its
outlines at one size into an atlas. `DrawText` draws a line,
`DrawTextBlock` wraps and aligns paragraphs, and `Font.Measure` sizes
text without drawing it. Text is shaped with HarfBuzz, so kerning,
ligatures, mark placement and Arabic joining are right, right-to-left
runs are reordered, and lines break by the Unicode rules.

`FontOptions` adds fallback fonts for scripts the main one lacks,
OpenType features such as `"smcp"` or `"-liga"`, and variable-font
axes. `TextOptions` sets the size, a rotation angle, baseline placement,
letter spacing, justification (`AlignJustify`), hyphenation through a
`Hyphenator` (`EnglishHyphenator` is built in from the TeX patterns),
the direction (automatic, left to right, right to left, or vertical)
and the language. `Font.Shape` returns positioned glyphs for custom
drawing or hit-testing. `NewSDFFont` builds a signed-distance atlas that
stays sharp at any size and angle. Colour emoji draw in their own
colours when a bitmap emoji font is given as a fallback.

`ParseRich` reads a small markup (`[b]`, `[i]`, `[u]`, `[#ff8800]`,
`[link=name]`) into a `RichText` that `DrawRichText` lays out across
regular, bold and italic faces with per-run colours, underlines and
links, returning each link's rectangle for clicks.

### Paths

A `Path` collects lines, Bézier curves and arcs, with `Rect`,
`RoundRect`, `Circle`, `Ellipse` and `Polygon` helpers. `FillPath`
fills it under the non-zero or even-odd rule, optionally with a
texture; `StrokePath` outlines it with a width, caps and joins. Both are
anti-aliased and go through the same stream as sprites, so they sort by
layer and clip like everything else. A `Gradient`, linear or radial,
colours a fill or a stroke by position, and `FillGradient` fills a
rectangle with one. `StrokeOptions.Dash` makes dashed and dotted lines,
and `DrawTextOnPath` runs a line of text along a path. `FillCircle`,
`StrokeRect` and `StrokeLine` cover the quick cases.

### Colour matrices and lit sprites

`ColorMatrixed` (or `SetColorMatrix`) recolours everything drawn inside
it through a `ColorMatrix`. `Saturation`, `HueRotate`, `Brightness`,
`Contrast`, `Invert`, `Sepia` and `Tint` compose with `Mul`, so a hit
flash or a night tint is one line. `SetLights2D` places up to eight
point lights above the sprite plane, and `DrawLit` draws a sprite lit
by them through a tangent-space normal map, for torchlight across a
dungeon wall. Lit sprites do not cast shadows on each other.

### Particles

The [particle](../pkg/particle.html) package simulates emitters on the
CPU and draws them through the sprite stream: a rate or bursts, a shape
to emit from, speed, spread, gravity, damping, size and colour curves
over each particle's lifetime, a texture or sheet frames, a blend mode
and a layer. `Fire`, `Smoke`, `Sparks`, `Rain` and `Confetti` are
ready-made emitters, and the `particles` example shows them with a
tuning panel.

### Blending, transforms and clipping

`Blended` (or `SetBlend`) picks a blend mode for a stretch of drawing:
additive glows, multiplied shadows, screen, lighten, darken, replace or
erase. `Transformed` (or `PushTransform`) maps everything drawn inside
it through a `lin.Affine`; translate, rotate, scale and shear compose
with `Mul`. `Clip` (or `PushClip` and `PopClip`) limits drawing to a
`lin.Rect`, which is how scroll areas work. Every rectangle in the
engine is a `lin.Rect`: clips, widgets, a camera's visible area, a
texture region.

### Shaders

Fragment shaders written by the game colour sprites and shape mesh
surfaces; the [shaders guide](shaders.html) covers them.

### Statistics

`ctx.Stats` and `Graphics.Stats` count the last frame's 2D draw calls
and vertices, 3D draw calls and instances, and the draws culled as out
of view. The F3 overlay shows them, and `Config.DrawBudget` turns the
count into a warning. A rising 2D draw count means state changes are
breaking batches.

## 3D

### Meshes and models

`NewMesh` uploads vertices and indices. `CubeMesh`, `SphereMesh`,
`QuadMesh`, `PlaneMesh`, `CylinderMesh`, `ConeMesh`, `CapsuleMesh` and
`TorusMesh` generate shapes, and `HeightfieldMesh` builds terrain from a
grid of heights. `ComputeNormals` smooths geometry built by hand,
`FlatShaded` gives it the faceted look, and `TransformVertices` and
`AppendMesh` place parts and merge them into one draw.

`Mesh.Update` replaces a mesh's geometry: a voxel chunk after a block is
dug, a terrain edit, a procedural mesh that grows. Draws already queued
this frame keep the old geometry until the frame ends, so an update is
safe at any point.

`LoadModel` uploads a parsed glTF document with its materials, textures,
skins, animation clips and morph targets. `DrawModel` draws it;
`DrawModelAnimated` draws it posed by an `AnimPlayer`, which the
[animation guide](animation.html) covers. `NewSkinnedMesh` and
`DrawSkinned` take joint matrices the game computes itself, as the
`lighting` example does.

### Materials

`Material` is metallic-roughness PBR: a base colour or albedo texture,
metallic and roughness factors or a texture, a normal map, an emissive
texture or strength, and an occlusion texture for baked ambient
occlusion (on the second UV set with `OcclusionUV2`). `UVTransform`
scrolls, tiles or rotates the textures. `AlphaCutoff` makes hard-edged
cutouts that cut their shadows too; `Blend` draws the material after
the opaque scene, sorted back to front. `Unlit`, `DoubleSided`,
`NoDepthTest` and `NoDepthWrite` do what their names say. Vertices carry
a colour and a second UV set, which glTF files fill from COLOR_0 and
TEXCOORD_1.

The layered features come from the matching glTF extensions.
`Clearcoat` adds a varnish lobe with its own `ClearcoatRoughness`, for
car paint and lacquer. `Sheen` adds soft light at grazing angles in a
colour, for velvet and cloth. `Subsurface` lets light through thin
parts, shaped by a `ThicknessTexture`, for leaves, wax and skin.
`Transmission` makes glass, water and ice: the opaque scene shows
through, refracted by `IOR` across `Thickness` units of material,
blurred by the roughness and absorbed towards `AttenuationColor` over
`AttenuationDistance`; a `TransmissionTexture` lets one material hold
an opaque frame and glass panes. Transmissive meshes draw after the
opaque scene, like blended ones.

`Outline` draws a silhouette line of that many pixels in `OutlineColor`
through the stencil buffer, for selection rings and cartoon edges.
`XRay` tints the parts of a mesh hidden behind other geometry, so a unit
shows through walls. `DrawDecal` projects a texture onto whatever lies
inside a box, for bullet holes, blood and road markings; it fades on
surfaces that face away from the box's axis. A `Shader` on the material
adjusts the surface before lighting; the [shaders guide](shaders.html)
covers it. The `materials` example shows every one of these features.

### Cameras

`SetCamera` takes a `Camera`: a position, a target, a field of view, or
`Ortho` for an orthographic view in which distance does not shrink
things, as isometric strategy games want. `OrbitCamera` builds one from
yaw, pitch and distance, and `bunyip.FlyCamera` is a free-flying camera
for looking round a scene while it is being built.

### Lights and shadows

`SetLight` sets the directional light with its colour and an ambient
term, and `Shadows` turns on cascaded shadow maps for it, reaching
`ShadowDistance` from the camera. `AddPointLight` shines in every
direction from a point, fading to nothing at its range. `AddSpotLight`
shines in a cone along a direction with a soft edge between its inner
and outer angles; `AddSpot` takes a `SpotLight` value instead, and with
`Shadows` set the light casts shadows from its own depth map. A frame
keeps its first `MaxLights` (32) point and spot lights and gives shadow
maps to the first `MaxSpotShadows` (4) that ask, so when a scene has
more, add the nearest to the camera first. The cascades and the spot
maps share one depth atlas, so shadows cost one texture binding however
many lights cast them. Point lights do not cast shadows.

### Sky and environment

`Light.Sky` is a procedural environment built from what the game
already knows: an `Up` axis, `Zenith`, `Horizon` and `Ground` colours,
and how much air there is. It costs nothing to change every frame.
Raising `Vacuum` towards 1 fades the sky to black while the `Stars` come
out, so a ship can climb from a runway to orbit without a seam;
pointing `Up` away from a nearby planet and setting `Ground` to its
colour lights the ship's night side with planet-shine. Rough surfaces
take the sky's tint from every direction and metals reflect its
gradient. With `Light.Background` the sun's disc, its haze and the
stars are drawn behind the scene. When no sky is set, `Ambient` lights
everything evenly.

`NewEnvironment` turns an equirectangular panorama into image-based
lighting, and `NewEnvironmentHDR` does the same for a Radiance `.hdr`
panorama read with `DecodeHDR`, so a bright sky keeps its range. Set it
as `Light.Environment` and it replaces the sky: metals reflect it,
rough surfaces take its tint from every direction (nine spherical
harmonics for the diffuse part, a prefiltered cube map for the
specular part), and `Light.Background` draws it behind the scene.

### Fog

`Light.Fog` fades distant geometry into a colour: linearly between
`Start` and `End`, exponentially by `Density`, or as ground fog that
thins above `Height`. It hides the far plane and gives a scene depth
for no cost. The sky is not fogged, so outdoors pick a fog colour close
to the horizon's.

### Instancing, culling and levels of detail

Draws that share a mesh and a material are batched into one instanced
call, so hundreds of asteroids cost one draw. Every draw is tested
against the camera's frustum and skipped when none of its bounds is in
view; culled draws still cast shadows. A game with many chunks or
regions should test them itself before building their draws: `Frustum`
from `Camera.Frustum` or `Graphics.Frustum` has `ContainsBox` and
`ContainsSphere`.

`NewLOD` takes meshes from finest to coarsest and the distances at which
each hands over to the next; `DrawLOD` picks by the camera's distance to
the model, and a nil last mesh draws nothing beyond the last distance.

### Billboards and labels

`DrawBillboard` puts a textured quad in the scene that turns to face the
camera: health bars, name tags, distant trees, smoke. `Upright` keeps
it vertical, `Cutout` gives it hard edges that cast shadows, and
`OnTop` draws it over everything. `DrawText3D` draws a font's text the
same way, as one instanced draw per label. Both go through the mesh
path, so they are lit and fogged when asked. `Project` maps a world
point to the view, for a 2D label drawn over the scene instead.

### Post-processing and render textures

`SetPost` controls exposure, bloom, vignette, saturation, contrast,
ambient occlusion and FXAA. `PostSettings.LUT` runs the finished frame
through a colour grading lookup table: `NeutralLUT` gives the identity
strip, a screenshot with the strip pasted in is graded in an image
editor, the strip is cropped back out and loaded with `NewLUT`, and
every frame then gets the same grade, blended in by `LUTStrength`.

`DrawTo` renders into a `RenderTexture` that later draws like any
texture: minimaps, portals, mirrors, picture-in-picture.
`NewRenderTextureOptions` chooses nearest or repeating sampling for it.

### Picking

`ScreenRay` turns a screen point into a world ray, and `Mesh.Intersect`
or `Model.Intersect` report where it hits. For physics-backed scenes,
`phys.Raycast3` does the same against colliders.

### Debug drawing

`DrawLine3D`, `DrawWireBox`, `DrawWireCube`, `DrawWireSphere`,
`DrawWireFrustum` and `DrawAxes` draw lines over the scene that ignore
depth, so colliders, paths, rays, bones and cameras can be seen while a
game is being written. `DebugText` prints in the engine's own font
without loading one, and `DebugText3D` puts that text at a world point.

## Tests

The renderer's tests run on a headless surface and read pixels back.
Every example saves a screenshot with `-shot`, and `examples_test.go`
runs each one headless and checks that it drew something. When changing
a shader or a material, take a screenshot before and after and compare
them.
