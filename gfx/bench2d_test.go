package gfx

// Benchmarks of 2D drawing, text and particles. Frame benchmarks open and
// submit a real frame with validation off; queue benchmarks record into
// an open frame and reset the queue each iteration, so they measure the
// CPU work of queueing alone.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"slices"
	"strings"
	"testing"
	"unsafe"

	ot "github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/lin"
)

// a2dTextures makes n distinct 16x16 textures.
func a2dTextures(b *testing.B, g *Graphics, n int) []*Texture {
	b.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	out := make([]*Texture, n)
	for i := range out {
		tex, err := g.NewTexture(img, TextureOptions{})
		if err != nil {
			b.Fatalf("texture: %v", err)
		}
		b.Cleanup(tex.Destroy)
		out[i] = tex
	}
	return out
}

// streamQuads is the number of quads the main queue's 2D stream holds.
func streamQuads(g *Graphics) int { return len(g.main.stream.verts) / 6 }

// a2dFrame opens a frame, runs draw and submits it.
func a2dFrame(b *testing.B, g *Graphics, draw func()) {
	b.Helper()
	ok, err := g.begin(Black)
	if err != nil {
		b.Fatalf("begin: %v", err)
	}
	if !ok {
		b.Skip("no frame")
	}
	draw()
	if _, err := g.end(false); err != nil {
		b.Fatalf("end: %v", err)
	}
}

// a2dQueue opens a frame and runs draw once per iteration, resetting the
// queue between iterations, then submits one empty frame.
func a2dQueue(b *testing.B, g *Graphics, draw func()) {
	b.Helper()
	ok, err := g.begin(Black)
	if err != nil {
		b.Fatalf("begin: %v", err)
	}
	if !ok {
		b.Skip("no frame")
	}
	draw() // warm caches and grow the stream
	g.main.reset()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		draw()
		g.main.reset()
	}
	b.StopTimer()
	if _, err := g.end(false); err != nil {
		b.Fatalf("end: %v", err)
	}
}

// a2dSprites50k draws 50k 8x8 sprites over 16 layers and 8 textures.
// With grouped, the texture changes every 64 sprites (a game drawing
// entity batches); otherwise every sprite.
func a2dSprites50k(g *Graphics, texes []*Texture, grouped bool, rotate bool) {
	for i := range 50000 {
		t := i % len(texes)
		if grouped {
			t = (i / 64) % len(texes)
		}
		g.SetLayer(i % 16)
		s := Sprite{Pos: lin.V2(float32(i%320)*4, float32(i/320)*4), Size: lin.V2(8, 8)}
		if rotate {
			s.Rotation, s.Origin = float32(i%7)*0.2, lin.V2(0.5, 0.5)
		}
		g.Draw(texes[t], s)
	}
	g.SetLayer(0)
}

// Benchmark2DSprites50kFrame is a whole frame of 50k sprites: queue,
// layer sort, merge, upload and record.
func Benchmark2DSprites50kFrame(b *testing.B) {
	for _, grouped := range []bool{true, false} {
		name := "interleaved"
		if grouped {
			name = "grouped64"
		}
		b.Run(name, func(b *testing.B) {
			g := drawBenchHeadless(b, 1280, 720)
			g.SetView(1280, 720)
			texes := a2dTextures(b, g, 8)
			a2dFrame(b, g, func() { a2dSprites50k(g, texes, grouped, false) })
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				a2dFrame(b, g, func() { a2dSprites50k(g, texes, grouped, false) })
			}
			b.StopTimer()
			b.ReportMetric(float64(g.Stats().Draws2D), "draws")
		})
	}
}

// Benchmark2DSprites50kQueue is the queueing half alone: Draw for each
// sprite, with and without a camera and a rotation.
func Benchmark2DSprites50kQueue(b *testing.B) {
	for _, tc := range []struct {
		name           string
		camera, rotate bool
	}{{"screen", false, false}, {"camera", true, false}, {"camera_rotated", true, true}} {
		b.Run(tc.name, func(b *testing.B) {
			g := drawBenchHeadless(b, 1280, 720)
			g.SetView(1280, 720)
			texes := a2dTextures(b, g, 8)
			a2dQueue(b, g, func() {
				if tc.camera {
					// Half the sprites are left of the view.
					g.SetCamera2D(Camera2D{Position: lin.V2(1280, 360)})
				}
				a2dSprites50k(g, texes, true, tc.rotate)
			})
		})
	}
}

// Benchmark2DTilemap256 draws a 256x256 map of 16px tiles scrolled a
// pixel a frame, once under a camera (culled to the view) and once in
// screen space with the scroll passed as the map's origin.
func Benchmark2DTilemap256(b *testing.B) {
	for _, camera := range []bool{true, false} {
		name := "screen_offset"
		if camera {
			name = "camera"
		}
		b.Run(name, func(b *testing.B) {
			g := drawBenchHeadless(b, 1280, 720)
			g.SetView(1280, 720)
			img := image.NewRGBA(image.Rect(0, 0, 256, 256))
			for i := range img.Pix {
				img.Pix[i] = 200
			}
			tex, err := g.NewTexture(img, TextureOptions{})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(tex.Destroy)
			tm := NewTilemap(NewSheet(tex, 16, 16), 256, 256)
			for i := range tm.Tiles {
				tm.Tiles[i] = i % 256
			}
			frame := 0
			var queued int
			a2dQueue(b, g, func() {
				frame++
				scroll := float32(frame % 2000)
				if camera {
					g.SetCamera2D(Camera2D{Position: lin.V2(640+scroll, 360+scroll)})
					g.DrawTilemap(tm, 0, 0, White)
				} else {
					g.DrawTilemap(tm, -scroll, -scroll, White)
				}
				queued = streamQuads(g)
			})
			b.ReportMetric(float64(queued), "tiles_queued")
		})
	}
}

// Benchmark2DText5kLabels draws n distinct short labels a frame, every
// one cached after the first frame if the cache holds them.
func Benchmark2DText5kLabels(b *testing.B) {
	for _, n := range []int{1000, 5000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			g := drawBenchHeadless(b, 1280, 720)
			g.SetView(1280, 720)
			f := benchFont(b, g, 14)
			labels := make([]string, n)
			for i := range labels {
				labels[i] = fmt.Sprintf("Label %d", i)
			}
			a2dQueue(b, g, func() {
				for i, s := range labels {
					g.DrawText(f, s, float32(i%10)*120, float32(i/10%60)*12, White)
				}
			})
		})
	}
}

// Benchmark2DMeasureLabels measures n distinct labels a frame, the way a
// table or list lays out its rows.
func Benchmark2DMeasureLabels(b *testing.B) {
	for _, n := range []int{1000, 3000, 5000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			g := drawBenchHeadless(b, 640, 480)
			f := benchFont(b, g, 14)
			labels := make([]string, n)
			for i := range labels {
				labels[i] = fmt.Sprintf("Row %d column value", i)
			}
			for range 3 {
				for _, s := range labels {
					f.Measure(s, TextOptions{})
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for _, s := range labels {
					f.Measure(s, TextOptions{})
				}
			}
		})
	}
}

// Benchmark2DLayoutHit is the per-call cost of finding a label's cached
// layout: what DrawText pays before its first glyph.
func Benchmark2DLayoutHit(b *testing.B) {
	g := drawBenchHeadless(b, 640, 480)
	f := benchFont(b, g, 14)
	if _, err := f.Layout("Save and quit", TextOptions{}); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := f.Layout("Save and quit", TextOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

// a2dColrFont is Go Regular with 'A' made a COLR version 1 glyph (a
// solid layer and a gradient layer), the stand-in for emoji.
func a2dColrFont(b *testing.B, g *Graphics) *Font {
	b.Helper()
	probe, err := g.NewFont(goregular.TTF, 8, FontOptions{AtlasSize: 64})
	if err != nil {
		b.Fatal(err)
	}
	gid := func(r rune) uint16 {
		id, ok := probe.faces[0].face.NominalGlyph(r)
		if !ok {
			b.Fatalf("no glyph for %q", r)
		}
		return uint16(id)
	}
	a, bar := gid('A'), gid('l')
	probe.Destroy()
	ld, err := ot.NewLoader(bytes.NewReader(goregular.TTF))
	if err != nil {
		b.Fatal(err)
	}
	add := []ot.Table{
		colrV1Table(a, []colrV1Layer{
			{gid: a, palette: 0},
			{gid: bar, gradient: true, from: 1, to: 2, x0: 0, y0: 0, x1: 0, y1: 1400, x2: 1400, y2: 0},
		}),
		cpalTable([]color.RGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}}),
	}
	var tables []ot.Table
	for _, tag := range ld.Tables() {
		if slices.ContainsFunc(add, func(t ot.Table) bool { return t.Tag == tag }) {
			continue
		}
		raw, err := ld.RawTable(tag)
		if err != nil {
			b.Fatal(err)
		}
		tables = append(tables, ot.Table{Tag: tag, Content: raw})
	}
	tables = append(tables, add...)
	slices.SortFunc(tables, func(x, y ot.Table) int { return int(x.Tag) - int(y.Tag) })
	f, err := g.NewFont(ot.WriteTTF(tables), 16, FontOptions{AtlasSize: 1024})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(f.Destroy)
	return f
}

// a2dRichMarkup is about 2000 characters of markup with some forty style
// runs and colour glyphs ('A') scattered through it.
func a2dRichMarkup() string {
	var sb strings.Builder
	styles := []string{"[b]%s[/b]", "[i]%s[/i]", "[#ff8800]%s[/#]", "[link=l%d]%s[/link]", "%s"}
	for i := 0; sb.Len() < 2000; i++ {
		word := fmt.Sprintf("A bunyip %d lurks in the billabong at dusk, ", i)
		st := styles[i%len(styles)]
		if strings.Contains(st, "link") {
			fmt.Fprintf(&sb, st, i, word)
		} else {
			fmt.Fprintf(&sb, st, word)
		}
	}
	return sb.String()
}

// Benchmark2DRichParagraph2k draws a 2000-character rich paragraph with
// colour glyphs every frame (cached), and lays it out from cold.
func Benchmark2DRichParagraph2k(b *testing.B) {
	g := drawBenchHeadless(b, 1280, 720)
	g.SetView(1280, 720)
	f := a2dColrFont(b, g)
	rt := ParseRich(a2dRichMarkup())
	fonts := RichFonts{Regular: f}
	opts := TextOptions{Width: 900}
	b.Run("draw_cached", func(b *testing.B) {
		a2dQueue(b, g, func() { g.DrawRichText(fonts, rt, 10, 10, opts, White) })
	})
	b.Run("layout_lookup", func(b *testing.B) {
		// What finding the cached layout costs before any glyph is drawn.
		b.ReportAllocs()
		for b.Loop() {
			if _, err := fonts.Layout(rt, opts); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("layout_cold", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			f.dropLayouts()
			if _, err := fonts.Layout(rt, opts); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("measure_rich", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			fonts.MeasureRich(rt, opts)
		}
	})
}

// Benchmark2DOutlinedLabels200 draws two hundred labels in a whole frame,
// plain and outlined, and reports the draw calls they cost.
func Benchmark2DOutlinedLabels200(b *testing.B) {
	for _, outline := range []float32{0, 1.5} {
		b.Run(fmt.Sprint("outline=", outline), func(b *testing.B) {
			g := drawBenchHeadless(b, 1280, 720)
			g.SetView(1280, 720)
			f, err := g.NewSDFFont(goregular.TTF, 16, FontOptions{})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(f.Destroy)
			labels := benchLabels()
			draw := func() {
				for i, s := range labels {
					g.DrawTextBlock(f, s, float32(i%5)*200, float32(i/5)*16, TextOptions{OutlineWidth: outline}, White)
				}
			}
			a2dFrame(b, g, draw)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				a2dFrame(b, g, draw)
			}
			b.StopTimer()
			b.ReportMetric(float64(g.Stats().Draws2D), "draws")
		})
	}
}

// Benchmark2DHyphenation covers the hyphenator: loading the English
// patterns, soft-hyphenating a paragraph, and drawing a hyphenated
// paragraph whose layout is cached.
func Benchmark2DHyphenation(b *testing.B) {
	b.Run("load_en_us", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			hyphMu.Lock()
			clear(hyphCache)
			hyphMu.Unlock()
			if _, err := HyphenatorFor("en-us"); err != nil {
				b.Fatal(err)
			}
		}
	})
	h, err := HyphenatorFor("en-us")
	if err != nil {
		b.Fatal(err)
	}
	b.Run("soft_hyphens_880", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = h.SoftHyphens(benchParagraph)
		}
	})
	b.Run("draw_cached_880", func(b *testing.B) {
		g := drawBenchHeadless(b, 640, 480)
		g.SetView(640, 480)
		f := benchFont(b, g, 16)
		opts := TextOptions{Width: 300, AutoHyphenate: true}
		a2dQueue(b, g, func() { g.DrawTextBlock(f, benchParagraph, 8, 8, opts, White) })
	})
	b.Run("draw_cached_880_nohyph", func(b *testing.B) {
		g := drawBenchHeadless(b, 640, 480)
		g.SetView(640, 480)
		f := benchFont(b, g, 16)
		opts := TextOptions{Width: 300}
		a2dQueue(b, g, func() { g.DrawTextBlock(f, benchParagraph, 8, 8, opts, White) })
	})
}

// a2dOccluders is n 24x24 boxes spread over a 1280x720 view.
func a2dOccluders(n int) ([]lin.Vec2, []int32) {
	var pts []lin.Vec2
	var runs []int32
	for i := range n {
		x := float32(i%25)*50 + 10
		y := float32(i/25)*36 + 5
		pts = append(pts, lin.V2(x, y), lin.V2(x+24, y), lin.V2(x+24, y+24), lin.V2(x, y+24))
		runs = append(runs, 4)
	}
	return pts, runs
}

// Benchmark2DShadows builds the polar shadow rows for the eight shadowed
// lights a frame can hold against five hundred box occluders, which is
// the CPU work of a frame whose lights or occluders changed.
func Benchmark2DShadows(b *testing.B) {
	pts, runs := a2dOccluders(500)
	row := make([]byte, shadowAngles2D*4)
	dist := make([]float32, shadowAngles2D)
	for _, radius := range []float32{300, 1000} {
		b.Run(fmt.Sprint("radius=", radius), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for l := range maxLights2D {
					light := lin.V2(80+float32(l)*150, 200+float32(l%3)*150)
					shadowRow(row, dist, light, radius, pts, runs)
				}
			}
		})
	}
}

// Benchmark2DDrawLit1000 queues a thousand lit sprites.
func Benchmark2DDrawLit1000(b *testing.B) {
	g := drawBenchHeadless(b, 1280, 720)
	g.SetView(1280, 720)
	texes := a2dTextures(b, g, 2)
	normal := texes[1]
	a2dQueue(b, g, func() {
		g.SetLights2D(RGB(40, 40, 50), Light2D{Pos: lin.V2(300, 300), Shadows: true}, Light2D{Pos: lin.V2(900, 300)})
		for i := range 1000 {
			g.DrawLit(texes[0], normal, Sprite{Pos: lin.V2(float32(i%40)*32, float32(i/40)*28), Size: lin.V2(32, 32)})
		}
	})
}

// Benchmark2DRecordSizes reports the bytes of the per-sprite records the
// stream copies, compares and sorts.
func Benchmark2DRecordSizes(b *testing.B) {
	for b.Loop() {
	}
	b.ReportMetric(float64(unsafe.Sizeof(state2D{})), "state2D_bytes")
	b.ReportMetric(float64(unsafe.Sizeof(item2D{})), "item2D_bytes")
	b.ReportMetric(float64(unsafe.Sizeof(draw2D{})), "draw2D_bytes")
}

// emojiCollection is the system colour emoji collection, which the
// font fallback benchmarks load when the machine has it.
const emojiCollection = "/System/Library/Fonts/Apple Color Emoji.ttc"

// BenchmarkNewFontCollectionFallback makes a font with a system font
// collection as its fallback, the way a game adds colour emoji behind a
// Latin face. It reports the bytes allocated beyond the file itself.
func BenchmarkNewFontCollectionFallback(b *testing.B) {
	emoji, err := os.ReadFile(emojiCollection)
	if err != nil {
		b.Skipf("no system font collection: %v", err)
	}
	g := drawBenchHeadless(b, 64, 64)
	b.ReportAllocs()
	for b.Loop() {
		f, err := g.NewFont(goregular.TTF, 16, FontOptions{AtlasSize: 256, Fallbacks: [][]byte{emoji}})
		if err != nil {
			b.Fatal(err)
		}
		f.Destroy()
	}
}

// Benchmark2DParticles100kFrame is a whole frame with a hundred thousand
// instanced particles.
func Benchmark2DParticles100kFrame(b *testing.B) {
	g := drawBenchHeadless(b, 1280, 720)
	quads := benchQuads(100_000, 1280, 720)
	a2dFrame(b, g, func() { g.DrawParticles(nil, quads) })
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		a2dFrame(b, g, func() { g.DrawParticles(nil, quads) })
	}
}
