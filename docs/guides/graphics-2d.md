---
title: 2D graphics
group: Graphics
order: 1
summary: sprites, atlases, the camera, layers and sorting, tilemaps, autotiling, text, vector paths, particles, lights and render textures for a 2D game
---

The [gfx](../pkg/gfx.html) package is one drawing context for a window.
All of a 2D game's drawing goes through it. This guide covers the parts
in the order a game needs them, from the frame to the batch statistics
at the end.

## The frame

`Draw` is called once per frame with the screen already cleared to
`ctx.Clear`. Every `Draw*` call queues work and the engine submits it
when `Draw` returns, so order matters only within a layer.

Coordinates are float32 view units with the origin at the top-left and
+Y down. Angles are radians, clockwise on screen. Rectangles are
`lin.Rect` (top-left corner and size), positions `lin.Vec2`. Colours are
`gfx.Color`, linear and non-premultiplied, from `gfx.RGB`, `gfx.RGBA`,
`gfx.Hex` or `gfx.FromHSV`; a zero colour where a tint is expected means
white.

By default the view is the window's size in points, so `ctx.Width` and
`ctx.Height` change on a resize. For a game designed at one resolution,
set `Config.ViewWidth` and `ViewHeight` instead. The engine then scales
that fixed view into the window and centres it, and the two values stay
constant. `Config.Scaling` chooses the scaling. `ScaleFit` keeps the
aspect ratio and adds bars. `ScaleInteger` scales by whole numbers only,
which keeps pixel art crisp; leave those textures on the default nearest
sampling. `ScaleStretch` fills the window. The
[window guide](window.html) has the rest.

```go
bunyip.Run(bunyip.Config{
	Title: "Shooter", Width: 1280, Height: 720, Resizable: true,
	ViewWidth: 320, ViewHeight: 180, Scaling: bunyip.ScaleInteger,
}, &game{})
```

## Textures and atlases

`Graphics.NewTexture` uploads an `image.Image`. `TextureOptions` chooses
`Linear` filtering for smooth scaling, `NoMipmaps`, `Repeat` to tile,
and `Data` for pixels that are not sRGB colour, such as masks and normal
maps. Every texture has `Destroy`; call it from `Shutdown`. To load a
texture by name, use the [asset](../pkg/asset.html) package. It resolves
a name against loose directories, pack files and embedded filesystems in
that order, so a loose file takes priority over a shipped one.

```go
//go:embed assets
var embedded embed.FS

g.fs, err = asset.OpenFS(asset.Dir("assets"), asset.FSSource(embedded))
g.tex, err = asset.Texture(ctx.Gfx, g.fs, "sprites/hero.png", gfx.TextureOptions{})
```

A `Region` is a rectangle of a texture, made by `gfx.NewRegion` or
returned by a sheet or an atlas. A `Sheet` cuts a texture into a grid of
equal frames numbered row-major from the top-left; a `Sheet` literal
takes `Margin` and `Spacing` when the grid has padding. `ParseAtlas`
reads packed atlases with named frames, in TexturePacker JSON (hash or
array) or Aseprite's JSON export; `AtlasData.Bind` ties the description
to the uploaded texture, `Atlas.Region` looks a frame up by name and
`Tag` returns an animation tag's frames in play order, with `Durations`
for their per-frame times. `Atlas.Animation` wraps a tag as a
`RegionAnimation` that plays with the timings the file gave each frame:
`At(t)` returns the region to draw at a time from the start. With the
asset package, `asset.Atlas` reads the JSON, loads the image it names
from beside it and binds them in one call.

```go
sheet := gfx.NewSheet(tex, 16, 16)
icon := gfx.NewRegion(tex, lin.R(96, 0, 16, 16))

atlas, err := asset.Atlas(ctx.Gfx, fs, "sprites/hero.json", gfx.TextureOptions{})
run := atlas.Animation("run")          // frames and durations as authored
frame, _ := run.At(ctx.Time)
gr.DrawRegion(frame, gfx.Sprite{Pos: g.hero})
idle, ok := atlas.Region("hero_idle_0")
```

`DrawNineSlice` stretches a `NineSlice` over any rectangle while its
corners keep their size, so a 24 by 24 png draws a panel or a speech
bubble at any size. Set `Tile` to repeat the edges and centre instead of
stretching them. `NewBlankTexture` and `Texture.Write` replace a
texture's pixels, streamed without waiting for the GPU, so a painting
tool or a video can change one every frame; `Texture.Read` copies
pixels back.

```go
ns := gfx.NineSlice{Tex: g.panel, Left: 8, Top: 8, Right: 8, Bottom: 8}
gr.DrawNineSlice(ns, lin.R(12, 12, 300, 92), gfx.White)
```

## Sprites

A `Sprite` is one textured quad: a position, a size in view units, the
UV window into the texture, a tint, a rotation, an origin as a fraction
of the size, `FlipX` and `FlipY`, and a `Filter` that overrides the
texture's own sampling for that draw. `Graphics.Draw` queues one, and a
nil texture draws a 1x1 white pixel, so a plain coloured quad is a
tinted sprite. `DrawTexture` places a whole texture at its own size;
`DrawRegion` and `DrawFrame` fill the UVs in from a region or a sheet
frame, where a zero `Size` means its own size. `DrawIndexed` and
`DrawTriangles` take raw `Vertex2D` geometry. Sheet animation uses two
values: an `Animation` lists frames and a rate, and an `AnimState` plays
it, with `Advance`, `Frame` and `Done`.

```go
gr.Draw(g.tex, gfx.Sprite{
	Pos:    e.pos,
	Size:   lin.V2(48, 48),
	Origin: lin.V2(0.5, 0.5), // rotate about the middle
	Rotation: e.angle, FlipX: e.facingLeft, Color: gfx.RGB(255, 220, 180),
})

g.walk = gfx.Animation{Frames: []int{4, 5, 6, 7}, FPS: 8, Loop: true}
g.anim.Play(&g.walk)
g.anim.Advance(ctx.Delta)                                      // Update
gr.DrawFrame(sheet, g.anim.Frame(), gfx.Sprite{Pos: g.player}) // Draw
```

Consecutive sprites that share a texture, blend mode, shader, clip and
colour matrix merge into one draw call. Changing any of those breaks the
batch, so a hundred sprites from one atlas cost one draw and alternating
between two textures costs a hundred. Pack sprites into one atlas to
keep them grouped by texture, and use `SetLayer` to control what draws
in front without breaking that grouping.

## Tint, blending and transforms

The sprite's `Color` multiplies the texture. Use it for team colours,
fading and a damage flash. For more, a `ColorMatrix` recolours
everything drawn inside it: `Saturation`, `HueRotate`, `Brightness`,
`Contrast`, `Invert`, `Grayscale`, `Sepia` and `Tint`, composed with
`Mul`. To set a blend mode for a stretch of drawing, call `Blended`.
`BlendAdd` suits glows and explosions and `BlendMultiply` suits shadows
and tinted glass; `BlendScreen`, `BlendLighten`, `BlendDarken`,
`BlendReplace` and `BlendErase` are the rest, and `BlendErase` cuts a
hole.

`Transformed` maps everything drawn inside it through a `lin.Affine`
(`Translate2`, `Rotate2`, `Scale2`, `Shear2`, composed with `Mul`), and
`Clip` limits drawing to a rectangle, which keeps a scrolling list
inside its panel. `SetColorMatrix`, `SetBlend`, `PushTransform`/
`PopTransform` and `PushClip`/`PopClip` are the non-closure forms.

```go
gr.ColorMatrixed(gfx.Brightness(0.6).Mul(gfx.Tint(gfx.RGB(255, 90, 90))), func() {
	gr.DrawRegion(hurt, gfx.Sprite{Pos: g.player})
})
gr.Blended(gfx.BlendAdd, func() { gr.Draw(g.glow, muzzleFlash) })
```

## The camera, layers and the HUD

`SetCamera2D` makes later drawing world-space. A `Camera2D` has a
position (the world point at the centre of the view), a `Zoom` where 2
shows half as much, and a `Rotation`. `Follow` moves it towards a target
at a rate per second, the same at any frame rate; `Clamp` keeps the view
inside the level's rectangle; `Shake` throws the view about for a moment
and `Advance`, called once per update, runs the shake and lets it settle.
`ViewToWorld` maps the pointer back into the world, `WorldToView` goes
the other way for a marker pinned to an entity, and `VisibleRect`
returns the world rectangle on screen. Sprites wholly outside that
rectangle are dropped before they reach the vertex stream, and
`FrameStats.Culled2D` counts them. `ScreenSpace` returns to view
coordinates for the HUD.

```go
g.cam.Follow(g.player.Pos, 8, ctx.Delta) // in Update
g.cam.Clamp(g.level.Bounds(), ctx.Width, ctx.Height)
g.cam.Advance(ctx.Delta)
if landed {
	g.cam.Shake(6, 0.3)
}
```

`SetLayer` orders drawing across calls. Sprites draw in ascending layer
order and, within a layer, by sort key and then in submission order, so
a game can write its draw code in any order and still get the right
stacking. For parallax, put each background on its own layer and
translate it by its share of the camera's offset. To sort sprites by
their feet in a top-down game, set `SetSortKey` to each sprite's foot
position before drawing it: a character standing lower on the screen
then draws over one behind it, whatever order the draw calls came in.

```go
for _, a := range g.actors {
	gr.SetSortKey(a.pos.Y + a.height)
	gr.DrawRegion(a.region, gfx.Sprite{Pos: a.pos})
}
gr.SetSortKey(0)
```

```go
// Update: ease towards the player, then shake.
g.cam.Position = g.cam.Position.Lerp(g.player, 1-float32(math.Pow(0.02, ctx.Delta)))
if g.shake -= float32(ctx.Delta) * 3; g.shake > 0 {
	kick := lin.V2(g.rng.Between(-1, 1), g.rng.Between(-1, 1)).Mul(g.shake * 8)
	g.cam.Position = g.cam.Position.Add(kick)
}

// Draw
gr.SetCamera2D(g.cam)
for i, bg := range g.parallax { // 0 is furthest
	gr.SetLayer(i)
	share := 0.8 - 0.3*float32(i)
	gr.Transformed(lin.Translate2(g.cam.Position.X*share, 0), func() { gr.DrawTexture(bg, 0, 0) })
}
gr.SetLayer(10)
gr.DrawTilemap(g.tilemap, 0, 0, gfx.White)
gr.SetLayer(20)
g.drawActors(gr)

gr.ScreenSpace() // the HUD, in view coordinates
gr.SetLayer(100)
gr.DrawText(g.font, fmt.Sprintf("HP %d", g.hp), 12, 12, gfx.White)
gr.SetLayer(0)
```

## Tilemaps

A `Tilemap` is a grid of frame indices into a `Sheet`; -1 is an empty
cell. `TileW` and `TileH` set the drawn size of a tile, so 16-pixel art
can be drawn at 32 units. `DrawTilemap` skips the cells outside the
active camera's view, so the cost depends on what is on screen rather
than on the size of the map.

A cell can carry flip bits above the frame index, so one sheet frame
serves eight orientations: `TileFlipped` packs them, `TileFrame` unpacks
a cell, and the bits (`TileFlipX`, `TileFlipY`, `TileFlipDiag`) match
the Tiled editor's convention. `Animate` makes one frame index cycle
through others, for water and torches, and `Advance` steps every
animation on the map. For a very large or infinite world, keep one
tilemap per chunk and draw the chunks that intersect `VisibleRect`;
building a tilemap is cheap.

```go
g.tilemap = gfx.NewTilemap(sheet, 256, 256)
g.tilemap.TileW, g.tilemap.TileH = 32, 32
g.tilemap.Set(x, y, frameWall)
g.tilemap.Set(x+1, y, gfx.TileFlipped(frameArrow, true, false, false))
solid := g.tilemap.Get(x, y) == frameWall
g.tilemap.Animate(frameWater, gfx.TileAnimation{
	Frames: []int{frameWater, frameWaterB}, Durations: []float32{0.4}})
g.tilemap.Advance(ctx.Delta) // in Update
```

## Autotiling

To keep a map of plain terrain ids and let the tiles pick themselves,
use the [autotile](../pkg/grid/autotile.html) package. A
`Mapper` turns terrain ids into frame indices: `Apply` fills a whole
tilemap and `Cell` patches the neighbourhood of one edited cell. Four
rule kinds cover the usual tilesets: `Edge16` matches the four edge
neighbours with 16 tiles (walls, pipes, fences), `Blob47` matches all
eight neighbours with the 47 distinct blob tiles, `Corner16` is the
dual grid where each tile sits on a corner between four cells, and
`Wang` matches terrain colours on tile edges or corners for any number
of terrains meeting with transitions. `ExpandBlob` composes the 47 blob
tiles from a six-tile template, so an artist draws six tiles instead of
47. Variants weight alternative frames per neighbourhood, chosen by a
stable hash of the cell position.

```go
img, frames := autotile.ExpandBlob(template, 16)
tex, _ := ctx.Gfx.NewTexture(img, gfx.TextureOptions{})
grassMap := gfx.NewTilemap(gfx.NewSheet(tex, 16, 16), w, h)
grass := &autotile.Mapper{Rules: autotile.Blob47(1, frames)}
grass.Apply(w, h, terrainAt, grassMap.Set)   // the whole map once
grass.Cell(x, y, w, h, terrainAt, grassMap.Set) // after one edit
```

Terrain sets painted in the Tiled editor's terrain tool come in through
the tiled package: `Map.WangSet` finds a set by name and its `Rules`
method converts it, with tile ids as frames. The
[autotile example](https://github.com/matjam/bunyip/tree/main/examples/autotile)
paints grass, walls and water with the mouse over one shared terrain
grid, one rule kind each.

## Maps from the Tiled editor

The [tiled](../pkg/tiled.html) package reads maps saved by the Tiled
editor in its JSON form (`.tmj`, `.tsj`) or its XML form (`.tmx`,
`.tsx`). `Parse` tells the forms apart by the first byte; `Load` reads
from disk and `LoadFS` through an asset filesystem. `Build` loads the
tileset images, makes one `gfx.Tilemap` per tileset a layer uses, wires
up the per-tile animations and returns a `Level` whose `Draw` draws the
layers in order with the group state above them applied. `Advance` steps
the tile animations, `Size` gives the map's pixel size, `Layer` finds a
layer by name and `Destroy` releases the textures.

Object layers are left to the game. They hold rectangles, ellipses,
points, polygons and polylines with names, classes and typed custom
properties; read them for spawn points, triggers and collision shapes.
Tiled's flip and rotation bits survive the import. Zstd-compressed
layers are not supported; CSV, base64, zlib and gzip are.

```go
m, err := tiled.LoadFS(g.fs, "maps/level1.tmj")
g.level, err = tiled.Build(ctx.Gfx, m, tiled.ImagesFrom(g.fs, "maps"))
for _, l := range g.level.Layers {
	for _, o := range l.Objects { // empty on tile layers
		switch o.Class {
		case "spawn":
			g.player = lin.V2(o.X, o.Y).Add(l.Offset)
		case "solid":
			g.solids = append(g.solids, o.Rect())
		case "door":
			g.doors = append(g.doors, door{o.Rect(), o.Properties.String("target")})
		}
	}
}
```

## Text

`NewFont` parses an OpenType font and rasterises its glyphs at one size
into an atlas. Text is shaped with HarfBuzz, so kerning, ligatures, mark
placement and Arabic joining are right, right-to-left runs are
reordered, and lines break by the Unicode rules. Glyphs render at the
framebuffer's pixel density and draw in view units, so text is crisp on
a high-DPI display. `FontOptions` adds fallback fonts for scripts the
main one lacks, OpenType features, variable-font axes and ranges to
render up front.

`DrawText` draws one line from the top-left. `DrawTextBlock` wraps,
aligns and rotates a paragraph through `TextOptions`: a `Width` to wrap
in, an `Align` (`AlignLeft`, `AlignCenter`, `AlignRight`,
`AlignJustify`), `LineSpacing`, a `Size`, an `Angle`, `LetterSpacing`,
`Baseline`, a `Hyphenate` hyphenator (`EnglishHyphenator` is built in),
a `Direction` and a `Language`. `Font.Measure` sizes text without
drawing it and `Font.Layout` returns the wrapped lines, without the soft
hyphens a hyphenator inserted. A font caches what it shapes, wraps,
measures and lays out, keyed by the text and the options, so drawing or
measuring the same string every frame costs a map lookup and the entries
a frame uses stay resident however many one-off strings pass through.

```go
g.font, err = asset.Font(ctx.Gfx, g.fs, "fonts/body.ttf", 18, gfx.FontOptions{
	Fallbacks: [][]byte{cjkTTF, emojiTTC}, // consulted in order
	Features:  []string{"-liga"},
})

opts := gfx.TextOptions{Width: 420, Align: gfx.AlignJustify, Hyphenate: gfx.EnglishHyphenator()}
w, h := g.font.Measure(story, opts)
gr.FillRect(38, y-4, w+4, h+8, gfx.RGBA(0, 0, 0, 160))
gr.DrawTextBlock(g.font, story, 40, y, opts, gfx.White)
```

`NewSDFFont` builds a signed-distance atlas that stays sharp at any size
and angle, so one font object serves damage numbers and a zooming
strategy map. Colour emoji draw in their own colours when a bitmap emoji
font is given as a fallback. `ParseRich` reads a small
markup (`[b]`, `[i]`, `[u]`, `[#ff8800]`, `[link=name]`) into a
`RichText` that `DrawRichText` lays out across regular, bold and italic
faces, returning each link's rectangle for clicks.

```go
rich := gfx.ParseRich("You found the [#ffcc44]Brass Key[/#]. [link=map]Open it[/link].")
links := gr.DrawRichText(gfx.RichFonts{Regular: g.body, Bold: g.bold}, rich, 40, y,
	gfx.TextOptions{Width: 700}, gfx.White)
for _, l := range links {
	if l.Rect.Contains(mouse) && clicked {
		g.open(l.Name)
	}
}
```

`DrawTextOnPath` draws a line of text along a path with each glyph
rotated to follow it, for labels such as a river name on a strategy map.
`Font.Shape` returns positioned glyphs for custom drawing and
hit-testing, `DrawGlyphs` draws them, and fonts have `Destroy`.

## Shapes and paths

`FillRect`, `FillCircle`, `FillPolygon`, `StrokeRect`, `StrokeCircle`
and `StrokeLine` draw simple shapes. They go through the sprite stream,
so they sort by layer and clip like everything else.

A `Path` collects lines, quadratic and cubic curves and arcs, with
`Rect`, `RoundRect`, `Circle`, `Ellipse` and `Polygon` helpers. It holds
no GPU state, so one path value can be reset and rebuilt every frame.
`FillPath` fills it under the non-zero or even-odd rule and `StrokePath`
outlines it, both anti-aliased. `StrokeOptions` sets the width, `Cap`,
`Join`, miter limit and a `Dash` pattern; `FillOptions` maps a `Texture`
over the path or colours it with a `Gradient`. A `Gradient` is baked
from stops and given a direction with `Linear` or `Radial`; it holds a
small texture, so `Destroy` it at the end.

```go
var p gfx.Path
p.MoveTo(40, 340).QuadTo(140, 220, 240, 340).CubicTo(300, 420, 360, 220, 420, 340)
gr.StrokePath(&p, gfx.RGB(120, 220, 160), gfx.StrokeOptions{Width: 6, Cap: gfx.CapRound})

p.Reset()
p.Circle(470, 110, 80).Circle(470, 110, 45) // a ring: the inner circle is a hole
gr.FillPath(&p, gfx.RGB(90, 170, 220), gfx.FillOptions{Rule: gfx.FillEvenOdd})

g.sky, err = ctx.Gfx.NewGradient(
	gfx.GradientStop{T: 0, Color: gfx.RGB(20, 30, 70)},
	gfx.GradientStop{T: 1, Color: gfx.RGB(220, 120, 60)})
g.sky.Linear(lin.V2(0, 0), lin.V2(0, 180))
gr.FillGradient(lin.R(0, 0, ctx.Width, 180), g.sky)
```

## Particles

The [particle](../pkg/particle.html) package simulates emitters on the
CPU and draws them through the sprite stream, so they batch and sort
with everything else. An `Emitter` is a plain struct of documented
zero-default fields: a `Rate` or a `Burst`, a `Shape` to emit from, the
speed, direction and spread, the lifetime, acceleration and damping,
size and colour curves over each particle's life, a texture, region or
sheet, a `Blend` and a `Layer`. `Fire`, `Smoke`, `Sparks`, `Rain` and
`Confetti` are presets to start from.

A `System` owns the live particles: `Update` advances it, `Draw` queues
them, `SetPosition` moves it, `Burst` fires a one-shot and `Finished`
reports one that has run out, so the game can drop it. `WorldSpace`
decides whether particles stay where they were emitted when the system
moves, which suits smoke, or travel with it, which suits a thruster
flame. Thousands are cheap; tens of thousands still draw as one batch
but cost CPU in `Update`.

```go
e := particle.Fire()
e.Position = hearth
e.Texture = g.soft // particle.SoftCircle(64), uploaded as a texture
e.Prewarm = 1.5    // already burning on its first frame
e.Layer = 2
g.fire = particle.New(e)

g.fire.Update(ctx.Delta) // Update
g.fire.Draw(gr)          // Draw
```

## Lights on sprites

`SetLights2D` places an ambient colour and up to eight `Light2D` point
lights above the sprite plane for the frame, and `DrawLit` draws a
sprite lit by them through a tangent-space normal map uploaded with
`TextureOptions{Data: true}`. Lit sprites take light from every
direction. There are no 2D shadows cast by occluders, which is a known
gap. For hard light shafts, draw your own occlusion, usually by
rendering a light mask to a render texture and compositing it with
`BlendMultiply`.

```go
gr.SetLights2D(gfx.RGB(30, 30, 45),
	gfx.Light2D{Pos: g.player, Radius: 220, Color: gfx.RGB(255, 200, 120)},
	gfx.Light2D{Pos: brazier, Radius: 140, Height: 20, Color: gfx.RGB(255, 140, 60)},
)
gr.DrawLit(g.wallTex, g.wallNormal, gfx.Sprite{Pos: lin.V2(x, y), Size: lin.V2(64, 64)})
```

## Render textures

A `RenderTexture` is an offscreen surface that draws like the screen and
is then used like a texture, for minimaps, portraits, mirrors or a whole
low-resolution scene. Set `RenderTextureOptions.Nearest` to keep its
pixels sharp when it is scaled up. `DrawTo` runs a closure with the
render texture as the output; it is rendered before the main frame, so
the result can be drawn in the same frame.

```go
g.mini, err = ctx.Gfx.NewRenderTextureOptions(320, 180, gfx.RenderTextureOptions{Nearest: true})
g.mini.SetView(320, 180)
// in Draw:
gr.DrawTo(g.mini, gfx.RGB(5, 5, 12), func() {
	gr.SetCamera2D(gfx.Camera2D{Position: g.level.Size().Mul(0.5), Zoom: 0.1})
	gr.DrawTilemap(g.tilemap, 0, 0, gfx.White)
})
gr.ScreenSpace()
gr.Draw(g.mini.Texture(), gfx.Sprite{Pos: lin.V2(ctx.Width-232, 12), Size: lin.V2(220, 124)})
```

For a post effect, render the whole game into a render texture and draw
it back as one sprite with a sprite shader on that draw, which gives a
vignette, a CRT curve or a palette swap. `PostSettings` and its bloom,
ambient occlusion and LUT grading apply to the 3D scene's HDR pass, not
to 2D drawing. `RenderTexture.Read` copies the pixels back, and
`ctx.Screenshot(path)` writes the whole frame to a PNG, which is what
every example does with `-shot`.

## Shaders

A game's own fragment shader, compiled to SPIR-V offline with
`bunyip-shader`, colours 2D drawing. `SetShader` or `Shaded` sets it for
a stretch of sprites, `Shader.SetUniforms` passes a struct of parameters
and `Shader.SetImage` passes up to four extra textures. Everything drawn
while one shader is set is one batch as long as nothing else changes.
The [shaders guide](shaders.html) covers writing and building them.

## Performance

`ctx.Stats` and `Graphics.Stats` report the last frame's cost. `Draws2D`
is the 2D draw calls after batching and `Vertices2D` the vertices they
covered; `Draws3D`, `Instances` and `Culled` are the 3D counterparts. A
rising `Draws2D` for the same scene means state changes are breaking
batches: alternating textures, a blend mode toggled per sprite, a clip
pushed around each item, a colour matrix set and unset. Pack sprites
into one atlas, group draws by texture with layers, and set blend modes
and shaders around groups rather than around single draws.

F3 toggles an overlay with the frame time, the update and draw times,
the draw counts and any `ctx.Profile` scopes; `Config.Debug` shows it
from the start. `Config.DrawBudget` sets the number of draw calls a
frame should stay under, and the overlay warns when a frame goes over,
so a batching regression shows up as soon as it appears. Sprites outside
the 2D camera's view are dropped before they cost anything, as are
tilemap cells and 3D meshes, but the game still walks its entity list to
issue the draws; in a large world, test against the camera's visible
rectangle first so that walk is short too.

```go
view := g.cam.VisibleRect(ctx.Width, ctx.Height).Inset(-64) // margin for big sprites
for _, e := range g.entities {
	if view.Contains(e.pos) {
		gr.DrawRegion(e.region, gfx.Sprite{Pos: e.pos})
	}
}
```

## Debug drawing

`DebugText` and `Debugf` put a line of text on screen in the engine's
own font with a dark shadow, so you can print a value before any font is
loaded; `DebugFont` returns that font for measuring. For shapes, call
`StrokeRect` and `StrokeCircle` on a high layer to draw collision boxes
and trigger volumes over the scene.

```go
gr.SetLayer(1000)
gr.Debugf(8, 8, "player %.0f,%.0f  draws %d", g.player.X, g.player.Y, ctx.Stats.Draws2D)
for _, s := range g.solids {
	gr.StrokeRect(s.X, s.Y, s.W, s.H, 1, gfx.RGBA(255, 80, 80, 140))
}
gr.SetLayer(0)
```

The `sprites`, `tiles`, `tiled`, `text`, `vector`, `particles`,
`roguelike` and `tetris` examples are complete programs for each of
these areas, and every one runs with `-seconds 3 -shot out.png`.
