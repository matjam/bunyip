package render

import (
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/matjam/bunyip/internal/render/shaders"
	"github.com/matjam/bunyip/internal/vk"
)

func storeOpRenderer(tb testing.TB, validation bool) *Renderer {
	tb.Helper()
	if err := vk.Load(); err != nil {
		tb.Skipf("no Vulkan: %v", err)
	}
	cfg := Config{AppName: "storeop_test", Validation: validation,
		Log: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))}
	r, err := NewRenderer(cfg, HeadlessSurfaceExtensions(), NewHeadlessSurface, vk.VkExtent2D{Width: 64, Height: 64}, true)
	if err != nil {
		tb.Skipf("no headless renderer: %v", err)
	}
	tb.Cleanup(r.Destroy)
	return r
}

// TestDiscardMSStillResolves draws a triangle into a multisampled target
// whose pass discards its samples, and checks that the colour and the
// depth still resolve into the single-sample images, then records a
// frame that draws nothing, the shape that once faulted after a
// multisampled pass.
func TestDiscardMSStillResolves(t *testing.T) {
	r := storeOpRenderer(t, true)
	if n := r.Device.MaxSamples(); n < 4 {
		t.Skipf("device supports at most %d samples", n)
	}
	extent := vk.VkExtent2D{Width: 64, Height: 64}
	tgt, err := r.Device.NewTargetDesc(TargetDesc{
		Extent: extent, ColorFormat: r.Swapchain.Format, DepthFormat: r.DepthFormat, Samples: 4,
		ColorUsage: vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT, DepthUsage: vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tgt.Destroy()
	p, err := r.Device.NewPipeline(PipelineDesc{
		Vert: shaders.TriangleVert, Frag: shaders.TriangleFrag,
		ColorFormat: r.Swapchain.Format, DepthFormat: r.DepthFormat, Samples: 4, DepthTest: true, DepthWrite: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	for frame := range 2 {
		fr, ok, err := r.BeginFrame()
		if err != nil || !ok {
			t.Fatalf("BeginFrame: ok=%v err=%v", ok, err)
		}
		if frame == 0 {
			pass := PassDesc{Target: tgt, ClearDepth: 1, DiscardMS: true}
			BeginTargetPass(fr.CB, pass)
			vk.VkCmdBindPipeline(fr.CB, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Handle)
			vk.VkCmdDraw(fr.CB, 3, 1, 0, 0)
			EndTargetPassDesc(fr.CB, pass)
		}
		if _, err := r.EndFrame(fr, false); err != nil {
			t.Fatalf("EndFrame: %v", err)
		}
	}
	if err := r.Device.WaitIdle(); err != nil {
		t.Fatal(err)
	}
	img, err := r.Device.ReadImage(tgt.Color)
	if err != nil {
		t.Fatal(err)
	}
	if c := img.RGBAAt(32, 40); c.R+c.G+c.B < 100 {
		t.Errorf("centre pixel %v is not the resolved triangle", c)
	}
	if c := img.RGBAAt(1, 1); c.R|c.G|c.B != 0 {
		t.Errorf("corner pixel %v should be the clear colour", c)
	}
	depth, err := r.Device.ReadDepth(tgt.Depth)
	if err != nil {
		t.Fatal(err)
	}
	if d := depth[40*64+32]; d != 0 {
		t.Errorf("centre depth %g, want the triangle's 0", d)
	}
	if d := depth[1*64+1]; d != 1 {
		t.Errorf("corner depth %g, want the clear value 1", d)
	}
}

// TestReadOnlyDepthKeepsDepth fills a depth image in one pass, then tests
// against it in a second pass that borrows it read-only, and checks the
// second pass both used the depth and left it as the first pass wrote it.
func TestReadOnlyDepthKeepsDepth(t *testing.T) {
	r := storeOpRenderer(t, true)
	extent := vk.VkExtent2D{Width: 64, Height: 64}
	format := vk.VkFormat(vk.VK_FORMAT_R8G8B8A8_UNORM)
	scene, err := r.Device.NewTargetDesc(TargetDesc{
		Extent: extent, ColorFormat: format, DepthFormat: r.DepthFormat, DepthUsage: vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer scene.Destroy()
	other, err := r.Device.NewTargetDesc(TargetDesc{Extent: extent, ColorFormat: format, ColorUsage: vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Destroy()
	write, err := r.Device.NewPipeline(PipelineDesc{Vert: shaders.TriangleVert, Frag: shaders.TriangleFrag,
		ColorFormat: format, DepthFormat: r.DepthFormat, DepthTest: true, DepthWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer write.Destroy()
	// The same triangle again at the same depth fails a less-than test
	// wherever the first one drew, so the second pass only shows where
	// the depth it borrowed says nothing is.
	test, err := r.Device.NewPipeline(PipelineDesc{Vert: shaders.TriangleVert, Frag: shaders.TriangleFrag,
		ColorFormat: format, DepthFormat: r.DepthFormat, DepthTest: true, DepthCompare: vk.VK_COMPARE_OP_LESS})
	if err != nil {
		t.Fatal(err)
	}
	defer test.Destroy()
	fr, ok, err := r.BeginFrame()
	if err != nil || !ok {
		t.Fatalf("BeginFrame: ok=%v err=%v", ok, err)
	}
	BeginTargetPass(fr.CB, PassDesc{Target: scene, ClearDepth: 1})
	vk.VkCmdBindPipeline(fr.CB, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, write.Handle)
	vk.VkCmdDraw(fr.CB, 3, 1, 0, 0)
	EndTargetPass(fr.CB, scene)
	pass := PassDesc{Target: other, Depth: scene.Depth, LoadDepth: true, ReadOnlyDepth: true}
	BeginTargetPass(fr.CB, pass)
	vk.VkCmdBindPipeline(fr.CB, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, test.Handle)
	vk.VkCmdDraw(fr.CB, 3, 1, 0, 0)
	EndTargetPassDesc(fr.CB, pass)
	if _, err := r.EndFrame(fr, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Device.WaitIdle(); err != nil {
		t.Fatal(err)
	}
	img, err := r.Device.ReadImage(other.Color)
	if err != nil {
		t.Fatal(err)
	}
	if c := img.RGBAAt(32, 40); c.R|c.G|c.B != 0 {
		t.Errorf("centre pixel %v drew through the borrowed depth", c)
	}
	depth, err := r.Device.ReadDepth(scene.Depth)
	if err != nil {
		t.Fatal(err)
	}
	if d := depth[40*64+32]; d != 0 {
		t.Errorf("centre depth %g after the read-only pass, want the 0 the first pass wrote", d)
	}
	if d := depth[1*64+1]; d != 1 {
		t.Errorf("corner depth %g after the read-only pass, want 1", d)
	}
}

// BenchmarkMSAAStore renders one triangle into a 4x multisampled
// RGBA16F colour and depth-stencil pair at 2560x1440 that resolves into
// single-sample images, as the scene pass does, with the samples stored
// or discarded (PassDesc.DiscardMS).
func BenchmarkMSAAStore(b *testing.B) {
	r := storeOpRenderer(b, false)
	d := r.Device
	extent := vk.VkExtent2D{Width: 2560, Height: 1440}
	const format = vk.VK_FORMAT_R16G16B16A16_SFLOAT
	samples := d.SampleCount(4)
	t, err := d.NewTargetDesc(TargetDesc{Extent: extent, ColorFormat: format, DepthFormat: r.DepthFormat, Samples: samples})
	if err != nil {
		b.Fatal(err)
	}
	defer t.Destroy()
	p, err := d.NewPipeline(PipelineDesc{Vert: shaders.TriangleVert, Frag: shaders.TriangleFrag, ColorFormat: format,
		DepthFormat: r.DepthFormat, Samples: samples, DepthTest: true, DepthWrite: true})
	if err != nil {
		b.Fatal(err)
	}
	defer p.Destroy()
	for _, discard := range []bool{false, true} {
		b.Run(fmt.Sprintf("discard=%v", discard), func(b *testing.B) {
			frame := func() {
				fr, ok, err := r.BeginFrame()
				if err != nil || !ok {
					b.Fatal(err)
				}
				pass := PassDesc{Target: t, ClearDepth: 1, DiscardMS: discard}
				BeginTargetPass(fr.CB, pass)
				vk.CmdBindPipeline(fr.CB, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Handle)
				vk.CmdDraw(fr.CB, 3, 1, 0, 0)
				EndTargetPassDesc(fr.CB, pass)
				if _, err := r.EndFrame(fr, false); err != nil {
					b.Fatal(err)
				}
			}
			for range 4 {
				frame()
			}
			for b.Loop() {
				frame()
			}
		})
	}
}
