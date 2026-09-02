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
- The X11 clipboard: serving selections needs a request loop the
  platform layer does not run yet; macOS and Windows have it.

## Platforms and window

- Web (WebAssembly) and mobile (Android, iOS). Both need a second
  graphics backend, since Vulkan is unavailable there; WebGPU is the
  plausible common target. The largest single gap.
- Windows and Linux have been cross-compiled and vetted but never run on
  real machines. Native Wayland is not written (XWayland works meanwhile).
- Window position, always on top and custom cursor images work on
  macOS (`SetPosition`, `Position`, `SetAlwaysOnTop`, `SetCursorImage`)
  and are no-ops on Windows and X11 until those layers run on
  hardware. Decorations, transparent and click-through windows, choice
  of monitor, starting fullscreen or unfocused are not written.
- Drag and drop of files onto the window and native file dialogs;
  `OpenURL` opens the browser on every platform.
- Multiple windows.
- Windows IME and X input methods; only macOS composes text natively.
- App bundling for Windows and Linux (installers, AppImage); code signing.

## Game loop

`Config.MaxCatchUp` and `MaxSteps` cap the catch-up after a stall, and
`PauseUnfocused` stops updates and the mixer while another window has
focus. What remains:

- Pausing when the window is hidden or minimised but still focused;
  the platform layers do not report visibility yet.

## Input

Action maps with rebinding and JSON bindings, held-key durations, the
list of keys down, double clicks, and gamepad connect and disconnect
events are in. What remains:

- Touch input with multi-touch points and the usual gestures.
- Gamepad rumble; a mapping database so unusual controllers land on the
  standard layout; the current mapping is unverified against real
  hardware.
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
- Tiled's zstd-compressed layers (a pure-Go zstd would be a new
  dependency); the JSON and XML forms with CSV, base64, zlib and gzip
  load.
- Compiling GLSL at runtime would need a pure-Go compiler and is out of
  scope; the offline tool plus `Shader.Reload` is the design.
  `Config.DrawBudget` turns the overlay's counts into a warning.

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

Billboards and 3D text, debug frustums and 3D debug text, distance and
ground fog, frustum culling with a public `Frustum`, levels of detail,
spot lights, thirty-two lights a frame, heightfield and primitive
meshes, and dynamic mesh updates are in. What remains:

- Point light shadows (cube maps); the directional light and up to four
  spot lights a frame cast shadows.
- Occlusion culling; impostors (billboards baked from a model).
- Clustered lighting for hundreds of lights; a frame keeps its first
  thirty-two.
- Terrain splat maps and heightfield LOD as built-ins; a game does
  both with `HeightfieldMesh`, a mesh shader and `LOD` today.
- Global illumination beyond one environment map: light probes, baked
  lightmaps, reflection probes per area, screen-space reflections.
- Volumetrics: god rays, atmospheric scattering for the sky rather than
  the parametric gradient; fog is a per-pixel fade, not a medium.
- Temporal anti-aliasing and MSAA; FXAA is the only option.
- Depth of field, motion blur, lens effects; colour grading LUTs are in.
- Order-independent transparency; blended draws are sorted per mesh.
- Render texture options beyond nearest and repeat sampling: colour
  format, no depth, multisampling, and reading its depth.
- Culling uses each mesh's bind-pose bounds (doubled for skinned
  meshes) and skips meshes whose shader moves vertices; a shader that
  moves them far can still be culled when it should not be.

## Materials and lighting

- Iridescence, anisotropy, specular colour and the specular-glossiness
  workflow from glTF extensions. (Clearcoat, sheen, subsurface,
  transmission with volume attenuation, vertex colours, a second UV set,
  texture transforms, decals, outlines and x-ray are in.)
- Per-material stencil state beyond outlines and x-ray. (Transmission
  textures and glTF thickness textures load now.)
- Hair, fur and cloth shading (anisotropic highlights, shell rendering).
- OpenEXR panoramas for environments; Radiance .hdr and LDR images decode
  today, and the procedural sky covers most outdoor and space scenes
  without any image.

## Animation

Animation events, root motion, layers with masks (override and
additive), two-bone IK and look-at through node overrides, morph
targets from glTF (blended on the CPU and uploaded when weights change),
and blend spaces and trees as data (`BlendSpace1D`, `BlendSpace2D`,
`BlendTree`, phase-synchronised so feet do not slide) are in. What
remains:

- GPU morph targets; the CPU blend is fine for a few characters with a
  few thousand vertices each.
- Sprite animation authoring from Aseprite or similar beyond the atlas
  frame tags `ParseAtlas` reads.

## Audio

Reverb zones and per-bus reverb, occlusion, mute and solo on voices
and buses, Doppler, click-free pausing, and tracker seek, position and
per-channel mute and solo are in. What remains:

- Hardware or platform mixing (spatialiser plugins, HRTF); the mixer is
  a Go loop.
- Microphone input.
- Windows and Linux audio layers untested on hardware.

## User interface

Multi-line editing with selection, clipboard and undo, anchors and
splits, tabs, tables, trees, menus, modals, draggable windows, radios,
integer sliders, spinners, list boxes, colour pickers, images, icon
buttons, rich labels with links, and an accessibility tree are in. What
remains:

Arrow, Home, End and page navigation inside lists, tables, trees,
tabs, radios and open dropdowns, drag and drop between widgets
(`DragSource`, `DropTarget`) and `ReorderableList` are in as well.
What remains:

- Handing the accessibility tree to a platform screen reader; the tree
  is there, the bridge to VoiceOver and friends is not.

## Physics

Capsules, convex hulls, triangle meshes, compounds, 2D capsules, edges
and chains, overlap, shape-cast, nearest and raycast-all queries,
distance, hinge, revolute, spring and fixed joints, continuous
collision for fast bodies, sleeping, and character controllers are in;
the physics-lab example draws colliders and contacts with the 3D debug
lines. What remains:

Hinge and revolute limits and motors, ball joints with cone and twist
limits, `NewRagdoll3` and continuous collision between two fast
dynamic bodies are in as well. What remains:

- Soft bodies, cloth simulation, fluids.
- Prismatic (slider) joints and 2D wheel joints; a distance joint with
  a spring covers most suspensions.
- A stack of four boxes at the default solver quality jitters just
  above the sleep threshold; raise `Substeps` and `Iterations` for
  stacks that should sleep.

## Entities, data and scripting

World saves and loads, prefabs with children, cloning, the `locale`
package (string tables, placeholders, plural rules, fallbacks) and
`timer.Sequence` for cutscene-style sequencing are in. What remains:

- A scene format and a scene editor; a prefab's JSON is the data format
  for now.
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
near enough to send. What remains:

- NAT traversal and matchmaking, which need a server on the internet.
- Accounts and authentication beyond a pinned certificate.

## Tooling and workflow

`bunyip.FlyCamera` is the debug camera, `Config.LogFile` writes the
log and a panic's stack to a file for crash reports, and shader hot
reload is `Shader.Reload` behind an `asset.Watcher`. What remains:

- An in-engine console; a frame profiler with GPU timestamps (CPU
  scopes exist).
- An asset pipeline that converts textures to compressed GPU formats
  (BC, ASTC) and generates mip chains offline.
- Material hot reload as a built-in; a game reloads a material's
  textures through the watcher today.

## Quality and process

- Hardware verification on Windows and Linux; a GPU matrix in CI.
- Screenshot comparison for the examples; `examples/examples_test.go`
  runs each one headless (`BUNYIP_HEADLESS=1`) and checks it drew
  something, but not what.
- Longer fuzzing campaigns; every parser (glTF, the sound decoders,
  tracker loaders, HDR, atlas, rich text, Tiled maps and tilesets in
  both forms) has a fuzz target, run with `go test -fuzz=Fuzz` in its
  package, and the first runs found two third-party decoder panics now
  turned into errors.
- A public API stability policy once the engine tags 1.0, after the
  plumbing above is hidden.
