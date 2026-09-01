package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestInstancingAndBlend draws a grid of identical cubes (one instanced
// draw) with a translucent red pane in front: the pane must tint what is
// behind it rather than hide it, and a corner of the grid must render.
func TestInstancingAndBlend(t *testing.T) {
	g := newHeadless(t, 128, 128)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1})
	var img *image.RGBA
	for range 2 {
		ok, err := g.Begin(Black)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		g.SetCamera(Camera{Position: lin.V3(0, 0, 12), Target: lin.V3(0, 0, 0)})
		g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{3, 3, 3, 1}, Ambient: Color{0.2, 0.2, 0.2, 1}})
		for y := -3; y <= 3; y++ {
			for x := -3; x <= 3; x++ {
				g.DrawMesh(cube, Material{BaseColor: White, Roughness: 1}, lin.Translate(lin.V3(float32(x)*1.4, float32(y)*1.4, 0)))
			}
		}
		// A translucent red sheet in front of the middle of the grid.
		g.DrawMesh(cube, Material{BaseColor: Color{1, 0, 0, 0.5}, Blend: true, Roughness: 1},
			lin.Translate(lin.V3(0, 0, 3)).Mul(lin.Scale(lin.V3(3, 3, 0.05))))
		if img, err = g.End(true); err != nil {
			t.Fatal(err)
		}
	}
	centre := img.RGBAAt(64, 64)
	if centre.R < 120 || centre.G < 40 || centre.G > 200 {
		t.Errorf("centre %v: expected a red tint over a lit white cube", centre)
	}
	corner := img.RGBAAt(64+36, 64+36) // the (3,3) cube of the grid, outside the sheet
	if corner.R < 100 || corner.R != corner.G {
		t.Errorf("corner %v: expected a lit white cube from the instanced grid", corner)
	}
	if n := len(g.main.draws); n != 50 {
		t.Errorf("queued %d draws, want 50", n)
	}
}
