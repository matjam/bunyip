# What Bunyip is still missing

A working list of what the engine does not do yet and where its public
API could be better, from a review of every exported package against
what a game needs day to day and what mature 2D engines in Go offer.
Items within a section are roughly ordered by how often a game would
want them. The list is meant to be edited as work lands: strike or
delete items when they are done.

## API polish

The review of the public surface (one rectangle type, nine-slices and
regions as values, colour and vector helpers, text options for every
font kind, float32 input, byte-taking decoders, buses, embedded assets
and one-call loaders, debug drawing, the fixed view, headless runs, the
window controls, the naming and zero-value conventions in package
comments) has landed. What remains:

- Engine plumbing still sits on public types: `gfx.New` (whose
  parameter is an internal type), `Graphics.Begin`, `End`, `Resize`,
  `SetTime` and `Destroy`, `input.State.Feed*`, `EndUpdate`, `EndFrame`
  and `SetDrawing`, and `audio.Mixer.Mix`. They are documented as
  plumbing now; before a 1.0 they should move behind an internal hook
  package registered from `init`, so the game-facing surface is all a
  game sees.
- Atlas formats with named frames (TexturePacker, Aseprite JSON) on top
  of `Region`, so exported atlases drop in without UV pairs.
- The X11 clipboard: serving selections needs a request loop the
  platform layer does not run yet; macOS and Windows have it.
- `tween` only tweens `float32`; vectors and colours go through `anim`
  curves.

## Platforms and window

- Web (WebAssembly) and mobile (Android, iOS). Both need a second
  graphics backend, since Vulkan is unavailable there; WebGPU is the
  plausible common target. The largest single gap.
- Windows and Linux have been cross-compiled and vetted but never run on
  real machines. Native Wayland is not written (XWayland works meanwhile).
- Window control beyond title, icon, size limits and fullscreen:
  position, decorations, always on top, transparent and click-through
  windows, choice of monitor, starting fullscreen or unfocused.
- A way to keep or stop updating when unfocused; focus state and close
  interception exist.
- Custom cursor images; the system shapes and visibility exist.
- Drag and drop of files onto the window, opening a URL in the browser,
  native file dialogs.
- Multiple windows.
- Windows IME and X input methods; only macOS composes text natively.
- App bundling for Windows and Linux (installers, AppImage); code signing.

## Game loop

- A frame-time cap and maximum catch-up steps in `Config`; today a
  stall is clamped to a quarter second.
- Pausing the loop (and the mixer) when the window is hidden or
  minimised, as an option; `Mixer.SetPaused` exists for the game to
  call.

## Input

- Touch input with multi-touch points and the usual gestures.
- Gamepad rumble; connect and disconnect events; controller names and a
  mapping database so unusual controllers land on the standard layout;
  the current mapping is unverified against real hardware.
- Input action maps: named actions bound to keys, buttons and axes,
  with dead zones, rebinding at runtime and a serialisable binding set.
- Held-key durations and a list of keys currently down, for rebinding
  screens and cheat codes; `KeyRepeated` reports OS repeats.
- Double-click detection.
- Key names in the user's layout (what is printed on the key) beside the
  positional `Key.String`, for on-screen prompts.

## 2D drawing

Streaming texture writes, colour matrices, flips and per-draw
filtering, gradients, dashed strokes, text on paths, indexed draws, the
`particle` package, lit sprites, tilemap flips and animations, the
`tiled` importer, TexturePacker and Aseprite atlases, tiled nine-slices,
`Shader.Reload` and batch statistics are in. What remains:

- 2D shadows cast by occluders from the point lights; lit sprites take
  light from every direction today.
- GPU-instanced particles for very large counts; the system is CPU
  simulated and drawn through the sprite stream, fine for thousands.
- A particle editor in the gallery.
- Tiled's XML forms (.tmx, .tsx) and zstd-compressed layers; the JSON
  forms with CSV, base64, zlib and gzip load.
- A draw-call budget warning in the overlay; the counts are shown.
- Compiling GLSL at runtime would need a pure-Go compiler and is out of
  scope; the offline tool plus `Shader.Reload` is the design.

## Text

Bitmap colour emoji, letter spacing, justification, text on a path,
rich text with links and hyphenation are in. What remains:

- COLR and SVG colour glyphs; only bitmap strikes (sbix, CBDT) draw in
  colour, which covers Apple and Google's emoji fonts.
- Hyphenation patterns beyond American English; `ParseTeXPatterns`
  loads any TeX pattern file a game ships.
- Rich text shapes each word on its own, so ligatures and kerning do
  not cross a style change; a glyph made new in a frame appears from
  the next frame.

## 3D rendering

- Billboarded sprites in 3D for labels and health bars; `Project` maps
  a world point to the view for 2D overlays today.
- Debug frustums and text in 3D; lines, boxes, spheres and axes exist.
- Distance fog as a light setting; the cheapest large visual win.
- Level of detail for meshes and impostors; frustum and occlusion
  culling (draws are currently all submitted).
- Point-light shadows (cube shadow maps) and spot lights; more than eight
  point lights (clustered or tiled lighting).
- Terrain: heightfield meshes with LOD, splat maps as a built-in.
- Global illumination beyond one environment map: light probes, baked
  lightmaps, reflection probes per area, screen-space reflections.
- Volumetrics: god rays, height fog, atmospheric scattering for the sky
  rather than the parametric gradient.
- Temporal anti-aliasing and MSAA; FXAA is the only option.
- Depth of field, motion blur, colour grading LUTs, lens effects.
- Order-independent transparency; blended draws are sorted per mesh.
- Render texture options: colour format, no depth, multisampling, and
  reading its depth.

## Materials and lighting

- Iridescence, anisotropy, specular colour and the specular-glossiness
  workflow from glTF extensions. (Clearcoat, sheen, subsurface,
  transmission with volume attenuation, vertex colours, a second UV set,
  texture transforms, decals, outlines and x-ray are in.)
- Transmission textures and the glTF thickness texture's green channel;
  only the factors load from KHR_materials_transmission and volume.
- Per-material stencil state beyond outlines and x-ray.
- Hair, fur and cloth shading (anisotropic highlights, shell rendering).
- OpenEXR panoramas for environments; Radiance .hdr and LDR images decode
  today, and the procedural sky covers most outdoor and space scenes
  without any image.

## Animation

- Blend trees, layered animation and masks; inverse kinematics; root
  motion extraction; animation events at keyframes.
- Morph targets from glTF.
- Sprite animation authoring from Aseprite or similar.

## Audio

- Pause everything on focus loss, as an option; `Mixer.SetPaused` is
  the call.
- Hardware or platform mixing (spatialiser plugins, HRTF); the mixer is
  a Go loop.
- Occlusion and reverb zones driven by the scene.
- Streaming tracker music with seek; per-channel mute and solo.
- Microphone input.
- Windows and Linux audio layers untested on hardware.

## User interface

Multi-line editing with selection, clipboard and undo, anchors and
splits, tabs, tables, trees, menus, modals, draggable windows, radios,
integer sliders, spinners, list boxes, colour pickers, images, icon
buttons, rich labels with links, and an accessibility tree are in. What
remains:

- Handing the accessibility tree to a platform screen reader; the tree
  is there, the bridge to VoiceOver and friends is not.
- Keyboard navigation inside lists, tables and trees (Tab reaches each
  row; arrows do not yet move within them).
- Drag and drop between widgets, and reordering lists.

## Physics

- 3D shapes beyond spheres and boxes: capsules, convex hulls, triangle
  meshes for static geometry, compound colliders.
- Spatial queries: overlap tests against a shape, shape casts (sweeps),
  nearest collider; only rays exist.
- Joints and constraints (hinges, springs, ragdolls) in both 2D and 3D.
- Continuous collision detection for fast small bodies; sleeping bodies.
- Character controllers (capsule with step and slope handling).
- Debug visualisation of colliders and contacts (needs 3D debug lines).
- Soft bodies, cloth simulation, fluids.
- 2D: chain shapes and edge colliders for terrain.

## Entities, data and scripting

- Serialisation of ECS worlds for saves and networking.
- Prefabs and spawning from data files.
- A scene format and a scene editor.
- Scripting for game logic in something other than Go (Lua or similar),
  or hot code reload.
- Localisation: string tables, plural rules, right-to-left layout of UI.
- Coroutine-style sequencing for cutscenes beyond timer and tween.

## Networking

- Client-side prediction, interpolation and lag compensation helpers.
- Reliable-ordered channels over UDP; NAT traversal; matchmaking.
- Snapshot delta compression and interest management.
- Encryption and authentication.

## Tooling and workflow

- An in-engine debug camera and console; a frame profiler with GPU
  timestamps (CPU scopes exist).
- An asset pipeline that converts textures to compressed GPU formats
  (BC, ASTC) and generates mip chains offline.
- Material and shader hot reload during development.
- Crash reporting and structured logging to a file.

## Quality and process

- Hardware verification on Windows and Linux; a GPU matrix in CI.
- Screenshot-comparison tests for examples, on `Config.Headless`.
- Fuzzing of loaders (glTF, WAV, MOD, S3M, XM, IT, HDR, PNG paths).
- A public API stability policy once the engine tags 1.0, after the
  plumbing above is hidden.
