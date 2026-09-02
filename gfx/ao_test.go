package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestAmbientOcclusion renders a box standing on a floor with and without
// SSAO: the floor right at the box's base must get darker, the open floor
// must not.
func TestAmbientOcclusion(t *testing.T) {
	g := newHeadless(t, 128, 128)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	render := func(strength float32) *image.RGBA {
		g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, AmbientOcclusion: strength})
		var img *image.RGBA
		for range 2 {
			ok, err := g.begin(Black)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				continue
			}
			g.SetCamera(Camera{Position: lin.V3(0, 4, 6), Target: lin.V3(0, 0, 0)})
			g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{0.5, 0.5, 0.5, 1}, Ambient: Color{1, 1, 1, 1}})
			g.DrawMesh(cube, Material{BaseColor: White, Roughness: 1}, lin.Translate(lin.V3(0, -0.5, 0)).Mul(lin.Scale(lin.V3(12, 0.2, 12))))
			g.DrawMesh(cube, Material{BaseColor: White, Roughness: 1}, lin.Translate(lin.V3(0, 0.5, 0)).Mul(lin.Scale(lin.V3(2, 2, 2))))
			if img, err = g.end(true); err != nil {
				t.Fatal(err)
			}
		}
		return img
	}
	off := render(0)
	on := render(1)
	// Find the floor just in front of the box's base: scan down from the
	// centre column for the first floor pixel below the box.
	baseY := -1
	for y := 64; y < 127; y++ {
		if off.RGBAAt(64, y).R != off.RGBAAt(64, y+1).R {
			baseY = y + 3
			break
		}
	}
	if baseY < 0 {
		t.Fatal("could not locate the box base")
	}
	a, b := off.RGBAAt(64, baseY), on.RGBAAt(64, baseY)
	if int(b.R) > int(a.R)-8 {
		t.Errorf("floor at the box base: without AO %v, with AO %v; expected darker", a, b)
	}
	c, d := off.RGBAAt(120, 120), on.RGBAAt(120, 120)
	if int(c.R)-int(d.R) > 12 {
		t.Errorf("open floor: without AO %v, with AO %v; expected nearly equal", c, d)
	}
}
