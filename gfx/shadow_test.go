package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestShadows lights a floor from above with a cube hovering over its
// centre: the floor under the cube must be darker than the open floor when
// shadows are on, and the same brightness when they are off.
func TestShadows(t *testing.T) {
	g := newHeadless(t, 128, 128)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1}) // no bloom, no vignette
	render := func(shadows bool) *image.RGBA {
		var img *image.RGBA
		for range 2 {
			ok, err := g.Begin(Black)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				continue
			}
			// Camera looks straight down at the floor from y=12.
			g.SetCamera(Camera{Position: lin.V3(0, 12, 0.01), Target: lin.V3(0, 0, 0)})
			// The light slants so the cube's shadow falls beside it rather than under it.
			g.SetLight(Light{Direction: lin.V3(0.6, -1, 0), Color: White, Ambient: Color{0.1, 0.1, 0.1, 1}, Shadows: shadows, ShadowDistance: 30})
			floor := lin.Translate(lin.V3(0, -1, 0)).Mul(lin.Scale(lin.V3(20, 0.2, 20)))
			g.DrawMesh(cube, Material{BaseColor: White, Roughness: 1}, floor)
			g.DrawMesh(cube, Material{BaseColor: White, Roughness: 1}, lin.Translate(lin.V3(0, 2, 0)))
			if img, err = g.End(true); err != nil {
				t.Fatal(err)
			}
		}
		return img
	}
	on := render(true)
	// The cube covers about 5 px around the centre; its shadow lands 9..18 px to +x.
	shadowed := on.RGBAAt(64+13, 64)
	open := on.RGBAAt(64+50, 64)
	if int(shadowed.R) > int(open.R)-40 {
		t.Errorf("with shadows: floor beside the cube %v should be darker than open floor %v", shadowed, open)
	}
	off := render(false)
	a, b := off.RGBAAt(64+14, 64), off.RGBAAt(64+50, 64)
	if int(a.R)-int(b.R) > 12 || int(b.R)-int(a.R) > 12 {
		t.Errorf("without shadows: %v and %v should match", a, b)
	}
}
