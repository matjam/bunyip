# Design: user shaders, blending and vector paths, shaped text

This is the design as it was written before the work was done. It is
kept as a record of the reasoning. Some details changed as the code
landed: mesh shaders now use five descriptor sets with the uniform block
in set 4, and colour matrices and colour glyphs (bitmap strikes, COLR
layers and SVG documents), listed as out of scope here, were added
later. The [2D graphics](../guides/graphics-2d.md) and
[shaders](../guides/shaders.md) guides describe what shipped.

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

Bunyip compiles WGSL to SPIR-V using the native Go gogpu/naga dependency.
The same compiler serves runtime source methods and the offline
`bunyip-shader` tool. No external executable or cgo is required.

Sprite source defines `fn fragment(uv: vec2f, color: vec4f) -> vec4f`.
Mesh source defines `fn surface(s: Surface) -> Surface`, with optional
`finish` and `vertex` functions. The engine adds bindings and entry points.
The [shader guide](../guides/shaders.html) documents source syntax,
resource ownership, sampling helpers, uniform layout and complete examples.

`Graphics.CompileShader` and `CompileMeshShader` create GPU resources
from source. `Shader.ReloadSource` preserves the previous shader when
compilation fails. Worker compilation uses `shaders.Compiler`, followed
by `NewShader`, `NewMeshShader` or `Reload` on the game goroutine.

Uniforms use exported Go fields with explicit scalar, vector and matrix types.
`SetUniforms` packs std140-compatible offsets and array strides; WGSL
declarations must match them, including padded scalar arrays and integer
representations of booleans. Each draw retains its uniform and image snapshot.

WGSL uses separate texture and sampler bindings. The sprite/effect descriptor
helper expands each logical texture to a pair. Mesh materials keep 17 images
and four separate immutable sampler bindings; sampling helpers select among
them without requiring nonuniform descriptor indexing. Vulkan push constants
use the compiler's `push_constant` address-space extension.


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
glyphs, err := f.Shape("مرحبا", gfx.TextOptions{}) // positioned glyphs for custom drawing
if err != nil {
	return err
}
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
