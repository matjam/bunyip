package render

import (
	"image"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/bunyip/internal/vk"
)

// TestClearHeadless brings the whole stack up on a headless surface, clears a
// frame to a known colour, reads it back and checks the corners. It skips
// when no Vulkan driver is present so CI without a GPU stays green.
func TestClearHeadless(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := Config{AppName: "render_test", Validation: true, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := NewRenderer(cfg, HeadlessSurfaceExtensions(), NewHeadlessSurface, vk.VkExtent2D{Width: 64, Height: 32}, true)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer r.Destroy()

	// sRGB surface: a linear clear of 0.5 encodes to about 188.
	var img *pngImage
	for range 3 { // a few frames so the ring wraps once
		fr, ok, err := r.BeginFrame()
		if err != nil {
			t.Fatalf("BeginFrame: %v", err)
		}
		if !ok {
			continue
		}
		r.BeginSwapchainPass(fr, [4]float32{1, 0.5, 0, 1})
		got, err := r.EndFrame(fr, true)
		if err != nil {
			t.Fatalf("EndFrame: %v", err)
		}
		img = &pngImage{got}
	}
	if img == nil {
		t.Fatal("no frame was rendered")
	}
	b := img.Bounds()
	if b.Dx() != 64 || b.Dy() != 32 {
		t.Fatalf("image is %dx%d, want 64x32", b.Dx(), b.Dy())
	}
	for _, p := range [][2]int{{0, 0}, {63, 0}, {0, 31}, {63, 31}, {32, 16}} {
		c := img.RGBAAt(p[0], p[1])
		if c.R != 255 || c.G < 180 || c.G > 196 || c.B != 0 || c.A != 255 {
			t.Errorf("pixel %v = %v, want {255, ~188, 0, 255}", p, c)
		}
	}
	out := filepath.Join(t.TempDir(), "clear.png")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", out)
}

type pngImage struct{ *image.RGBA }
