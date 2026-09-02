package gfx

import (
	"image"
	"image/color"
	"log/slog"
	"math"
	"os"
	"testing"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// drawBenchHeadless is newHeadless for the draw benchmarks, which get a
// testing.B. Validation is off: it would dominate the measurement.
func drawBenchHeadless(tb testing.TB, w, h int) *Graphics {
	tb.Helper()
	if err := vk.Load(); err != nil {
		tb.Skipf("no Vulkan: %v", err)
	}
	// Warnings and worse only: the device log would land in the middle of
	// the benchmark's own output line.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := render.Config{AppName: "gfx_draw_bench", Log: log}
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

// BenchmarkFillPath_50RoundRects fills fifty rounded rectangles into an
// open frame, the shape a panel-heavy interface draws every frame. The
// 2D stream is reset each iteration so the measurement is the path work
// and not the stream growing without bound.
func BenchmarkFillPath_50RoundRects(b *testing.B) {
	g := drawBenchHeadless(b, 256, 256)
	ok, err := g.begin(Black)
	if err != nil {
		b.Fatalf("begin: %v", err)
	}
	if !ok {
		b.Skip("no frame")
	}
	var p Path
	c := RGB(200, 120, 60)
	for b.Loop() {
		g.main.stream.reset()
		for i := range 50 {
			p.Reset()
			x := float32(i%10) * 24
			y := float32(i/10) * 24
			p.RoundRect(x, y, 20, 20, 6)
			g.FillPath(&p, c, FillOptions{})
		}
	}
	b.StopTimer()
	if _, err := g.end(false); err != nil {
		b.Fatalf("end: %v", err)
	}
}

// BenchmarkStrokePath_50RoundRects is the stroking counterpart.
func BenchmarkStrokePath_50RoundRects(b *testing.B) {
	g := drawBenchHeadless(b, 256, 256)
	ok, err := g.begin(Black)
	if err != nil {
		b.Fatalf("begin: %v", err)
	}
	if !ok {
		b.Skip("no frame")
	}
	var p Path
	c := RGB(200, 120, 60)
	for b.Loop() {
		g.main.stream.reset()
		for i := range 50 {
			p.Reset()
			x := float32(i%10) * 24
			y := float32(i/10) * 24
			p.RoundRect(x, y, 20, 20, 6)
			g.StrokePath(&p, c, StrokeOptions{Width: 2})
		}
	}
	b.StopTimer()
	if _, err := g.end(false); err != nil {
		b.Fatalf("end: %v", err)
	}
}

// benchDraws builds n mesh draws with a spread of depths, materials,
// shaders and meshes, the mixture prepareDraws has to order. Nothing is
// dereferenced by the sort, so the pointers stand in for real objects.
func benchDraws(n int) []meshDraw {
	shaders := make([]*Shader, 4)
	for i := range shaders {
		shaders[i] = &Shader{}
	}
	meshes := make([]*Mesh, 16)
	for i := range meshes {
		meshes[i] = &Mesh{}
	}
	draws := make([]meshDraw, n)
	for i := range draws {
		d := &draws[i]
		d.mesh = meshes[(i*7)%len(meshes)]
		d.shader = shaders[(i*3)%len(shaders)]
		d.uniform = int32(i % 3)
		d.set = vk.VkDescriptorSet(uintptr(1 + i%5))
		d.depth = float32(math.Mod(float64(i)*37.5, 100))
		d.culled = i%9 == 0
		d.skinned = i%11 == 0
		d.mat = Material{Roughness: 0.5}
		if i%4 == 0 {
			d.mat.Blend = true // a quarter of the scene is transparent
		}
		d.blended = d.mat.blended() // prepareDraws resolves this before sorting
	}
	return draws
}

// BenchmarkMeshSort_8000 measures the ordering of a frame's mesh draws,
// which is the whole cost of prepareDraws once the material sets are
// cached. It needs no device.
func BenchmarkMeshSort_8000(b *testing.B) {
	src := benchDraws(8000)
	var q drawQueue
	q.draws = make([]meshDraw, len(src))
	for b.Loop() {
		copy(q.draws, src)
		sink = q.sortDraws()
	}
}

// sink keeps the sort's result live so it is not optimised away.
var sink drawList

// BenchmarkMaterialSet_8000 resolves the descriptor set for a frame's
// worth of draws sharing one material, which is what prepareDraws does
// before it sorts.
func BenchmarkMaterialSet_8000(b *testing.B) {
	g := drawBenchHeadless(b, 64, 64)
	scene := g.post.main.scene
	mat := Material{Roughness: 0.5}
	if _, err := g.materialSet(&mat, nil, scene); err != nil {
		b.Fatalf("materialSet: %v", err)
	}
	for b.Loop() {
		for range 8000 {
			if _, err := g.materialSet(&mat, nil, scene); err != nil {
				b.Fatalf("materialSet: %v", err)
			}
		}
	}
}

// benchSprites drives the 2D stream the way a frame of 20k sprites does:
// each sprite is its own item, states cycle over eight textures, and the
// layer either varies (forcing the layer sort) or does not.
func benchSprites(b *testing.B, layered bool) {
	b.Helper()
	const n = 20000
	var s stream2D
	proj := lin.Ortho2D(1280, 720)
	verts := spriteVertices(Sprite{Size: lin.V2(16, 16), Color: White, UV1: lin.V2(1, 1)}, nil)
	sets := [8]vk.VkDescriptorSet{}
	for i := range sets {
		sets[i] = vk.VkDescriptorSet(uintptr(i + 1))
	}
	for b.Loop() {
		s.reset()
		pr := s.proj(proj)
		for i := range n {
			var layer int32
			if layered {
				layer = int32(i % 16)
			}
			s.add(state2D{set: sets[i%len(sets)], uniform: -1, blend: BlendAlpha, proj: pr}, layer, verts)
		}
		s.build()
	}
}

// BenchmarkFlush2D_200Runs records a frame whose 2D stream breaks into
// two hundred draw runs: consecutive sprites alternate between two
// textures, so no two merge. It measures what recording a run costs, and
// after warm-up it must not allocate.
func BenchmarkFlush2D_200Runs(b *testing.B) {
	g := drawBenchHeadless(b, 256, 256)
	one := image.NewRGBA(image.Rect(0, 0, 1, 1))
	one.SetRGBA(0, 0, color.RGBA{255, 255, 255, 255})
	texA, err := g.NewTexture(one, TextureOptions{})
	if err != nil {
		b.Fatalf("texture: %v", err)
	}
	defer texA.Destroy()
	texB, err := g.NewTexture(one, TextureOptions{})
	if err != nil {
		b.Fatalf("texture: %v", err)
	}
	defer texB.Destroy()
	frame := func() {
		ok, err := g.begin(Black)
		if err != nil {
			b.Fatalf("begin: %v", err)
		}
		if !ok {
			b.Skip("no frame")
		}
		for i := range 200 {
			tex := texA
			if i%2 == 1 {
				tex = texB
			}
			g.Draw(tex, Sprite{Pos: lin.V2(float32(i%16), float32(i/16)), Size: lin.V2(8, 8)})
		}
		if _, err := g.end(false); err != nil {
			b.Fatalf("end: %v", err)
		}
	}
	frame() // warm up the pipelines, descriptor sets and stream buffers
	b.ResetTimer()
	for b.Loop() {
		frame()
	}
}

// BenchmarkSprites_20kLayered is 20k sprites spread over sixteen layers,
// so the stream has to order them before merging draws.
func BenchmarkSprites_20kLayered(b *testing.B) { benchSprites(b, true) }

// BenchmarkSprites_20kUnlayered is the same 20k sprites all on layer
// zero, which submission order already orders.
func BenchmarkSprites_20kUnlayered(b *testing.B) { benchSprites(b, false) }
