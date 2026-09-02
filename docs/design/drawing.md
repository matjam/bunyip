# Design: user shaders, blending and vector paths, shaped text

This document specifies three additions to `gfx`: fragment shaders
written by the game, blend modes with anti-aliased vector paths and 2D
transforms, and text shaping through HarfBuzz. The guiding rules were:
plain Go API,
zero values that mean "the default", closures for scoped state (as the
UI package already does), and one drawing model for sprites, text and
shapes so every feature works with every other.

## 1. One 2D vertex stream

Sprites were instanced quads with their own vertex layout. Shapes,
strokes and custom shaders all want arbitrary triangles, so the 2D
path now emits plain vertices:

```
vertex2D { pos vec2; uv vec2; color vec4 }   // 32 bytes, premultiplied colour
```

A sprite becomes six vertices expanded on the CPU. Everything drawn
in 2D (sprites, text, tilemaps, nine-slices, paths, strokes) lands in
one ordered stream per queue and is sorted by layer, stable in call
order. Consecutive vertices with the same texture, shader, uniforms,
blend, clip and projection form one draw call.

### Transforms

`lin.Affine` is a 2×3 matrix: `lin.Translate2`, `Rotate2`, `Scale2`,
`Shear2`, `Mul`, `Apply`, `Inverse`. `Graphics` keeps a transform
stack for 2D drawing:

```go
g.Transformed(lin.Rotate2(a).Mul(lin.Shear2(0.3, 0)), func() {
	g.Draw(tex, sprite) // vertices pass through the stack
})
g.PushTransform(m); ...; g.PopTransform()
```

The stack composes with the sprite's own position, rotation and
origin, and with `Camera2D`. Text and paths go through it too.

### Blend modes

```go
type Blend uint8
const (
	BlendAlpha Blend = iota // source over, the default
	BlendAdd                // glow, fire, particles
	BlendMultiply           // shadows, tinting
	BlendScreen
	BlendLighten
	BlendDarken
	BlendReplace            // copy, no blending
	BlendErase              // cut holes: destination-out
)
g.SetBlend(BlendAdd)
g.Blended(BlendAdd, func() { ... })
```

Blending is fixed-function; colours are premultiplied throughout, so
these are the premultiplied factor tables. Each blend is a pipeline
variant created on first use.

## 2. Vector paths

```go
var p gfx.Path
p.MoveTo(10, 10).LineTo(90, 10).QuadTo(100, 50, 60, 90).Close()
p.Circle(50, 50, 20) // a second sub-path: with FillEvenOdd, a hole
g.FillPath(&p, gfx.RGB(255, 200, 80), gfx.FillOptions{Rule: gfx.FillEvenOdd})
g.StrokePath(&p, gfx.White, gfx.StrokeOptions{Width: 3, Join: gfx.JoinRound})
```

`Path` has `MoveTo`, `LineTo`, `QuadTo`, `CubicTo`, `ArcTo` (tangent
arc as in canvas), `Arc`, `Close`, `Reset`, and the shape helpers
`Rect`, `RoundRect`, `Circle`, `Ellipse`, `Polygon`. Methods return the
path for chaining. Convenience draws exist for the common cases:
`FillCircle`, `StrokeCircle`, `StrokeRect`, `StrokeLine`, `FillPolygon`.

`FillOptions`: `Rule` (`FillNonZero` default, `FillEvenOdd`),
`Texture` with `TextureOrigin` and `TextureSize` to map an image over
the path, `NoAntiAlias`. `StrokeOptions`: `Width` (default 1), `Cap`
(`CapButt`, `CapRound`, `CapSquare`), `Join` (`JoinMiter`, `JoinRound`,
`JoinBevel`), `MiterLimit` (default 4), `NoAntiAlias`.

Implementation, all on the CPU into the 2D stream: curves are
flattened adaptively; fills are decomposed by a scanline sweep into
trapezoids between consecutive edge crossings with the winding rule
applied, which is exact for self-intersecting paths and holes and
needs no stencil buffer; strokes are offset polylines with joins and
caps. Anti-aliasing is a one-pixel fringe of alpha ramp along every
boundary edge, drawn outward so the interior never double-blends.
Boundary edges are known from the sweep (where the winding crosses
between inside and outside), so fringes are never drawn inside a
shape. The fringe width is one framebuffer pixel in whatever the
current transform and view scale are.

## 3. Shaders

Bunyip compiles GLSL to SPIR-V offline and embeds it; the game does
the same. A shader file holds only the part that varies:

```glsl
// wave.glsl, a 2D shader
layout(set = 1, binding = 0) uniform Params { float amplitude; float time; } u;

vec4 fragment(vec2 uv, vec4 color) {
	uv.x += sin(uv.y * 20.0 + u.time) * u.amplitude;
	return texture(tex, uv) * color;
}
```

`bunyip-shader` wraps it with the engine's prelude (the texture
`tex`, `image0`..`image3`, the interpolants, `frame.time`,
`frame.view`) and runs glslangValidator:

```
//go:generate go run github.com/matjam/bunyip/cmd/bunyip-shader -o wave.spv wave.glsl
//go:embed wave.spv
var waveSPV []byte
```

```go
wave, err := ctx.Gfx.NewShader(waveSPV)
wave.SetUniforms(struct{ Amplitude, Time float32 }{0.02, t})
wave.SetImage(0, noise)
g.Shaded(wave, func() { g.Draw(tex, s) })
g.SetShader(wave) // or as state; nil restores the default
```

Uniforms are any struct laid out by std140 rules (use `float32`,
`lin.Vec2`, `lin.Vec4`, `lin.Mat4`; `lin.Vec3` needs padding, as the
documentation says). `SetUniforms` copies the bytes into a per-frame
arena, so changing uniforms between draws is cheap and each draw keeps
the values it was queued with. Images 0..3 are extra textures.

Mesh shaders are surface shaders: the game adjusts the inputs to the
engine's lighting rather than replacing it, and can post-process the
lit result:

```glsl
// lava.glsl, a mesh shader
layout(set = 3, binding = 1) uniform Params { float time; } u;

void surface(inout Surface s) {
	float glow = sin(s.worldPos.x * 3.0 + u.time) * 0.5 + 0.5;
	s.emissive = vec3(1.0, 0.3, 0.05) * glow;
	s.roughness = 0.9;
}
vec4 finish(vec4 lit, Surface s) { return lit; } // optional
```

`Surface` carries `albedo`, `alpha`, `normal`, `metallic`, `roughness`,
`emissive`, `uv`, `worldPos`, `viewDir`. `Material.Shader` selects the
shader; `SetUniforms` and `SetImage` work the same way; opaque, blended
and skinned pipeline variants are created on first use.

Descriptor layout (fixed, so pipelines are interchangeable):

- 2D: set 0 = `tex` plus `image0..3` (five samplers), set 1 = uniforms
  (dynamic offset into the arena). Push constants: projection, then
  `frame` = (time, view width, view height, pixel scale).
- Mesh: set 0 = the four material textures plus `image0..3`, set 1 =
  frame block, set 2 = shadow cascades, set 3 = joints (binding 0) and
  uniforms (binding 1). Four sets, the Vulkan minimum guarantee.

## 4. Shaped text

`Font` is rebuilt over go-text/typesetting: OpenType parsing, HarfBuzz
shaping, script and bidi segmentation and Unicode line breaking. The
existing API keeps working; these are new:

```go
f, _ := g.NewFont(ttf, 16, gfx.FontOptions{
	Fallbacks:  [][]byte{arabicTTF, cjkTTF},        // tried per run when a glyph is missing
	Features:   []string{"smcp", "-liga"},          // OpenType features on or off
	Variations: map[string]float32{"wght": 650},    // variable font axes
})
g.DrawTextBlock(f, text, x, y, gfx.TextOptions{
	Width:     300,
	Direction: gfx.DirectionAuto, // or LTR, RTL, TTB (vertical)
	Language:  "tr",
}, c)
glyphs := f.Shape("مرحبا", gfx.TextOptions{}) // positioned glyphs for custom drawing
g.DrawGlyphs(f, glyphs, x, y, c)
```

Glyphs are cached by glyph id rather than rune, rasterised from the
font's outlines with golang.org/x/image/vector at the framebuffer's
pixel density (or as distance fields for SDF fonts). Shaped runs are
cached by text and options, since a UI draws the same strings every
frame. Kerning, ligatures and mark positioning come from HarfBuzz;
right-to-left runs are reordered by the segmenter's bidi pass; vertical
text lays out top to bottom with vertical advances when the font has
them.

## Out of scope

A shading language of our own (Kage-style); colour matrices (a
five-line 2D shader); stencil-based fills; bitmap and colour-emoji
glyphs, which draw as the notdef box.
