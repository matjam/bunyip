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
in each package's comment.

- The Linux clipboard: X11 selections need a request loop the platform
  layer does not run yet, and Wayland needs `wl_data_device` with a pipe
  to read an offer from. macOS and Windows have it.

## Platforms and window

- Web (WebAssembly) and mobile (Android, iOS). Both need a second
  graphics backend, since Vulkan is unavailable there; WebGPU is the
  plausible common target. This is the largest single gap.
- Windows has been cross-compiled and vetted but never run on a real
  machine. The Linux window layer has: both the Wayland and X11 backends
  have opened windows, presented frames and delivered input on a Linux
  desktop. Linux audio and gamepads remain unexercised.
- What the Wayland layer does not do yet: the clipboard through
  `wl_data_device`, text input through `zwp_text_input_v3`, fractional
  scale through `wp_fractional_scale_v1` and `wp_viewporter` (the buffer
  scale is an integer today), and the window icon through
  `xdg-toplevel-icon-v1`. Without `zxdg_decoration_manager_v1` the window
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
- X11 key repeat: X delivers a repeat as a release and press pair, and
  the layer forwards both without marking the press as a repeat, so a
  held key releases every repeat on X11. Detectable auto-repeat through
  XKB would fix it; Wayland, Windows and macOS mark repeats.
- App bundling for Windows and Linux (installers, AppImage); code
  signing.

## Game loop

`Config.MaxCatchUp` and `MaxSteps` cap the catch-up after a stall, and
`PauseUnfocused` stops updates and the mixer while another window has
focus.

- Pausing when the window is hidden or minimised but still focused; the
  platform layers do not report visibility yet.

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
- Post-processing on a 2D-only frame. Bloom, ambient occlusion,
  vignette, the LUT and FXAA all run in the composite pass, which
  `renderQueue` skips when the frame has no 3D draws, no background and
  no debug lines (`has3D` in `gfx/graphics.go`). A 2D game draws to a
  `RenderTexture` and blits it with its own sprite shader instead.
- GPU-instanced particles for very large counts; the system is CPU
  simulated and drawn through the sprite stream, which is fine for
  thousands.
- A particle editor in the gallery.
- Compiling GLSL at runtime would need a pure-Go compiler and is out of
  scope; the offline tool plus `Shader.Reload` is the design.

## Text

Bitmap colour emoji, letter spacing, justification, text on a path,
rich text with links and hyphenation are in.

- COLR and SVG colour glyphs; only bitmap strikes (sbix, CBDT) draw in
  colour, which covers Apple's and Google's emoji fonts.
- Hyphenation patterns beyond American English; `ParseTeXPatterns`
  loads any TeX pattern file a game ships.
- Rich text shapes each word on its own, so ligatures and kerning do not
  cross a style change, and a glyph first drawn in a frame appears from
  the next frame.

## 3D rendering

Billboards and 3D text, debug frustums and 3D debug text, distance and
ground fog, frustum culling with a public `Frustum`, levels of detail,
spot lights with shadows, thirty-two lights a frame, heightfield and
primitive meshes, dynamic mesh updates, colour grading LUTs, and
nearest or repeating sampling for render textures are in.

- Point light shadows (cube maps); the directional light and up to four
  spot lights a frame cast shadows.
- Occlusion culling, and impostors (billboards baked from a model).
- Clustered lighting for hundreds of lights; a frame keeps its first
  thirty-two and `FrameStats.LightsDropped` counts the rest.
- Per-light culling in the shadow pass: every opaque draw is rendered
  into every cascade and every shadowed spot light's map, up to seven
  times, whether or not it can fall inside that map.
- The 3D draw sort compares whole draw records; at several thousand
  draws it is around a millisecond a frame. A packed sort key would cut
  it.
- A cascade's near plane sits one slice radius above the slice, so a
  caster far above it (a bridge over cascade zero) casts no shadow into
  that cascade; depth clamping on the shadow pipelines would fix it.
- Mesh and texture uploads go through a one-shot command buffer that
  waits for the queue, and every `Destroy` waits for the device, so a
  `Mesh.Update` every frame (a morph target animation) leaves no
  overlap between CPU and GPU. A per-slot retire ring, freed at the
  frame's fence, is the shape of the fix.
- Terrain splat maps and heightfield LOD as built-ins; a game does both
  with `HeightfieldMesh`, a mesh shader and `LOD` today.
- Global illumination beyond one environment map: light probes, baked
  lightmaps, reflection probes per area, screen-space reflections.
- Volumetrics: god rays, and atmospheric scattering for the sky rather
  than the parametric gradient. Fog is a per-pixel fade, not a medium.
- Temporal anti-aliasing and MSAA; FXAA is the only option.
- Depth of field, motion blur and lens effects.
- Order-independent transparency; blended draws are sorted per mesh.
- Render texture options beyond sampling: colour format, no depth,
  multisampling, and reading the depth back.
- Culling uses each mesh's bind-pose bounds (doubled for skinned meshes)
  and skips meshes whose shader moves vertices. A shader that moves
  vertices far, or an animation that leaves the bind-pose bounds by more
  than double, can still be culled when it should not be.

## Materials and lighting

Clearcoat, sheen, subsurface, transmission with volume attenuation and
transmission and thickness textures, vertex colours, a second UV set,
texture transforms, decals, outlines and x-ray are in.

- Iridescence, anisotropy, specular colour and the specular-glossiness
  workflow from glTF extensions.
- A material override on `DrawModel`, which draws every part with the
  material glTF gave it. Changing one part means walking `Model.Parts`
  and calling `DrawMesh` for each.
- Per-material stencil state beyond outlines and x-ray.
- Hair, fur and cloth shading (anisotropic highlights, shell rendering).
- OpenEXR panoramas for environments. Radiance `.hdr` and LDR images
  decode today, and the procedural sky covers most outdoor and space
  scenes without any image.

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
buses, Doppler, click-free pausing, and tracker seek, position and
per-channel mute and solo are in.

- Hardware or platform mixing (spatialiser plugins, HRTF); the mixer is
  a Go loop.
- Microphone input.
- `Voice.Stop`, `StopAll` and voice stealing cut the signal at once
  rather than fading it out over a millisecond, so a stopped voice can
  click; pausing fades.
- A reverb `RoomSize` above about 1.07 has comb feedback at or above
  one and runs away to NaN, which the output clamp passes through.
- The Windows and Linux audio layers are untested on hardware.

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
distance, hinge, revolute, ball, spring and fixed joints with limits and
motors, ragdolls, continuous collision for fast bodies (against static
geometry and between two moving bodies), sleeping, and character
controllers are in; the physics-lab example draws colliders and
contacts with the 3D debug lines.

- Soft bodies, cloth simulation and fluids.
- Continuous collision, shape casts and the character controller build
  their convex parts afresh per query, several hundred allocations a
  step with a hundred fast bodies over a large static set, and scan
  every entry rather than the sorted sweep.
- Prismatic (slider) joints and 2D wheel joints; a distance joint with a
  spring covers most suspensions.
- A stack of four boxes at the default solver quality jitters just above
  the sleep threshold; raise `Substeps` and `Iterations` for stacks that
  should sleep.

## Entities, data and scripting

World saves and loads, prefabs with children, cloning, the `locale`
package (string tables, placeholders, plural rules, fallbacks) and
`timer.Sequence` for cutscene-style sequencing are in.

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
near enough to send.

- NAT traversal and matchmaking, which need a server on the internet.
- Accounts and authentication beyond a pinned certificate.

## Tooling and workflow

`bunyip.FlyCamera` is the debug camera, `Config.LogFile` writes the log
and a panic's stack trace to a file for crash reports, and shader hot
reload is `Shader.Reload` behind an `asset.Watcher`.

- An in-engine console, and a frame profiler with GPU timestamps (CPU
  scopes exist).
- An asset pipeline that converts textures to compressed GPU formats
  (BC, ASTC) and generates mip chains offline.
- Material hot reload as a built-in; a game reloads a material's
  textures through the watcher today.

## Quality and process

- Hardware verification on Windows, of the Linux audio and gamepad
  layers, and of the Linux window layer beyond the one desktop it has
  run on; a GPU matrix in CI.
- Screenshot comparison for the examples. `examples/examples_test.go`
  runs each one headless (`BUNYIP_HEADLESS=1`) and checks that it drew
  something, but not what.
- Longer fuzzing campaigns. Every parser (glTF, the sound decoders, the
  tracker loaders, HDR, atlases, Aseprite files, rich text, Tiled maps
  and tilesets in both forms) has a fuzz target, run with `go test -fuzz=Fuzz` in its
  package; the first runs found two third-party decoder panics, now
  turned into errors, and an unchecked accessor bound in the glTF
  reader.
- A public API stability policy once the engine tags 1.0.
