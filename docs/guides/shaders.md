---
title: Shaders
order: 9
summary: fragment shaders written by the game, for sprites and for mesh surfaces
---

Bunyip compiles its GLSL to SPIR-V offline and embeds it, and a game does
the same. You write only the part that varies; the engine's prelude
provides the textures, the uniforms and the lighting, and `bunyip-shader`
puts them together and runs glslangValidator (from the Vulkan SDK or the
glslang package, which must be on your PATH).

## A sprite shader

A sprite shader colours 2D drawing: sprites, text, tilemaps, paths. It
defines one function and can declare one uniform block:

```glsl
// wave.glsl
UNIFORMS uniform Params {
	float amplitude;
	float frequency;
} u;

vec4 fragment(vec2 uv, vec4 color) {
	uv.x += sin(uv.y * u.frequency + time() * 3.0) * u.amplitude;
	return texture(tex, uv) * color;
}
```

`tex` is the texture being drawn (white for plain rectangles), `color`
is the premultiplied tint, and the result is a premultiplied colour.
`image0` to `image3` are the shader's own images, `time()` is the game
clock, `viewSize()` and `pixelScale()` describe the view, and
`position()` is the fragment's position in view units.

Compile it beside the source and embed the result:

```go
//go:generate go run github.com/matjam/bunyip/cmd/bunyip-shader -o wave.spv wave.glsl

//go:embed wave.spv
var waveSPV []byte
```

Then in the game:

```go
wave, err := ctx.Gfx.NewShader(waveSPV)
wave.SetImage(0, noise)
...
wave.SetUniforms(struct{ Amplitude, Frequency float32 }{0.03, 24})
ctx.Gfx.Shaded(wave, func() {
	ctx.Gfx.Draw(tex, sprite)
})
```

`Shaded` sets the shader for the draws inside the closure; `SetShader`
sets it as state, and nil restores the default. Each draw keeps the
uniform values and images that were set when it was queued, so changing
them between draws is fine and cheap.

Uniforms are a struct copied byte for byte, laid out by std140 rules:
`float32`, `int32`, `lin.Vec2`, `lin.Vec4` and `lin.Mat4` fields line up
as you would expect; a `lin.Vec3` takes sixteen bytes, so follow it with
a `float32` of padding; a block is at most 1024 bytes.

## A mesh shader

A mesh shader adjusts the surface the engine lights, and can post-process
the lit result:

```glsl
// lava.glsl
UNIFORMS uniform Params { float heat; } u;

void surface(inout Surface s) {
	float n = texture(image0, s.worldPos.xz * 0.4).r;
	s.roughness = mix(0.95, 0.4, n);
	s.emissive += vec3(1.0, 0.35, 0.05) * smoothstep(0.45, 0.55, n) * u.heat;
}

vec4 finish(vec4 lit, Surface s) { return lit; } // optional
```

`Surface` has `albedo`, `alpha`, `normal` (world space), `metallic`,
`roughness`, `emissive`, `occlusion`, `unlit`, `uv`, `worldPos` and
`viewDir`, filled in from the material's textures and factors before
`surface` runs. Everything else is the standard pipeline: the shadowed
directional light, point lights, hemisphere ambient, alpha cutout,
bloom and anti-aliasing. Compile with `-kind mesh`, create it with
`NewMeshShader`, and set it on a material:

```go
lava, err := ctx.Gfx.NewMeshShader(lavaSPV)
lava.SetImage(0, noise)
lava.SetUniforms(struct{ Heat float32 }{1})
ctx.Gfx.DrawMesh(slab, gfx.Material{Shader: lava}, model)
```

## Moving vertices

A mesh shader may also define a vertex hook, which runs before the
model matrix in object space (after skinning, for skinned meshes):

```glsl
void vertex(inout VertexData v) {
	v.position.z += sin(v.uv.x * 6.0 - time() * 4.0) * 0.15 * v.uv.x;
}
```

`VertexData` has `position`, `normal` and `uv`; `model()` is the
instance's matrix and the material's textures and the shader's images
can be sampled for displacement maps. The hook runs in the shadow pass
too, so displaced geometry casts the right shadow. When a source has a
vertex hook, `bunyip-shader` writes a bundle of all five programs
(fragment; static and skinned vertex, lit and shadow) to the one output
file, and `NewMeshShader` reads either that or plain fragment SPIR-V.
The `shaders` example's flag ripples this way.

## Material features

Beyond textures and factors, `Material` carries `AlphaCutoff` (hard-edged
cutouts that also cut their shadows), `OcclusionTexture` with
`OcclusionStrength` (baked ambient occlusion), `Unlit` (base colour and
emissive without lighting), `DoubleSided` (back faces lit with a flipped
normal), and `NoDepthTest` and `NoDepthWrite` for overlays and effects.
The glTF loader fills all of these from a file's materials, including
the alpha mode, the unlit and emissive-strength extensions.

## Blend modes and transforms

Blending is separate from shaders and works with any of them:

```go
ctx.Gfx.Blended(gfx.BlendAdd, func() { ... })   // glows and fire
ctx.Gfx.SetBlend(gfx.BlendMultiply)             // as state
```

`BlendAlpha` is the default; `BlendAdd`, `BlendMultiply`, `BlendScreen`,
`BlendLighten`, `BlendDarken`, `BlendReplace` and `BlendErase` are the
others, all on premultiplied colour.

A 2D transform stack maps everything drawn through it:

```go
ctx.Gfx.Transformed(lin.Translate2(x, y).Mul(lin.Rotate2(a)).Mul(lin.Shear2(0.3, 0)), func() {
	ctx.Gfx.Draw(tex, sprite)
	ctx.Gfx.FillPath(&p, c, gfx.FillOptions{})
})
```

`lin.Affine` composes with `Mul` (the right-hand transform applies
first) and inverts with `Inverse`.

## Reading compiler errors

glslangValidator reports line numbers of the composed file, which begin
with the prelude. `bunyip-shader` prints the offset to subtract; `-print`
writes the composed GLSL to standard output so you can read the whole
thing. The `shaders` example has three working shaders to start from.
