package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// frames renders draw twice and returns the second frame, so pipelines
// and descriptor sets are warm and the stats belong to a settled frame.
func frames(t *testing.T, g *Graphics, draw func()) *image.RGBA {
	t.Helper()
	var img *image.RGBA
	for range 2 {
		ok, err := g.begin(Black)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		draw()
		if img, err = g.end(true); err != nil {
			t.Fatal(err)
		}
	}
	return img
}

// TestShadowCullSpot puts two cubes under a spot light with a narrow
// cone: both are recorded into its shadow map while both stand under it,
// and only the one inside the cone once the other is moved away.
func TestShadowCullSpot(t *testing.T) {
	g := newHeadless(t, 64, 64)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	shadowDraws := func(second lin.Vec3) int {
		frames(t, g, func() {
			g.SetCamera(Camera{Position: lin.V3(0, 3, 4), Target: lin.V3(0, 0, 0)})
			g.SetLight(Light{Direction: lin.V3(0, -1, 0)}) // no cascades, so the spot map is the only one
			g.AddSpot(SpotLight{Position: lin.V3(0, 3, 0), Direction: lin.V3(0, -1, 0),
				Color: Color{6, 6, 6, 1}, Range: 8, OuterAngle: lin.Radians(30), Shadows: true})
			g.DrawMesh(cube, Material{Roughness: 1}, lin.Translate(lin.V3(0, 0.5, 0)))
			g.DrawMesh(cube, Material{Roughness: 1}, lin.Translate(second))
		})
		return g.Stats().ShadowDraws
	}
	if n := shadowDraws(lin.V3(0.6, 0.5, 0)); n != 2 {
		t.Errorf("two cubes under the light record %d shadow instances, want 2", n)
	}
	if n := shadowDraws(lin.V3(30, 0.5, 0)); n != 1 {
		t.Errorf("a cube outside the cone records %d shadow instances, want 1", n)
	}
}

// TestCascadeNearPlane hangs a small box far above a floor with the sun
// straight down. The box is well above the cascade's near plane, so the
// shadow pipelines have to clamp depth (or the cascade has to move its
// near plane back) for the floor beneath it to darken.
func TestCascadeNearPlane(t *testing.T) {
	g := newHeadless(t, 128, 128)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	img := frames(t, g, func() {
		// Straight down at the floor, which fills the view.
		g.SetCamera(Camera{Position: lin.V3(0, 12, 0.01), Target: lin.V3(0, 0, 0)})
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: White, Ambient: Color{0.05, 0.05, 0.05, 1},
			Shadows: true, ShadowDistance: 30})
		g.DrawMesh(cube, Material{BaseColor: White, Roughness: 1},
			lin.Translate(lin.V3(0, -0.1, 0)).Mul(lin.Scale(lin.V3(40, 0.2, 40))))
		g.DrawMesh(cube, Material{BaseColor: White, Roughness: 1},
			lin.Translate(lin.V3(0, 60, 0)).Mul(lin.Scale(lin.V3(2, 2, 2))))
	})
	under, open := img.RGBAAt(64, 64), img.RGBAAt(64+45, 64)
	if int(under.R) > int(open.R)-40 {
		t.Errorf("floor under the high box %v should be darker than open floor %v", under, open)
	}
}
