package gfx

import (
	"image"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// TestSDFText draws the same word small and large with a distance-field
// font and checks that ink stays inside each measured box and that the
// large word has far more of it.
func TestSDFText(t *testing.T) {
	g := newHeadless(t, 512, 256)
	f, err := g.NewSDFFont(goregular.TTF, 24, FontOptions{AtlasSize: 512})
	if err != nil {
		t.Fatalf("NewSDFFont: %v", err)
	}
	defer f.Destroy()
	var img *image.RGBA
	for range 2 {
		ok, err := g.Begin(Black)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		g.DrawTextSized(f, "Bunyip", 10, 10, 16, 0, White)
		g.DrawTextSized(f, "Bunyip", 10, 60, 96, 0, White)
		if img, err = g.End(true); err != nil {
			t.Fatal(err)
		}
	}
	count := func(x0, y0, x1, y1 int) int {
		n := 0
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if img.RGBAAt(x, y).R > 128 {
					n++
				}
			}
		}
		return n
	}
	sw, sh := f.MeasureSized("Bunyip", 16)
	lw, lh := f.MeasureSized("Bunyip", 96)
	small := count(10, 10, 10+int(sw)+2, 10+int(sh)+2)
	large := count(10, 60, 10+int(lw)+2, 60+int(lh)+2)
	if small < 20 || large < small*10 {
		t.Errorf("ink small=%d large=%d; large should be much more", small, large)
	}
	if stray := count(0, 0, 512, 256) - small - large; stray > 0 {
		t.Errorf("%d lit pixels outside both text boxes", stray)
	}
}
