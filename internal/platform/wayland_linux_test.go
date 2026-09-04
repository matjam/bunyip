package platform

import (
	"image"
	"image/color"
	"testing"
)

func TestSquareIcon(t *testing.T) {
	// A wide image is padded top and bottom and centred.
	wide := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	for x := 0; x < 8; x++ {
		for y := 0; y < 4; y++ {
			wide.Set(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	out := squareIcon(wide)
	if got := out.Bounds(); got != image.Rect(0, 0, 8, 8) {
		t.Fatalf("bounds = %v, want 8x8", got)
	}
	if _, _, _, alpha := out.At(0, 0).RGBA(); alpha != 0 {
		t.Errorf("the padding at the top has alpha %d, want 0", alpha)
	}
	if _, _, _, alpha := out.At(0, 7).RGBA(); alpha != 0 {
		t.Errorf("the padding at the bottom has alpha %d, want 0", alpha)
	}
	for y := 2; y < 6; y++ {
		r, _, _, alpha := out.At(4, y).RGBA()
		if alpha == 0 || r == 0 {
			t.Errorf("row %d of the image is missing: r=%d alpha=%d", y, r, alpha)
		}
	}

	// A tall image is padded left and right.
	tall := squareIcon(image.NewNRGBA(image.Rect(0, 0, 3, 9)))
	if got := tall.Bounds(); got != image.Rect(0, 0, 9, 9) {
		t.Errorf("bounds = %v, want 9x9", got)
	}

	// A square image is handed back as it is, with no copy.
	square := image.NewNRGBA(image.Rect(0, 0, 6, 6))
	if squareIcon(square) != image.Image(square) {
		t.Error("a square image was copied, want it returned as it is")
	}

	// An image with an offset origin still lands in a square starting at
	// the origin, which is what the buffer needs.
	offset := squareIcon(image.NewNRGBA(image.Rect(10, 20, 14, 22)))
	if got := offset.Bounds(); got != image.Rect(0, 0, 4, 4) {
		t.Errorf("bounds = %v, want 4x4 at the origin", got)
	}
}
