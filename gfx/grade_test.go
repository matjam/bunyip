package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestColorLUT(t *testing.T) {
	g := newHeadless(t, 64, 64)
	v, i := CubeMesh()
	cube, err := g.NewMesh(v, i)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	// A neutral LUT changes nothing; one with red and blue swapped turns
	// a red cube blue.
	neutral, err := g.NewLUT(NeutralLUT(16))
	if err != nil {
		t.Fatal(err)
	}
	defer neutral.Destroy()
	swapped := NeutralLUT(16)
	for y := range swapped.Bounds().Dy() {
		for x := range swapped.Bounds().Dx() {
			c := swapped.RGBAAt(x, y)
			swapped.SetRGBA(x, y, color.RGBA{c.B, c.G, c.R, 255})
		}
	}
	swap, err := g.NewLUT(swapped)
	if err != nil {
		t.Fatal(err)
	}
	defer swap.Destroy()
	shot := func(lut *Texture) color.RGBA {
		g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true, LUT: lut})
		if ok, err := g.begin(Black); err != nil || !ok {
			t.Fatal(err)
		}
		g.SetCamera(Camera{Position: lin.V3(0, 0, 3), Target: lin.V3(0, 0, 0)})
		g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{1, 1, 1, 1}, Ambient: Color{0.2, 0.2, 0.2, 1}})
		g.DrawMesh(cube, Material{BaseColor: Color{1, 0.1, 0.1, 1}, Unlit: true}, lin.Identity())
		img, err := g.end(true)
		if err != nil {
			t.Fatal(err)
		}
		return img.RGBAAt(32, 32)
	}
	plain := shot(nil)
	same := shot(neutral)
	if abs(int(plain.R)-int(same.R)) > 8 || abs(int(plain.B)-int(same.B)) > 8 {
		t.Errorf("neutral LUT changed %v to %v", plain, same)
	}
	// The swap moves the tone-mapped red into blue and the dim green and
	// blue into red.
	if got := shot(swap); abs(int(got.B)-int(plain.R)) > 12 || abs(int(got.R)-int(plain.B)) > 12 {
		t.Errorf("swapped LUT gives %v from %v, want red and blue exchanged", got, plain)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestTransmissionTexture(t *testing.T) {
	g := newHeadless(t, 64, 64)
	v, i := QuadMesh()
	quad, err := g.NewMesh(v, i)
	if err != nil {
		t.Fatal(err)
	}
	defer quad.Destroy()
	// Left half of the map blocks transmission, right half lets it through.
	mask := image.NewRGBA(image.Rect(0, 0, 2, 1))
	mask.SetRGBA(0, 0, color.RGBA{0, 0, 0, 255})
	mask.SetRGBA(1, 0, color.RGBA{255, 255, 255, 255})
	tex, err := g.NewTexture(mask, TextureOptions{Data: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	img := renderMaterial(t, g, func() {
		// A bright backdrop behind a glass pane.
		g.DrawMesh(quad, Material{BaseColor: Color{1, 1, 1, 1}, Unlit: true, Emissive: 1}, lin.Translate(lin.V3(0, 0, -1)).Mul(lin.Scale(lin.V3(8, 8, 1))))
		g.DrawMesh(quad, Material{BaseColor: Color{0.02, 0.02, 0.02, 1}, Transmission: 1, TransmissionTexture: tex, Roughness: 0.05}, lin.Scale(lin.V3(4, 4, 1)))
	})
	if bright(img, 16, 32) {
		t.Error("the opaque half of the pane shows the backdrop")
	}
	if !bright(img, 48, 32) {
		t.Error("the transmissive half of the pane hides the backdrop")
	}
}
