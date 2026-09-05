package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// redBlue is a two-texel texture, red on the left and blue on the right.
func redBlue(t *testing.T, g *Graphics, opts TextureOptions) *Texture {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	src.SetRGBA(1, 0, color.RGBA{0, 0, 255, 255})
	tex, err := g.NewTexture(src, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tex.Destroy)
	return tex
}

// channels reports a pixel's red and blue as bytes.
func channels(img *image.RGBA, x, y int) (r, b int) {
	c := img.RGBAAt(x, y)
	return int(c.R), int(c.B)
}

// TestMaterialSamplerRepeat draws a repeating texture with a texture
// transform that takes the coordinates past one, which only tiles if the
// material set picked the texture's own repeating sampler out of the
// shared array. The quad covers screen x 13 to 51, so its four quarters
// are red, blue, red, blue.
func TestMaterialSamplerRepeat(t *testing.T) {
	g := newHeadless(t, 64, 64)
	quad := facingQuad(t, g)
	tex := redBlue(t, g, TextureOptions{Repeat: true})
	uv := lin.Identity2()
	uv.A = 2 // scale u by two
	img := renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{Texture: tex, Unlit: true, UVTransform: uv}, lin.Identity())
	})
	for _, c := range []struct {
		x   int
		red bool
	}{{18, true}, {27, false}, {36, true}, {46, false}} {
		r, b := channels(img, c.x, 32)
		if (r > b) != c.red {
			t.Errorf("at x=%d the tiled texture reads r=%d b=%d, want red=%v", c.x, r, b, c.red)
		}
	}
}

// TestMaterialSamplerFilter draws the same texture with linear filtering
// and clamped edges, which blends across the middle of the quad. With
// the wrong sampler the two texels meet at a hard edge instead.
func TestMaterialSamplerFilter(t *testing.T) {
	g := newHeadless(t, 64, 64)
	quad := facingQuad(t, g)
	tex := redBlue(t, g, TextureOptions{Linear: true})
	img := renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{Texture: tex, Unlit: true}, lin.Identity())
	})
	for _, x := range []int{31, 33} {
		r, b := channels(img, x, 32)
		if r < 40 || b < 40 {
			t.Errorf("at x=%d the linear texture reads r=%d b=%d, want both channels blended", x, r, b)
		}
	}
	// The ends still show their own texel.
	if r, b := channels(img, 18, 32); r <= b {
		t.Errorf("left of the quad reads r=%d b=%d, want red", r, b)
	}
	if r, b := channels(img, 46, 32); b <= r {
		t.Errorf("right of the quad reads r=%d b=%d, want blue", r, b)
	}
}
