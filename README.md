# Bunyip

A game engine in Go for real-time and turn-based games: roguelikes, 4X, and
anything else that wants 2D sprites and 3D models on the same screen.

- Vulkan rendering through a generated, cgo-free binding (MoltenVK on macOS).
- Native window, input and audio layers per platform; no SDL, no GLFW.
- `CGO_ENABLED=0` everywhere. Native libraries are opened at runtime.
- Immediate-mode, themeable UI.

macOS is the first target; Linux and Windows follow.

## Layout

| Path | What |
|---|---|
| `internal/vk` | Generated Vulkan binding plus hand-written loader |
| `cmd/vkgen` | Generator; run `go generate ./internal/vk` after updating `third_party/vulkan/vk.xml` |
| `cmd/bunyip-info` | Reports the graphics stack without opening a window |

## Requirements

macOS: the Vulkan loader and MoltenVK from Homebrew (`brew install vulkan-loader molten-vk`),
or the LunarG Vulkan SDK. Set `BUNYIP_VULKAN_LIBRARY` to point at a specific library.

## Developing

```
go generate ./internal/vk
go test ./...
go run ./cmd/bunyip-info
```
