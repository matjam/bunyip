// Command text shows shaped text: kerning and ligatures from HarfBuzz,
// Arabic joining and right-to-left order, mixed-direction lines, a
// fallback font behind the main one, Unicode line wrapping, vertical
// text, and scalable distance-field text. Pass -font with a TTF that has
// Arabic and Hebrew glyphs; on macOS Arial is found automatically.
// Escape quits.
package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
)

type game struct {
	seconds  float64
	shot     string
	fontPath string

	body     *gfx.Font // Go Regular with the world font behind it
	heading  *gfx.Font
	sdf      *gfx.Font
	world    []byte
	shotDone bool
}

func (g *game) Init(ctx *bunyip.Context) error {
	if g.fontPath == "" {
		g.fontPath = "/System/Library/Fonts/Supplemental/Arial.ttf"
	}
	if data, err := os.ReadFile(g.fontPath); err == nil {
		g.world = data
	}
	var err error
	opts := gfx.FontOptions{}
	if g.world != nil {
		opts.Fallbacks = [][]byte{g.world}
	}
	if g.body, err = ctx.Gfx.NewFont(goregular.TTF, 18, opts); err != nil {
		return err
	}
	if g.heading, err = ctx.Gfx.NewFont(gobold.TTF, 26, gfx.FontOptions{}); err != nil {
		return err
	}
	if g.sdf, err = ctx.Gfx.NewSDFFont(gobold.TTF, 32, gfx.FontOptions{}); err != nil {
		return err
	}
	return nil
}

func (g *game) Shutdown(ctx *bunyip.Context) {
	g.sdf.Destroy()
	g.heading.Destroy()
	g.body.Destroy()
}

func (g *game) Update(ctx *bunyip.Context) error {
	if ctx.Input.KeyPressed(input.KeyEscape) || (g.seconds > 0 && ctx.Time >= g.seconds) {
		ctx.Quit()
	}
	if g.shot != "" && !g.shotDone && (g.seconds == 0 || ctx.Time >= g.seconds/2) {
		ctx.Screenshot(g.shot)
		g.shotDone = true
	}
	return nil
}

func (g *game) Draw(ctx *bunyip.Context) error {
	gr := ctx.Gfx
	white, dim := gfx.RGB(235, 235, 240), gfx.RGB(150, 155, 170)
	y := float32(24)
	gr.DrawText(g.heading, "Shaped text", 40, y, white)
	y += 44
	gr.DrawText(g.body, "Kerning from the font: AVATAR Type Wavy. Ligatures where the font has them: office, waffle.", 40, y, white)
	y += 40

	if g.world == nil {
		gr.DrawText(g.body, "No world font found; pass -font path/to/font.ttf for Arabic and Hebrew.", 40, y, dim)
		y += 40
	} else {
		gr.DrawText(g.body, "Arabic joins its letters and runs right to left:", 40, y, dim)
		y += 28
		gr.DrawTextBlock(g.body, "السلام عليكم، هذا نص عربي يُعرض بشكل صحيح", 40, y, gfx.TextOptions{Width: 600, Align: gfx.AlignRight}, white)
		y += 40
		gr.DrawText(g.body, "Mixed directions on one line: the word שלום is Hebrew, and 123 stays 123.", 40, y, white)
		y += 40
	}

	gr.DrawText(g.body, "Wrapped by the Unicode line-breaking rules, centred in 420 units:", 40, y, dim)
	y += 28
	para := "Bunyip shapes text with HarfBuzz through go-text, so marks land on their bases, scripts that join do so, and lines break where they should, in any language the font covers."
	gr.DrawTextBlock(g.body, para, 40, y, gfx.TextOptions{Width: 420, Align: gfx.AlignCenter}, white)
	_, ph := g.body.Measure(para, gfx.TextOptions{Width: 420})
	gr.StrokeRect(40, y-4, 420, ph+8, 1, gfx.RGB(70, 75, 95))

	// Vertical text in a column on the right of the paragraph.
	gr.DrawText(g.body, "vertical:", 520, y, dim)
	gr.DrawTextBlock(g.body, "top to bottom", 700, y, gfx.TextOptions{Direction: gfx.DirectionTTB}, white)
	y += ph + 40

	// Distance-field text at several sizes and a rotation.
	gr.DrawText(g.body, "Distance-field text scales and rotates without blur:", 40, y, dim)
	y += 30
	x := float32(40)
	for _, size := range []float32{14, 22, 36, 56} {
		gr.DrawTextBlock(g.sdf, "Aa", x, y, gfx.TextOptions{Size: size}, gfx.RGB(255, 200, 90))
		w, _ := g.sdf.Measure("Aa", gfx.TextOptions{Size: size})
		x += w + 20
	}
	gr.DrawTextBlock(g.sdf, "tilted", x+20, y+50, gfx.TextOptions{Size: 30, Angle: -0.4}, gfx.RGB(140, 210, 255))
	return nil
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	font := flag.String("font", "", "a TTF with Arabic and Hebrew glyphs, used as a fallback")
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip text", Width: 900, Height: 640, Validation: true},
		&game{seconds: *seconds, shot: *shot, fontPath: *font})
	if err != nil {
		fmt.Fprintln(os.Stderr, "text:", err)
		os.Exit(1)
	}
}
