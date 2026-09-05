package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// solidImage is a w by h image of one colour.
func solidImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// drawSprite draws the texture over the whole 64 by 64 view and returns
// the frame, so a test can read what the texture holds now.
func drawSprite(t *testing.T, g *Graphics, tex *Texture) *image.RGBA {
	t.Helper()
	ok, err := g.begin(Black)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !ok {
		t.Fatal("begin skipped the frame")
	}
	g.Draw(tex, Sprite{Pos: v2(0, 0), Size: v2(64, 64)})
	img, err := g.end(true)
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	return img
}

// TestTextureReplace checks that replacing a texture's image, at the
// same size and at a different one, changes what a sprite holding the
// same *Texture draws.
func TestTextureReplace(t *testing.T) {
	g := newHeadless(t, 64, 64)
	tex, err := g.NewTexture(solidImage(4, 4, color.RGBA{255, 0, 0, 255}), TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	if got := drawSprite(t, g, tex).RGBAAt(32, 32); !closeColor(got, color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("first draw = %v, want red", got)
	}

	// The same size keeps the image and writes over its pixels.
	if err := tex.Replace(solidImage(4, 4, color.RGBA{0, 255, 0, 255})); err != nil {
		t.Fatal(err)
	}
	if tex.Width != 4 || tex.Height != 4 {
		t.Errorf("size %dx%d after a same-size replace, want 4x4", tex.Width, tex.Height)
	}
	if got := drawSprite(t, g, tex).RGBAAt(32, 32); !closeColor(got, color.RGBA{0, 255, 0, 255}) {
		t.Errorf("after a same-size replace = %v, want green", got)
	}

	// A different size takes a fresh image, and the sprite still names
	// the same texture.
	if err := tex.Replace(solidImage(16, 8, color.RGBA{0, 0, 255, 255})); err != nil {
		t.Fatal(err)
	}
	if tex.Width != 16 || tex.Height != 8 {
		t.Errorf("size %dx%d after a resize, want 16x8", tex.Width, tex.Height)
	}
	if got := drawSprite(t, g, tex).RGBAAt(32, 32); !closeColor(got, color.RGBA{0, 0, 255, 255}) {
		t.Errorf("after a resize = %v, want blue", got)
	}
	// The old image was retired rather than freed under the frames that
	// drew from it, so nothing waited on the GPU.
	if w := g.Stats().Waits; w != 0 {
		t.Errorf("%d GPU waits during the replaces, want none", w)
	}
}

// TestTextureReplaceInMaterial checks that a mesh drawn with a material
// naming the texture picks up a replacement of a different size, which
// is the case that has to drop the cached material descriptor sets.
func TestTextureReplaceInMaterial(t *testing.T) {
	g := newHeadless(t, 64, 64)
	quad := facingQuad(t, g)
	tex, err := g.NewTexture(solidImage(4, 4, color.RGBA{255, 0, 0, 255}), TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	draw := func() *image.RGBA {
		return renderMaterial(t, g, func() {
			g.DrawMesh(quad, Material{Texture: tex, Unlit: true}, lin.Identity())
		})
	}
	if got := draw().RGBAAt(32, 32); got.R <= got.B {
		t.Fatalf("first mesh draw = %v, want a red-dominated pixel", got)
	}
	if err := tex.Replace(solidImage(16, 8, color.RGBA{0, 0, 255, 255})); err != nil {
		t.Fatal(err)
	}
	if got := draw().RGBAAt(32, 32); got.B <= got.R {
		t.Errorf("mesh draw after a resize = %v, want a blue-dominated pixel", got)
	}
}

// TestTextureReplaceRejects checks the two cases Replace refuses: an
// empty image and a render texture's image, which the render texture
// owns.
func TestTextureReplaceRejects(t *testing.T) {
	g := newHeadless(t, 64, 64)
	tex, err := g.NewTexture(solidImage(2, 2, color.RGBA{255, 255, 255, 255}), TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	if err := tex.Replace(image.NewRGBA(image.Rect(0, 0, 0, 0))); err == nil {
		t.Error("an empty image replaced a texture")
	}
	rt, err := g.NewRenderTexture(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Destroy()
	if err := rt.Texture().Replace(solidImage(4, 4, color.RGBA{1, 2, 3, 255})); err == nil {
		t.Error("a render texture's image was replaced")
	}
}
