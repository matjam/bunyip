package gfx

import (
	"bytes"
	"math"
	"testing"
)

func TestDecodeHDR(t *testing.T) {
	// A 2x2 flat (uncompressed) RGBE image. Exponent 129 scales the
	// mantissas by 2^(129-136) = 1/128, so (128, 64, 32) is (1, 0.5, 0.25).
	var b bytes.Buffer
	b.WriteString("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y 2 +X 2\n")
	b.Write([]byte{128, 64, 32, 129, 0, 0, 0, 0, 255, 255, 255, 130, 128, 128, 128, 128})
	img, err := DecodeHDR(&b)
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 2 || img.Height != 2 || len(img.Pix) != 12 {
		t.Fatalf("got %dx%d with %d floats", img.Width, img.Height, len(img.Pix))
	}
	want := []float32{1, 0.5, 0.25, 0, 0, 0, 255 / 64.0, 255 / 64.0, 255 / 64.0, 0.5, 0.5, 0.5}
	for i, w := range want {
		if math.Abs(float64(img.Pix[i]-w)) > 1e-5 {
			t.Errorf("pix[%d] = %v, want %v", i, img.Pix[i], w)
		}
	}
	// The same image with a run-length encoded scanline: each of the four
	// channels is one run of two identical bytes.
	b.Reset()
	b.WriteString("#?RADIANCE\n\n-Y 1 +X 8\n")
	b.Write([]byte{2, 2, 0, 8})
	for _, v := range []byte{128, 64, 32, 129} {
		b.Write([]byte{128 + 8, v})
	}
	if img, err = DecodeHDR(&b); err != nil {
		t.Fatal(err)
	}
	if img.Width != 8 || img.Pix[0] != 1 || img.Pix[1] != 0.5 || img.Pix[2] != 0.25 || img.Pix[21] != 1 {
		t.Errorf("rle scanline decoded to %v", img.Pix)
	}
}
