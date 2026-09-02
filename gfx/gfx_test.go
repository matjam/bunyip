package gfx

import (
	"image"
	"image/color"
	"log/slog"
	"os"
	"testing"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

func newHeadless(t *testing.T, w, h int) *Graphics {
	t.Helper()
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := render.Config{AppName: "gfx_test", Validation: true, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := render.NewRenderer(cfg, render.HeadlessSurfaceExtensions(), render.NewHeadlessSurface,
		vk.VkExtent2D{Width: uint32(w), Height: uint32(h)}, true)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	g, err := newGraphics(r)
	if err != nil {
		r.Destroy()
		t.Fatalf("gfx: new graphics: %v", err)
	}
	t.Cleanup(func() { g.destroy(); r.Destroy() })
	return g
}

// TestSprites draws a solid rectangle and a scaled 2x2 checker texture and
// checks that each quadrant of the checker and the rectangle land where
// the coordinate system says they should.
func TestSprites(t *testing.T) {
	g := newHeadless(t, 128, 128)
	checker := image.NewRGBA(image.Rect(0, 0, 2, 2))
	checker.Set(0, 0, color.RGBA{255, 0, 0, 255})
	checker.Set(1, 0, color.RGBA{0, 255, 0, 255})
	checker.Set(0, 1, color.RGBA{0, 0, 255, 255})
	checker.Set(1, 1, color.RGBA{255, 255, 255, 255})
	tex, err := g.NewTexture(checker, TextureOptions{})
	if err != nil {
		t.Fatalf("NewTexture: %v", err)
	}
	defer tex.Destroy()

	var img *image.RGBA
	for range 2 {
		ok, err := g.begin(Black)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if !ok {
			continue
		}
		g.FillRect(0, 0, 64, 64, RGB(0, 0, 255))
		g.Draw(tex, Sprite{Pos: v2(64, 64), Size: v2(64, 64)})
		if img, err = g.end(true); err != nil {
			t.Fatalf("End: %v", err)
		}
	}
	cases := []struct {
		name string
		x, y int
		want color.RGBA
	}{
		{"rect", 10, 10, color.RGBA{0, 0, 255, 255}},
		{"checker red", 74, 74, color.RGBA{255, 0, 0, 255}},
		{"checker green", 118, 74, color.RGBA{0, 255, 0, 255}},
		{"checker blue", 74, 118, color.RGBA{0, 0, 255, 255}},
		{"checker white", 118, 118, color.RGBA{255, 255, 255, 255}},
		{"clear", 100, 10, color.RGBA{0, 0, 0, 255}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := img.RGBAAt(c.x, c.y); !closeColor(got, c.want) {
				t.Errorf("pixel (%d,%d) = %v, want %v", c.x, c.y, got, c.want)
			}
		})
	}
}

func closeColor(a, b color.RGBA) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= 2 && d(a.G, b.G) <= 2 && d(a.B, b.B) <= 2 && d(a.A, b.A) <= 2
}
