// Command gallery shows every ui widget in both bundled themes, with a
// live theme switch, over an animated background.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/ui"
)

type gallery struct {
	seconds float64
	shot    string

	font     *gfx.Font
	ui       *ui.Context
	dark     bool
	check    bool
	volume   float32
	name     string
	clicks   int
	shotDone bool
}

func (g *gallery) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 16, gfx.FontOptions{}); err != nil {
		return err
	}
	g.dark, g.volume, g.check = true, 0.65, true
	g.ui = ui.New(ctx.Gfx, ui.DarkTheme(g.font))
	return nil
}

func (g *gallery) Shutdown(ctx *bunyip.Context) { g.font.Destroy() }

func (g *gallery) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) && !g.ui.WantsKeyboard() || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	return nil
}

func (g *gallery) Draw(ctx *bunyip.Context) error {
	t := float32(ctx.Time)
	for i := range 12 {
		x := ctx.Width/2 + 200*float32(math.Cos(float64(t*0.4+float32(i))))
		y := ctx.Height/2 + 150*float32(math.Sin(float64(t*0.6+float32(i)*1.3)))
		ctx.Gfx.FillRect(x-30, y-30, 60, 60, gfx.RGBA(uint8(80+i*12), 90, uint8(200-i*10), 120))
	}
	u := g.ui
	u.Begin(ctx.Input)
	u.Panel("Bunyip UI gallery", ui.Rect{X: 24, Y: 24, W: 320, H: 380})
	u.Label("Widgets rebuild every frame from Theme values.")
	u.Row(2)
	if u.Button("Dark") {
		g.dark = true
		u.Theme = ui.DarkTheme(g.font)
	}
	if u.Button("Light") {
		g.dark = false
		u.Theme = ui.LightTheme(g.font)
	}
	if u.Button(fmt.Sprintf("Clicked %d times", g.clicks)) {
		g.clicks++
	}
	u.Checkbox("Show hints", &g.check)
	u.Space(10)
	u.Slider("Volume", &g.volume, 0, 1)
	u.TextField("Type a name", &g.name)
	u.Progress(fmt.Sprintf("Loading %d%%", int(50+50*math.Sin(float64(t)))), 0.75+0.25*float32(math.Sin(float64(t))))
	if g.check {
		u.Label("Escape quits; click a field and type.")
	}
	u.EndPanel()
	u.End()
	return nil
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip gallery", Width: 900, Height: 560, Resizable: true, Validation: true},
		&gallery{seconds: *seconds, shot: *shot})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gallery:", err)
		os.Exit(1)
	}
}
