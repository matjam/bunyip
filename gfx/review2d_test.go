package gfx

import (
	"image"
	"image/color"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// render2D draws one frame of 2D calls on a headless Graphics and reads
// it back.
func render2D(t *testing.T, g *Graphics, clear Color, draw func()) *image.RGBA {
	t.Helper()
	if ok, err := g.begin(clear); err != nil || !ok {
		t.Fatal(err)
	}
	draw()
	img, err := g.end(true)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// A half-transparent white texel over a white background must stay
// white: the upload premultiplies in linear light, so the sRGB sampler
// does not decode a*c as a darker colour.
func TestTranslucentTextureIsNotDarkened(t *testing.T) {
	g := newHeadless(t, 8, 8)
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{255, 255, 255, 128})
	tex, err := g.NewTexture(src, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	img := render2D(t, g, White, func() {
		g.Draw(tex, Sprite{Pos: lin.V2(0, 0), Size: lin.V2(8, 8)})
	})
	if c := img.RGBAAt(4, 4); c.R < 250 || c.G < 250 || c.B < 250 {
		t.Errorf("translucent white over white came out %v", c)
	}
	// The caller's image is not rewritten.
	if p := src.NRGBAAt(0, 0); p != (color.NRGBA{255, 255, 255, 128}) {
		t.Errorf("source image changed to %v", p)
	}
}

func TestLinearPremultiply(t *testing.T) {
	// Opaque and clear texels are untouched; a translucent grey moves.
	pix := []byte{200, 100, 50, 255, 0, 0, 0, 0, 128, 128, 128, 128}
	linearPremultiply(pix)
	if pix[0] != 200 || pix[1] != 100 || pix[2] != 50 || pix[4] != 0 {
		t.Errorf("opaque or clear texel changed: %v", pix)
	}
	// 128/128 is straight white at half alpha: half of linear white,
	// encoded, is sRGB 188.
	if pix[8] != 188 || pix[9] != 188 || pix[10] != 188 || pix[11] != 128 {
		t.Errorf("translucent white: %v, want 188 188 188 128", pix[8:])
	}
}

// A tiled nine-slice whose borders meet in the middle has no centre to
// repeat; it must draw the borders and return rather than loop.
func TestTiledNineSliceWithoutCentre(t *testing.T) {
	g := newHeadless(t, 32, 32)
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			src.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	tex, err := g.NewTexture(src, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	ns := NineSlice{Tex: tex, Left: 4, Right: 4, Top: 4, Bottom: 4, Tile: true}
	img := render2D(t, g, Black, func() { g.DrawNineSlice(ns, lin.R(0, 0, 20, 20), White) })
	if c := img.RGBAAt(1, 1); c.R < 200 {
		t.Errorf("corner not drawn: %v", c)
	}
}

// Two disjoint nested clips clip everything, including the pixel at the
// inner rectangle's fractional origin.
func TestDisjointClipDrawsNothing(t *testing.T) {
	g := newHeadless(t, 32, 32)
	img := render2D(t, g, Black, func() {
		g.PushClip(lin.R(0, 0, 10, 10))
		g.PushClip(lin.R(20.6, 20.6, 5, 5))
		g.FillRect(0, 0, 32, 32, Color{R: 1, A: 1})
		g.PopClip()
		g.PopClip()
	})
	for y := range 32 {
		for x := range 32 {
			if c := img.RGBAAt(x, y); c.R > 10 {
				t.Fatalf("pixel %d,%d drawn through a disjoint clip: %v", x, y, c)
			}
		}
	}
}

func TestPixelRectDisjoint(t *testing.T) {
	vp := vk.VkRect2D{Extent: vk.VkExtent2D{Width: 100, Height: 100}}
	r := pixelRect(vp, intersectClip(lin.R(0, 0, 10, 10), lin.R(20.6, 20.6, 5, 5)), 1, 1)
	if r.Extent.Width != 0 || r.Extent.Height != 0 {
		t.Errorf("disjoint clip covers %v", r.Extent)
	}
	huge := pixelRect(vp, lin.R(-1e12, -1e12, 2e12, 2e12), 1, 1)
	if huge.Extent.Width != 100 || huge.Extent.Height != 100 {
		t.Errorf("huge clip should cover the viewport, got %v", huge.Extent)
	}
}

// A colour matrix set outside DrawTo survives a different one inside it.
func TestColorMatrixSurvivesDrawTo(t *testing.T) {
	g := newHeadless(t, 16, 16)
	rt, err := g.NewRenderTexture(8, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Destroy()
	img := render2D(t, g, Black, func() {
		g.ColorMatrixed(Invert(), func() {
			g.FillRect(0, 0, 8, 16, Color{R: 1, A: 1}) // inverted: cyan
			g.DrawTo(rt, Black, func() {
				g.ColorMatrixed(ColorMatrix{}, func() { g.FillRect(0, 0, 8, 8, Color{G: 1, A: 1}) })
			})
			g.FillRect(8, 0, 8, 16, Color{R: 1, A: 1}) // still inverted: cyan
		})
	})
	before, after := img.RGBAAt(4, 8), img.RGBAAt(12, 8)
	if before.R > 30 || before.G < 200 || before.B < 200 {
		t.Errorf("rect before DrawTo not inverted: %v", before)
	}
	if after.R > 30 || after.G < 200 || after.B < 200 {
		t.Errorf("rect after DrawTo lost the matrix: %v", after)
	}
}

// A tilemap drawn through a transform is culled in its own units, not
// the untransformed ones.
func TestTilemapCullUnderTransform(t *testing.T) {
	g := newHeadless(t, 64, 64)
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			src.SetRGBA(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	tex, err := g.NewTexture(src, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	tm := NewTilemap(NewSheet(tex, 4, 4), 4, 4)
	for y := range 4 {
		for x := range 4 {
			tm.Set(x, y, 0)
		}
	}
	img := render2D(t, g, Black, func() {
		g.SetCamera2D(Camera2D{Position: lin.V2(32, 32)})
		g.Transformed(lin.Translate2(-400, -400), func() {
			g.DrawTilemap(tm, 400+24, 400+24, White)
		})
	})
	if c := img.RGBAAt(32, 32); c.B < 200 {
		t.Errorf("transformed tilemap culled away: %v", c)
	}
}

func TestDashedNegativePattern(t *testing.T) {
	sub := subpath{pts: []lin.Vec2{{X: 0, Y: 0}, {X: 10, Y: 0}}}
	out := dashed(sub, []float32{-1, 1}, 0.5) // returns rather than looping
	if len(out) == 0 {
		t.Error("no dashes")
	}
}

// A bad index drops its own triangle and leaves the rest intact.
func TestDrawIndexedBadIndex(t *testing.T) {
	g := newHeadless(t, 8, 8)
	verts := []Vertex2D{{Pos: lin.V2(0, 0)}, {Pos: lin.V2(8, 0)}, {Pos: lin.V2(0, 8)}, {Pos: lin.V2(8, 8)}}
	render2D(t, g, Black, func() {
		g.DrawIndexed(nil, verts, []uint32{0, 99, 2, 1, 3, 2})
	})
	if n := len(g.scratch); n != 3 {
		t.Errorf("%d vertices queued, want 3 (one good triangle)", n)
	}
}

// A second DrawTo on the same texture in a frame adds to the first pass
// rather than replacing it.
func TestDrawToTwiceAccumulates(t *testing.T) {
	g := newHeadless(t, 16, 16)
	rt, err := g.NewRenderTexture(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Destroy()
	img := render2D(t, g, Black, func() {
		g.DrawTo(rt, Black, func() { g.FillRect(0, 0, 8, 16, Color{R: 1, A: 1}) })
		g.DrawTo(rt, Black, func() { g.FillRect(8, 0, 8, 16, Color{G: 1, A: 1}) })
		g.DrawTexture(rt.Texture(), 0, 0)
	})
	if l := img.RGBAAt(4, 8); l.R < 200 {
		t.Errorf("first pass lost: %v", l)
	}
	if r := img.RGBAAt(12, 8); r.G < 200 {
		t.Errorf("second pass lost: %v", r)
	}
	if n := len(g.subFrames); n != 0 && n != 1 {
		t.Errorf("%d sub frames queued for one texture", n)
	}
}

// Destroying a texture inside a frame keeps it alive until the frame has
// been submitted.
func TestDestroyInsideFrameDefers(t *testing.T) {
	g := newHeadless(t, 8, 8)
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	tex, err := g.NewTexture(src, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	img := render2D(t, g, Black, func() {
		g.Draw(tex, Sprite{Size: lin.V2(8, 8)})
		tex.Destroy()
		if tex.img == nil {
			t.Fatal("texture freed while its sprite is queued")
		}
	})
	if c := img.RGBAAt(4, 4); c.R < 200 {
		t.Errorf("sprite of a texture destroyed mid-frame not drawn: %v", c)
	}
	if tex.img != nil {
		t.Error("texture not freed after the frame")
	}
}

// Descriptor pools grow: more render textures than the first pool holds
// still allocate.
func TestManyRenderTextures(t *testing.T) {
	g := newHeadless(t, 16, 16)
	var rts []*RenderTexture
	defer func() {
		for _, rt := range rts {
			rt.Destroy()
		}
	}()
	for i := range 24 {
		rt, err := g.NewRenderTexture(8, 8)
		if err != nil {
			t.Fatalf("render texture %d: %v", i, err)
		}
		rts = append(rts, rt)
	}
	// Textures past the first pool as well.
	var texs []*Texture
	defer func() {
		for _, tex := range texs {
			tex.Destroy()
		}
	}()
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	for i := range 2100 {
		tex, err := g.NewTexture(src, TextureOptions{})
		if err != nil {
			t.Fatalf("texture %d: %v", i, err)
		}
		texs = append(texs, tex)
	}
}

// The glyph atlas is written in place: the texture stays the same object
// after new glyphs are rasterised, and a glyph first drawn in a frame is
// visible in that frame.
func TestFontAtlasStable(t *testing.T) {
	g := newHeadless(t, 64, 32)
	f, err := g.NewFont(goregular.TTF, 16, FontOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Destroy()
	first := f.Texture()
	img := render2D(t, g, Black, func() {
		g.DrawText(f, "éüñ", 2, 4, White) // not in the ASCII preload
	})
	if f.Texture() != first {
		t.Error("atlas texture replaced")
	}
	lit := false
	for y := 0; y < 32 && !lit; y++ {
		for x := 0; x < 64; x++ {
			if c := img.RGBAAt(x, y); c.R > 60 {
				lit = true
				break
			}
		}
	}
	if !lit {
		t.Error("glyphs rasterised this frame did not draw this frame")
	}
}
