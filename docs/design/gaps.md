# What Bunyip is still missing

A working list of what the engine does not do yet, kept up to date as
work lands. It came out of a review of every exported package against
what a game needs day to day and what mature 2D engines in Go offer.
Each section names what is in place, then lists what is not. Items
are roughly in the order a game would want them.

## API polish

The public surface has been through one polish pass: one rectangle
type, nine-slices and regions as values, colour and vector helpers,
text options shared by every font kind, float32 input, decoders that
take bytes, buses, embedded assets and one-call loaders, debug drawing,
the fixed view, headless runs, the window controls, atlas formats with
named frames, tweens over any value, engine plumbing hidden behind an
internal hook package, and the naming and zero-value conventions stated
in each package's comment. The clipboard now works on every platform:
macOS and Windows through the system clipboard, X11 by owning the
CLIPBOARD selection and answering requests for it (INCR included), and
Wayland through `wl_data_device` with a pipe.

## Platforms and window

- Web (WebAssembly) and mobile (Android, iOS). Both need a second
  graphics backend, since Vulkan is unavailable there; WebGPU is the
  plausible common target. This is the largest single gap.
- Windows has been cross-compiled and vetted but never run on a real
  machine. The Linux window layer has: both the Wayland and X11 backends
  have opened windows, presented frames and delivered input on a Linux
  desktop. Linux audio and gamepads remain unexercised, and so do the
  clipboard, X11 detectable key repeat, Wayland fractional scale and the
  Wayland window icon, which were written against the protocols and
  compile but have not been driven by hand.
- What the Wayland layer does not do yet: text input through
  `zwp_text_input_v3`. Fractional scale is in, through
  `wp_fractional_scale_v1` and `wp_viewporter`, with the integer buffer
  scale as the fallback where a compositor lacks them, and the window
  icon through `xdg-toplevel-icon-v1`, which is a no-op where a
  compositor lacks it. Drag and drop is not handled, so a drag offer is
  dropped rather than read; the clipboard side of `wl_data_device` is in.
  Without
  `zxdg_decoration_manager_v1` the window
  has no title bar, because drawing one client-side is not written.
  libwayland 1.20 or later is required, for `wl_proxy_marshal_flags`.
- Window position, always-on-top and custom cursor images work on macOS
  (`SetPosition`, `Position`, `SetAlwaysOnTop`, `SetCursorImage`) and
  are no-ops on Windows, Wayland and X11. Wayland has no protocol for
  the first two at all. Transparent and click-through windows, choice of
  monitor, and starting fullscreen or unfocused are not written.
- Drag and drop of files onto the window, and native file dialogs.
  `OpenURL` opens the browser on every platform.
- Multiple windows.
- Windows IME, X input methods and Wayland `zwp_text_input_v3`; only
  macOS composes text natively.
- App bundling for Windows and Linux (installers, AppImage); code
  signing.

## Game loop

`Config.MaxCatchUp` and `MaxSteps` cap the catch-up after a stall,
`PauseUnfocused` stops updates and the mixer while another window has
focus, and `PauseHidden` does the same while the window cannot be seen;
every platform layer reports visibility and `ctx.Visible` reads it.

## Input

Action maps with rebinding and JSON bindings, held-key durations, the
list of keys down, double clicks, and gamepad connect and disconnect
events are in.

- Touch input with multi-touch points and the usual gestures.
- Gamepad rumble, and a mapping database so unusual controllers land on
  the standard layout; the current mapping is unverified against real
  hardware.
- Key names in the user's layout (what is printed on the key) beside the
  positional `Key.String`, for on-screen prompts.

## 2D drawing

Streaming texture writes, colour matrices, flips and per-draw
filtering, gradients, dashed strokes, text on paths, indexed draws, the
`particle` package, lit sprites with shadows cast from occluder
outlines, tilemap flips and animations, autotiling (blob, edge,
dual-grid and Wang rules in `grid/autotile`, template expansion, Tiled
terrain sets, and square, hexagonal and isometric layouts), the `tiled`
importer in both of Tiled's file forms with every layer encoding it
writes (CSV, base64 plain, zlib, gzip and zstd), TexturePacker and
Aseprite JSON atlases with `asset.Atlas` to load one, Aseprite's own
binary files through `ParseAseprite` and `asset.Aseprite`,
`Atlas.Animation` to play a tag at its own timings, sprite culling
against the 2D camera by the sprite's own corners and under the
transform stack, a sort key within a layer (`SetSortKey`), camera
follow, clamp and shake on `Camera2D`, tiled nine-slices,
`Shader.Reload`, batch statistics and a draw budget warning are in.

- 2D shadows are cast by the occluder outlines a frame adds, not by the
  sprites themselves: nothing derives an occluder from a sprite's alpha,
  and an occluder blocks a light whatever height the light is at.
- Hexagonal Wang sets match the six sides of a hexagon. A set that
  paints its six vertices instead has nowhere to put them: the eight
  direction slots hold the sides, and the mapper computes a corner
  colour from the cells around it only on a square or isometric layout.
- Tiled's "staggered" orientation, which is an isometric map on the
  staggered grid a hexagonal map uses. `Map.Layout` gives it `Square`,
  which matches the wrong cells; only orthogonal, isometric and
  hexagonal maps have a layout that fits.
- A 2D frame that goes through the post pass pays for a second image
  and a full-screen copy: `PostSettings.Post2D` draws the 2D stream into
  the LDR image and composites from there, rather than colouring the
  stream in place. Ambient occlusion, depth of field, motion blur,
  temporal anti-aliasing and god rays need a depth buffer a 2D frame
  does not have, so they stay off in that mode.
- GPU-instanced particles for very large counts; the system is CPU
  simulated and drawn through the sprite stream, which is fine for
  thousands.
- A particle editor in the gallery.
- Compiling GLSL at runtime would need a pure-Go compiler and is out of
  scope; the offline tool plus `Shader.Reload` is the design.

## Text

Colour glyphs from COLR layers, SVG documents and bitmap strikes, letter
spacing, justification, text on a path, rich text with links and
hyphenation are in. A glyph first drawn in a
frame appears in that frame: the atlas upload is recorded into the frame
before the render pass. Hyphenation patterns for thirteen languages ship
with the engine and `AutoHyphenate` picks one from the text's
`Language`. Rich text shapes each stretch of one face as a whole, so
kerning and ligatures cross the style changes inside it.

- Rich text breaks lines at spaces and newlines rather than by the
  Unicode rules, and does not hyphenate.
- Parts of the colour glyph formats. In COLR, the variation deltas of
  the variable paint tables are not applied, the four non-separable
  blend modes (hue, saturation, colour and luminosity) composite as
  source over, and a layer that asks for the text colour draws white,
  since a colour glyph is drawn untinted. In SVG, strokes, clipping
  paths, masks, filters, patterns, images, text, style sheets and
  animation are not drawn, the even-odd fill rule draws as non-zero, a
  gradient does not inherit from another through `href`, and a group's
  opacity applies to each of its shapes rather than to the group as one.
  A
  distance-field font draws a colour glyph as its outline.
- Hyphenation patterns for languages outside the shipped set, which are
  Danish, Dutch, English (American and British), Finnish, French,
  German, Italian, Norwegian, Polish, Portuguese, Russian, Spanish and
  Swedish; `ParseTeXPatterns` loads any other TeX pattern file a game
  ships.

## 3D rendering

Billboards and 3D text, debug frustums and 3D debug text, distance and
ground fog, an atmospheric sky with aerial perspective, order-independent
transparency, frustum culling with a public `Frustum`, bounds that follow
a skinned pose and `Mesh.SetBounds` and `Shader.VertexBounds` for the
meshes culling cannot bound on its own, levels of detail, spot and point
lights with shadows, per-light culling in the shadow pass, clustered
forward lighting for a thousand lights a frame, heightfield and primitive
meshes, dynamic mesh updates, colour grading LUTs, and nearest or
repeating sampling for render textures are in.

- Soft shadows beyond the fixed nine-tap filter: no contact hardening,
  and a cube face's filter is clamped inside the face, so a point light's
  shadow hardens over the seam between faces.
- Occlusion culling, and impostors (billboards baked from a model).
- The cluster grid is built on the CPU and is fixed at 16 by 9 by 24,
  a light is bounded by the screen rectangle of its box rather than of
  its sphere, and a cluster keeps 64 lights, so a light past that in a
  crowded cluster does not light it. A compute pass over the grid, and
  tighter bounds, would raise all three.
- The shader uniform arena and the joint storage buffer still wait for
  the device when they grow, because growing rewrites descriptor sets a
  frame in flight may have bound. Both double, so it happens a handful
  of times at most, and `FrameStats.Waits` counts it; new descriptor
  sets retired with the old buffers would remove it.
- Terrain splat maps and heightfield LOD as built-ins; a game does both
  with `HeightfieldMesh`, a mesh shader and `LOD` today.
- Baked lightmaps: light arriving at a surface, stored per texel on the
  second UV set rather than sampled on a lattice. `LightProbeGrid` bakes
  irradiance at points and `ReflectionProbe` reflections in a volume, so
  a room's bounce light and reflections are there, but neither resolves
  the shadow of a chair leg on the floor beneath it.
- Two reflection probes are never blended over the space between them:
  one cube map is bound per draw, so the deepest containing probe wins
  and `ReflectionProbe.Margin` fades its reflection towards the frame's
  average environment at the volume's edge. Blending would need a second
  cube binding in the material set.
- A probe bake reads the scene back to the host and prefilters it on the
  CPU, so a probe of any size costs a stall and a fraction of a second.
  Prefiltering on the GPU would make probes cheap enough to refresh as a
  scene changes.
- Screen-space reflections trace against the depth buffer with normals
  reconstructed from it, so a surface reflects as flat as its triangles,
  and what is off screen or hidden falls back to the probe or the
  environment. There is no temporal accumulation, so a rough surface's
  rays stay noisy; keep `PostSettings.ReflectionRoughness` low.
- Volumetrics: light shafts through a medium. Fog is a per-pixel fade,
  `Sky.Atmosphere` scatters single bounces only, and the god rays are a
  screen-space radial blur over the depth buffer's sky mask, so none of
  the three casts a shaft through anything.
- Order-independent transparency is the weighted blended approximation
  (`PostSettings.OrderIndependent`), so a deep stack of layers comes out
  flatter than compositing them in order would, and transmissive draws
  stay sorted. Per-pixel lists or depth peeling would be exact.
- MSAA. FXAA and temporal anti-aliasing are the two options.
- Motion vectors come from a pass of their own rather than from a second
  colour attachment on the HDR pass, so a frame with moving meshes in it
  rasterises their geometry twice. Multiple render targets in
  `internal/render` would let the lit pass write them as it goes.
- A moving mesh has to say so: `DrawMeshMoved` and its companions take
  the transform the draw had last frame, because immediate-mode drawing
  has no identity across frames. A mesh that moves and is drawn through
  plain `DrawMesh` softens under temporal anti-aliasing and does not
  blur along its own path.
- A skinned mesh's motion vectors carry its model matrix and not its
  pose, so a limb swinging in place has none. Keeping the previous
  frame's joint matrices would fix it and would double the joint buffer.
- Depth of field gathers one disc at full resolution rather than a
  half-resolution near and far layer, so a very wide bokeh costs more
  than it should and a bright out-of-focus highlight does not bloom into
  the shape of the aperture.
- Motion blur gathers along each pixel's own vector, with no tile-max
  pass to dilate a fast object's blur past its silhouette, so an object
  smears inside its own outline and leaves no trail behind it.
- Render texture options beyond sampling: colour format, no depth,
  multisampling, and reading the depth back.
- Culling is per draw and by bounding sphere. There is no bounding
  volume hierarchy or spatial index, so a frame still pays a frustum
  test for every draw it queues, and a draw whose shape leaves its
  geometry needs `Mesh.SetBounds` or `Shader.VertexBounds` to say where
  it went.

## Materials and lighting

Clearcoat, sheen, subsurface, transmission with volume attenuation and
transmission and thickness textures, iridescence, anisotropy, specular
colour and strength, fur and grass as shells, vertex colours, a second UV
set, texture transforms, decals, outlines, x-ray, per-material stencil
state, a material override on `DrawModel`, and panoramas from OpenEXR,
Radiance and ordinary images are in. The glTF extensions behind the
material fields load, and the specular-glossiness workflow is converted
to metallic-roughness as a file is read.

- The thin film is an approximation at three wavelengths rather than the
  Belcour and Barla model, so an iridescent surface has the right hues in
  the right places but not the spectrum a renderer would integrate.
- A specular-glossiness file whose specular colour varies across one
  material keeps only its glossiness; the colour comes from the converted
  factors alone.
- The iridescence, anisotropy, specular and fur maps are always sampled
  linear and repeating: the packed sampler index has two bits for each of
  the first eleven texture slots and no room for more.
- Fur shells are drawn from the inside out with the depth buffer left
  alone, so two furry meshes that overlap blend in the order they were
  queued rather than by depth.
- PIZ, PXR24, B44, B44A, DWAA and DWAB compression in OpenEXR files, and
  tiled, deep and multi-part ones. Each is refused with an error naming
  what the file is.

## Animation

Animation events, root motion, layers with masks (override and
additive), two-bone IK and look-at through node overrides, morph
targets from glTF (blended on the CPU and uploaded when weights change),
and blend spaces and trees as data (phase-synchronised so feet do not
slide) are in.

- GPU morph targets. The CPU blend is fine for a few characters with a
  few thousand vertices each.
- Sparse accessors in glTF, which Blender writes for morph targets, are
  read as dense; a file that relies on them loads with zero deltas.
- Aseprite tilemap layers and tilesets, which `ParseAseprite` skips, and
  the blend modes past normal, which it draws as normal. Layers, groups,
  cels, tags, slices, palettes and the three colour modes read.

## Audio

Reverb zones and per-bus reverb, occlusion, mute and solo on voices and
buses, Doppler, binaural rendering, click-free pausing and stopping,
microphone capture, and tracker seek, position and per-channel mute and
solo are in.

- Measured head-related transfer functions. The binaural mode is a
  parametric head model, so it cannot tell front from back and is the
  same head for every player; loading a SOFA file of measured responses
  and convolving per ear would fix both.
- Hardware or platform mixing (spatialiser plugins); the mixer is a Go
  loop.
- Choosing which input or output device to use, and being told when the
  machine's default changes; both directions open the default device and
  keep it.
- The Windows audio layer, and macOS capture, are untested on hardware.
  Linux output and capture run on real hardware.

## User interface

Multi-line editing with selection, clipboard and undo, anchors and
splits, tabs, tables, trees, menus, modals, draggable windows, radios,
integer sliders, spinners, list boxes, colour pickers, images, icon
buttons, rich labels with links, arrow-key navigation inside lists,
tables, trees, tabs, radios and dropdowns, drag and drop, reorderable
lists, and an accessibility tree are in.

- Handing the accessibility tree to a platform screen reader. The tree
  exists; the bridge to VoiceOver and its counterparts does not.

## Physics

Capsules, convex hulls, triangle meshes, compounds, 2D capsules, edges
and chains, overlap, shape-cast, nearest and raycast-all queries,
distance, hinge, revolute, ball, prismatic, wheel, spring and fixed
joints with limits, motors and springs,
ragdolls, continuous collision for fast bodies (against static
geometry and between two moving bodies), sleeping, and character
controllers are in; the physics-lab example draws colliders and
contacts with the 3D debug lines. Cloth, volumetric soft bodies and 2D
fluids are in `phys/soft`, on the same colliders, with the softbody
example. Casts, sweeps and character moves take their candidates from
the sorted sweep and allocate nothing once their buffers have grown.

- Soft bodies do not push rigid bodies back: a cloth or a jelly reads
  the static and kinematic colliders and never writes an impulse to a
  dynamic one, so a crate cannot be knocked over by a falling jelly.
- Cloth has no self-collision, so a sheet folded onto itself passes
  through. A fluid particle and a cloth particle do not meet either;
  the three soft components only collide with the `phys` colliders.
- Soft bodies are a surface of particles with one volume constraint,
  not a tetrahedral mesh, so a body squashed hard can invert a face,
  and stiffness is uniform rather than a material with a Poisson ratio.
- Fluids are 2D only. There is no 3D fluid, and no surface meshing or
  screen-space rendering for the 2D one: a game draws the particles.
- `phys.SignedDistance2` and `SignedDistance3` cover spheres, boxes,
  capsules, circles, polygons and compounds of those. Convex hulls,
  triangle meshes, edges and chains have no signed distance, so soft
  bodies pass through them.
- A stack whose boxes are turned relative to each other keeps creeping
  into place at the default solver quality, so it sleeps late or not at
  all; an aligned stack settles within a second or two.

## Entities, data and scripting

World saves and loads, prefabs with children, cloning, the `ecs.Scene`
document format with `Instantiate`, `ExportScene`, prefab libraries and
`asset.Scene`, the `locale` package (string tables, placeholders, plural
rules, fallbacks) and `timer.Sequence` for cutscene-style sequencing are
in.

- A scene editor. The format and the API are what one would read and
  write; placing entities is still a matter of editing JSON or building
  the scene in code and calling `ExportScene`.
- Scripting for game logic in something other than Go (Lua or similar),
  or hot code reload.
- Right-to-left layout of the interface; the text itself shapes
  correctly.

## Networking

`Interpolator`, `Predictor`, `History` and `Clock` cover interpolation,
prediction with reconciliation, lag compensation and server time;
`SendReliable` gives reliable-ordered delivery over UDP with connection
events and link statistics; `ListenTLS`, `DialTLS` and
`SelfSignedConfig` encrypt TCP with a pinned certificate; `EncodeDelta`
with `SnapshotBuffer` sends only what changed; `Interest` picks what is
near enough to send.

- NAT traversal and matchmaking, which need a server on the internet.
- Accounts and authentication beyond a pinned certificate.

## Tooling and workflow

`bunyip.FlyCamera` is the debug camera, `Config.LogFile` writes the log
and a panic's stack trace to a file for crash reports, and hot reload is
`asset.Reloader`: it watches the loose files a game loaded through it
and swaps them into the objects the game holds, a texture's image
through `gfx.Texture.Replace` and a shader's pipelines through
`Shader.Reload`, so a material's textures follow the files without any
bookkeeping. `bunyip-tex` and `gfx/ktx2` are the texture pipeline:
images compress to BC1, BC3, BC4, BC5 or BC7 with their mip chains built
offline in linear light, into KTX2 files that `asset.Texture` and
`gfx.NewCompressedTexture` upload block for block, falling back to a
software decode where a device cannot sample the format.
`Config.Console`
turns on the in-game console: commands, variables, key bindings, the
log, and panels for the frame timings and profile scopes, the live
post-processing settings and GPU resources, a world's entities,
components, resources and systems, the physics simulation, the mixer,
the input devices and a game's own services. Every pass is timed on the
GPU with timestamp queries read back without waiting, reported by
`gfx.FrameStats.GPU` and `GPUFrameMS` and by `bunyip.Stats`, graphed and
listed by the engine panel and totalled by the F3 overlay.

- Nothing times the parts of a pass, only the pass: a slow shader inside
  the opaque pass shows as a slow opaque pass. Splitting it further
  would mean timestamps between draw runs, which is a query pair per run.
- ASTC is carried but not encoded. `gfx/ktx2` writes BC1, BC3, BC4, BC5
  and BC7, and a KTX2 file that already holds ASTC parses and uploads on
  a device that samples it, but nothing here produces ASTC or decodes it,
  so there is no software fallback for one. Mobile is where ASTC matters
  and mobile needs a second graphics backend first.
- BC7 is written in two of its eight modes, one subset over RGBA and two
  subsets over RGB, and the decoder reads back the same two. A file from
  another tool uploads whatever modes it uses, but the software fallback
  for a device without BC7 cannot expand one that uses the other six.
- BC6H, for high dynamic range images, is neither written nor read;
  environment maps load from Radiance `.hdr` instead.
- Models and environments are not hot reloaded. A reloaded glTF file
  gives back different meshes, a different skeleton and different clips,
  which every `AnimPlayer`, mesh pointer and node index a game holds
  refers to; an `Environment` is a prefiltered cube map, which a probe
  bake also owns, so swapping the panorama behind one means rebuilding
  every level of it. `Reloader` leaves both to the game rather than
  swapping under it.

## Quality and process

- Hardware verification on Windows, of the Linux audio and gamepad
  layers, and of the Linux window layer beyond the one desktop it has
  run on; a GPU matrix in CI.
- Screenshot comparison for the examples. `examples/examples_test.go`
  runs each one headless (`BUNYIP_HEADLESS=1`) and checks that it drew
  something, but not what.
- Regenerating the example screenshots in `docs/examples/` is manual.
  The test in `cmd/bunyip-docs` catches a walkthrough whose excerpts have
  drifted from the source, but nothing notices when a committed
  screenshot no longer shows what the example draws.
- Longer fuzzing campaigns. Every parser (glTF, the sound decoders, the
  tracker loaders, HDR, atlases, Aseprite files, rich text, Tiled maps
  and tilesets in both forms) has a fuzz target, run with `go test -fuzz=Fuzz` in its
  package; the first runs found two third-party decoder panics, now
  turned into errors, and an unchecked accessor bound in the glTF
  reader.
- A public API stability policy once the engine tags 1.0.
