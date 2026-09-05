---
title: Particles
example: particles
summary: a campfire of fire and smoke, instanced rain, sparks on click and confetti on Space, with a panel that retunes the fire while it burns
---

This program is a tour of the [particle](../pkg/particle.html) package.
Several systems run at once: a campfire of fire and smoke, thousands of
raindrops falling from a line above the screen as one instanced draw,
sparks fired where the mouse is clicked, and a confetti burst on Space.
A panel of sliders changes the fire's emission rate and gravity while it
is burning.

This example uses the particle package's 2D effects over sprite drawing in
[gfx](../pkg/gfx.html). An `Emitter` is a plain value describing what to
emit and how the particles behave; `particle.New` turns one into a
running `System`; the system is stepped from `Update` and drawn from
`Draw`. The presets used here, `particle.Fire`, `particle.Smoke`,
`particle.Rain`, `particle.Sparks` and `particle.Confetti`, return
emitters already filled in, and a program changes the fields it cares
about. The panel comes from [ui](../pkg/ui.html); see
[the interface guide](../guides/ui.html) for that half.

The rain is the exception: it runs on a `GPUSystem`, which takes the same
`Emitter` but keeps its particles in parallel arrays and draws the whole
storm as one instanced call rather than a sprite each. Its emitter is
also `Stateless`, so no individual particle state is kept between frames:
the system retains its settings and clock, and every drop is
a closed form of the seed, its index in the stream and the clock. Pass
`-drops 200000` and the storm still draws in one call.

Run it:

```bash
go run ./examples/particles -seconds 3 -shot out.png
```

The flags are `-seconds N` to quit after that long, `-shot file.png` to
write a screenshot halfway through the run, `-headless` to render
without a window, and `-drops N` for the size of the instanced storm.
Escape quits, a left click throws sparks, and Space pops confetti.

## Package and state

The game type holds the fonts and textures to destroy at the end, the
four systems, a slice of confetti bursts, and the two slider values. It
also keeps `fireE`, the fire's emitter, because a slider edits that value
and hands it back to the running system.

```go
// Command particles shows the particle package: a campfire of fire and
// smoke, rain from a line along the top of the screen, sparks where the
// mouse is clicked, and a confetti burst on Space. A panel of sliders
// retunes the fire's rate and gravity while it burns. Escape quits.
package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/particle"
	"github.com/matjam/bunyip/ui"
)

type game struct {
	seconds float64
	shot    string
	drops   int

	font  *gfx.Font
	ui    *ui.Context
	soft  *gfx.Texture
	fireE particle.Emitter
	fire  *particle.System
	smoke *particle.System
	// The rain is a GPUSystem rather than a System: tens of thousands of
	// drops as one instanced draw call rather than one sprite each.
	rain     *particle.GPUSystem
	sparks   *particle.System
	bursts   []*particle.System // confetti pops, dropped once Finished
	fireRate float32
	gravity  float32
	shotDone bool
	demoDone bool
}
```

## Init: building the emitters

`Init` runs once with a live graphics device, which is where GPU
resources are created. The font and the soft circle texture are made
here and destroyed in `Shutdown`. `particle.SoftCircle(64)` returns a
64 by 64 image of a disc that fades out at its edge, the usual sprite for
fire and smoke; `gfx.TextureOptions{Linear: true}` asks for smooth
sampling, since the particles are scaled and rotated.

The hearth is placed in view units, with the origin at the top left and
positive Y downwards, so `ctx.Height-150` is 150 units up from the bottom
of the view. Each emitter starts from a preset and is then adjusted:

- `Position` is where particles are born, and `Shape` spreads them over a
  region. The rain uses `particle.Line`, a horizontal line the width of
  the view plus a margin, so drops arrive from off screen.
- `Prewarm` advances the stateful fire and smoke before the first frame.
  Stateless rain ignores this setting: its closed-form stream already
  includes drops born before time zero.
- `Layer` is the sort key the sprites are drawn with. Smoke is 1, fire 2,
  sparks 3 and confetti 4, so each draws over the one before it whatever
  order the `Draw` calls happen to be in.
- `Burst` set to zero prevents the initial burst from `New`. The sparks
  preset already has a zero `Rate`, so later sparks only appear when the
  program calls `Burst`.

`Size` is a `particle.Range`, a minimum and a maximum the system picks
between for each particle. The fire's `Rate` and `Acceleration.Y` are
copied into the slider values so the panel starts at the preset's own
numbers.

```go
func (g *game) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 15, gfx.FontOptions{}); err != nil {
		return err
	}
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	if g.soft, err = ctx.Gfx.NewTexture(particle.SoftCircle(64), gfx.TextureOptions{Linear: true}); err != nil {
		return err
	}
	hearth := lin.V2(ctx.Width/2, ctx.Height-150)

	g.fireE = particle.Fire()
	g.fireE.Position = hearth
	g.fireE.Texture = g.soft
	g.fireE.Prewarm = 1.5
	g.fireE.Layer = 2
	g.fireRate, g.gravity = g.fireE.Rate, g.fireE.Acceleration.Y
	g.fire = particle.New(g.fireE)

	smoke := particle.Smoke()
	smoke.Position = hearth.Add(lin.V2(0, -30))
	smoke.Texture = g.soft
	smoke.Prewarm = 3
	smoke.Layer = 1
	g.smoke = particle.New(smoke)

	// Rain through the instanced path. Stateless means no per-particle
	// state is kept at all: every drop is a closed form of the seed, its
	// index and the clock, so the storm is already falling at time zero
	// with no Prewarm and costs the same memory at any size.
	rain := particle.Rain()
	rain.Position = lin.V2(-40, -20)
	rain.Shape = particle.Line(lin.V2(ctx.Width+80, 0))
	rain.Stateless = true
	rain.Max = g.drops
	rain.Rate = float32(g.drops) / 1.6 // the rate that fills Max over a lifetime
	g.rain = particle.NewGPU(rain)

	sparks := particle.Sparks()
	sparks.Burst = 0 // only on click
	sparks.Texture = g.soft
	sparks.Size = particle.Range{Min: 4, Max: 7}
	sparks.Layer = 3
	g.sparks = particle.New(sparks)
	return nil
}
```

## Shutdown: releasing the GPU objects

Everything with a `Destroy` method is destroyed here, on the same
goroutine that created it. The particle systems hold no GPU objects of
their own; they draw with the texture they were given.

```go
func (g *game) Shutdown(ctx *bunyip.Context) {
	g.soft.Destroy()
	g.font.Destroy()
}
```

## Update: input and simulation

`Update` runs at the fixed step, so `ctx.Delta` is always the same
duration and the effects advance at the same rate however fast the
machine draws. `ctx.Time` is the seconds since the run began, which is
what the `-seconds` and `-shot` flags are compared against.

`g.ui.WantsMouse` reports whether the interface is under the pointer.
The click test asks it before throwing sparks, so dragging a slider does
not also fire sparks behind the panel. A timed run pops the sparks and
the confetti just before the screenshot, so the picture on this page
shows the one-shot effects as well as the continuous ones.

Each system is stepped with `Update(ctx.Delta)`. The confetti bursts are
finite, so the loop over `g.bursts` drops the ones that report
`Finished`, rebuilding the slice in place through `g.bursts[:0]` rather
than allocating a new one each frame.

```go
func (g *game) Update(ctx *bunyip.Context) error {
	in := ctx.Input
	if in.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	if in.MousePressed(input.MouseLeft) && !g.ui.WantsMouse() {
		g.sparks.SetPosition(in.MousePos())
		g.sparks.Burst(40)
	}
	if in.KeyPressed(input.KeySpace) {
		g.pop(lin.V2(ctx.Width/2, ctx.Height/2))
	}
	// A timed run pops both one-shot effects so a screenshot shows them.
	if g.seconds > 0 && !g.demoDone && ctx.Time >= g.seconds/2-0.5 {
		g.demoDone = true
		g.sparks.SetPosition(lin.V2(ctx.Width*0.75, ctx.Height*0.4))
		g.sparks.Burst(40)
		g.pop(lin.V2(ctx.Width/2, ctx.Height/2))
	}
	g.fire.Update(ctx.Delta)
	g.smoke.Update(ctx.Delta)
	g.rain.Update(ctx.Delta)
	g.sparks.Update(ctx.Delta)
	live := g.bursts[:0]
	for _, b := range g.bursts {
		b.Update(ctx.Delta)
		if !b.Finished() {
			live = append(live, b)
		}
	}
	g.bursts = live
	return nil
}
```

`pop` creates one confetti system per burst. `Seed` makes each burst
differ from the last: a system with the same seed emits the same
particles, which is what a replay or a test wants, so the counter here
deliberately changes it.

```go
// pop starts a confetti burst at p.
func (g *game) pop(p lin.Vec2) {
	e := particle.Confetti()
	e.Position = p
	e.Layer = 4
	e.Seed = uint64(len(g.bursts) + 1)
	g.bursts = append(g.bursts, particle.New(e))
}
```

## Draw: the scene

`Draw` runs once per frame and queues drawing; nothing is submitted until
it returns. `ctx.Clear` is the colour the frame starts from, in linear
space, and `gfx.RGB` converts from the sRGB bytes an image editor shows.

The ground and the two logs are drawn at layer 0, under everything.
`gfx.Sprite` is a value: `Origin` is the fraction of the sprite the
position refers to, so `lin.V2(0.5, 0.5)` centres it, and `Rotation` is
in radians. Drawing with a nil texture fills the rectangle with the
sprite's colour. The systems draw themselves next, each into the layer
its emitter named.

```go
func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	ctx.Clear = gfx.RGB(18, 20, 30)
	// The ground and a couple of logs under the fire.
	hearth := g.fire.Position()
	gr.SetLayer(0)
	gr.FillRect(0, hearth.Y+8, ctx.Width, ctx.Height-hearth.Y-8, gfx.RGB(34, 40, 36))
	log := gfx.Sprite{Pos: hearth.Add(lin.V2(0, 4)), Size: lin.V2(70, 12), Origin: lin.V2(0.5, 0.5), Color: gfx.RGB(90, 60, 35)}
	log.Rotation = 0.35
	gr.Draw(nil, log)
	log.Rotation = -0.35
	gr.Draw(nil, log)

	g.rain.Draw(gr)
	g.smoke.Draw(gr)
	g.fire.Draw(gr)
	g.sparks.Draw(gr)
	for _, b := range g.bursts {
		b.Draw(gr)
	}
```

The particle count is drawn at layer 10, over the scene. `DebugText` is
the built-in font, meant for numbers on top of a scene rather than for a
game's own text.

```go
	gr.SetLayer(10)
	alive := g.fire.Alive() + g.smoke.Alive() + g.rain.Alive() + g.sparks.Alive()
	for _, b := range g.bursts {
		alive += b.Alive()
	}
	gr.DebugText(12, ctx.Height-46, fmt.Sprintf("Click for sparks, Space for confetti. %d particles live.", alive))
	gr.DebugText(12, ctx.Height-28, fmt.Sprintf("%d of them are stateless rain, drawn as one instanced call.", g.rain.Alive()))
```

## Draw: the panel

The interface is rebuilt every frame inside `u.Begin`, which takes the
frame's input and a closure. Containers take closures too, so `u.Panel`
scopes its widgets by nesting rather than by a matching end call.

`u.Slider` takes a pointer to the value it edits and returns whether the
value changed this frame. Both sliders are called every frame, and the
`|| changed` keeps the second call from being skipped by short-circuit
evaluation. When either moved, the emitter value is updated and
`SetEmitter` hands it back to the running system. Existing particles keep
their birth size, lifetime and palette tint, while acceleration, damping
and appearance curves change for live particles too. Later births use
the new settings. The last line restores layer 0 for the next frame, since
the layer
is graphics state rather than a per-call argument.

```go
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Fire", ui.Rect{X: 12, Y: 12, W: 240, H: 150}, func() {
			changed := u.Slider("Rate (per second)", &g.fireRate, 0, 400)
			changed = u.Slider("Gravity", &g.gravity, -200, 200) || changed
			if changed {
				g.fireE.Rate = g.fireRate
				g.fireE.Acceleration.Y = g.gravity
				g.fire.SetEmitter(g.fireE)
			}
			u.Label(fmt.Sprintf("%d flames, %d smoke", g.fire.Alive(), g.smoke.Alive()))
		})
	})
	gr.SetLayer(0)
	return nil
}
```

## main

`bunyip.Run` owns the window, the renderer and the loop. Every field of
`bunyip.Config` has a usable zero value; this one sets a title, a size, a
resizable window, the Vulkan validation layers, and headless mode from
the flag.

```go
func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	headless := flag.Bool("headless", false, "render without a window, for screenshots")
	drops := flag.Int("drops", 3000, "raindrops in the instanced storm; try 200000")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip particles", Width: 960, Height: 640, Resizable: true, Validation: true, Headless: *headless},
		&game{seconds: *seconds, shot: *shot, drops: max(*drops, 1)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "particles:", err)
		os.Exit(1)
	}
}
```

## What to try

- Change `particle.Fire()` in `Init` to `particle.Smoke()` and watch the
  same emitter shape produce a different effect, then set the fields the
  preset left alone.
- Set the rain's `Acceleration.X` in `Init` and see the drops slant;
  acceleration is applied to every particle every second.
- Add a slider for the confetti's particle count in `Draw` and read it in
  `pop`.
- Raise the sparks' `Layer` above the panel in `Init` and see the sort
  key decide what covers what.
- Remove the `Prewarm` lines in `Init` and start the program: the fire
  lights from nothing over the first second.
