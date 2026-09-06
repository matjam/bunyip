package gfx

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"reflect"
)

// Image owns editable CPU pixels in straight-alpha NRGBA form. NewImage copies
// its source, so later edits do not affect that source. The embedded NRGBA and
// its Pix slice are exposed for standard image/draw operations; subimages share
// those pixels. The zero value is empty. Methods are not synchronized.
type Image struct{ *image.NRGBA }

// NewImage copies src to owned, zero-based bounds. Nil or empty sources and
// dimensions whose RGBA storage would overflow return an error.
func NewImage(src image.Image) (*Image, error) {
	b, err := imageBounds(src)
	if err != nil {
		return nil, err
	}
	out := &Image{image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))}
	draw.Draw(out.NRGBA, out.Bounds(), imageSource(src), b.Min, draw.Src)
	return out, nil
}

// Bounds returns the pixel bounds, or an empty rectangle for the zero value.
func (i *Image) Bounds() image.Rectangle {
	if i == nil || i.NRGBA == nil {
		return image.Rectangle{}
	}
	return i.NRGBA.Bounds()
}

// ColorModel returns color.NRGBAModel, including for the zero value.
func (i *Image) ColorModel() color.Model { return color.NRGBAModel }

// At returns a straight-alpha pixel, or transparent black outside the image.
func (i *Image) At(x, y int) color.Color {
	if i == nil || i.NRGBA == nil {
		return color.NRGBA{}
	}
	return i.NRGBA.At(x, y)
}

// Set replaces a pixel. Coordinates outside the image are ignored.
func (i *Image) Set(x, y int, c color.Color) {
	if i != nil && i.NRGBA != nil {
		i.NRGBA.Set(x, y, c)
	}
}

// FlipHorizontal reverses each row in place. An empty image is unchanged.
func (i *Image) FlipHorizontal() {
	b := i.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x, other := b.Min.X, b.Max.X-1; x < other; x, other = x+1, other-1 {
			a, z := i.PixOffset(x, y), i.PixOffset(other, y)
			for k := range 4 {
				i.Pix[a+k], i.Pix[z+k] = i.Pix[z+k], i.Pix[a+k]
			}
		}
	}
}

// FlipVertical reverses the row order in place. An empty image is unchanged.
func (i *Image) FlipVertical() {
	b := i.Bounds()
	for y, other := b.Min.Y, b.Max.Y-1; y < other; y, other = y+1, other-1 {
		a, z := i.PixOffset(b.Min.X, y), i.PixOffset(b.Min.X, other)
		for k := range b.Dx() * 4 {
			i.Pix[a+k], i.Pix[z+k] = i.Pix[z+k], i.Pix[a+k]
		}
	}
}

// Mask sets alpha to zero for pixels whose straight RGB exactly matches c.
// The supplied alpha is ignored; existing RGB values are preserved. Nil colours
// and empty images have no effect. There is no tolerance or colour-space conversion.
func (i *Image) Mask(c color.Color) {
	if c == nil {
		return
	}
	m := color.NRGBAModel.Convert(c).(color.NRGBA)
	b := i.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := i.PixOffset(x, y)
			if i.Pix[p] == m.R && i.Pix[p+1] == m.G && i.Pix[p+2] == m.B {
				i.Pix[p+3] = 0
			}
		}
	}
}

// CopyFrom copies the full source at dst using draw.Src, clipped to this image.
// Source bounds may start anywhere. Overlapping self-copies read original pixels
// before writing, including when src is a subimage sharing this image's storage.
func (i *Image) CopyFrom(src image.Image, dst image.Point) error {
	b, err := imageBounds(src)
	if err != nil {
		return err
	}
	if i.Bounds().Empty() {
		return fmt.Errorf("gfx: copy to an empty image")
	}
	if dst.X > math.MaxInt-b.Dx() || dst.Y > math.MaxInt-b.Dy() {
		return fmt.Errorf("gfx: image destination overflows")
	}
	r := image.Rectangle{Min: dst, Max: dst.Add(b.Size())}.Intersect(i.Bounds())
	if r.Empty() {
		return nil
	}
	// Stage only the clipped area so arbitrary shared subimages are safe.
	tmp := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(tmp, tmp.Bounds(), imageSource(src), b.Min.Add(r.Min.Sub(dst)), draw.Src)
	draw.Draw(i.NRGBA, r, tmp, image.Point{}, draw.Src)
	return nil
}

// WritePNG encodes the image to w and leaves the borrowed writer open.
func (i *Image) WritePNG(w io.Writer) error { return writePNG(w, i) }

// SavePNG creates or truncates path, writes PNG pixels, and closes the file.
func (i *Image) SavePNG(path string) error { return savePNG(path, i) }

func imageSource(src image.Image) image.Image {
	if i, ok := src.(*Image); ok {
		return i.NRGBA
	}
	return src
}
func imageBounds(src image.Image) (image.Rectangle, error) {
	if src == nil || reflect.ValueOf(src).Kind() == reflect.Pointer && reflect.ValueOf(src).IsNil() {
		return image.Rectangle{}, fmt.Errorf("gfx: nil image")
	}
	b := src.Bounds()
	if b.Max.X <= b.Min.X || b.Max.Y <= b.Min.Y {
		return b, fmt.Errorf("gfx: image has empty bounds %v", b)
	}
	w, h := uint64(b.Max.X)-uint64(b.Min.X), uint64(b.Max.Y)-uint64(b.Min.Y)
	if w > uint64(math.MaxInt)/4 || h > uint64(math.MaxInt)/4/w {
		return b, fmt.Errorf("gfx: image dimensions overflow RGBA storage")
	}
	return b, nil
}
func writePNG(w io.Writer, src image.Image) error {
	if w == nil {
		return fmt.Errorf("gfx: nil PNG writer")
	}
	if _, err := imageBounds(src); err != nil {
		return err
	}
	return png.Encode(w, imageSource(src))
}
func savePNG(path string, src image.Image) error {
	if _, err := imageBounds(src); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	return errors.Join(writePNG(f, src), f.Close())
}
