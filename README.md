# Bunyip

A game engine in Go for real-time and turn-based games: roguelikes, 4X, and
anything else that wants 2D sprites and 3D models on the same screen.

- Vulkan rendering through a generated, cgo-free binding (MoltenVK on macOS).
- Native window, input and audio layers per platform; no SDL, no GLFW.
- `CGO_ENABLED=0` everywhere. Native libraries are opened at runtime with purego.
- Physically based rendering with shadow maps, bloom and tone mapping; sprites and scalable SDF text on top.
- Immediate-mode, themeable UI. glTF 2.0 models. Fullscreen, cursor capture, gamepads.
- Audio mixer with WAV, Ogg Vorbis, MP3, and a tracker player for MOD, S3M, XM and IT.
- Two loop modes: fixed-timestep real time, or turn-based where the process
  sleeps in the OS until input arrives.

macOS is the first target; Linux and Windows follow behind the same
`internal/platform` and `internal/audioout` interfaces.

## Packages

| Package | What |
|---|---|
| `bunyip` | `Run`, `Config`, `Game`, `Context`: the loop and everything a game touches |
| `gfx` | textures, sprites, text, meshes, materials, camera, light, models |
| `ui` | immediate-mode widgets with a `Theme` |
| `audio` | mixer, voices, streams; WAV, Ogg Vorbis and MP3 decoding; tone synthesis |
| `audio/tracker` | MOD, S3M, XM and IT loader and player |
| `input` | key codes, modifiers, mouse buttons, per-update `State` |
| `gltf` | glTF 2.0 loader (no GPU dependency) |
| `lin` | vectors, matrices, quaternions |
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
| `go run ./examples/gallery [-beep]` | every UI widget, theme switch, audio beep |
| `go run ./cmd/bunyip-info` | the Vulkan stack, without a window |
| `go run ./cmd/bunyip-play song.xm` | plays a WAV, Ogg, MP3, MOD, S3M, XM or IT file; `-dump out.wav` records what the device received |

## Requirements

macOS: the Vulkan loader and MoltenVK (`brew install vulkan-loader molten-vk`),
or the LunarG Vulkan SDK. Optional: `brew install vulkan-validationlayers`
for validation in tests and examples. Set `BUNYIP_VULKAN_LIBRARY` to point at
a specific library.

## Developing

```
go generate ./internal/vk        # after updating third_party/vulkan/vk.xml
go generate ./gfx/shaders ./internal/render/shaders   # needs glslangValidator
go test ./...                    # headless GPU tests skip without a driver
go vet ./...
```

Renderer tests run on a headless surface, read the swapchain back and check
pixels, so correctness never depends on a person looking at a window.
