package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// twoPartModel is a model of two quads, left and right of the camera,
// with a named material each. It is built by hand rather than loaded, so
// the test says nothing about glTF.
func twoPartModel(t *testing.T, g *Graphics) *Model {
	t.Helper()
	quad := facingQuad(t, g)
	red := Material{BaseColor: RGB(220, 0, 0), Unlit: true}
	place := func(x float32) lin.Mat4 {
		return lin.Translate(lin.V3(x, 0, 0)).Mul(lin.Scale(lin.V3(0.45, 0.45, 1)))
	}
	return &Model{Parts: []ModelPart{
		{Mesh: quad, Name: "left", Material: red, World: place(-1)},
		{Mesh: quad, Name: "right", Material: red, World: place(1)},
	}}
}

func TestDrawModelWith(t *testing.T) {
	g := newHeadless(t, 64, 64)
	model := twoPartModel(t, g)
	// The quads land around x = 13 and x = 51 with the test camera.
	const leftX, rightX, midY = 13, 51, 32
	img := renderMaterial(t, g, func() { g.DrawModel(model, lin.Identity()) })
	for _, x := range []int{leftX, rightX} {
		if c := img.RGBAAt(x, midY); c.R < 150 || c.B > 60 {
			t.Fatalf("DrawModel part at x %d is %v, want the file's red", x, c)
		}
	}
	// One part overridden by name.
	img = renderMaterial(t, g, func() {
		g.DrawModelWith(model, lin.Identity(), func(i int, p ModelPart) Material {
			if p.Name != "right" {
				return p.Material
			}
			m := p.Material
			m.BaseColor = RGB(0, 0, 220)
			return m
		})
	})
	if c := img.RGBAAt(leftX, midY); c.R < 150 || c.B > 60 {
		t.Errorf("the part that was not overridden is %v, want red", c)
	}
	if c := img.RGBAAt(rightX, midY); c.B < 150 || c.R > 60 {
		t.Errorf("the part overridden by name is %v, want blue", c)
	}
	// One material for every part, whatever the file said.
	img = renderMaterial(t, g, func() {
		g.DrawModelWith(model, lin.Identity(), func(i int, p ModelPart) Material {
			return Material{BaseColor: RGB(0, 220, 0), Unlit: true}
		})
	})
	for _, x := range []int{leftX, rightX} {
		if c := img.RGBAAt(x, midY); c.G < 150 || c.R > 60 {
			t.Errorf("part at x %d is %v under a whole-model override, want green", x, c)
		}
	}
	// The model's own materials are left alone by an override.
	if model.Parts[1].Material.BaseColor != RGB(220, 0, 0) {
		t.Errorf("an override changed the model's stored material: %v", model.Parts[1].Material.BaseColor)
	}
}
