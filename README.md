# Bunyip

A complete game engine in Go. It draws 2D sprites and physically based 3D
models in the same frame, and includes an entity component system,
physics, animation, celestial mechanics, interface toolkit, audio mixer,
asset pipeline, saves, translation and networking, all written for this
engine and documented in one voice. It is built for games that simulate
as much as they render: roguelikes, 4X strategy, space games, arcade
games. Pure Go, no cgo, and every example runs to a screenshot without a
window.

**How it is built**

- Pure Go, `CGO_ENABLED=0` everywhere. Vulkan is called through a
  generated binding and every native library is opened at run time with
  purego; MoltenVK provides Vulkan on macOS.
- Native window, input and audio layers per platform. No SDL, no GLFW.
- Two loop modes: a fixed timestep for real-time games, or turn-based,
  where the process sleeps in the operating system until input arrives.
- Every example runs to a screenshot without a window, and the renderer's
  tests render into offscreen images and read the pixels back.

**What it does**

- 2D: sprites, sheets and tilemaps, autotiling with blob, edge, dual-grid
  and Wang tilesets on square, hexagonal and isometric grids, Tiled maps
  and terrain sets, a camera with follow,
  clamp and shake that culls what it cannot see, sort keys within a
  layer, atlases with tag animations, Aseprite files read as they are
  saved, vector paths
  with gradients and dashes, particles, HarfBuzz-shaped text with colour
  glyphs (COLR, SVG and bitmap emoji), rich markup and hyphenation in
  thirteen languages, colour matrices, lit sprites with shadows cast
  from occluders, blend modes and
  game-written fragment shaders.
- 3D: physically based materials with clearcoat, sheen, subsurface and
  glass; glTF models with skeletal animation, blend spaces, IK and morph
  targets; cascaded shadows, spot and point lights with shadows, a
  procedural sky or image-based lighting, fog; instancing, frustum
  culling and levels of detail; billboards, decals, outlines and x-ray; dynamic meshes and
  terrain; SSAO, bloom, FXAA, tone mapping and colour grading; render
  textures and picking.
- Interface: immediate-mode widgets with themes and skins, from panels
  and windows to tables, trees, menus, modals, text editing, drag and
  drop, and keyboard or gamepad navigation, with an accessibility tree.
- Audio: a mixer with streamed WAV, Ogg Vorbis and MP3, positional
  voices with panning or a binaural head model, Doppler and occlusion,
  buses, reverb zones, microphone capture, and a tracker player for MOD,
  S3M, XM and IT.
- Simulation: an archetype entity component system with saving, prefabs,
  cloning and a JSON scene format; 2D and 3D rigid bodies with joints,
  ragdolls, character controllers and queries; cloth, volumetric soft
  bodies and 2D fluids on the same colliders; celestial mechanics for
  any star system.
- Services: assets and pack files with background loading and hot
  reload, saves and settings, translation with plural rules, seeded
  random numbers, timers and cutscene sequences, tweens, grids with
  pathfinding and field of view, and networking over TCP (with TLS) and
  UDP (with reliable channels, prediction and interpolation helpers).
- The window: a fixed view scaled into the window, fullscreen, cursor
  capture and images, the clipboard on every platform, pausing while
  unfocused or hidden, gamepads, IME text input, action maps with
  rebinding, a frame-timing overlay on F3, and optional pprof.
- Debugging: an in-game console with commands, variables, key bindings
  and the log, and panels for the frame timings, the GPU resources and
  post-processing, a world's entities and systems, the physics
  simulation, the mixer, the input devices and a game's own services.

macOS is the tested target. The Windows (Win32, WASAPI, XInput) and Linux
(Wayland or X11, ALSA, joystick devices) layers sit behind the same
`internal/platform` and `internal/audioout` interfaces. The Linux window
layer has run on real hardware, both Wayland and X11, and so have Linux
audio output and capture; the Linux gamepad layer, macOS capture and the
whole Windows layer cross-compile and vet but have not. On Linux the
window layer uses
Wayland where a compositor is running and falls back to X11 through xcb;
`BUNYIP_X11=1` forces X11.

## Documentation

Guides (including a step-by-step Tetris) and the API reference for every
package, with runnable examples and symbol search, are published at
https://matjam.github.io/bunyip/ by the Docs workflow on each push to
`main`. The site is built by `go run ./cmd/bunyip-docs -out site` from
the Markdown in `docs/guides` and the packages' godoc, so `go doc ./gfx`
shows the same text and `go test ./...` runs every example that prints
output. The guides are grouped: Start (introduction, getting started,
Tetris), Engine (the window, input, entities and systems, game
services), Graphics (2D graphics, 3D graphics, shaders, animation, the
interface), Simulation (physics, orbits) and Audio.

Every page is also published as Markdown for language models and other
tools: the guides at `guides/<name>.md`, each package at
`pkg/<package>.md`, an index at [llms.txt](https://matjam.github.io/bunyip/llms.txt)
and everything in one file at `llms-full.txt`. `CLAUDE.md` in the
repository is written for a model working on the engine or writing a game
with it.

## Packages

| Package | What |
|---|---|
| `bunyip` | `Run`, `Config`, `Game`, `Context`: the loop and the values a game uses |
| `gfx` | textures, sprites, paths, text, meshes, materials, cameras, lights, fog, culling, LOD, billboards, models, post-processing |
| `ui` | immediate-mode widgets, containers, menus and modals with a `Theme` |
| `console` | in-game debug console: commands, variables, log capture, and panels for the engine, graphics, entities, physics, audio, input and services |
| `particle` | CPU particle systems drawn through the sprite batch |
| `tiled` | maps from the Tiled editor in JSON or XML form, built into drawable levels |
| `audio` | mixer, voices, streams, microphone capture; WAV, Ogg Vorbis and MP3 decoding; tone synthesis |
| `audio/tracker` | MOD, S3M, XM and IT loader and player |
| `input` | key codes, modifiers, mouse buttons, gamepads, per-update `State`, action maps |
| `gltf` | glTF 2.0 loader (no GPU dependency) |
| `lin` | vectors, matrices, quaternions |
| `ecs` | archetype-based entity component system: queries, systems, resources, events, hierarchy, saves, prefabs, scene documents |
| `anim` | keyframe curves and clips for 2D sprites, 3D transforms and any component field; players with crossfades, flipbooks, skeletons with layers, events, root motion, IK and morph targets |
| `phys` | 2D and 3D rigid bodies: circles, boxes, polygons, capsules, edges and chains, spheres, hulls, meshes, compounds; impulse solver with friction and restitution; joints, sleeping, continuous collision, character controllers; triggers, layers, rays, overlaps, shape casts and signed distance to a placed shape |
| `phys/soft` | cloth, volumetric soft bodies and 2D fluids as particles: extended position-based dynamics, distance, bending, volume and density constraints, shape matching, wind, mesh helpers; collides with the static and kinematic `phys` colliders |
| `orbit`, `orbit/sol` | celestial mechanics for any star system: orbital elements, exact two-body propagation, N-body leapfrog, ships under thrust; real-world constants |
| `asset` | files from directories and pack files, one-call loaders, async loading, hot reload |
| `save` | JSON saves and settings in the platform's data directory |
| `rng` | seeded PCG32 with forks, dice, picks and shuffles |
| `timer`, `tween` | game-time timers and step sequences for cutscenes; eased value animation |
| `locale` | string tables with placeholders, plural rules and fallbacks |
| `grid` | cell grids, A*, Dijkstra maps, lines, field of view, flood fill |
| `network` | typed messages over TCP (ordered, optionally TLS) and UDP (fast, with reliable-ordered channels); interpolation, prediction, lag compensation, clock sync, snapshot deltas and interest management |
| `internal/vk` | generated Vulkan binding plus hand-written loader |
| `internal/render` | Vulkan backend: device, swapchain, frames in flight, pipelines, uploads, readback |
| `internal/platform` | per-OS window, events, surface creation |
| `internal/audioout` | per-OS audio output and microphone input |

## A game

```go
type game struct{ tex *gfx.Texture }

func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	g.tex, err = ctx.Gfx.NewTexture(img, gfx.TextureOptions{})
	return err
}

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) {
		ctx.Quit()
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	ctx.Gfx.DrawTexture(g.tex, 100, 100)
	return nil
}

func main() {
	bunyip.Run(bunyip.Config{Title: "Hello", Width: 1280, Height: 720}, &game{})
}
```

## Examples

Every example takes `-seconds N` and `-shot file.png`, so a run verifies
itself without a person watching the screen.

Each one has a walkthrough on the documentation site that quotes the
whole program and explains it section by section:
[matjam.github.io/bunyip/examples/](https://matjam.github.io/bunyip/examples/index.html).

| Command | Shows |
|---|---|
| `go run ./examples/sprites` | 300 tinted, rotating, alpha-blended sprites, and a lit brick floor where a moving lamp throws shadows from three crates |
| `go run ./examples/viewer [-model file.glb]` | lit 3D scene or a glTF model, orbit camera, sprite overlay |
| `go run ./examples/window` | the platform layer's smoke test: a window, a swapchain of cleared frames, and every event printed as it arrives |
| `go run ./examples/clear` | the renderer's smoke test: a window cleared to a cycling colour, with `-shot` to check one frame's pixels |
| `go run ./examples/roguelike` | turn-based dungeon crawl with line of sight |
| `go run ./examples/gallery [-skin] [-theme nord] [-debug]` | every UI widget, the built-in themes, a texture skin, audio beep, frame-timing overlay, and the debug console on ` and F4 |
| `go run ./examples/tiles` | sprite sheet, tilemap with culling, following Camera2D (zoom, rotate), walking animation, layers, timers and tweens, nine-slice HUD with wrapped text |
| `go run ./examples/audio [-music file.ogg] [-zone] [-mic]` | positional voices with panning or the binaural head model, reverb and low-pass sliders, fades, pitch, voice priorities, a synthesised Stream, streamed music files and a microphone level meter |
| `go run ./examples/solar` | the ECS driving a scene: a scene document and a prefab loaded from embedded files, hierarchy, orbit and spin systems, instanced asteroid belt, click picking, render-texture minimap, profile scopes |
| `go run ./examples/lighting [-model file.glb] [-env panorama.png]` | skinned meshes bent by joint matrices, cascaded shadows, point lights, a procedural sky with a slider that raises the altitude to orbit, image-based lighting from a panorama, every post-processing setting on a slider, glTF animation clips |
| `go run ./examples/pathfinding` | A*, Dijkstra maps, field of view, flood fill and lines on a paintable grid; save and load through the save package |
| `go run ./examples/network -listen :7777` / `-join host:7777` | chat over TCP and pointer positions over UDP, turn-based with wake-ups on traffic; `-reliable` sends chat over reliable UDP and shows the link's round trip and loss |
| `go run ./examples/assets` | asset directory plus pack file, async loading with a progress bar, hot reload of changed files, persistent settings |
| `go run ./examples/inputs` | keys, mouse, wheel, cursor capture, fullscreen, typed text with IME composition, gamepads |
| `go run ./examples/animation` | keyframe clips on 2D sprites and 3D transforms, a flipbook walker, crossfades between a hero's clips, Finished events chaining back to idle, a robot arm firing an animation event and another reaching a target by two-bone IK |
| `go run ./examples/physics3d` | five hundred cubes of plastic, metal, gold, car paint, velvet, glass and glowing materials dropped into a pile, with a raycast highlighting the one under the pointer |
| `go run ./examples/physics2d` | balls, boxes and triangles in a pit with a ramp, a kinematic paddle, a car driving on sprung wheel joints, a trigger zone and a raycast |
| `go run ./examples/physics-lab` | capsules, hulls and spheres tumbling onto a mesh terrain, a hinge chain, a motorised paddle wheel, a ragdoll, a character controller climbing stairs, colliders and contacts drawn with debug lines, and the debug console driving the world |
| `go run ./examples/softbody` | a cloth flag flapping on a pole in a swinging gust, a jelly cube that drops and can be kicked beside a rigid crate, and a tank of 2D fluid breaking around a post |
| `go run ./examples/space` | a ship under thrust in a fictional star system: seven Kepler planets with moons, an asteroid belt and a comet, N-body gravity, orbit rings, predicted path, focus cycling, time warp |
| `go run ./examples/tetris` | the complete game the Tetris guide builds on the ECS: systems, resources, events, timers, tweens, UI panel, synthesised sounds |
| `go run ./examples/materials [-env panorama.hdr]` | every material feature on a row of spheres: metal, clearcoat, sheen, subsurface, vertex colours, unlit, refracting glass with absorption; alpha-cutout leaves with cutout shadows, a scrolling texture transform, a projected decal, an outline, an x-ray tint through a wall |
| `go run ./examples/tiled` | a map from the Tiled editor: layers, an external tileset, flipped and rotated tiles, an animated pond, object outlines |
| `go run ./examples/autotile` | paint terrain that picks its own tiles: a 47-tile blob set composed from a six-tile template, an edge-matched wall set, a corner Wang water set with curving shores, weighted flower variants, and a 64-tile hexagonal edge set on a staggered-row layout |
| `go run ./examples/particles` | a campfire of fire and smoke, rain, sparks on click and confetti on Space from the particle package, with a tuning panel |
| `go run ./examples/shaders` | fragment shaders written by the game: a wave and a dissolve on sprites, a lava surface shader under the engine's lighting, blend modes, a sheared sprite |
| `go run ./examples/vector` | paths filled under both rules, curves and arcs, every cap and join, textured fills, all seven blend modes, the transform stack, anti-aliased |
| `go run ./examples/text [-font file.ttf]` | HarfBuzz-shaped text: kerning and ligatures, Arabic joining, right-to-left and mixed lines, a fallback font, Unicode wrapping, hyphenation by language, rich markup, colour emoji, vertical text, distance-field text |
| `go run ./examples/terrain` | a heightfield with a lake, billboard trees, rocks at three levels of detail, campfires and a searchlight, distance and valley fog, labels in the world, frustum culling counts, and terrain dug with a click |
| `go run ./cmd/bunyip-docs -out site` | renders the documentation site (guides plus API reference) |
| `go run ./cmd/bunyip-info` | the Vulkan stack, without a window |
| `go run ./cmd/bunyip-play song.xm` | plays a WAV, Ogg, MP3, MOD, S3M, XM or IT file; `-dump out.wav` records what the device received |
| `go run ./cmd/bunyip-pack -o assets.pak assets/` | bundles an asset directory into a pack file |
| `go run ./cmd/bunyip-shader [-kind mesh] -o out.spv in.glsl` | compiles a game's sprite or mesh shader against the engine's prelude |
| `go run ./cmd/bunyip-bundle -name "Game" -exe ./game -assets assets/` | makes a macOS .app with MoltenVK inside, or a folder elsewhere |

## Requirements

Go 1.26 or later, and a Vulkan driver.

macOS: the Vulkan loader and MoltenVK (`brew install vulkan-loader molten-vk`),
or the LunarG Vulkan SDK. Optional: `brew install vulkan-validationlayers`
for validation in tests and examples. Set `BUNYIP_VULKAN_LIBRARY` to point at
a specific library.

Linux: a Vulkan driver, `libasound`, and either `libwayland-client` 1.20 or
later for a Wayland session or `libxcb` for an X11 one. `libxkbcommon` gives
text input on both, `libxkbcommon-x11` is needed for X11, `libxcb-xkb` gives
X11 detectable key repeat, and `libwayland-cursor` gives the pointer its
shapes under Wayland. Windows: a
Vulkan driver (`vulkan-1.dll` ships with GPU drivers).

## Developing

```
go generate ./internal/vk        # after updating third_party/vulkan/vk.xml
go generate ./gfx/shaders ./internal/render/shaders   # needs glslangValidator
go test ./...                    # headless GPU tests skip without a driver
go vet ./...
```

Renderer tests render into offscreen images, read the frame back and check
pixels; headless mode needs no window system and no surface extension, so
it works on any conformant Vulkan driver. `go test ./examples/` runs every example headless for a moment and
checks that it drew something; it needs a GPU and is skipped with `-short`.
The parsers have fuzz targets: `go test -fuzz=Fuzz ./audio/...` and the
same in `gltf`, `tiled` and `gfx`.
