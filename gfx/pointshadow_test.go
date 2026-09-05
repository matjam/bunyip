package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// pointShadowScene draws a wall lit by one point light with a small
// block between the light and the wall, off to one side so the camera
// sees both the block's shadow and the open wall beside it. decoys
// shadowed point lights are added first, to use up the cube maps.
func pointShadowScene(t *testing.T, g *Graphics, shadows bool, decoys int) *image.RGBA {
	t.Helper()
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	return frames(t, g, func() {
		g.SetCamera(Camera{Position: lin.V3(0, 1.5, 5), Target: lin.V3(0, 1.5, -1)})
		g.SetLight(Light{Direction: lin.V3(0, -1, 0)}) // no sun, no ambient
		for i := range decoys {
			// Far away and short ranged: these light nothing, they only
			// take the cube maps.
			g.AddPoint(PointLight{Position: lin.V3(float32(100+i), 0, 0), Color: White, Range: 1, Shadows: true})
		}
		g.AddPoint(PointLight{Position: lin.V3(0, 1.5, 2), Color: Color{40, 40, 40, 1}, Range: 12, Shadows: shadows})
		g.DrawMesh(cube, Material{Roughness: 1}, lin.Translate(lin.V3(0, 1.5, -1)).Mul(lin.Scale(lin.V3(10, 6, 0.2))))
		g.DrawMesh(cube, Material{Roughness: 1}, lin.Translate(lin.V3(0.7, 1.5, 0.6)).Mul(lin.Scale(lin.V3(0.3, 0.3, 0.3))))
	})
}

// TestPointShadow puts a block between a point light and a wall: the
// wall behind the block darkens and the wall beside it does not.
func TestPointShadow(t *testing.T) {
	g := newHeadless(t, 128, 128)
	on := pointShadowScene(t, g, true, 0)
	shadowed, open := on.RGBAAt(92, 64), on.RGBAAt(36, 64)
	if int(open.R) < 40 {
		t.Fatalf("the wall beside the block is dark with the light on: %v", open)
	}
	if int(shadowed.R) > int(open.R)/2 {
		t.Errorf("the block's shadow is missing: wall behind it %v, beside it %v", shadowed, open)
	}
	off := pointShadowScene(t, g, false, 0)
	a, b := off.RGBAAt(92, 64), off.RGBAAt(36, 64)
	if int(a.R) < int(b.R)/2 {
		t.Errorf("without shadows the wall behind the block %v should match the wall beside it %v", a, b)
	}
}

// TestPointShadowCap checks that a fifth shadowed point light is drawn
// without a shadow rather than writing over another light's cube map:
// with four cube maps already taken its block casts nothing, and the
// wall beside the block is lit either way.
func TestPointShadowCap(t *testing.T) {
	g := newHeadless(t, 128, 128)
	first := pointShadowScene(t, g, true, 0)
	fifth := pointShadowScene(t, g, true, MaxPointShadows)
	open := fifth.RGBAAt(36, 64)
	if int(open.R) < 40 {
		t.Fatalf("the wall beside the block is dark past the cube map limit: %v", open)
	}
	if got, want := int(fifth.RGBAAt(92, 64).R), int(open.R)/2; got < want {
		t.Errorf("the fifth shadowed point light still cast a shadow: wall behind the block %d, beside it %d", got, open.R)
	}
	if int(first.RGBAAt(92, 64).R) > int(first.RGBAAt(36, 64).R)/2 {
		t.Errorf("the first shadowed point light cast no shadow, so the comparison says nothing")
	}
}
