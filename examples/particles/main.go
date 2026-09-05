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

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.soft.Destroy()
	g.font.Destroy()
}

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

// pop starts a confetti burst at p.
func (g *game) pop(p lin.Vec2) {
	e := particle.Confetti()
	e.Position = p
	e.Layer = 4
	e.Seed = uint64(len(g.bursts) + 1)
	g.bursts = append(g.bursts, particle.New(e))
}

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

	gr.SetLayer(10)
	alive := g.fire.Alive() + g.smoke.Alive() + g.rain.Alive() + g.sparks.Alive()
	for _, b := range g.bursts {
		alive += b.Alive()
	}
	gr.DebugText(12, ctx.Height-46, fmt.Sprintf("Click for sparks, Space for confetti. %d particles live.", alive))
	gr.DebugText(12, ctx.Height-28, fmt.Sprintf("%d of them are stateless rain, drawn as one instanced call.", g.rain.Alive()))

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
