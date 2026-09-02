package bunyip

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/internal/vk"
)

// letterboxGame draws a full-view green rectangle in a fixed 160x90 view
// and takes a screenshot on its second frame.
type letterboxGame struct {
	shot   string
	frames int
}

func (g *letterboxGame) Update(ctx *Context) error { return nil }

func (g *letterboxGame) Draw(ctx *Context) error {
	ctx.Gfx.FillRect(0, 0, ctx.Width, ctx.Height, gfx.RGB(0, 255, 0))
	g.frames++
	if g.frames == 2 {
		ctx.Screenshot(g.shot)
	}
	if g.frames == 3 {
		ctx.Quit()
	}
	return nil
}

func TestHeadlessLetterbox(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	shot := filepath.Join(t.TempDir(), "shot.png")
	game := &letterboxGame{shot: shot}
	// A 160x90 view integer-scaled into a 400x200 window fits twice: a
	// 320x180 green area centred with black bars around it.
	err := Run(Config{Width: 400, Height: 200, ViewWidth: 160, ViewHeight: 90, Scaling: ScaleInteger, Headless: true, NoAudio: true, Validation: true}, game)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(shot)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	green := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return g > 0x8000 && r < 0x3000 && b < 0x3000
	}
	black := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return r < 0x1000 && g < 0x1000 && b < 0x1000
	}
	if !green(200, 100) || !green(41, 11) || !green(358, 188) {
		t.Error("the scaled view is not green where it should be")
	}
	if !black(20, 100) || !black(380, 100) || !black(200, 5) || !black(200, 195) {
		t.Error("the bars around the view are not black")
	}
}
