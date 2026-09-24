package gfx

// Load and startup benchmarks: renderer creation, environment
// prefiltering, panorama decoders, SDF fonts, texture, mesh and KTX2
// uploads. Each reports MB/s where a byte count is meaningful.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"os"
	"testing"

	"golang.org/x/image/font/gofont/gobold"

	"github.com/matjam/bunyip/gfx/ktx2"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

func loadBenchRenderer(tb testing.TB) *render.Renderer {
	tb.Helper()
	if err := vk.Load(); err != nil {
		tb.Skipf("no Vulkan: %v", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	r, err := render.NewRenderer(render.Config{AppName: "load_bench", Log: log}, render.HeadlessSurfaceExtensions(),
		render.NewHeadlessSurface, vk.VkExtent2D{Width: 256, Height: 256}, true)
	if err != nil {
		tb.Skipf("no headless renderer: %v", err)
	}
	return r
}

// BenchmarkLoadNewRenderer is instance, device, allocator and headless
// target creation, the part of startup before Graphics.
func BenchmarkLoadNewRenderer(b *testing.B) {
	loadBenchRenderer(b).Destroy()
	for b.Loop() {
		loadBenchRenderer(b).Destroy()
	}
}

// BenchmarkLoadSDFFont is NewSDFFont with the ASCII preload.
func BenchmarkLoadSDFFont(b *testing.B) {
	g := drawBenchHeadless(b, 256, 256)
	b.ReportAllocs()
	for b.Loop() {
		f, err := g.NewSDFFont(gobold.TTF, 32, FontOptions{})
		if err != nil {
			b.Fatal(err)
		}
		f.Destroy()
	}
}

func loadBenchPanorama(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), uint8((x ^ y) & 255), 255})
		}
	}
	return img
}

// BenchmarkLoadEnvironment is NewEnvironment from an sRGB panorama (the
// per-pixel conversion plus the CPU prefilter) and NewEnvironmentHDR (the
// prefilter alone), at the default and a larger cube size.
func BenchmarkLoadEnvironment(b *testing.B) {
	g := drawBenchHeadless(b, 256, 256)
	pano := loadBenchPanorama(2048, 1024)
	hdr := &HDRImage{Width: 2048, Height: 1024, Pix: make([]float32, 2048*1024*3)}
	for i := range hdr.Pix {
		hdr.Pix[i] = float32(i%97) / 50
	}
	b.Run("srgb2048_size128", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			env, err := g.NewEnvironment(pano, EnvironmentOptions{})
			if err != nil {
				b.Fatal(err)
			}
			env.Destroy()
		}
	})
	for _, size := range []int{128, 256} {
		b.Run(fmt.Sprintf("hdr2048_size%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				env, err := g.NewEnvironmentHDR(hdr, EnvironmentOptions{Size: size})
				if err != nil {
					b.Fatal(err)
				}
				env.Destroy()
			}
		})
	}
}

// BenchmarkLoadDecodeEXR decodes a 4096x2048 half-float RGB panorama, ZIP
// compressed and uncompressed.
func BenchmarkLoadDecodeEXR(b *testing.B) {
	const w, h = 4096, 2048
	pix := exrGradient(w, h)
	for _, c := range []struct {
		name string
		comp int
	}{{"zip", exrZIP}, {"none", exrNone}} {
		data := encodeEXR(w, h, pix, true, c.comp)
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(w * h * 3 * 4)) // decoded float bytes
			for b.Loop() {
				if _, err := DecodeEXR(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// loadBenchHDRFile writes a flat (not run-length encoded) Radiance file.
func loadBenchHDRFile(w, h int) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y %d +X %d\n", h, w)
	row := make([]byte, w*4)
	for y := range h {
		for x := range w {
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = byte(x), byte(y), 128, 129
		}
		// A first pixel of 2,2 would read as the RLE marker.
		row[0] = 3
		buf.Write(row)
	}
	return buf.Bytes()
}

// BenchmarkLoadDecodeHDR decodes a 4096x2048 Radiance file.
func BenchmarkLoadDecodeHDR(b *testing.B) {
	data := loadBenchHDRFile(4096, 2048)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := DecodeHDR(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoadTexture is NewTexture outside a frame, the Init path.
func BenchmarkLoadTexture(b *testing.B) {
	g := drawBenchHeadless(b, 256, 256)
	for _, side := range []int{64, 2048} {
		img := loadBenchPanorama(side, side)
		b.Run(fmt.Sprint(side), func(b *testing.B) {
			b.SetBytes(int64(side * side * 4))
			for b.Loop() {
				t, err := g.NewTexture(img, TextureOptions{Linear: true})
				if err != nil {
					b.Fatal(err)
				}
				t.Destroy()
			}
		})
	}
}

// BenchmarkLoadMesh is NewMesh of a cube outside a frame, the Init path.
func BenchmarkLoadMesh(b *testing.B) {
	g := drawBenchHeadless(b, 256, 256)
	v, i := CubeMesh()
	for b.Loop() {
		m, err := g.NewMesh(v, i)
		if err != nil {
			b.Fatal(err)
		}
		m.Destroy()
	}
}

// BenchmarkLoadMany is what a game's Init does: 200 meshes and 100 small
// textures created outside a frame, then one frame drawn, which needs
// them all on the GPU. It reports the time per resource; destroying them
// afterwards is not timed.
func BenchmarkLoadMany(b *testing.B) {
	g := drawBenchHeadless(b, 64, 64)
	v, idx := CubeMesh()
	img := loadBenchPanorama(64, 64)
	const meshes, textures = 200, 100
	ms := make([]*Mesh, 0, meshes)
	ts := make([]*Texture, 0, textures)
	for b.Loop() {
		for range meshes {
			m, err := g.NewMesh(v, idx)
			if err != nil {
				b.Fatal(err)
			}
			ms = append(ms, m)
		}
		for range textures {
			t, err := g.NewTexture(img, TextureOptions{})
			if err != nil {
				b.Fatal(err)
			}
			ts = append(ts, t)
		}
		if err := g.r.Device.WaitIdle(); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		for _, m := range ms {
			m.Destroy()
		}
		for _, t := range ts {
			t.Destroy()
		}
		ms, ts = ms[:0], ts[:0]
		b.StartTimer()
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N)/(meshes+textures), "us/resource")
}

// loadBenchScene is a small lit room for the bake benchmarks.
func loadBenchScene(b *testing.B, g *Graphics) func() {
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(cube.Destroy)
	return func() {
		g.SetLight(Light{Direction: lin.V3(-0.3, -1, -0.2), Color: Color{1, 1, 1, 1}, Sky: Sky{Zenith: Color{0.2, 0.3, 0.8, 1}}})
		g.DrawMesh(cube, Material{BaseColor: Color{1, 0, 0, 1}}, lin.Scale(lin.V3(-6, -6, -6)))
		g.DrawMesh(cube, Material{BaseColor: Color{0, 1, 0, 1}}, lin.Translate(lin.V3(2, 0, 0)))
	}
}

// BenchmarkLoadBakeProbe is BakeProbe of one reflection probe at the
// default resolution.
func BenchmarkLoadBakeProbe(b *testing.B) {
	g := drawBenchHeadless(b, 64, 64)
	scene := loadBenchScene(b, g)
	p := &ReflectionProbe{Extent: lin.V3(5, 5, 5)}
	b.Cleanup(p.Destroy)
	for b.Loop() {
		if err := g.BakeProbe(p, scene); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoadBakeLightProbes is BakeLightProbes of a 4x2x4 grid.
func BenchmarkLoadBakeLightProbes(b *testing.B) {
	g := drawBenchHeadless(b, 64, 64)
	scene := loadBenchScene(b, g)
	grid := &LightProbeGrid{Origin: lin.V3(-4, -2, -4), Spacing: lin.V3(2, 2, 2), Counts: [3]int{4, 2, 4}}
	for b.Loop() {
		if err := g.BakeLightProbes(grid, scene); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoadKTX2 is NewCompressedTexture of a 2048 BC1 file with its
// mip chain, and ktx2.Parse alone.
func BenchmarkLoadKTX2(b *testing.B) {
	g := drawBenchHeadless(b, 256, 256)
	f, err := ktx2.Encode(loadBenchPanorama(2048, 2048), ktx2.Options{Format: ktx2.BC1RGBSRGB})
	if err != nil {
		b.Fatal(err)
	}
	data, err := f.Bytes()
	if err != nil {
		b.Fatal(err)
	}
	b.Run("parse", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			if _, err := ktx2.Parse(data); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("upload", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		for b.Loop() {
			t, err := g.NewCompressedTexture(data, TextureOptions{})
			if err != nil {
				b.Fatal(err)
			}
			t.Destroy()
		}
	})
}
