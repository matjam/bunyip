package gfx

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"math"
	"runtime"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/lin"
)

// mallocs counts the heap allocations draw makes.
func mallocs(draw func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	draw()
	runtime.ReadMemStats(&after)
	return after.Mallocs - before.Mallocs
}

// Drawing and measuring more distinct labels a frame than one generation
// of the layout cache holds reaches a steady state with no allocation:
// the cache grows to the frame's text instead of retiring, mid-frame,
// what the frame has just drawn.
func TestTextSteadyStateAllocs(t *testing.T) {
	g := newHeadless(t, 640, 480)
	g.SetView(640, 480)
	f, err := g.NewFont(goregular.TTF, 14, FontOptions{AtlasSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	const n = 5000
	labels := make([]string, n)
	rows := make([]string, n)
	for i := range labels {
		labels[i] = fmt.Sprintf("Label %d", i)
		rows[i] = fmt.Sprintf("Row %d column value", i)
	}
	draw := func() {
		for i, s := range labels {
			g.DrawText(f, s, float32(i%10)*60, float32(i/10%40)*12, White)
		}
		for _, s := range rows {
			f.Measure(s, TextOptions{})
		}
	}
	for frame := range 5 {
		if ok, err := g.begin(Black); err != nil || !ok {
			t.Fatal(err)
		}
		allocs := mallocs(draw)
		if _, err := g.end(false); err != nil {
			t.Fatal(err)
		}
		// The first frame lays everything out and the second keeps it.
		if frame >= 2 && allocs != 0 {
			t.Fatalf("frame %d allocated %d times drawing and measuring %d labels each", frame, allocs, n)
		}
	}
}

// Outlined text is one draw for a layout's outlines and one for its
// glyphs, however many glyphs it has, and the outlines sit under every
// glyph.
func TestOutlinedTextDrawCount(t *testing.T) {
	g := newHeadless(t, 256, 64)
	g.SetView(256, 64)
	f, err := g.NewSDFFont(goregular.TTF, 16, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	img := frame2D(t, g, func() {
		g.DrawTextBlock(f, "Outlined words here", 4, 4, TextOptions{OutlineWidth: 1.5, OutlineColor: RGB(255, 0, 0)}, White)
	})
	if d := g.Stats().Draws2D; d != 2 {
		t.Errorf("an outlined label took %d draws, want 2", d)
	}
	white, red := 0, 0
	for y := range 64 {
		for x := range 256 {
			c := img.RGBAAt(x, y)
			switch {
			case c.R > 200 && c.G > 200 && c.B > 200:
				white++
			case c.R > 200 && c.G < 60 && c.B < 60:
				red++
			}
		}
	}
	if white < 50 || red < 50 {
		t.Errorf("%d white glyph and %d red outline pixels", white, red)
	}
	// Two labels with the same outline share its uniform block, so their
	// outlines could merge; different outlines each get their own.
	frame2D(t, g, func() {
		for i := range 4 {
			g.DrawTextBlock(f, "same", 4, float32(i)*14, TextOptions{OutlineWidth: 1.5}, White)
		}
	})
	if d := g.Stats().Draws2D; d > 8 {
		t.Errorf("four outlined labels took %d draws", d)
	}
}

// transformsUnderTest are the transforms screen-space culling is checked
// under: rotated, scaled, sheared, mirrored and shrunk.
var transformsUnderTest = []struct {
	name string
	m    lin.Affine
}{
	{"identity", lin.Identity2()},
	{"scrolled", lin.Translate2(-37, -21)},
	{"rotated and scaled", lin.Translate2(64, 48).Mul(lin.Rotate2(0.7)).Mul(lin.Scale2(1.7, 1.7)).Mul(lin.Translate2(-60, -40))},
	{"shrunk", lin.Translate2(8, 4).Mul(lin.Scale2(0.3, 0.25))},
	{"sheared", lin.Translate2(20, 0).Mul(lin.Shear2(0.8, 0.1))},
	{"mirrored", lin.Translate2(120, 90).Mul(lin.Scale2(-1.2, -0.9))},
	{"turned a quarter", lin.Translate2(128, -10).Mul(lin.Rotate2(math.Pi / 2))},
}

// checkerSheet is a sheet of 16 differently coloured 8x8 frames.
func checkerSheet(t *testing.T, g *Graphics) *Sheet {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := range img.Pix {
		img.Pix[i] = byte(i*37 + i/4*11)
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	tex, err := g.NewTexture(img, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tex.Destroy)
	return NewSheet(tex, 8, 8)
}

// tintShader is a sprite shader with a uniform block and an image, for
// checking the state sprites draw under follows both.
func tintShader(t *testing.T, g *Graphics) *Shader {
	t.Helper()
	const source = `struct Params { v: vec4f, }; @group(1) @binding(0) var<uniform> u: Params;
fn fragment(uv: vec2f, color: vec4f) -> vec4f { return u.v * textureSample(image0, image0Sampler, uv); }`
	s, err := g.CompileShader(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Destroy)
	return s
}

// A tilemap drawn in screen space visits only the tiles the view can
// see, under any transform, and draws exactly what drawing every tile
// draws.
func TestTilemapScreenCullingIsConservative(t *testing.T) {
	g := newHeadless(t, 128, 96)
	g.SetView(128, 96)
	sheet := checkerSheet(t, g)
	tm := NewTilemap(sheet, 60, 50)
	for i := range tm.Tiles {
		tm.Tiles[i] = (i*7 + i/60) % 16
	}
	for _, tc := range transformsUnderTest {
		t.Run(tc.name, func(t *testing.T) {
			var queued int
			got := frame2D(t, g, func() {
				g.Transformed(tc.m, func() { g.DrawTilemap(tm, -30, -20, White) })
				queued = streamQuads(g)
			})
			// The reference draws every tile as plain triangles, which are
			// never culled.
			want := frame2D(t, g, func() {
				g.Transformed(tc.m, func() {
					for ty := range tm.Height {
						for tx := range tm.Width {
							s := Sprite{Pos: lin.V2(-30+float32(tx)*8, -20+float32(ty)*8), Size: lin.V2(8, 8), Color: White}
							s.UV0, s.UV1 = sheet.UV(tm.Tiles[ty*tm.Width+tx])
							p := s.Corners()
							uv := [4]lin.Vec2{s.UV0, {X: s.UV1.X, Y: s.UV0.Y}, s.UV1, {X: s.UV0.X, Y: s.UV1.Y}}
							var v []Vertex2D
							for _, k := range [6]int{0, 1, 2, 0, 2, 3} {
								v = append(v, Vertex2D{Pos: p[k], UV: uv[k], Color: White})
							}
							g.DrawTriangles(sheet.Texture, v)
						}
					}
				})
			})
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Fatal("the culled tilemap draws differently from every tile drawn")
			}
			if queued >= tm.Width*tm.Height {
				t.Errorf("queued all %d tiles", queued)
			}
		})
	}
}

// Sprites in screen space are dropped when they cannot reach the view,
// under any transform, and what is kept draws exactly what drawing every
// sprite draws.
func TestSpriteScreenCullingIsConservative(t *testing.T) {
	g := newHeadless(t, 128, 96)
	g.SetView(128, 96)
	sheet := checkerSheet(t, g)
	var sprites []Sprite
	for i := range 400 {
		s := Sprite{Pos: lin.V2(float32(i%20)*13-70, float32(i/20)*11-60), Size: lin.V2(9+float32(i%5), 7+float32(i%3)), Color: White}
		s.UV0, s.UV1 = sheet.UV(i % 16)
		if i%3 == 0 {
			s.Rotation, s.Origin = float32(i)*0.37, lin.V2(0.5, 0.5)
		}
		sprites = append(sprites, s)
	}
	for _, tc := range transformsUnderTest {
		t.Run(tc.name, func(t *testing.T) {
			got := frame2D(t, g, func() {
				g.Transformed(tc.m, func() {
					for _, s := range sprites {
						g.Draw(sheet.Texture, s)
					}
				})
			})
			culled := g.Stats().Culled2D
			want := frame2D(t, g, func() {
				g.Transformed(tc.m, func() {
					for _, s := range sprites {
						p := s.Corners()
						uv := [4]lin.Vec2{s.UV0, {X: s.UV1.X, Y: s.UV0.Y}, s.UV1, {X: s.UV0.X, Y: s.UV1.Y}}
						var v []Vertex2D
						for _, k := range [6]int{0, 1, 2, 0, 2, 3} {
							v = append(v, Vertex2D{Pos: p[k], UV: uv[k], Color: White})
						}
						g.DrawTriangles(sheet.Texture, v)
					}
				})
			})
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Fatal("the culled sprites draw differently from every sprite drawn")
			}
			if culled == 0 {
				t.Error("no sprite was culled, though most lie outside the view")
			}
		})
	}
}

// A text layout wholly outside the view is dropped in one test, one that
// reaches into it is drawn, and a rotated one is tested where it lands.
func TestTextLayoutCulling(t *testing.T) {
	g := newHeadless(t, 128, 64)
	g.SetView(128, 64)
	f, err := g.NewFont(goregular.TTF, 16, FontOptions{AtlasSize: 256})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	img := frame2D(t, g, func() {
		g.DrawText(f, "gone", 300, 10, White)
		g.DrawText(f, "gone", 10, -40, White)
	})
	if s := g.Stats(); s.Culled2D != 8 || s.Draws2D != 0 {
		t.Errorf("two labels out of view: culled %d glyphs in %d draws, want 8 in 0", s.Culled2D, s.Draws2D)
	}
	for _, p := range img.Pix[:len(img.Pix)] {
		if p != 0 && p != 255 {
			t.Fatal("a culled label drew")
		}
	}
	// A label hanging off the right edge still draws its visible part,
	// and a label placed off the edge but turned back into view draws.
	img = frame2D(t, g, func() {
		g.DrawText(f, "edge", 110, 10, White)
		g.DrawTextBlock(f, "turned", 140, 40, TextOptions{Angle: math.Pi}, White)
	})
	if s := g.Stats(); s.Culled2D != 0 {
		t.Errorf("culled %d glyphs of labels reaching into the view", s.Culled2D)
	}
	lit := func(x0, x1 int) int {
		n := 0
		for y := range 64 {
			for x := x0; x < x1; x++ {
				if img.RGBAAt(x, y).R > 128 {
					n++
				}
			}
		}
		return n
	}
	if lit(110, 128) == 0 {
		t.Error("the label hanging off the edge did not draw")
	}
	if lit(80, 128) <= lit(110, 128) {
		t.Error("the turned label did not draw")
	}
}

// The cached 2D state follows everything that feeds it: a shader's
// uniforms set between two sprites of one texture, its images, a texture
// replaced in the same frame and a uniform block another output placed.
func TestSpriteStateCacheFollowsChanges(t *testing.T) {
	g := newHeadless(t, 32, 32)
	g.SetView(32, 32)
	sh := tintShader(t, g)
	tex := checkerSheet(t, g).Texture
	other := checkerSheet(t, g).Texture
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	q := g.cur
	state := func(i int) state2D { return q.stream.states[q.stream.items[i].state] }
	g.SetShader(sh)
	if err := sh.SetUniforms(struct{ V lin.Vec4 }{lin.V4(1, 0, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	g.Draw(tex, Sprite{Size: lin.V2(4, 4)})
	if err := sh.SetUniforms(struct{ V lin.Vec4 }{lin.V4(0, 1, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	g.SetLayer(1) // a run of its own, so the items can be told apart
	g.Draw(tex, Sprite{Size: lin.V2(4, 4)})
	if state(0).uniform == state(1).uniform {
		t.Error("a sprite after SetUniforms kept the old uniform block")
	}
	// The same values again keep the block, so the runs can merge.
	if err := sh.SetUniforms(struct{ V lin.Vec4 }{lin.V4(0, 1, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	g.SetLayer(2)
	g.Draw(tex, Sprite{Size: lin.V2(4, 4)})
	if state(1).uniform != state(2).uniform {
		t.Error("setting the same uniforms again placed a new block")
	}
	sh.SetImage(0, other)
	g.SetLayer(3)
	g.Draw(tex, Sprite{Size: lin.V2(4, 4)})
	if state(2).set == state(3).set {
		t.Error("a sprite after SetImage kept the old image set")
	}
	g.SetShader(nil)
	g.SetLayer(4)
	g.Draw(tex, Sprite{Size: lin.V2(4, 4)})
	if state(4).shader == sh {
		t.Error("a sprite after SetShader(nil) kept the game's shader")
	}
	// A render texture's queue placing the uniforms elsewhere does not
	// leave the screen's cached state pointing at a stale block.
	g.SetShader(sh)
	g.SetLayer(5)
	g.Draw(tex, Sprite{Size: lin.V2(4, 4)})
	rt, err := g.NewRenderTexture(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Destroy()
	g.DrawTo(rt, Black, func() {
		if err := sh.SetUniforms(struct{ V lin.Vec4 }{lin.V4(0, 0, 1, 0)}); err != nil {
			t.Fatal(err)
		}
		g.SetShader(sh)
		g.Draw(tex, Sprite{Size: lin.V2(4, 4)})
	})
	g.SetLayer(6)
	g.Draw(tex, Sprite{Size: lin.V2(4, 4)})
	if state(5).uniform == state(6).uniform {
		t.Error("the screen's sprite kept a block placed before the uniforms changed")
	}
	if _, err := g.end(false); err != nil {
		t.Fatal(err)
	}
}

// Shadow maps are only rebuilt and uploaded when the shadowed lights or
// the occluders change.
func TestShadowStripRebuildsOnlyOnChange(t *testing.T) {
	g := newHeadless(t, 64, 64)
	g.SetView(64, 64)
	tex := checkerSheet(t, g).Texture
	frame := func(lightX float32, wall float32) {
		t.Helper()
		if ok, err := g.begin(Black); err != nil || !ok {
			t.Fatal(err)
		}
		g.SetLights2D(RGB(20, 20, 20), Light2D{Pos: lin.V2(lightX, 32), Shadows: true})
		g.AddOccluder2D(lin.V2(wall, 10), lin.V2(wall, 50))
		g.DrawLit(tex, tex, Sprite{Size: lin.V2(64, 64)})
		if _, err := g.end(false); err != nil {
			t.Fatal(err)
		}
	}
	frame(10, 30)
	first := append([]byte(nil), g.main.shadowPix...)
	g.main.shadowPix[0] ^= 0xFF // a rebuild would write this texel again
	frame(10, 30)
	if g.main.shadowPix[0] == first[0] {
		t.Fatal("the strip was rebuilt for the same light and occluder")
	}
	frame(10, 40)
	if bytes.Equal(g.main.shadowPix, first) {
		t.Fatal("moving the occluder did not rebuild the strip")
	}
	frame(12, 40)
	moved := append([]byte(nil), g.main.shadowPix...)
	frame(10, 30)
	if !bytes.Equal(g.main.shadowPix, first) || bytes.Equal(moved, first) {
		t.Fatal("the strip does not follow the light back")
	}
}

// Lights past the eighth are dropped and counted.
func TestLights2DDroppedCounted(t *testing.T) {
	g := newHeadless(t, 16, 16)
	lights := make([]Light2D, 11)
	frame2D(t, g, func() { g.SetLights2D(White, lights...) })
	if n := g.Stats().Lights2DDropped; n != 3 {
		t.Errorf("Lights2DDropped = %d, want 3", n)
	}
	frame2D(t, g, func() { g.SetLights2D(White, lights[:8]...) })
	if n := g.Stats().Lights2DDropped; n != 0 {
		t.Errorf("Lights2DDropped = %d for eight lights", n)
	}
}

// The shape helpers build their path in storage kept on Graphics.
func TestShapeHelpersDoNotAllocate(t *testing.T) {
	g := newHeadless(t, 64, 64)
	g.SetView(64, 64)
	pts := []lin.Vec2{{X: 1, Y: 1}, {X: 30, Y: 4}, {X: 12, Y: 40}}
	shapes := func() {
		g.FillCircle(20, 20, 10, White)
		g.StrokeCircle(20, 20, 12, 2, White)
		g.StrokeRect(4, 4, 30, 20, 2, White)
		g.StrokeLine(0, 0, 60, 60, 3, White)
		g.FillPolygon(pts, White)
	}
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	shapes()
	g.main.reset()
	if n := mallocs(shapes); n != 0 {
		t.Errorf("the shape helpers allocated %d times", n)
	}
	if _, err := g.end(false); err != nil {
		t.Fatal(err)
	}
}

// Rich layouts are cached by a hash of their runs, and two different
// runs never share a layout even when they hash alike.
func TestRichLayoutCacheTellsRunsApart(t *testing.T) {
	g := newHeadless(t, 64, 64)
	f, err := g.NewFont(goregular.TTF, 16, FontOptions{AtlasSize: 256})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	fonts := RichFonts{Regular: f}
	a := RichText{Runs: []RichRun{{Text: "one "}, {Text: "two", Color: RGB(255, 0, 0)}}}
	b := RichText{Runs: []RichRun{{Text: "one "}, {Text: "two", Color: RGB(0, 255, 0)}}}
	la1, _ := fonts.Layout(a, TextOptions{})
	la2, _ := fonts.Layout(a, TextOptions{})
	la3, _ := fonts.Layout(a, TextOptions{})
	if la3 != la2 {
		t.Error("the same runs were laid out again")
	}
	_ = la1
	lb, _ := fonts.Layout(b, TextOptions{})
	if lb == la3 || lb.glyphs[len(lb.glyphs)-1].style.Color == la3.glyphs[len(la3.glyphs)-1].style.Color {
		t.Error("different runs shared a layout")
	}
	// Force a collision: store b's layout under a's key.
	key := textLayoutKey{rich: richHash(a.Runs), fonts: fonts}
	f.layouts.store(key, lb, f.frame())
	if l, _ := fonts.Layout(a, TextOptions{}); l == lb {
		t.Error("a hash collision returned another text's layout")
	}
}
