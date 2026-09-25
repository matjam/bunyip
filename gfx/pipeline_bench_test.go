package gfx

import (
	"log/slog"
	"os"
	"testing"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// benchRenderer is a headless renderer without validation, for the
// startup benchmarks. Its pipeline cache lives in memory.
func benchRenderer(tb testing.TB) *render.Renderer {
	tb.Helper()
	if err := vk.Load(); err != nil {
		tb.Skipf("no Vulkan: %v", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	r, err := render.NewRenderer(render.Config{AppName: "gfx_pipeline_bench", Log: log}, render.HeadlessSurfaceExtensions(),
		render.NewHeadlessSurface, vk.VkExtent2D{Width: 256, Height: 256}, true)
	if err != nil {
		tb.Skipf("no headless renderer: %v", err)
	}
	return r
}

// BenchmarkNewGraphics is newGraphics on a live renderer until every
// pipeline it builds is ready: what a game's startup waits for before it
// can draw a 3D frame. The renderer's pipeline cache is warm after the
// first iteration, as it is on a second start that loads its file.
func BenchmarkNewGraphics(b *testing.B) {
	r := benchRenderer(b)
	defer r.Destroy()
	for b.Loop() {
		g, err := newGraphics(r)
		if err != nil {
			b.Fatal(err)
		}
		if err := g.waitPipelines(); err != nil {
			b.Fatal(err)
		}
		g.destroy()
	}
}

// BenchmarkNewGraphics2D is newGraphics until it returns: what a game
// that draws only 2D waits for, since the scene's pipelines go on
// building on workers.
func BenchmarkNewGraphics2D(b *testing.B) {
	r := benchRenderer(b)
	defer r.Destroy()
	for b.Loop() {
		g, err := newGraphics(r)
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		g.destroy()
		b.StartTimer()
	}
}

// BenchmarkPipelineVariant is one mesh pipeline variant of the default
// PBR program built from nothing but the device's pipeline cache: the
// cost of a variant a draw asks for the first time.
func BenchmarkPipelineVariant(b *testing.B) {
	g := drawBenchHeadless(b, 256, 256)
	for b.Loop() {
		s := &Shader{g: g, frag: shaders.PBRFrag, oitFrag: shaders.PBROITFrag, mesh: true, pipes: map[pipeKey]*render.Pipeline{}}
		if _, err := s.pipeline(pipeKey{blend: BlendReplace}); err != nil {
			b.Fatal(err)
		}
		s.Destroy()
	}
}

// BenchmarkPipelineVariants is the four variants a new blended material
// asks for at once (opaque and blended, each at one and four samples),
// started together as startSceneVariants and startSampleVariants start
// them.
func BenchmarkPipelineVariants(b *testing.B) {
	g := drawBenchHeadless(b, 256, 256)
	keys := []pipeKey{
		{blend: BlendReplace}, {blend: BlendAlpha},
		{blend: BlendReplace, out: outKey{samples: 4}}, {blend: BlendAlpha, out: outKey{samples: 4}},
	}
	for b.Loop() {
		s := &Shader{g: g, frag: shaders.PBRFrag, oitFrag: shaders.PBROITFrag, mesh: true, pipes: map[pipeKey]*render.Pipeline{}}
		for _, k := range keys {
			s.start(k)
		}
		for _, k := range keys {
			if _, err := s.pipeline(k); err != nil {
				b.Fatal(err)
			}
		}
		s.Destroy()
	}
}
