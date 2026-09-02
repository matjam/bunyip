# What Bunyip is still missing

A working list of what the engine does not do yet and where its public
API could be better, from a review of every exported package against
what a game needs day to day and what mature 2D engines in Go offer.
Items within a section are roughly ordered by how often a game would
want them. The list is meant to be edited as work lands: strike or
delete items when they are done.

## Where to start

The items below would pay off first. Each is small or medium work and
is felt by nearly every game.

1. A logical resolution and scaling policy (fit, integer scale,
   letterbox) as a `Config` option, plus the interpolation factor between
   fixed updates, so pixel-art and smooth-motion games both look right
   without per-frame `SetView` calls.
2. `asset.FS` over an `fs.FS`, so games ship assets with `go:embed`, and
   one-call loaders that bridge assets to textures, fonts and sounds.
3. One rectangle type (`lin.Rect`) used by clip rects, the UI, cameras
   and nine-slices, and a `gfx.NineSlice` value shared with the UI skin.
4. Texture updates: write pixels into an existing texture, and read
   pixels back from a render texture.
5. Mixer buses (music, effects, voice) with their own volume, and a
   voice's playback position and seek, for settings screens and rhythm
   or cutscene sync.
6. Debug text with the engine's built-in font and 3D debug lines,
   boxes and spheres, so physics and cameras can be seen while they are
   being written.
7. An orthographic 3D camera and a world-to-screen projection helper,
   which isometric strategy games and labelled space scenes both need.
8. Window basics: title changes at runtime, icon, size limits, cursor
   visibility and shape, close interception, focus state, clipboard.

## API polish

Cross-cutting findings from reading the public surface as a whole. None
of these change what the engine can do; they change how it reads.

### Types to unify

- Rectangles are spelled three ways: `gfx.ClipRect{X, Y, W, H}`,
  `ui.Rect{X, Y, W, H}` and the four floats `Camera2D.VisibleRect`
  returns. One `lin.Rect` with `Contains`, `Intersects`, `Union`,
  `Inset` and `Center` should serve all three.
- Nine-slices are spelled twice: `Graphics.DrawNineSlice` takes ten
  parameters and `ui.Slice` carries the same texture and insets as a
  value. A `gfx.NineSlice{Tex, Left, Top, Right, Bottom}` drawn with
  `DrawNineSlice(ns, rect, tint)` lets the UI skin reuse it.
- Colour helpers live in the wrong places: `anim.LerpColor` should be
  `Color.Lerp`, and `Color` wants `Mul` (tint by tint), `Scale`
  (brightness), `HSV`/`FromHSV` and `Premultiplied` as methods rather
  than each caller writing them.
- `lin` is missing the small things games write by hand: `Vec2.Neg`,
  `Perp`, `Angle`, `Rotate`, `Distance`; `Vec3.Distance`, `Abs`, `Min`,
  `Max`, `Project`; `Quat.FromEuler`, `Euler`, `LookAt`, `AxisAngle`
  the other way (angle and axis out); `Mat4.Mat3` for normal matrices;
  `Mat4.Translation`, `Decompose`.

### Naming and consistency

- Text drawing has three sizes of the same call: `DrawText`,
  `DrawTextBlock` and `DrawTextSized`, where only the last takes a size
  and angle and only works with SDF fonts. `TextOptions` should carry
  `Size`, `Angle` and a scale for every font kind, with the bitmap path
  scaling its quads, and the family collapses to `DrawText(f, text, x,
  y, c)` and `DrawTextBlock(f, text, x, y, opts, c)`. `Font.Measure`,
  `MeasureSized` and `MeasureBlock` collapse to `Measure(text, opts)`.
- Decoders take different inputs: `audio.Decode([]byte)`,
  `DecodeWAV([]byte)`, `DecodeOGG(io.Reader)`, `DecodeMP3(io.Reader)`,
  `gfx.DecodeHDR(io.Reader)`, `tracker.Load([]byte)`, `gltf.Parse([]byte)`.
  Pick one shape (an `io.Reader` with a `[]byte` convenience, or the
  reverse) across packages.
- `input.State.Mouse`, `MouseDelta` and `Scroll` return `float64` while
  every other view-unit value in the engine is `float32`; returning
  `lin.Vec2` would match `Sprite.Pos` and the transform stack.
- `ecs.Each` and `Each2` exist but `Each3` and `Each4` do not, while
  `NewQuery1` through `NewQuery4` do; either finish the set or point
  one-off iteration at the queries.
- `phys` names its dimensions by suffix (`Body2`, `Body3`, `Box2`,
  `Box3`) but its round shapes by word (`Circle`, `Sphere`); a
  `Circle`/`Sphere` pair is fine, though `Polygon2` with no `Polygon3`
  and `Trigger2`/`Trigger3` events show the scheme is carried by habit
  rather than rule. Worth writing the rule down in the package comment.
- The "zero means default" convention (`Roughness` 0 is 0.6, `IOR` 0 is
  1.5, `SheenRoughness` 0 is 0.5, `Sky.Vacuum` inverted so 0 is a full
  sky) is used throughout but documented field by field. One paragraph
  in the `gfx` package comment stating the rule, and that a field whose
  zero must be expressible is named for its zero (`Vacuum`, `NoVSync`,
  `NoMipmaps`), would make new fields predictable.

### Engine plumbing in public types

Several methods exist only so the engine loop and platform layer can
call them, but sit on the public types beside the game-facing API:
`gfx.New` (whose parameter is an internal type no game can construct),
`Graphics.Begin`, `End`, `Resize`, `SetTime` and `Destroy`,
`input.State.Feed*`, `EndUpdate`, `EndFrame` and `SetDrawing`, and
`audio.Mixer.Mix`. Options, from cheapest to cleanest: group them under
an "Engine plumbing" heading in each package's documentation; move the
feeders onto a separate `input.Feeder` type that wraps the state; or
register the constructors and feeders into an internal hook package
from `init` so they can be unexported. The first should happen now; the
last is worth doing before a 1.0.

### Conveniences games write every time

- One-call loaders: a texture, font, sound or model from an `asset.FS`
  name or a path, in the style of `gfx.LoadTexture(fs, name, opts)`,
  rather than open, read, decode, upload by hand in every project.
- `asset.FS` sources from an `fs.FS`, so `go:embed` directories work
  alongside loose files and packs.
- Debug text with the engine's own overlay font, `Graphics.DebugText(x,
  y, text)` or `ctx.Debugf`, so a game can print a value on screen in
  one line without loading a font first.
- Sub-textures as values: a `gfx.Region{Tex, UV0, UV1}` or named
  `Sheet` frames accepted wherever a `*Texture` is, so atlases exported
  by tools drop in without carrying UV pairs around.
- `Sound.Duration()` in seconds beside `Frames()`; `Voice.Position()`
  and `Seek()`; `Music.Duration()`.
- `Font.Metrics()` (ascent, descent, line gap) so text can be aligned
  on a baseline, and a way to draw text at a baseline rather than only
  a top-left corner.
- `KeyRepeated(k)` for menus and fields that should honour the system
  key-repeat rate, since the repeat flag already arrives in `FeedKey`.
- Public headless mode (`bunyip.RunHeadless` or a `Config.Headless`)
  for screenshot tests in CI and for dedicated servers; the test suite
  has one privately.

## Platforms and window

- Web (WebAssembly) and mobile (Android, iOS). Both need a second
  graphics backend, since Vulkan is unavailable there; WebGPU is the
  plausible common target. The largest single gap.
- Windows and Linux have been cross-compiled and vetted but never run on
  real machines. Native Wayland is not written (XWayland works meanwhile).
- Logical resolution and scaling policy: a fixed design resolution
  scaled to fit, integer-scaled, or letterboxed, chosen in `Config`,
  with `SetView` becoming the exception rather than the rule.
- Window control at runtime: title, icon, position, minimum and maximum
  size, decorations, always on top, transparent and click-through
  windows, choice of monitor, starting fullscreen or unfocused.
- Close interception (`Closing` so a game can ask about unsaved work),
  focus state and focus events, and a way to keep or stop updating when
  unfocused.
- Cursor visibility and shape, and custom cursor images; only capture
  exists today.
- Clipboard read and write, drag and drop of files onto the window,
  opening a URL in the browser, native file dialogs.
- Multiple windows.
- Windows IME and X input methods; only macOS composes text natively.
- App bundling for Windows and Linux (installers, AppImage); code signing.

## Game loop

- An interpolation factor between fixed updates (`Context.Alpha`) so
  draws can blend the previous and current state when the display rate
  differs from the update rate; today motion steps at the fixed rate.
- A frame-time cap and maximum catch-up steps in `Config`, so a stalled
  frame does not spiral.
- Pausing the loop (and the mixer) when the window is hidden or
  minimised, as an option.

## Input

- Touch input with multi-touch points and the usual gestures.
- Gamepad rumble; connect and disconnect events; controller names and a
  mapping database so unusual controllers land on the standard layout;
  the current mapping is unverified against real hardware.
- Input action maps: named actions bound to keys, buttons and axes,
  with dead zones, rebinding at runtime and a serialisable binding set.
- Held-key durations and a list of keys currently down, for rebinding
  screens and cheat codes.
- Double-click detection.
- Key names in the user's layout (what is printed on the key) beside the
  positional `Key.String`, for on-screen prompts.

## 2D drawing

- Texture updates: write a region of pixels into an existing texture
  (video, procedural textures, painting), create a blank texture of a
  size, and read pixels back from a render texture. Textures are
  immutable after upload today.
- Colour matrices per draw (hue, saturation, brightness, invert) as a
  built-in rather than a five-line shader.
- Sprite flip flags, and per-draw filtering (nearest or linear) rather
  than only per texture.
- Gradient fills (linear, radial) for paths and rectangles; dashed
  strokes; stroke-along-path text.
- Indexed triangle draws (`DrawIndexed`) for large 2D meshes and
  particle systems.
- Particle systems as a first-class feature: emitters, curves over
  lifetime, GPU-instanced, with a small editor in the gallery.
- Lit 2D: normal-mapped sprites with point lights, 2D shadows.
- Tilemaps: layers, per-tile flip and rotation, a collision layer,
  animated tiles, and Tiled (TMX/TSX) import. Atlas formats from
  TexturePacker and Aseprite for sheets with named frames.
- Nine-slice with tiled (not stretched) edges.
- Shader hot reload during development: rerun `bunyip-shader` on save
  and swap the pipelines. Compiling GLSL at runtime would need a
  pure-Go compiler and is out of scope; the offline tool is the design.
- Sprite batching statistics and a draw-call budget in the debug overlay.

## Text

- Colour and bitmap emoji glyphs (drawn as notdef today).
- Baseline-relative drawing and font metrics (see conveniences).
- Text on a path, letter spacing, justification.
- Rich text: inline colours, bold runs and links in one block.
- Hyphenation.

## 3D rendering

- An orthographic camera for isometric and strategy views; `Camera` is
  perspective only.
- A world-to-screen projection helper (`Graphics.Project`) for labels
  and health bars; the space example projects by hand. Billboarded
  sprites in 3D for the same jobs.
- Debug drawing in 3D: lines, wire boxes, wire spheres, axes and
  frustums, for physics and camera work.
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

- Buses (music, effects, voice, ambience) with their own volume and
  pause, so a settings screen has something to bind to; today there is
  one master volume.
- Playback position, seek and rewind on voices and music; a callback or
  event when a voice ends.
- Pause everything on focus loss, as an option.
- Sound loading from `asset.FS` in one call.
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
- Widgets: radio buttons, integer sliders and spinners, list boxes with
  selection, colour pickers for tools, images and icons in buttons.
- Accessibility: screen-reader names; high-contrast is a palette only.
- Rich text in labels.

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
- Tweening of vectors and colours in `tween` itself (today only
  `float32`; `anim` curves cover the rest).

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
- Screenshot-comparison tests for examples, on the public headless mode.
- Fuzzing of loaders (glTF, WAV, MOD, S3M, XM, IT, HDR, PNG paths).
- A public API stability policy once the engine tags 1.0, after the
  polish above has landed.
