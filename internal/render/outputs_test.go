package render

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/matjam/bunyip/internal/vk"
)

func TestConcurrentRenderersShareInstance(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := Config{AppName: "two outputs", Validation: true, Log: slog.Default()}
	a, err := NewRenderer(cfg, HeadlessSurfaceExtensions(), NewHeadlessSurface, vk.VkExtent2D{Width: 16, Height: 16}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Destroy()
	b, err := NewRenderer(cfg, HeadlessSurfaceExtensions(), NewHeadlessSurface, vk.VkExtent2D{Width: 16, Height: 16}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()
	if a.Instance.Handle != b.Instance.Handle {
		t.Fatal("concurrent renderers created incompatible global instance dispatch")
	}
	if a.Instance == b.Instance {
		t.Fatal("outputs need independent retained references")
	}
	if a.Device == b.Device || a.Device.Handle == b.Device.Handle {
		t.Fatal("outputs share a device retirement ring")
	}
}

type outputValidation struct {
	mu     sync.Mutex
	errors []string
}

func (h *outputValidation) Enabled(context.Context, slog.Level) bool { return true }
func (h *outputValidation) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		h.mu.Lock()
		h.errors = append(h.errors, r.Message)
		h.mu.Unlock()
	}
	return nil
}
func (h *outputValidation) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *outputValidation) WithGroup(string) slog.Handler      { return h }

func TestOutputsAlternateAndSurviveOwnerClose(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	h := &outputValidation{}
	cfg := Config{AppName: "retained outputs", Validation: true, Log: slog.New(h)}
	a, err := NewRenderer(cfg, nil, NewHeadlessSurface, vk.VkExtent2D{Width: 16, Height: 12}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Destroy()
	b, err := a.NewOutput(nil, NewHeadlessSurface, vk.VkExtent2D{Width: 8, Height: 20}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Destroy()
	owner := a.Instance.owner
	if owner.refs != 2 {
		t.Fatalf("references %d", owner.refs)
	}
	called := false
	if _, err := a.NewOutput([]string{"not-an-enabled-surface-extension"}, func(vk.VkInstance) (vk.VkSurfaceKHR, error) { called = true; return 0, nil }, vk.VkExtent2D{Width: 1, Height: 1}, false); err == nil || called {
		t.Fatal("unsupported extension reached surface creation")
	}
	failure := errors.New("surface creation failed")
	if _, err := a.NewOutput(nil, func(vk.VkInstance) (vk.VkSurfaceKHR, error) { return 0, failure }, vk.VkExtent2D{Width: 1, Height: 1}, false); !errors.Is(err, failure) {
		t.Fatal("surface failure lost")
	}
	if owner.refs != 2 {
		t.Fatal("failed output retained instance")
	}
	if _, err := NewInstance(Config{Validation: false, Log: cfg.Log}, nil); err == nil {
		t.Fatal("conflicting live instance accepted")
	}
	draw := func(r *Renderer, color [4]float32, width, height int) {
		t.Helper()
		frame, ok, err := r.BeginFrame()
		if err != nil || !ok {
			t.Fatalf("begin frame %v %v", ok, err)
		}
		r.BeginSwapchainPass(frame, color)
		img, err := r.EndFrame(frame, true)
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != width || img.Bounds().Dy() != height {
			t.Fatalf("output dimensions %v", img.Bounds())
		}
		pixel := img.RGBAAt(width/2, height/2)
		if pixel.R != uint8(color[0]*255) || pixel.G != uint8(color[1]*255) || pixel.B != uint8(color[2]*255) {
			t.Fatalf("output pixel %v, color %v", pixel, color)
		}
	}
	for range 4 {
		draw(a, [4]float32{1, 0, 0, 1}, 16, 12)
		draw(b, [4]float32{0, 1, 0, 1}, 8, 20)
	}
	if a.Device.frameNo != 4 || b.Device.frameNo != 4 {
		t.Fatal("outputs share retirement epochs")
	}
	a.Destroy()
	a.Destroy()
	if owner.refs != 1 || b.Instance.Handle == 0 {
		t.Fatal("closing first output destroyed surviving instance")
	}
	for range 3 {
		draw(b, [4]float32{0, 0, 1, 1}, 8, 20)
	}
	b.Destroy()
	b.Destroy()
	if owner.refs != 0 || instanceFamily.owner != nil {
		t.Fatal("last output retained native instance")
	}
	if vk.VkCreateDevice != nil || vk.VkCmdDraw != nil {
		t.Fatal("last output retained instance command bindings")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.errors) > 0 {
		t.Fatalf("validation errors: %v", h.errors)
	}
}
