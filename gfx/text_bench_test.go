package gfx

import (
	"fmt"
	"log/slog"
	"os"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// benchParagraph is 880 bytes of plain Latin text, the size a wrapped
// paragraph of help or dialogue runs to.
const benchParagraph = "The bunyip is a creature from the aboriginal mythology of southeastern " +
	"Australia, said to lurk in swamps, billabongs, creeks, riverbeds and waterholes. " +
	"Descriptions of bunyips vary widely, and George French Angas records a bunyip as a " +
	"water spirit from the Moorundi people of the Murray River, downstream of the junction " +
	"with the Darling, saying it is much dreaded by them. Robert Brough Smyth devoted ten " +
	"pages to the bunyip in his work of eighteen seventy eight, but concluded that of the " +
	"bunyip he knew little beyond the fact that it was a source of great terror, and that " +
	"the accounts of it were so various that no clear picture could be drawn. Some modern " +
	"writers have linked the bunyip to the memory of extinct marsupials such as the " +
	"diprotodon, while others read it as a purely spiritual figure of the waterways."

// benchHeadless is newHeadless for benchmarks, which get a testing.B.
func benchHeadless(tb testing.TB, w, h int) *Graphics {
	tb.Helper()
	if err := vk.Load(); err != nil {
		tb.Skipf("no Vulkan: %v", err)
	}
	cfg := render.Config{AppName: "gfx_bench", Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	r, err := render.NewRenderer(cfg, render.HeadlessSurfaceExtensions(), render.NewHeadlessSurface,
		vk.VkExtent2D{Width: uint32(w), Height: uint32(h)}, true)
	if err != nil {
		tb.Skipf("no headless renderer: %v", err)
	}
	g, err := newGraphics(r)
	if err != nil {
		r.Destroy()
		tb.Fatalf("gfx: new graphics: %v", err)
	}
	tb.Cleanup(func() { g.destroy(); r.Destroy() })
	return g
}

// benchFont makes a font for benchmarking and warms its atlas so no
// iteration pays for rasterising a glyph.
func benchFont(tb testing.TB, g *Graphics, size float32) *Font {
	tb.Helper()
	f, err := g.NewFont(goregular.TTF, size, FontOptions{})
	if err != nil {
		tb.Fatalf("gfx: new font: %v", err)
	}
	tb.Cleanup(f.Destroy)
	return f
}

// benchLabels is 200 distinct short strings, the shape a frame of
// interface labels takes.
func benchLabels() []string {
	out := make([]string, 200)
	for i := range out {
		out[i] = fmt.Sprintf("Label %d", i)
	}
	return out
}

// BenchmarkDrawTextBlock_880 draws one wrapped 880-byte paragraph per
// iteration, the cost of a block of prose on screen every frame.
func BenchmarkDrawTextBlock_880(b *testing.B) {
	g := benchHeadless(b, 640, 480)
	g.SetView(640, 480)
	f := benchFont(b, g, 16)
	opts := TextOptions{Width: 560}
	g.DrawTextBlock(f, benchParagraph, 8, 8, opts, White)
	g.main.reset()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		g.DrawTextBlock(f, benchParagraph, 8, 8, opts, White)
		g.main.reset()
	}
}

// BenchmarkLayoutBlock_880 lays the paragraph out every iteration,
// skipping the glyph cache, so that the cost of the shaping, wrapping,
// alignment and rune-index work is visible on its own.
func BenchmarkLayoutBlock_880(b *testing.B) {
	g := benchHeadless(b, 640, 480)
	g.SetView(640, 480)
	f := benchFont(b, g, 16)
	opts := TextOptions{Width: 560}
	f.layoutBlock(benchParagraph, opts)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		f.layoutBlock(benchParagraph, opts)
	}
}

// BenchmarkDrawText_200Labels draws 200 distinct short labels per
// iteration, the interface workload: the same strings every frame, so
// the caches are warm from the second frame on.
func BenchmarkDrawText_200Labels(b *testing.B) {
	g := benchHeadless(b, 640, 480)
	g.SetView(640, 480)
	f := benchFont(b, g, 16)
	labels := benchLabels()
	for i, s := range labels {
		g.DrawText(f, s, 8, float32(i%40)*12, White)
	}
	g.main.reset()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for i, s := range labels {
			g.DrawText(f, s, 8, float32(i%40)*12, White)
		}
		g.main.reset()
	}
}

// BenchmarkMeasure_Static measures the same static label repeatedly, the
// way the interface measures a label every frame to lay it out.
func BenchmarkMeasure_Static(b *testing.B) {
	g := benchHeadless(b, 640, 480)
	g.SetView(640, 480)
	f := benchFont(b, g, 16)
	f.Measure("Save and quit", TextOptions{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		f.Measure("Save and quit", TextOptions{})
	}
}
