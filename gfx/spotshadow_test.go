package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestSpotShadow(t *testing.T) {
	g := newHeadless(t, 64, 64)
	pv, pi := PlaneMesh(1)
	floor, err := g.NewMesh(pv, pi)
	if err != nil {
		t.Fatal(err)
	}
	defer floor.Destroy()
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	// A lamp over a floor with a block beside its axis: the block's
	// shadow lands on the floor to the side, where the camera above sees
	// it. Without shadows the same spot on the floor is lit.
	var px, py float32
	luma := func(shadows bool) float32 {
		g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
		if ok, err := g.begin(Black); err != nil || !ok {
			t.Fatal(err)
		}
		g.SetCamera(Camera{Position: lin.V3(2.5, 4, 0.5), Target: lin.V3(0.5, 0, 0)})
		g.SetLight(Light{Direction: lin.V3(0, -1, 0)}) // no sun, no ambient
		g.AddSpot(SpotLight{Position: lin.V3(0, 2.5, 0), Direction: lin.V3(0, -1, 0), Color: Color{6, 6, 6, 1}, Range: 8, OuterAngle: lin.Radians(90), Shadows: shadows})
		g.DrawMesh(floor, Material{Roughness: 1}, lin.Scale(lin.V3(8, 1, 8)))
		g.DrawMesh(cube, Material{Roughness: 1}, lin.Translate(lin.V3(0.6, 1.2, 0)).Mul(lin.Scale(lin.V3(0.5, 0.5, 0.5))))
		px, py, _ = g.Project(lin.V3(1.15, 0, 0))
		img, err := g.end(true)
		if err != nil {
			t.Fatal(err)
		}
		c := img.RGBAAt(int(px), int(py))
		return float32(c.R) + float32(c.G) + float32(c.B)
	}
	lit := luma(false)
	dark := luma(true)
	if lit < 300 {
		t.Fatalf("the floor beside the block is dark without shadows (%v at %v,%v)", lit, px, py)
	}
	if dark > lit/3 {
		t.Errorf("the block's shadow is missing: %v lit, %v with shadows", lit, dark)
	}
}
