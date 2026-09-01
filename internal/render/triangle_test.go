package render

import (
	"log/slog"
	"os"
	"testing"

	"github.com/matjam/bunyip/internal/render/shaders"
	"github.com/matjam/bunyip/internal/vk"
)

// TestTriangleHeadless draws the smoke-test triangle and checks that its
// centre is lit and the corners still show the clear colour.
func TestTriangleHeadless(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := Config{AppName: "triangle_test", Validation: true, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := NewRenderer(cfg, HeadlessSurfaceExtensions(), NewHeadlessSurface, vk.VkExtent2D{Width: 128, Height: 128}, true)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer r.Destroy()
	p, err := r.Device.NewPipeline(PipelineDesc{Vert: shaders.TriangleVert, Frag: shaders.TriangleFrag, ColorFormat: r.Swapchain.Format})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer p.Destroy()

	fr, ok, err := r.BeginFrame([4]float32{0, 0, 0, 1})
	if err != nil || !ok {
		t.Fatalf("BeginFrame: ok=%v err=%v", ok, err)
	}
	SetViewport(fr.CB, fr.Extent)
	vk.VkCmdBindPipeline(fr.CB, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Handle)
	vk.VkCmdDraw(fr.CB, 3, 1, 0, 0)
	img, err := r.EndFrame(fr, true)
	if err != nil {
		t.Fatalf("EndFrame: %v", err)
	}
	centre := img.RGBAAt(64, 70)
	if centre.R+centre.G+centre.B < 200 {
		t.Errorf("centre pixel %v is not lit", centre)
	}
	for _, p := range [][2]int{{0, 0}, {127, 0}, {0, 127}, {127, 127}} {
		if c := img.RGBAAt(p[0], p[1]); c.R|c.G|c.B != 0 {
			t.Errorf("corner %v = %v, want black", p, c)
		}
	}
	// Vertex 0 (red) is at the top: with the viewport unflipped, NDC -Y is up.
	top := img.RGBAAt(64, 30)
	if top.R < 150 || top.G > 120 || top.B > 120 {
		t.Errorf("top pixel %v should be mostly red", top)
	}
}
