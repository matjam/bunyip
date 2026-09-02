# What Bunyip is still missing

A working list, grouped by area, of what the engine does not do yet.
Items are roughly ordered by how often a game would want them. Struck
items are done; the list is meant to be edited as work lands.

## Platforms and window

- Web (WebAssembly) and mobile (Android, iOS). Both need a second
  graphics backend, since Vulkan is unavailable there; WebGPU is the
  plausible common target. The largest single gap.
- Windows and Linux have been cross-compiled and vetted but never run on
  real machines. Native Wayland is not written (XWayland works meanwhile).
- Windows IME and X input methods; only macOS composes text natively.
- Touch input. Multi-monitor queries and choosing the display. Window
  icon, clipboard, drag and drop of files, custom cursor images, multiple
  windows.
- Gamepad rumble; gamepad mapping unverified against real controllers.
- App bundling for Windows and Linux (installers, AppImage); code signing.

## 2D rendering

- Colour matrices per draw (hue, saturation, brightness) as a built-in
  rather than a five-line shader.
- Dashed strokes and stroke-along-path text.
- Sprite batching statistics and a draw-call budget in the debug overlay.
- Lit 2D: normal-mapped sprites with point lights, 2D shadows.
- Particle systems as a first-class feature (emitters, curves over
  lifetime, GPU-instanced), rather than hand-drawn sprites.
- Tilemap editors and formats: Tiled (TMX/TSX) import.
- Nine-slice with tiled (not stretched) edges; sprite flip flags.

## 3D rendering and materials

- Iridescence, anisotropy, specular colour and the specular-glossiness
  workflow from glTF extensions. (Clearcoat, sheen, subsurface,
  transmission with volume attenuation, vertex colours, a second UV set,
  texture transforms, decals, outlines and x-ray are in.)
- Transmission textures and the glTF thickness texture's green channel;
  only the factors load from KHR_materials_transmission and volume.
- Per-material stencil state beyond outlines and x-ray.
- Hair, fur and cloth shading (anisotropic highlights, shell rendering).
- Terrain: heightfield meshes with LOD, splat maps as a built-in.
- Level of detail for meshes and impostors; frustum and occlusion culling
  (draws are currently all submitted).
- Point-light shadows (cube shadow maps) and spot lights; more than eight
  point lights (clustered or tiled lighting).
- Global illumination beyond one environment map: light probes, baked
  lightmaps, reflection probes per area, screen-space reflections.
- Volumetrics: fog, god rays, height fog, atmospheric scattering for the
  sky rather than a gradient.
- Temporal anti-aliasing and MSAA; FXAA is the only option.
- Depth of field, motion blur, colour grading LUTs, lens effects.
- Order-independent transparency; blended draws are sorted per mesh.
- OpenEXR panoramas for environments; Radiance .hdr and LDR images decode
  today, and the procedural sky covers most outdoor and space scenes
  without any image.

## Animation

- Blend trees, layered animation and masks; inverse kinematics; root
  motion extraction; animation events at keyframes.
- Morph targets from glTF.
- Sprite animation authoring from Aseprite or similar.

## Audio

- Hardware or platform mixing (spatialiser plugins, HRTF); the mixer is
  a Go loop.
- Occlusion and reverb zones driven by the scene.
- Streaming tracker music with seek; per-channel mute and solo.
- Microphone input.
- Windows and Linux audio layers untested on hardware.

## User interface

- Text editing beyond a single line: multi-line fields, selection,
  clipboard, undo.
- Layout: anchors and stretch rules, tables, tabs, tree views, menus,
  modal dialogs, windows the user can drag and resize.
- Images and sprites as widgets; icons in buttons.
- Accessibility: screen-reader names, high-contrast is a palette only.
- Rich text: inline colours, bold runs and links in one label.

## Text

- Colour and bitmap emoji glyphs (drawn as notdef today).
- Text on a path, letter spacing, justification.
- Hyphenation.

## Physics

- 3D shapes beyond spheres and boxes: capsules, convex hulls, triangle
  meshes for static geometry, compound colliders.
- Joints and constraints (hinges, springs, ragdolls) in both 2D and 3D.
- Continuous collision detection for fast small bodies; sleeping bodies.
- Character controllers (capsule with step and slope handling).
- Soft bodies, cloth simulation, fluids.
- 2D: chain shapes and edge colliders for terrain.

## Entities and services

- Serialisation of ECS worlds for saves and networking.
- Prefabs and spawning from data files.
- A scene format and a scene editor.
- Scripting for game logic in something other than Go (Lua or similar),
  or hot code reload.
- Localisation: string tables, plural rules, right-to-left layout of UI.
- Input action maps with rebinding UI.
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
- A material and shader hot reload during development.
- Crash reporting and structured logging to a file.

## Quality and process

- Hardware verification on Windows and Linux; a GPU matrix in CI.
- Screenshot-comparison tests for examples.
- Fuzzing of loaders (glTF, WAV, MOD, S3M, XM, IT, PNG paths).
- A public API stability policy once the engine tags 1.0.
