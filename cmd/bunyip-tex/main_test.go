package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/bunyip/gfx/ktx2"
)

// writePNG puts a small test image at a path.
func writePNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for y := range 12 {
		for x := range 16 {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 16), uint8(y * 20), 90, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// TestConvert checks that one file in gives one KTX2 file out, in the
// format asked for and with the whole mip chain down to a single texel.
func TestConvert(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "hero.png")
	writePNG(t, src)
	dst := filepath.Join(dir, "out", "hero.ktx2")
	if err := convert(src, dst, ktx2.Options{Format: ktx2.BC7SRGB}, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ktx2.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Format != ktx2.BC7SRGB || f.Width != 16 || f.Height != 12 {
		t.Fatalf("wrote %s %dx%d, want BC7_SRGB 16x12", f.Format, f.Width, f.Height)
	}
	if len(f.Levels) != 5 { // 16x12 down to 1x1
		t.Errorf("%d levels, want 5", len(f.Levels))
	}
}

// TestConvertLinearFormats checks that -linear picks the format that is
// not sRGB, and that the formats with no colour ignore it.
func TestConvertLinearFormats(t *testing.T) {
	cases := []struct {
		name   string
		linear bool
		want   ktx2.Format
	}{
		{"bc7", false, ktx2.BC7SRGB},
		{"bc7", true, ktx2.BC7Unorm},
		{"bc5", false, ktx2.BC5Unorm},
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.png")
	writePNG(t, src)
	for _, c := range cases {
		f, err := ktx2.Named(c.name, c.linear)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, c.name+".ktx2")
		if err := convert(src, dst, ktx2.Options{Format: f, NoMipmaps: true}, false); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ktx2.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		if got.Format != c.want {
			t.Errorf("%s with linear=%v wrote %s, want %s", c.name, c.linear, got.Format, c.want)
		}
	}
}

// TestConvertRejectsNonImage checks that a file that is not an image is
// reported rather than written as an empty texture.
func TestConvertRejectsNonImage(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(src, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "notes.ktx2")
	if err := convert(src, dst, ktx2.Options{}, false); err == nil {
		t.Error("converted something that is not an image")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("a file was written for an input that failed")
	}
}
