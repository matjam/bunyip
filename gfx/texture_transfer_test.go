package gfx

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"

	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

func TestTextureReadLogicalAlphaAndPNG(t *testing.T) {
	g := newHeadless(t, 16, 16)
	src := image.NewNRGBA(image.Rect(3, 4, 5, 5))
	src.SetNRGBA(3, 4, color.NRGBA{255, 255, 255, 128})
	src.SetNRGBA(4, 4, color.NRGBA{180, 80, 200, 128})
	tex, err := g.NewTexture(src, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	back, err := tex.Read()
	if err != nil {
		t.Fatal(err)
	}
	for x := range 2 {
		want := color.RGBAModel.Convert(src.At(x+3, 4)).(color.RGBA)
		got := back.RGBAAt(x, 0)
		for k, v := range []uint8{got.R, got.G, got.B, got.A} {
			w := []uint8{want.R, want.G, want.B, want.A}[k]
			if abs(int(v)-int(w)) > 1 {
				t.Fatal(got, want)
			}
		}
	}
	if back.RGBAAt(0, 0) != (color.RGBA{128, 128, 128, 128}) {
		t.Fatal(back.RGBAAt(0, 0))
	}
	copyTex, err := g.NewTexture(back, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	round, err := copyTex.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back.Pix, round.Pix) {
		t.Fatal("logical read/reupload changed pixels", back.Pix, round.Pix)
	}
	var buf bytes.Buffer
	if err := tex.WritePNG(&buf); err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(decoded.At(0, 0)); got != (color.NRGBA{255, 255, 255, 128}) {
		t.Fatal(got)
	}
}

func TestTextureCopyBytesSelfCopyAndMips(t *testing.T) {
	g := newHeadless(t, 16, 16)
	srcImg := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			srcImg.SetNRGBA(x, y, color.NRGBA{uint8(50 + x*50), uint8(y * 60), 180, 128})
		}
	}
	src, err := g.NewTexture(srcImg, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dst, err := g.NewBlankTexture(8, 4, TextureOptions{Linear: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := dst.CopyFrom(src, image.Rectangle{}, image.Point{}); err != nil {
		t.Fatal(err)
	}
	if err := dst.CopyFrom(dst, image.Rect(0, 0, 4, 4), image.Pt(4, 0)); err != nil {
		t.Fatal(err)
	}
	stored, err := g.r.Device.ReadImage(src.img)
	if err != nil {
		t.Fatal(err)
	}
	back, err := g.r.Device.ReadImage(dst.img)
	if err != nil {
		t.Fatal(err)
	}
	for y := range 4 {
		for x := range 8 {
			if back.RGBAAt(x, y) != stored.RGBAAt(x%4, y) {
				t.Fatal("GPU copy changed storage", x, y)
			}
		}
	}
	if err := dst.CopyFrom(dst, image.Rect(0, 0, 4, 4), image.Pt(1, 0)); err == nil {
		t.Fatal("accepted overlap")
	}
	// Upload/copy/destroy in one frame preserves the source until its use retires.
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	white, err := g.NewTexture(solidImage(8, 4, color.RGBA{255, 255, 255, 255}), TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := dst.CopyFrom(white, image.Rectangle{}, image.Point{}); err != nil {
		t.Fatal(err)
	}
	white.Destroy()
	g.Draw(dst, Sprite{Pos: lin.V2(8, 8), Size: lin.V2(1, 1), Color: White})
	shot, err := g.end(true)
	if err != nil {
		t.Fatal(err)
	}
	if c := shot.RGBAAt(8, 8); c.R < 240 || c.G < 240 || c.B < 240 {
		t.Fatal("copied mip chain remained stale", c)
	}
}

func TestTextureTransferValidationAndTiming(t *testing.T) {
	g := newHeadless(t, 16, 16)
	var nilRGBA *image.RGBA
	for _, src := range []image.Image{nil, nilRGBA, boundsOnlyImage{image.Rect(0, 0, math.MaxInt, 2)}} {
		if _, err := g.NewTexture(src, TextureOptions{}); err == nil {
			t.Fatal("accepted invalid image")
		}
	}
	if _, err := g.NewBlankTexture(math.MaxInt, 2, TextureOptions{}); err == nil {
		t.Fatal("accepted size overflow")
	}
	limit := int(g.r.Device.Limits().MaxImageDimension2D)
	if _, err := g.NewBlankTexture(limit+1, 1, TextureOptions{}); err == nil {
		t.Fatal("accepted unsupported size")
	}
	tex, err := g.NewBlankTexture(4, 4, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := tex.Replace(nil); err == nil {
		t.Fatal("Replace accepted nil")
	}
	if err := tex.Write(0, 0, nil); err == nil {
		t.Fatal("Write accepted nil")
	}
	if err := tex.Write(math.MaxInt, 0, solidImage(2, 2, color.RGBA{})); err == nil {
		t.Fatal("Write accepted coordinate overflow")
	}
	data, err := g.NewBlankTexture(4, 4, TextureOptions{Data: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := tex.CopyFrom(data, image.Rectangle{}, image.Point{}); err == nil {
		t.Fatal("accepted different formats")
	}
	other := *tex
	other.g = &Graphics{}
	if err := tex.CopyFrom(&other, image.Rectangle{}, image.Point{}); err == nil {
		t.Fatal("accepted different owner")
	}
	if err := tex.CopyFrom(nil, image.Rectangle{}, image.Point{}); err == nil {
		t.Fatal("accepted nil source")
	}
	if err := tex.CopyFrom(tex, image.Rect(0, 0, 1, 1), image.Pt(4, 0)); err == nil {
		t.Fatal("accepted out of bounds destination")
	}
	rt, err := g.NewRenderTexture(4, 4)
	if err != nil {
		t.Fatal(err)
	}
	copyImg, err := g.uploadTexture(vk.VkExtent2D{Width: 4, Height: 4}, rt.target.Color.Format, make([]byte, 64), false)
	if err != nil {
		t.Fatal(err)
	}
	defer copyImg.Destroy()
	copyDst := &Texture{Width: 4, Height: 4, img: copyImg, g: g}
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := tex.Read(); err == nil {
		t.Fatal("accepted read inside frame")
	}
	for _, format := range []ColorFormat{ColorScreen, ColorHDR, ColorMask} {
		rt.format = format
		if _, err := rt.Read(); err == nil {
			t.Fatal("accepted render read inside frame")
		}
	}
	rt.format = ColorScreen
	g.DrawTo(rt, White, func() {})
	if err := copyDst.CopyFrom(rt.Texture(), image.Rectangle{}, image.Point{}); err == nil {
		t.Fatal("accepted unrendered DrawTo source")
	}
	if err := rt.Texture().CopyFrom(tex, image.Rectangle{}, image.Point{}); err == nil {
		t.Fatal("accepted external destination")
	}
	if _, err := g.end(false); err != nil {
		t.Fatal(err)
	}
	if err := copyDst.CopyFrom(rt.Texture(), image.Rectangle{}, image.Point{}); err != nil {
		t.Fatal(err)
	}
	if got, err := g.r.Device.ReadImage(copyImg); err != nil || got.RGBAAt(0, 0).R < 240 {
		t.Fatal(got, err)
	}
}

func TestTextureUploadTrimsRGBAStorage(t *testing.T) {
	parent := image.NewRGBA(image.Rect(0, 0, 2, 4))
	for i := range parent.Pix {
		parent.Pix[i] = 255
	}
	sub := parent.SubImage(image.Rect(0, 1, 2, 2)).(*image.RGBA)
	if len(sub.Pix) <= 8 {
		t.Fatal("fixture needs trailing rows")
	}
	if got := texelsOf(sub, false); len(got) != 8 {
		t.Fatal("uploaded pixels outside bounds", len(got))
	}
}
