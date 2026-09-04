---
title: Sprites
example: sprites
summary: a few hundred textured sprites bouncing over a grid, with fullscreen, cursor capture and gamepad input
---

This is the smallest complete game in the repository. It generates a
texture at start-up, gives three hundred sprites a position, a velocity,
a tint and a spin, moves them in `Update` and draws them in `Draw`. The
whole program is the four methods of the [Game](../pkg/bunyip.html#Game)
interface plus one image helper, so it is the shortest path from an
empty file to something moving on screen.

It exercises 2D drawing from [gfx](../pkg/gfx.html): `NewTexture`,
`Draw` with a `Sprite`, and `FillRect` for the checkered background. It
also shows the parts of [input](../pkg/input.html) and
[bunyip](../pkg/bunyip.html) a game needs early: keyboard edges, the
mouse position, the mouse delta while the cursor is captured, a gamepad,
fullscreen, and `Screenshot`. The guides for this material are
[2D graphics](../guides/graphics-2d.html) and
[Input](../guides/input.html).

Run it with:

```bash
go run ./examples/sprites -seconds 3 -shot out.png
```

`-seconds N` quits after N seconds and `-shot file.png` writes a
screenshot halfway through, which is how the example test drives every
program here. `-fullscreen` starts full screen and `-capture` starts
with the cursor captured; F and C toggle them at run time.

## The two types

`ball` is the per-sprite state. Positions and velocities are
`lin.Vec2` in view units, the coordinate space `Draw` works in, with the
origin at the top-left corner and Y increasing downwards. `tint` is a
`gfx.Color` in linear space, and `spin` is in radians per second,
because every angle in the engine is radians.

`game` holds the flags parsed in `main` plus the texture and the
sprites. `shotTaken` exists so the screenshot is written once rather
than on every update after the halfway mark.

```go
type ball struct {
	pos, vel lin.Vec2
	tint     gfx.Color
	spin     float32
}

type game struct {
	seconds    float64
	shot       string
	fullscreen bool
	capture    bool
	tex        *gfx.Texture
	balls      []ball
	shotTaken  bool
}
```

## Init: the texture and the sprites

`Init` runs once with a live context, which is where GPU resources are
created. `NewTexture` takes any `image.Image` and a `TextureOptions`
whose zero value is nearest-neighbour sampling with mipmaps; `Linear:
true` asks for smooth sampling, which suits a round disc that is
rotated. Translucent texels are premultiplied in linear light during
upload, so the anti-aliased edge of the disc composites correctly.

The sprites are seeded from a fixed `rand.NewPCG(1, 2)`, so every run
looks the same and a screenshot can be compared between builds.
`ctx.Width` and `ctx.Height` are the view size in the same units the
sprites are positioned in. `gfx.RGB` converts sRGB bytes to a linear
`gfx.Color`, which is what makes the tints look like the numbers
suggest.

`Init` may run a second time if the GPU device is lost, so nothing here
depends on running exactly once.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	if g.fullscreen {
		ctx.SetFullscreen(true)
	}
	if g.capture {
		ctx.SetCursorCaptured(true)
	}
	var err error
	if g.tex, err = ctx.Gfx.NewTexture(discImage(48), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 300 {
		g.balls = append(g.balls, ball{
			pos:  lin.V2(rng.Float32()*ctx.Width, rng.Float32()*ctx.Height),
			vel:  lin.V2(rng.Float32()*400-200, rng.Float32()*400-200),
			tint: gfx.RGB(uint8(80+rng.IntN(175)), uint8(80+rng.IntN(175)), uint8(80+rng.IntN(175))),
			spin: rng.Float32()*4 - 2,
		})
	}
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) { g.tex.Destroy() }
```

`Shutdown` destroys the texture. Every GPU resource has `Destroy`, and
it must be called from the same goroutine that created the resource,
which for a game means from one of the `Game` methods.

## Update: input and motion

`Update` runs at a fixed step, `Config.FixedStep`, and `ctx.Delta` is
that step rather than the wall-clock time since the last frame. It may
run more than once between two `Draw` calls, or not at all, so all
simulation belongs here and nothing here should depend on the frame
rate.

`KeyPressed` reports the frame's edges: it is true on the update where
the key went down, not for as long as it is held, which is what a
toggle wants. `SetFullscreen` and `SetCursorCaptured` are read back
through `Fullscreen()` and `CursorCaptured()` so F and C flip the
current state. While the cursor is captured the pointer stays put and
`MouseDelta` reports its movement, which is the input a first-person
camera uses. `Gamepad(0)` returns the first pad, whose `Connected`
field is false when nothing is plugged in, so the call is always safe.

The motion is a velocity step with a bounce: a sprite that has passed an
edge has its velocity component negated and is clamped back inside, so
it cannot be knocked outside the view by a large step.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if ctx.Input.KeyPressed(input.KeyF) {
		ctx.SetFullscreen(!ctx.Fullscreen())
	}
	if ctx.Input.KeyPressed(input.KeyC) {
		ctx.SetCursorCaptured(!ctx.CursorCaptured())
	}
	if dx, dy := ctx.Input.MouseDelta(); ctx.CursorCaptured() && (dx != 0 || dy != 0) {
		ctx.Log.Info("sprites: captured mouse", "dx", dx, "dy", dy)
	}
	if pad := ctx.Input.Gamepad(0); pad.Connected && (pad.Pressed(input.ButtonA) || pad.Axis(input.AxisLeftX) != 0) {
		ctx.Log.Info("sprites: gamepad", "name", pad.Name, "leftX", pad.Axis(input.AxisLeftX), "a", pad.Down(input.ButtonA))
	}
	if g.shot != "" && !g.shotTaken && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotTaken = true
	}
	dt := float32(ctx.Delta)
	for i := range g.balls {
		b := &g.balls[i]
		b.pos = b.pos.Add(b.vel.Mul(dt))
		if b.pos.X < 0 || b.pos.X > ctx.Width {
			b.vel.X = -b.vel.X
			b.pos.X = lin.Clamp(b.pos.X, 0, ctx.Width)
		}
		if b.pos.Y < 0 || b.pos.Y > ctx.Height {
			b.vel.Y = -b.vel.Y
			b.pos.Y = lin.Clamp(b.pos.Y, 0, ctx.Height)
		}
	}
	return nil
}
```

Note the loop takes `b := &g.balls[i]` rather than ranging over values:
the elements are updated in place.

## Draw: the grid and the sprites

`Draw` runs once per frame and only queues work. Everything in one layer
is drawn in call order, so the background grid comes first, then the
mouse marker, then the sprites. Nothing is submitted until the frame
ends, and the 2D calls merge into as few draws as their texture, shader,
blend and clip allow, which is why a few hundred sprites of one texture
cost little.

`Sprite` is a struct of options whose zero value is a sensible sprite:
`Size` defaults to the texture size, `Color` defaults to white and
`Origin` defaults to the top-left corner. Here `Origin: lin.V2(0.5,
0.5)` moves the anchor to the middle so `Rotation` spins the disc about
its centre. The rotation is `spin * ctx.Time`, so it comes from the
elapsed time rather than being accumulated, and stays exact.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	const cell = 40
	for y := float32(0); y < ctx.Height; y += cell {
		for x := float32(0); x < ctx.Width; x += cell {
			shade := uint8(30 + 12*(int(x/cell+y/cell)%2))
			ctx.Gfx.FillRect(x, y, cell-1, cell-1, gfx.RGB(shade, shade, shade+8))
		}
	}
	mx, my := ctx.Input.Mouse()
	ctx.Gfx.FillRect(float32(mx)-4, float32(my)-4, 8, 8, gfx.RGB(255, 220, 0))
	for _, b := range g.balls {
		ctx.Gfx.Draw(g.tex, gfx.Sprite{
			Pos: b.pos, Size: lin.V2(48, 48), Origin: lin.V2(0.5, 0.5),
			Color: b.tint, Rotation: b.spin * float32(ctx.Time),
		})
	}
	return nil
}
```

Reading the mouse in `Draw` is safe: input edges are latched for the
whole frame while `Draw` runs, so an interface built here sees every
press even on a frame that ran no update.

## The texture image

The disc is drawn into an `image.RGBA` by hand. The alpha is the
distance from the centre clamped into the last pixel of the radius,
which gives one pixel of anti-aliasing at the edge, and `light` adds a
highlight offset towards the top-left. The texel is written
premultiplied, which is what `image.RGBA` expects; the engine converts
it to linear premultiplied form during upload.

```go
// discImage draws an anti-aliased disc with a highlight.
func discImage(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-r, float64(y)+0.5-r
			d := math.Sqrt(dx*dx+dy*dy) / r
			a := lin.Clamp(float32((1-d)*r), 0, 1)
			light := 0.6 + 0.4*math.Max(0, 1-math.Hypot(dx+r*0.3, dy+r*0.3)/r)
			v := uint8(255 * light * float64(a))
			img.SetRGBA(x, y, color.RGBA{v, v, v, uint8(255 * a)})
		}
	}
	return img
}
```

## main

`main` parses the flags and calls `bunyip.Run` with a `Config` and the
game value. `Config`'s zero values are defaults, so only the title,
size, resizability and validation are given here. `Validation: true`
turns on the Vulkan validation layers, which every example does because
they are development programs; a shipped game leaves it off.

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	fullscreen := flag.Bool("fullscreen", false, "start full screen (F toggles)")
	capture := flag.Bool("capture", false, "start with the cursor captured (C toggles)")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip sprites", Width: 960, Height: 600, Resizable: true, Validation: true},
		&game{seconds: *seconds, shot: *shot, fullscreen: *fullscreen, capture: *capture})
	if err != nil {
		fmt.Fprintln(os.Stderr, "sprites:", err)
		os.Exit(1)
	}
}
```

## What to try

- Raise the sprite count in `Init` from 300 to 30000 and press F3 to
  watch the frame time and the draw count; they stay one draw call.
- Give `ball` a scale field, set it in `Init` and use it for `Size` in
  `Draw`, so the sprites vary in size as well as colour.
- Add gravity in `Update` by adding to `b.vel.Y` each step and losing
  energy on the bounce.
- Change `discImage` to draw a square with a soft edge, and see the
  difference `TextureOptions{Linear: true}` makes under rotation.
- Make the sprites follow the pointer: read `ctx.Input.Mouse()` in
  `Update` and steer each velocity towards it.
