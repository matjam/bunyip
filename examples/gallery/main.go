// Command gallery shows every UI widget, the built-in colour themes, a
// texture skin, scalable SDF text and an audio beep.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip"
	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/ui"
)

type gallery struct {
	seconds float64
	shot    string
	beep    bool
	skinned bool
	theme   string
	tone    *audio.Sound

	font     *gfx.Font
	big      *gfx.Font
	ui       *ui.Context
	themeIdx int
	check    bool
	volume   float32
	name     string
	clicks   int
	quality  int
	skin     *ui.Skin
	skinTex  []*gfx.Texture
	useSkin  bool
	shotDone bool
}

func (g *gallery) Init(ctx *bunyip.Context) error {
	var err error
	if g.font, err = ctx.Gfx.NewFont(goregular.TTF, 16, gfx.FontOptions{}); err != nil {
		return err
	}
	if g.big, err = ctx.Gfx.NewSDFFont(goregular.TTF, 32, gfx.FontOptions{AtlasSize: 1024}); err != nil {
		return err
	}
	g.volume, g.check = 0.65, true
	names := ui.ThemeNames()
	for i, n := range names {
		if n == g.theme {
			g.themeIdx = i
		}
	}
	theme, _ := ui.NamedTheme(names[g.themeIdx], g.font)
	g.ui = ui.New(ctx.Gfx, theme)
	g.ui.OnTextInputRect = ctx.SetTextInputRect
	if g.skin, g.skinTex, err = makeSkin(ctx.Gfx); err != nil {
		return err
	}
	g.useSkin = g.skinned
	g.applyTheme()
	if g.tone, err = ctx.Audio.NewSound(audio.Sine(440, 0.35, ctx.Audio.Rate())); err != nil {
		return err
	}
	if g.beep {
		ctx.Audio.Play(g.tone, audio.PlayOptions{Volume: 0.4})
	}
	return nil
}

// applyTheme rebuilds the theme from the chosen palette and skin.
func (g *gallery) applyTheme() {
	theme, _ := ui.NamedTheme(ui.ThemeNames()[g.themeIdx], g.font)
	if g.useSkin {
		theme.Skin = g.skin
		theme.BorderWidth = 0
	}
	g.ui.Theme = theme
}

func (g *gallery) Shutdown(ctx *bunyip.Context) {
	g.font.Destroy()
	g.big.Destroy()
	for _, t := range g.skinTex {
		t.Destroy()
	}
}

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
	// Scalable text: one SDF atlas, drawn at three sizes and a slant.
	ctx.Gfx.DrawTextSized(g.big, "Bunyip", 380, 40, 72+8*float32(math.Sin(float64(t))), -0.08, gfx.RGB(255, 220, 120))
	ctx.Gfx.DrawTextSized(g.big, "scalable text from one atlas", 384, 130, 22, 0, gfx.RGB(200, 200, 215))
	ctx.Gfx.DrawTextSized(g.big, "tiny", 384, 160, 11, 0, gfx.RGB(150, 150, 170))
	u := g.ui
	u.Begin(ctx.Input, func() {
		u.Panel("Bunyip UI gallery", ui.Rect{X: 24, Y: 24, W: 320, H: 520}, func() {
			u.Label("Widgets rebuild every frame from Theme values; long labels wrap to the panel.")
			u.Columns([]float32{2, 1}, func() {
				if u.Dropdown("Theme", &g.themeIdx, ui.ThemeNames()) {
					g.applyTheme()
				}
				if u.Checkbox("Skin", &g.useSkin) {
					g.applyTheme()
				}
			})
			u.Row(2, func() {
				if u.Button(fmt.Sprintf("Clicked %d times", g.clicks)) {
					g.clicks++
				}
				u.Tooltip("Tab and Shift-Tab move focus; Enter activates.")
				if u.Button("Beep") {
					ctx.Audio.Play(g.tone, audio.PlayOptions{Volume: g.volume, Pan: 0})
				}
				u.Tooltip("Plays a 440 Hz sine through the mixer.")
			})
			u.Checkbox("Show hints", &g.check)
			u.Dropdown("Quality", &g.quality, []string{"Low", "Medium", "High", "Ultra"})
			u.Separator()
			u.Slider("Volume", &g.volume, 0, 1)
			u.TextField("Type a name", &g.name)
			u.Progress(fmt.Sprintf("Loading %d%%", int(50+50*math.Sin(float64(t)))), 0.75+0.25*float32(math.Sin(float64(t))))
			if g.check {
				u.Label("Escape quits; click a field and type.")
			}
			u.ScrollArea("log", ui.Rect{X: 36, Y: 420, W: 296, H: 110}, 20*28, func() {
				for i := range 20 {
					u.Label(fmt.Sprintf("Scrollable line %d", i+1))
				}
			})
		})
	})
	return nil
}

// makeSkin draws a small set of rounded, bevelled textures and wires
// them into a Skin, standing in for the art a game would load.
func makeSkin(g *gfx.Graphics) (*ui.Skin, []*gfx.Texture, error) {
	var texs []*gfx.Texture
	slice := func(img image.Image, border float32) (*ui.Slice, error) {
		tex, err := g.NewTexture(img, gfx.TextureOptions{Linear: true, NoMipmaps: true})
		if err != nil {
			return nil, err
		}
		texs = append(texs, tex)
		return &ui.Slice{Tex: tex, Left: border, Top: border, Right: border, Bottom: border}, nil
	}
	c := func(r, g, b, a uint8) color.NRGBA { return color.NRGBA{r, g, b, a} }
	var err error
	sk := &ui.Skin{}
	set := func(dst **ui.Slice, img image.Image, border float32) {
		if err == nil {
			*dst, err = slice(img, border)
		}
	}
	set(&sk.Panel, rounded(48, 12, 3, c(36, 30, 52, 235), c(140, 120, 190, 255), c(60, 50, 85, 235)), 14)
	set(&sk.Button, rounded(32, 9, 2, c(96, 72, 150, 255), c(170, 150, 220, 255), c(70, 52, 112, 255)), 11)
	set(&sk.ButtonHover, rounded(32, 9, 2, c(120, 92, 180, 255), c(200, 180, 240, 255), c(90, 70, 140, 255)), 11)
	set(&sk.ButtonActive, rounded(32, 9, 2, c(60, 44, 100, 255), c(120, 100, 170, 255), c(50, 36, 80, 255)), 11)
	set(&sk.Field, rounded(32, 7, 2, c(22, 18, 34, 255), c(110, 95, 150, 255), c(22, 18, 34, 255)), 9)
	set(&sk.FieldFocus, rounded(32, 7, 2, c(22, 18, 34, 255), c(250, 200, 90, 255), c(22, 18, 34, 255)), 9)
	set(&sk.Check, rounded(24, 6, 2, c(22, 18, 34, 255), c(110, 95, 150, 255), c(22, 18, 34, 255)), 8)
	set(&sk.CheckOn, rounded(24, 6, 2, c(250, 200, 90, 255), c(255, 230, 150, 255), c(220, 160, 60, 255)), 8)
	set(&sk.Track, rounded(16, 5, 1, c(30, 24, 44, 255), c(80, 66, 110, 255), c(30, 24, 44, 255)), 6)
	set(&sk.Fill, rounded(16, 5, 1, c(250, 200, 90, 255), c(255, 230, 150, 255), c(220, 160, 60, 255)), 6)
	set(&sk.Knob, rounded(24, 11, 2, c(240, 235, 250, 255), c(255, 255, 255, 255), c(180, 170, 210, 255)), 11)
	set(&sk.Thumb, rounded(16, 6, 1, c(150, 130, 200, 255), c(200, 180, 240, 255), c(120, 100, 170, 255)), 7)
	if err != nil {
		for _, t := range texs {
			t.Destroy()
		}
		return nil, nil, err
	}
	return sk, texs, nil
}

// rounded draws a size×size rounded square: fill graded from top to
// bottom colour with an edge ring of the given width.
func rounded(size int, radius, edge float64, top, ring, bottom color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	mix := func(a, b color.NRGBA, t float64) color.NRGBA {
		l := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
		return color.NRGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), l(a.A, b.A)}
	}
	for y := range size {
		for x := range size {
			// Signed distance to the rounded square's edge.
			px, py := float64(x)+0.5, float64(y)+0.5
			half := float64(size) / 2
			dx := math.Abs(px-half) - (half - radius)
			dy := math.Abs(py-half) - (half - radius)
			d := math.Hypot(math.Max(dx, 0), math.Max(dy, 0)) + math.Min(math.Max(dx, dy), 0) - radius
			if d > 0.5 {
				continue
			}
			col := mix(top, bottom, float64(y)/float64(size-1))
			if d > -edge {
				col = ring
			}
			if d > -0.5 { // anti-aliased rim
				col.A = uint8(float64(col.A) * (0.5 - d))
			}
			img.SetNRGBA(x, y, col)
		}
	}
	return img
}

func main() {
	seconds := flag.Float64("seconds", 0, "exit after this many seconds")
	shot := flag.String("shot", "", "write a screenshot to this PNG")
	beep := flag.Bool("beep", false, "play a tone at start")
	debug := flag.Bool("debug", false, "show the frame-timing overlay (F3 toggles it)")
	skin := flag.Bool("skin", false, "start with the texture skin on")
	theme := flag.String("theme", "dark", "starting theme: "+fmt.Sprint(ui.ThemeNames()))
	flag.Parse()
	err := bunyip.Run(bunyip.Config{Title: "Bunyip gallery", Width: 900, Height: 560, Resizable: true, Validation: true, Debug: *debug},
		&gallery{seconds: *seconds, shot: *shot, beep: *beep, skinned: *skin, theme: *theme})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gallery:", err)
		os.Exit(1)
	}
}
