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
options choose nearest or linear filtering, mipmaps and repeat. `Draw`
places a `Sprite`: position, size, a UV window, a tint, rotation and
origin. `DrawTexture` and `FillRect` cover the common cases.

**Sheets, animation and tilemaps.** A `Sheet` divides a texture into
frames; `DrawFrame` draws one. `Animation` and `AnimState` step through
frames at a rate. A `Tilemap` holds frame indices and draws only the
tiles inside the camera's view.

**Cameras and layers.** `SetCamera2D` makes later sprites world-space:
the camera has a position, zoom and rotation, and `ViewToWorld` maps the
mouse back into the world. `ScreenSpace` returns to view coordinates for
the HUD. `SetLayer` orders sprites across calls; within a layer,
submission order wins.

**Text.** `NewFont` rasterises a TrueType font at one size into an atlas;
`DrawText` draws a line and `DrawTextBlock` wraps and aligns paragraphs.
`NewSDFFont` builds a signed-distance atlas that `DrawTextSized` scales
and rotates without blur.

**Clipping.** `Clip` (or `PushClip` and `PopClip`) limits drawing to a
rectangle, which is how scroll areas work.

## 3D

**Meshes and models.** `NewMesh` uploads vertices and indices;
`CubeMesh` and `SphereMesh` generate shapes. `LoadModel` uploads a parsed
glTF document with its materials, textures, skins and animation clips.

**Materials.** `Material` is metallic-roughness PBR: a base colour or
albedo texture, metallic and roughness factors or a texture, a normal
map, emissive strength, and flags for blending and double-sided drawing.

**Camera and lights.** `SetCamera` takes a `Camera` (position, target,
field of view); `OrbitCamera` builds one from yaw, pitch and distance.
`SetLight` sets the directional light with sky and ground ambient and
optional cascaded shadow maps; `AddPointLight` adds up to eight local
lights per frame.

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

**Picking.** `ScreenRay` turns a screen point into a world ray, and
`Mesh.Intersect` or `Model.Intersect` report where it hits.

## Verifying without eyes

The renderer's tests run on a headless surface and read pixels back, and
every example saves a screenshot with `-shot`. When you change a shader or
a material, take a screenshot before and after; the difference is the
review.
