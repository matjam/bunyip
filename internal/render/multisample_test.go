package render

import (
	"log/slog"
	"os"
	"testing"

	"github.com/matjam/bunyip/internal/render/shaders"
	"github.com/matjam/bunyip/internal/vk"
)

// TestMultisampledTargetThenEmptyFrame draws into a multisampled target
// and then records a frame that draws nothing to the screen, which is
// what a frame that only updates a render texture looks like.
//
// It is a regression test for a GPU fault: a swapchain pass whose depth
// attachment is cleared and discarded, with no draw to make the driver
// program the render target, kept the sample count of the multisampled
// pass before it and wrote outside the single-sample depth image.
// BeginSwapchainPass stores its depth attachment to stay off that path.
func TestMultisampledTargetThenEmptyFrame(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := Config{AppName: "multisample_test", Validation: true, Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := NewRenderer(cfg, HeadlessSurfaceExtensions(), NewHeadlessSurface, vk.VkExtent2D{Width: 64, Height: 64}, true)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer r.Destroy()
	if n := r.Device.MaxSamples(); n < 4 {
		t.Skipf("device supports at most %d samples", n)
	}
	extent := vk.VkExtent2D{Width: 64, Height: 64}
	tgt, err := r.Device.NewTargetDesc(TargetDesc{
		Extent: extent, ColorFormat: r.Swapchain.Format, DepthFormat: r.DepthFormat, Samples: 4,
	})
	if err != nil {
		t.Fatalf("NewTargetDesc: %v", err)
	}
	defer tgt.Destroy()
	if tgt.MSColor == nil || tgt.MSDepth == nil {
		t.Fatal("a multisampled target needs multisampled attachments to resolve from")
	}
	p, err := r.Device.NewPipeline(PipelineDesc{
		Vert: shaders.TriangleVert, Frag: shaders.TriangleFrag,
		ColorFormat: r.Swapchain.Format, DepthFormat: r.DepthFormat, Samples: 4,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer p.Destroy()

	fr, ok, err := r.BeginFrame()
	if err != nil || !ok {
		t.Fatalf("BeginFrame: ok=%v err=%v", ok, err)
	}
	BeginTargetPass(fr.CB, PassDesc{Target: tgt, ClearDepth: 1})
	vk.VkCmdBindPipeline(fr.CB, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Handle)
	vk.VkCmdDraw(fr.CB, 3, 1, 0, 0)
	EndTargetPass(fr.CB, tgt)
	if _, err := r.EndFrame(fr, false); err != nil {
		t.Fatalf("EndFrame: %v", err)
	}
	if err := r.Device.WaitIdle(); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}
	// The resolve wrote the single-sample image: the triangle's own
	// colours in the middle, the clear at the corners.
	img, err := r.Device.ReadImage(tgt.Color)
	if err != nil {
		t.Fatalf("ReadImage: %v", err)
	}
	if c := img.RGBAAt(32, 40); c.R+c.G+c.B < 100 {
		t.Errorf("centre pixel %v is not the resolved triangle", c)
	}
	if c := img.RGBAAt(1, 1); c.R|c.G|c.B != 0 {
		t.Errorf("corner pixel %v should be the clear colour", c)
	}
}

// TestSampleCount checks the clamping of a requested sample count.
func TestSampleCount(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	cfg := Config{AppName: "samples_test", Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := NewRenderer(cfg, HeadlessSurfaceExtensions(), NewHeadlessSurface, vk.VkExtent2D{Width: 8, Height: 8}, true)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	defer r.Destroy()
	maxN := r.Device.MaxSamples()
	if maxN < 1 {
		t.Fatalf("MaxSamples = %d, want at least 1", maxN)
	}
	cases := []struct {
		name string
		ask  int
		want vk.VkSampleCountFlagBits
	}{
		{"zero means one", 0, vk.VK_SAMPLE_COUNT_1_BIT},
		{"one", 1, vk.VK_SAMPLE_COUNT_1_BIT},
		{"negative means one", -4, vk.VK_SAMPLE_COUNT_1_BIT},
		{"three rounds down to two", 3, vk.VK_SAMPLE_COUNT_2_BIT},
		{"above the maximum clamps", 1024, vk.VkSampleCountFlagBits(maxN)},
	}
	for _, c := range cases {
		if got := r.Device.SampleCount(c.ask); got != c.want {
			t.Errorf("%s: SampleCount(%d) = %d, want %d", c.name, c.ask, got, c.want)
		}
	}
}
