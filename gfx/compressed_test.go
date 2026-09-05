package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/gfx/ktx2"
	"github.com/matjam/bunyip/internal/vk"
)

// encodeKTX2 compresses an image and returns the file's bytes.
func encodeKTX2(t *testing.T, src *image.RGBA, opts ktx2.Options) []byte {
	t.Helper()
	f, err := ktx2.Encode(src, opts)
	if err != nil {
		t.Fatal(err)
	}
	data, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestCompressedSprite uploads a BC1 texture and draws it as a sprite,
// checking that each quarter of the image comes back the colour it was
// encoded from. It is the whole path in one: encode, container, upload
// into a compressed image, sample.
func TestCompressedSprite(t *testing.T) {
	g := newHeadless(t, 64, 64)
	// Four flat quarters, each large enough to fill whole blocks, so the
	// only error is the endpoints' own quantisation.
	want := [4]color.RGBA{
		{200, 30, 40, 255}, {40, 190, 60, 255},
		{50, 60, 210, 255}, {230, 220, 40, 255},
	}
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			src.SetRGBA(x, y, want[(y/8)*2+x/8])
		}
	}
	data := encodeKTX2(t, src, ktx2.Options{Format: ktx2.BC1RGBSRGB, NoMipmaps: true})
	tex, err := g.NewCompressedTexture(data, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	if tex.Width != 16 || tex.Height != 16 {
		t.Fatalf("texture is %dx%d, want 16x16", tex.Width, tex.Height)
	}
	img := drawSprite(t, g, tex)
	// The sprite covers the whole 64 by 64 view, so each quarter's centre
	// is well inside one of the four colours.
	at := [4][2]int{{16, 16}, {48, 16}, {16, 48}, {48, 48}}
	for i, p := range at {
		got := img.RGBAAt(p[0], p[1])
		// BC1's endpoints are five and six bits a channel, so a flat
		// block lands within one step of each.
		if !nearColor(got, want[i], 9) {
			t.Errorf("quarter %d at (%d,%d) = %v, want near %v", i, p[0], p[1], got, want[i])
		}
	}
}

// nearColor reports whether two colours are within tol on every channel.
func nearColor(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol && d(a.A, b.A) <= tol
}

// TestCompressedMatchesCPUDecode uploads a BC7 texture holding blocks
// that use both modes the encoder writes, draws it magnified with
// nearest sampling, and compares each texel against the package's own
// decoder. Any disagreement between the two would mean the encoder's
// partition and weight tables differ from the ones the hardware decodes
// with, which nothing else here would catch.
func TestCompressedMatchesCPUDecode(t *testing.T) {
	g := newHeadless(t, 64, 64)
	if !g.r.Device.SupportsFormat(vk.VK_FORMAT_BC7_SRGB_BLOCK) {
		t.Skip("the device does not sample BC7")
	}
	// Sixteen texels of two colour clusters, which is what pushes the
	// encoder into its two-subset mode and so uses the partition tables.
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			var c color.RGBA
			switch {
			case x < 2 && y%2 == 0:
				c = color.RGBA{40, 8, 8, 255}
			case x < 2:
				c = color.RGBA{200, 8, 8, 255}
			case y%2 == 0:
				c = color.RGBA{8, 8, 60, 255}
			default:
				c = color.RGBA{8, 8, 220, 255}
			}
			src.SetRGBA(x, y, c)
		}
	}
	// An sRGB format is sampled and written back through the same
	// transfer function, so what lands in the frame is the block's own
	// texels rather than a brightened copy of them.
	f, err := ktx2.Encode(src, ktx2.Options{Format: ktx2.BC7SRGB, NoMipmaps: true})
	if err != nil {
		t.Fatal(err)
	}
	cpu, err := f.DecodeLevel(0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	tex, err := g.NewCompressedTexture(data, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	if tex.Width != 4 {
		t.Fatalf("texture is %d wide, want 4", tex.Width)
	}
	img := drawSprite(t, g, tex)
	// Each texel covers 16 by 16 pixels of the view; read the middle of
	// each so nearest sampling cannot land on a neighbour.
	for y := range 4 {
		for x := range 4 {
			got := img.RGBAAt(x*16+8, y*16+8)
			want := cpu.RGBAAt(x, y)
			if !nearColor(got, want, 2) {
				t.Errorf("texel (%d,%d): the GPU decoded %v, this package decoded %v", x, y, got, want)
			}
		}
	}
}

// TestCompressedMips uploads a texture with its whole chain and checks
// the levels reached the image, by drawing it small enough that a lower
// level is sampled. A wrong level count or a misaligned copy shows as a
// black or garbled draw.
func TestCompressedMips(t *testing.T) {
	g := newHeadless(t, 64, 64)
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			src.SetRGBA(x, y, color.RGBA{180, 90, 45, 255})
		}
	}
	data := encodeKTX2(t, src, ktx2.Options{Format: ktx2.BC7SRGB})
	tex, err := g.NewCompressedTexture(data, TextureOptions{Linear: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	ok, err := g.begin(Black)
	if err != nil || !ok {
		t.Fatal(err)
	}
	// Drawn at an eighth of its size, so the sampler reaches level three.
	g.Draw(tex, Sprite{Pos: v2(0, 0), Size: v2(4, 4)})
	img, err := g.end(true)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.RGBAAt(2, 2); !nearColor(got, color.RGBA{180, 90, 45, 255}, 6) {
		t.Errorf("a minified compressed texture drew %v, want near (180,90,45)", got)
	}
}

// TestCompressedRejects checks the two ways a file is turned away: bytes
// that are not a KTX2 file at all, and a format the device cannot sample
// and this build cannot decode.
func TestCompressedRejects(t *testing.T) {
	g := newHeadless(t, 32, 32)
	if _, err := g.NewCompressedTexture([]byte("not a texture"), TextureOptions{}); err == nil {
		t.Error("uploaded something that is not a KTX2 file")
	}
	// An ASTC file parses and names its format, but nothing here decodes
	// it, so a device without ASTC must say so rather than draw nothing.
	astc := &ktx2.File{Format: ktx2.ASTC4x4Unorm, Width: 4, Height: 4, Levels: [][]byte{make([]byte, 16)}}
	data, err := astc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.NewCompressedTexture(data, TextureOptions{})
	if g.r.Device.SupportsFormat(vk.VK_FORMAT_ASTC_4x4_UNORM_BLOCK) {
		if err != nil {
			t.Errorf("a device that samples ASTC refused the file: %v", err)
		}
		return
	}
	if err == nil {
		t.Error("a device without ASTC accepted an ASTC file")
	}
}

// TestReplaceCompressed checks that a compressed texture's blocks can be
// swapped while the *Texture the game holds stays the one it draws with,
// which is how a .ktx2 file hot reloads.
func TestReplaceCompressed(t *testing.T) {
	g := newHeadless(t, 64, 64)
	red := encodeKTX2(t, solidImage(8, 8, color.RGBA{220, 20, 20, 255}), ktx2.Options{Format: ktx2.BC7SRGB, NoMipmaps: true})
	blue := encodeKTX2(t, solidImage(16, 16, color.RGBA{20, 20, 220, 255}), ktx2.Options{Format: ktx2.BC7SRGB, NoMipmaps: true})
	tex, err := g.NewCompressedTexture(red, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	if got := drawSprite(t, g, tex).RGBAAt(32, 32); got.R <= got.B {
		t.Fatalf("first draw = %v, want a red-dominated pixel", got)
	}
	if err := tex.ReplaceCompressed(blue); err != nil {
		t.Fatal(err)
	}
	if tex.Width != 16 || tex.Height != 16 {
		t.Errorf("size %dx%d after the swap, want 16x16", tex.Width, tex.Height)
	}
	if got := drawSprite(t, g, tex).RGBAAt(32, 32); got.B <= got.R {
		t.Errorf("after the swap = %v, want a blue-dominated pixel", got)
	}
	// Blocks are not texels, so writing an image into one is refused
	// rather than corrupting it.
	if err := tex.Write(0, 0, solidImage(4, 4, color.RGBA{1, 2, 3, 255})); err == nil {
		t.Error("wrote texels into a compressed texture")
	}
	if err := tex.Replace(solidImage(4, 4, color.RGBA{1, 2, 3, 255})); err == nil {
		t.Error("replaced a compressed texture with an image")
	}
	if _, err := tex.Read(); err == nil {
		t.Error("read texels back from a compressed texture")
	}
}
