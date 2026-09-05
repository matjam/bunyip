package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestLightProbeGrid bakes a grid across a space with one red wall at one
// end and checks a white diffuse ball near that end takes more red from
// the grid than the same ball at the far end, which is what a baked
// bounce is for.
func TestLightProbeGrid(t *testing.T) {
	g := newHeadless(t, 64, 64)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	sv, si := SphereMesh(16, 32)
	sphere, err := g.NewMesh(sv, si)
	if err != nil {
		t.Fatal(err)
	}
	defer sphere.Destroy()
	dark := Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Ambient: Color{}, Sky: Sky{Vacuum: 1}}
	// A bright red wall at one end of the space and nothing else, so every
	// bit of ambient light in the bake comes from it.
	scene := func() {
		g.SetLight(dark)
		g.DrawMesh(cube, Material{BaseColor: Color{1, 0, 0, 1}, Unlit: true},
			lin.Translate(lin.V3(6, 0, 0)).Mul(lin.Scale(lin.V3(0.5, 4, 4))))
	}
	grid := &LightProbeGrid{Origin: lin.V3(-4, 0, 0), Spacing: lin.V3(8, 4, 4), Counts: [3]int{2, 1, 1}, Resolution: 16}
	if err := g.BakeLightProbes(grid, scene); err != nil {
		t.Fatal(err)
	}
	if !grid.Baked() {
		t.Fatal("BakeLightProbes left no harmonics")
	}
	near, far := grid.Probe(1, 0, 0), grid.Probe(0, 0, 0)
	if near[0].X <= far[0].X {
		t.Errorf("the cell by the red wall has irradiance %.3f and the far one %.3f; want more by the wall", near[0].X, far[0].X)
	}
	// The same white ball at each end of the grid, lit by the grid alone.
	ball := func(x float32) *image.RGBA {
		return renderMaterial(t, g, func() {
			g.SetCamera(Camera{Position: lin.V3(x, 0, 3), Target: lin.V3(x, 0, 0)})
			g.SetLight(dark)
			g.SetLightProbes(grid)
			g.DrawMesh(sphere, Material{BaseColor: White, Roughness: 1}, lin.Translate(lin.V3(x, 0, 0)))
		})
	}
	byWall, awayFromWall := ball(4).RGBAAt(32, 32), ball(-4).RGBAAt(32, 32)
	if byWall.R <= awayFromWall.R+5 {
		t.Errorf("the ball by the red wall is %v and the far one %v; want the near one redder", byWall, awayFromWall)
	}
	if byWall.R <= byWall.B+5 {
		t.Errorf("the ball by the red wall is %v; want the grid's red bounce on it", byWall)
	}
	// Outside the grid the ambient falls back to the sky, which is black
	// here, so the same ball keeps none of the bounce.
	outside := renderMaterial(t, g, func() {
		g.SetCamera(Camera{Position: lin.V3(40, 0, 3), Target: lin.V3(40, 0, 0)})
		g.SetLight(dark)
		g.SetLightProbes(grid)
		g.DrawMesh(sphere, Material{BaseColor: White, Roughness: 1}, lin.Translate(lin.V3(40, 0, 0)))
	}).RGBAAt(32, 32)
	if outside.R >= byWall.R {
		t.Errorf("a ball outside the grid is %v and one inside it %v; want the grid to stop at its edge", outside, byWall)
	}
}
