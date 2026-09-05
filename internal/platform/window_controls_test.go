package platform

import (
	"image"
	"image/color"
	"reflect"
	"testing"
)

func TestCursorPixels(t *testing.T) {
	img := image.NewNRGBA(image.Rect(7, 9, 9, 10))
	img.SetNRGBA(7, 9, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	p, w, h, err := cursorPixels(img, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if w != 2 || h != 1 || !reflect.DeepEqual(p[:4], []byte{25, 50, 100, 128}) {
		t.Fatalf("BGRA pixels %v, size %dx%d", p, w, h)
	}
	for _, tc := range []struct {
		name string
		img  image.Image
		x, y int
	}{
		{"nil", nil, 0, 0}, {"empty", image.NewRGBA(image.Rect(0, 0, 0, 1)), 0, 0}, {"negative hotspot", img, -1, 0}, {"outside hotspot", img, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := cursorPixels(tc.img, tc.x, tc.y); err == nil {
				t.Fatal("invalid cursor accepted")
			}
		})
	}
}
