package gfx

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

var _ draw.Image = (*Image)(nil)

func TestImageOwnedPixelsFlipMaskAndCopy(t *testing.T) {
	src := image.NewNRGBA(image.Rect(4, 7, 7, 9))
	for y := 7; y < 9; y++ {
		for x := 4; x < 7; x++ {
			src.SetNRGBA(x, y, color.NRGBA{uint8((y-7)*3 + x - 3), 10, 20, 128})
		}
	}
	i, err := NewImage(src)
	if err != nil {
		t.Fatal(err)
	}
	if i.Bounds() != image.Rect(0, 0, 3, 2) {
		t.Fatal(i.Bounds())
	}
	i.FlipHorizontal()
	i.FlipVertical()
	for y := range 2 {
		for x := range 3 {
			if got := i.NRGBAAt(x, y).R; got != uint8(6-y*3-x) {
				t.Fatal(x, y, got)
			}
		}
	}
	if src.NRGBAAt(4, 7).R != 1 {
		t.Fatal("source modified")
	}
	i.Mask(color.NRGBA{R: 6, G: 10, B: 20, A: 1})
	if c := i.NRGBAAt(0, 0); c.A != 0 || c.R != 6 {
		t.Fatal(c)
	}
	if i.NRGBAAt(1, 0).A != 128 {
		t.Fatal("nonmatching RGB masked")
	}
	// Copy a shared subimage over the following pixels: original values survive.
	if err := i.CopyFrom(i.SubImage(image.Rect(0, 1, 2, 2)), image.Pt(1, 1)); err != nil {
		t.Fatal(err)
	}
	if i.NRGBAAt(1, 1).R != 3 || i.NRGBAAt(2, 1).R != 2 {
		t.Fatal("overlapping copy did not preserve source")
	}
	if err := i.CopyFrom(src, image.Pt(-1, 0)); err != nil {
		t.Fatal(err)
	}
	if i.NRGBAAt(0, 0).R != 2 || i.NRGBAAt(1, 1).R != 6 {
		t.Fatal("clipped/nonzero-origin copy is wrong")
	}
}

type boundsOnlyImage struct{ rect image.Rectangle }

func (i boundsOnlyImage) Bounds() image.Rectangle { return i.rect }
func (i boundsOnlyImage) ColorModel() color.Model { return color.RGBAModel }
func (i boundsOnlyImage) At(int, int) color.Color {
	panic("invalid bounds reached pixel allocation/copy")
}

func TestImageValidationAndZeroValue(t *testing.T) {
	var zero Image
	zero.FlipHorizontal()
	zero.FlipVertical()
	zero.Mask(color.White)
	if !zero.Bounds().Empty() || zero.At(0, 0).(color.NRGBA).A != 0 {
		t.Fatal("invalid zero image")
	}
	var nilRGBA *image.RGBA
	for _, src := range []image.Image{nil, nilRGBA, &zero, boundsOnlyImage{image.Rect(0, 0, math.MaxInt, 2)}, boundsOnlyImage{image.Rectangle{Min: image.Pt(math.MinInt, 0), Max: image.Pt(math.MaxInt, 1)}}} {
		if _, err := NewImage(src); err == nil {
			t.Fatal("accepted invalid source", src)
		}
	}
}

type pngFailureWriter struct{ err error }

func (w pngFailureWriter) Write([]byte) (int, error) { return 0, w.err }

func TestImagePNGExport(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{200, 100, 50, 128})
	i, err := NewImage(src)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := i.WritePNG(&buf); err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(decoded.At(0, 0)); got != src.NRGBAAt(0, 0) {
		t.Fatal(got)
	}
	broken := errors.New("PNG output failed")
	if err := i.WritePNG(pngFailureWriter{broken}); !errors.Is(err, broken) {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.png")
	if err := i.SavePNG(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	var zero Image
	if err := zero.SavePNG(path); err == nil {
		t.Fatal("saved empty image")
	}
	remaining, _ := os.ReadFile(path)
	if !bytes.Equal(data, remaining) {
		t.Fatal("invalid image truncated existing file")
	}
}
