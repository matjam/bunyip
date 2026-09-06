---
title: Shaders
group: Graphics
order: 3
summary: WGSL authoring, native Go compilation, engine hooks, uniforms and texture bindings
---

Bunyip shaders are written in **WGSL** and compiled to **SPIR-V** by
[gogpu/naga](https://github.com/gogpu/naga), a native Go dependency. Both runtime
compilation and the offline tool work with `CGO_ENABLED=0`; neither needs a
compiler executable, shared compiler library or Vulkan SDK to compile source.
Rendering still requires Bunyip's Vulkan runtime.

## Language and execution model

WGSL provides vectors, matrices, functions, loops, structures and GPU texture
operations. See the [WGSL specification](https://www.w3.org/TR/WGSL/) for syntax.
Bunyip adds ordinary declarations, helper functions and generated entry points.
These helpers do not create a separate language.

Game sources define a sprite `fragment` function or a mesh `surface` function.
The engine supplies the stage entry point; do not declare your own `@vertex`
or `@fragment` entry point in a composed game source. Complete modules with
explicit entry points can be compiled through `Compiler.CompileRaw` or
`bunyip-shader -raw`, but must match their pipeline's resource interface.

There is no GLSL preprocessor, `#version`, `UNIFORMS` macro or implicit numeric
conversion contract. Write WGSL declarations directly. The renderer exposes
vertex and fragment pipelines; compiling compute or other unsupported pipeline
interfaces does not provide an engine dispatch API.

Bunyip targets Vulkan through SPIR-V 1.3. Built-in shaders use the compiler's
`var<push_constant>` extension to preserve Vulkan push-data layouts. This is
an implementation choice, not a promise that the source runs unchanged in
WebGPU. Optional language/compiler features must also be supported by the
renderer and GPU. The engine does not enable arbitrary shader capabilities.

The pinned Naga compiler does not yet implement every WGSL feature. In
particular, Bunyip rejects module-level private variables with initializers
because this compiler version can silently drop their initialization. Use
`const` for fixed values, or assign a private variable in your hook or entry
point before reading it. Private variables without an explicit initializer
remain supported. Compiler errors leave an existing shader unchanged.

## Runtime compilation

```go
shader, err := ctx.Gfx.CompileShader(context.Background(), source)
if err != nil {
    return err
}
ctx.Gfx.Shaded(shader, func() {
    ctx.Gfx.Draw(texture, sprite)
})
```

`CompileMeshShader` creates a mesh shader from source. `Shader.ReloadSource`
recompiles the existing shader kind while keeping its uniforms and images.
Compile or pipeline-creation failures leave the previous shader intact.

These methods run synchronously on the game goroutine, where GPU resources
belong. Compile during loading or in a worker, rather than on every draw.
For background work, compile only the CPU representation:

```go
compiler := shaders.Compiler{}
data, err := compiler.Compile(context.Background(), shaders.Sprite, source)
if err != nil {
    return err
}
// Transfer data to the game goroutine before creating the GPU resource.
shader, err := ctx.Gfx.NewShader(data)
```

`Compiler` has useful zero defaults and can compile concurrently. Context
cancellation is checked between phases; it cannot interrupt a phase already
executing or Vulkan pipeline creation. No background workers are retained
after a call. Keep compiled bytes if you want a game-specific cache.

The creating `Graphics` owns the resulting shader. Optional early
`Destroy` retires GPU resources safely. Shaders and textures belong to one
`Graphics`; load separate resources for another window.

## Sprite shaders

```wgsl
struct Params {
    amplitude: f32,
    frequency: f32,
};
@group(1) @binding(0) var<uniform> u: Params;

fn fragment(inputUV: vec2f, color: vec4f) -> vec4f {
    var uv = inputUV;
    uv.x += sin(uv.y * u.frequency + time() * 3.0) * u.amplitude;
    return textureSample(tex, texSampler, uv) * color;
}
```

The input UV selects the sprite's texture region. The color is premultiplied
tint; return premultiplied color. Sampling a color texture supplies the
engine's linear, premultiplied texels. Preserve alpha conventions when
replacing or mixing colors.

| Declaration/helper | Meaning |
|---|---|
| `tex`, `texSampler` | Current drawable texture and sampler; white for untextured shapes |
| `image0` through `image3` | Extra textures assigned with `Shader.SetImage` |
| `image0Sampler` through `image3Sampler` | Their filtering/addressing samplers |
| `time()` | Game time in seconds |
| `viewSize()` | Current view dimensions in view units |
| `pixelScale()` | Framebuffer pixels per view unit |
| `position()` | Fragment position in view units |

Sprite texture/sampler pairs occupy group 0 bindings 0/1, 2/3, 4/5,
6/7 and 8/9. User uniforms occupy group 1 binding 0. Engine entry-point
inputs, outputs and push data are reserved; do not redeclare them.

## Uniform values

```go
if err := shader.SetUniforms(struct {
    Amplitude float32
    Frequency float32
}{0.03, 24}); err != nil {
    return err
}
```

`SetUniforms` copies an exported Go struct or pointer to one into a
std140-compatible block. It matches fields by declaration order, not names.
There is no shader reflection or automatic type conversion.

| Go value | WGSL representation |
|---|---|
| `float32`, `int32`, `uint32` | `f32`, `i32`, `u32` |
| `bool` | `u32`, containing 0 or 1; test with `!= 0u` |
| `lin.Vec2/Vec3/Vec4`, `gfx.Color` | `vec2f/vec3f/vec4f`; Color is vec4f |
| `lin.Mat3/Mat4` | Column-major `mat3x3f/mat4x4f` |
| Fixed arrays/nested structs | Matching declarations with std140-compatible padding |

Scalar arrays have a 16-byte stride in the Go packer. Use padded WGSL
elements rather than assuming `array<f32,N>` has that stride:

```wgsl
struct Weight { @size(16) value: f32, };
struct Params { weights: array<Weight, 2>, };
@group(1) @binding(0) var<uniform> u: Params;
// Corresponds to Go: struct { Weights [2]float32 }
// Read the first weight as u.weights[0].value.
```

Nested records need matching 16-byte structure alignment and tail padding
where std140 requires it. A Go `[4]float32` is a padded scalar array, not a
`vec4f`; use `lin.Vec4` for a vector. Most game parameters are simplest as
scalars and vectors, with explicit vector arrays for repeated records.

Blocks must fit 1024 packed bytes. Unsupported types, unexported fields,
empty structures and oversized blocks return errors without changing the
previous values. Each queued draw retains its uniform and image snapshot.

## Mesh surface and vertex hooks

```wgsl
fn surface(input: Surface) -> Surface {
    var s = input;
    s.roughness = 0.4;
    s.emissive += vec3f(1.0, 0.2, 0.0);
    return s;
}
```

Create with `CompileMeshShader` and assign `Material.Shader`. The engine
fills a `Surface` from material factors and maps before calling your hook,
then performs lighting, fog and the normal render passes.

Surface fields include:

- Base appearance: `albedo: vec3f`, `alpha: f32`, `normal: vec3f`,
  `metallic`, `roughness`, `emissive: vec3f`, `occlusion`, `unlit: bool`.
- Geometry: `uv/uv2: vec2f`, `color: vec4f`, `worldPos/viewDir: vec3f`.
- Layered materials: `clearcoat`, `clearcoatRoughness`, `sheen: vec3f`,
  `sheenRoughness`, `subsurface`, `thickness`, `transmission`,
  `ior`, `volume`, `attenuation: vec3f`, `attenuationDistance`.
- Specular detail: `specular`, `specularColor: vec3f`, `iridescence`,
  `iridescenceIOR`, `iridescenceThickness`, `anisotropy`,
  `tangent: vec3f`, `shell`. Unannotated fields here are f32.

Normals, positions and tangent directions are world-space in the surface.
Iridescence thickness is in nanometres. `shell` ranges from the underlying
surface to the outer fur shell. Local `Surface.unlit` is a boolean; this
does not imply booleans can be stored directly in a uniform buffer.

An optional `fn finish(lit: vec4f, s: Surface) -> vec4f` adjusts the lit
result. An optional vertex hook transforms object-space geometry after
morph blending and before skinning:

```wgsl
fn vertex(input: VertexData) -> VertexData {
    var v = input;
    v.position.z += sin(v.uv.x * 6.0 - time() * 4.0) * 0.15 * v.uv.x;
    return v;
}
```

`VertexData` contains position, normal, UV, second UV and color.
The engine compiles static/skinned vertex variants for lit and shadow passes,
so displacement also changes shadows. Set `Shader.VertexBounds` to a
conservative displacement as a multiple of the mesh bounding radius;
zero disables culling for a shader with a vertex hook.

Mesh image helpers `sampleImage0(uv)` through `sampleImage3(uv)` sample
the textures set with `SetImage`. Mesh material bindings are engine-owned.
Material sampling helpers return `vec4f`: `albedoTex`, `metalRoughTex`,
`normalTex`, `emissiveTex`, `occlusionTex`, `thicknessTex`,
`transmissionTex`, `iridescenceTex`, `anisotropyTex`, `specularTex` and
`furTex`. Each accepts a `vec2f` UV. The engine normally samples these maps
while constructing `Surface`; call them directly for custom material logic.
The mesh user block is group 4 binding 0 and is shared across stages (sprite
user blocks use group 1 binding 0). Source hooks must
remain valid in each generated stage, including vertex stages when present.

## Offline compilation and loading

```go
//go:generate go run github.com/matjam/bunyip/cmd/bunyip-shader -o wave.spv wave.wgsl
//go:embed wave.spv
var waveSPV []byte
```

`Graphics.NewShader` loads sprite SPIR-V; `NewMeshShader` loads the mesh
bundle produced with `-kind mesh`. Mesh bundles contain regular and
order-independent transparency fragments plus vertex variants when needed.
Runtime-created bytes have the same format. `Shader.Reload` accepts newly
compiled bytes, useful with `asset.Watcher`.

The command accepts `-stage frag|oitfrag|vert|skinvert|shadowvert|shadowskinvert`
for individual generated programs. `-print` prints composed WGSL for reading
compiler diagnostics; `-raw` compiles a complete module without composition.
Errors include the stage and prefix line count. Keep the composed program
when diagnosing errors inside engine-provided code.

See the [shader example](../examples/shaders.html) for wave, dissolve,
lava and flag shaders, with uniform values controlled by the UI.
