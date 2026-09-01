package gfx

import (
	"image"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

// TestText draws a word and checks that ink lands inside its measured box
// and nowhere else.
func TestText(t *testing.T) {
	g := newHeadless(t, 256, 64)
	f, err := g.NewFont(goregular.TTF, 24, FontOptions{AtlasSize: 256})
	if err != nil {
		t.Fatalf("NewFont: %v", err)
	}
	defer f.Destroy()
	w, h := f.Measure("Bunyip")
	if w < 40 || w > 120 || h < 20 || h > 40 {
		t.Fatalf("measure = %v x %v, implausible for 24px text", w, h)
	}
	var img *image.RGBA
	for range 2 {
		ok, err := g.Begin(Black)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		g.DrawText(f, "Bunyip", 10, 10, White)
		if img, err = g.End(true); err != nil {
			t.Fatal(err)
		}
	}
	inside, outside := 0, 0
	for y := range 64 {
		for x := range 256 {
			if c := img.RGBAAt(x, y); c.R > 100 {
				if x >= 10 && x <= 10+int(w)+1 && y >= 10 && y <= 10+int(h)+1 {
					inside++
				} else {
					outside++
				}
			}
		}
	}
	if inside < 50 {
		t.Errorf("only %d lit pixels inside the text box", inside)
	}
	if outside > 0 {
		t.Errorf("%d lit pixels outside the text box", outside)
	}
}
