# Bunyip

A game engine in Go for real-time and turn-based games: roguelikes, 4X, and
anything else that wants 2D sprites and 3D models on the same screen.

- Vulkan rendering through a generated, cgo-free binding (MoltenVK on macOS).
- Native window, input and audio layers per platform; no SDL, no GLFW.
- `CGO_ENABLED=0` everywhere. Native libraries are opened at runtime with purego.
- Physically based rendering with cascaded shadow maps, SSAO, bloom, FXAA and
  tone mapping; skeletal animation; automatic instancing; render textures;
  picking. Sprites, tilemaps, cameras and scalable SDF text on top.
- Immediate-mode, themeable UI with scroll areas, drop-downs, tooltips and
  keyboard or gamepad navigation. glTF 2.0 models. Fullscreen, cursor
  capture, gamepads, IME text input.
- Audio mixer with streamed WAV, Ogg Vorbis and MP3, positional voices,
  reverb and filters, priorities, and a tracker player for MOD, S3M, XM and IT.
- Game services: an archetype-based entity component system with systems,
  resources and events; assets and packs with async loading and hot reload,
  saves and settings, seeded RNG, timers and tweens, grids with pathfinding
  and field of view, TCP and UDP messaging.
- A fixed view scaled into the window (fit, integer or stretch), an
  interpolation factor for smooth motion, window title, icon, cursor and
  clipboard control, and headless runs for tests and screenshots.
- Two loop modes: fixed-timestep real time, or turn-based where the process
  sleeps in the OS until input (or `Context.Wake`) arrives. A frame-timing
  overlay on F3 and optional pprof.

macOS is the tested target. Windows (Win32, WASAPI, XInput) and Linux (X11
through xcb, ALSA, joystick devices) layers exist behind the same
`internal/platform` and `internal/audioout` interfaces; they cross-compile
and vet but have not yet run on hardware. Wayland desktops use XWayland.

## Documentation

Guides (including a step-by-step Tetris) and the API reference for every
package, with runnable examples and symbol search, are published at
https://matjam.github.io/bunyip/ by the Docs workflow on each push to
`main`. The site is built by `go run ./cmd/bunyip-docs -out site` from
the Markdown in `docs/guides` and the packages' godoc, so `go doc ./gfx`
shows the same text and `go test ./...` runs every example that prints
output.

## Packages

| Package | What |
|---|---|
| `bunyip` | `Run`, `Config`, `Game`, `Context`: the loop and everything a game touches |
| `gfx` | textures, sprites, text, meshes, materials, camera, light, models |
| `ui` | immediate-mode widgets with a `Theme` |
| `audio` | mixer, voices, streams; WAV, Ogg Vorbis and MP3 decoding; tone synthesis |
| `audio/tracker` | MOD, S3M, XM and IT loader and player |
| `input` | key codes, modifiers, mouse buttons, gamepads, per-update `State` |
| `gltf` | glTF 2.0 loader (no GPU dependency) |
| `lin` | vectors, matrices, quaternions |
| `ecs` | archetype-based entity component system: queries, systems, resources, events, hierarchy |
| `anim` | keyframe curves and clips for 2D sprites, 3D transforms and any component field; players with crossfades, flipbooks, skeletons |
| `phys` | 2D and 3D rigid bodies: circles, boxes, polygons, spheres; impulse solver with friction and restitution; triggers, layers, raycasts |
| `orbit`, `orbit/sol` | celestial mechanics for any star system: orbital elements, exact two-body propagation, N-body leapfrog, ships under thrust; real-world constants |
| `asset` | files from directories and pack files, async loading, hot reload |
| `save` | JSON saves and settings in the platform's data directory |
| `rng` | seeded PCG32 with forks, dice, picks and shuffles |
| `timer`, `tween` | game-time timers; eased value animation |
| `grid` | cell grids, A*, Dijkstra maps, lines, field of view, flood fill |
| `network` | typed messages over TCP (ordered) and UDP (fast) |
| `internal/vk` | generated Vulkan binding plus hand-written loader |
| `internal/render` | Vulkan backend: device, swapchain, frames in flight, pipelines, uploads, readback |
| `internal/platform` | per-OS window, events, surface creation |
| `internal/audioout` | per-OS audio output |

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

Every example takes `-seconds N` and `-shot file.png`, so a run is
self-verifying without anyone watching the screen.

| Command | Shows |
|---|---|
| `go run ./examples/sprites` | 300 tinted, rotating, alpha-blended sprites |
| `go run ./examples/viewer [-model file.glb]` | lit 3D scene or a glTF model, orbit camera, sprite overlay |
| `go run ./examples/roguelike` | turn-based dungeon crawl with line of sight |
| `go run ./examples/gallery [-skin] [-theme nord] [-debug]` | every UI widget, the built-in themes, a texture skin, audio beep, frame-timing overlay |
| `go run ./examples/tiles` | sprite sheet, tilemap with culling, following Camera2D (zoom, rotate), walking animation, layers, timers and tweens, nine-slice HUD with wrapped text |
| `go run ./examples/audio [-music file.ogg]` | positional voices, reverb and low-pass sliders, fades, pitch, voice priorities, a synthesised Stream and streamed music files |
| `go run ./examples/solar` | the ECS driving a scene: hierarchy, orbit and spin systems, instanced asteroid belt, click picking, render-texture minimap, profile scopes |
| `go run ./examples/lighting [-model file.glb] [-env panorama.png]` | skinned meshes bent by joint matrices, cascaded shadows, point lights, a procedural sky with a slider that climbs to orbit, image-based lighting from a panorama, every post-processing setting on a slider, glTF animation clips |
| `go run ./examples/pathfinding` | A*, Dijkstra maps, field of view, flood fill and lines on a paintable grid; save and load through the save package |
| `go run ./examples/network -listen :7777` / `-join host:7777` | chat over TCP and pointer positions over UDP, turn-based with wake-ups on traffic |
| `go run ./examples/assets` | asset directory plus pack file, async loading with a progress bar, hot reload of changed files, persistent settings |
| `go run ./examples/inputs` | keys, mouse, wheel, cursor capture, fullscreen, typed text with IME composition, gamepads |
| `go run ./examples/animation` | keyframe clips on 2D sprites and 3D transforms, a flipbook walker, crossfades between a hero's clips, Finished events chaining back to idle |
| `go run ./examples/physics3d` | five hundred cubes of plastic, metal, gold, car paint, velvet, glass and glowing materials dropped into a pile, with a raycast highlighting the one under the pointer |
| `go run ./examples/physics2d` | balls, boxes and triangles in a pit with a ramp, a kinematic paddle, a trigger zone and a raycast |
| `go run ./examples/space` | a ship under thrust in a fictional star system: seven Kepler planets with moons, an asteroid belt and a comet, N-body gravity, orbit rings, predicted path, focus cycling, time warp |
| `go run ./examples/tetris` | the complete game the Tetris guide builds on the ECS: systems, resources, events, timers, tweens, UI panel, synthesised sounds |
| `go run ./examples/materials [-env panorama.hdr]` | every material feature on a row of spheres: metal, clearcoat, sheen, subsurface, vertex colours, unlit, refracting glass with absorption; alpha-cutout leaves with cutout shadows, a scrolling texture transform, a projected decal, an outline, an x-ray tint through a wall |
| `go run ./examples/tiled` | a map from the Tiled editor: layers, an external tileset, flipped and rotated tiles, an animated pond, object outlines |
| `go run ./examples/particles` | a campfire of fire and smoke, rain, sparks on click and confetti on Space from the particle package, with a tuning panel |
| `go run ./examples/shaders` | fragment shaders written by the game: a wave and a dissolve on sprites, a lava surface shader under the engine's lighting, blend modes, a sheared sprite |
| `go run ./examples/vector` | paths filled under both rules, curves and arcs, every cap and join, textured fills, all seven blend modes, the transform stack, anti-aliased |
| `go run ./examples/text [-font file.ttf]` | HarfBuzz-shaped text: kerning and ligatures, Arabic joining, right-to-left and mixed lines, a fallback font, Unicode wrapping, vertical text, distance-field text |
| `go run ./cmd/bunyip-docs -out site` | renders the documentation site (guides plus API reference) |
| `go run ./cmd/bunyip-info` | the Vulkan stack, without a window |
| `go run ./cmd/bunyip-play song.xm` | plays a WAV, Ogg, MP3, MOD, S3M, XM or IT file; `-dump out.wav` records what the device received |
| `go run ./cmd/bunyip-pack -o assets.pak assets/` | bundles an asset directory into a pack file |
| `go run ./cmd/bunyip-shader [-kind mesh] -o out.spv in.glsl` | compiles a game's sprite or mesh shader against the engine's prelude |
| `go run ./cmd/bunyip-bundle -name "Game" -exe ./game -assets assets/` | makes a macOS .app with MoltenVK inside, or a folder elsewhere |

## Requirements

macOS: the Vulkan loader and MoltenVK (`brew install vulkan-loader molten-vk`),
or the LunarG Vulkan SDK. Optional: `brew install vulkan-validationlayers`
for validation in tests and examples. Set `BUNYIP_VULKAN_LIBRARY` to point at
a specific library.

Linux: a Vulkan driver, `libxcb`, and `libasound`; `libxkbcommon` and
`libxkbcommon-x11` for text input. Windows: a Vulkan driver (`vulkan-1.dll`
ships with GPU drivers).

## Developing

```
go generate ./internal/vk        # after updating third_party/vulkan/vk.xml
go generate ./gfx/shaders ./internal/render/shaders   # needs glslangValidator
go test ./...                    # headless GPU tests skip without a driver
go vet ./...
```

Renderer tests run on a headless surface, read the swapchain back and check
pixels, so correctness never depends on a person looking at a window.
