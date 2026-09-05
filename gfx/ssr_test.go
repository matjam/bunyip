package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// renderPost is renderMaterial with the game's own post settings, for the
// passes a test has to turn on.
func renderPost(t *testing.T, g *Graphics, post PostSettings, draw func()) *image.RGBA {
	t.Helper()
	g.SetPost(post)
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	draw()
	img, err := g.end(true)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestScreenSpaceReflections puts a bright green box over a mirror floor
// under a black sky, where the only green the floor can show is the box's
// reflection, and checks the reflection pass puts it there.
func TestScreenSpaceReflections(t *testing.T) {
	g := newHeadless(t, 128, 128)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	pv, pi := PlaneMesh(1)
	plane, err := g.NewMesh(pv, pi)
	if err != nil {
		t.Fatal(err)
	}
	defer plane.Destroy()
	scene := func() {
		g.SetCamera(Camera{Position: lin.V3(0, 1.6, 5), Target: lin.V3(0, 0.6, 0)})
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Ambient: Color{}, Sky: Sky{Vacuum: 1}})
		g.DrawMesh(plane, Material{BaseColor: White, Metallic: 1, Roughness: 0.05}, lin.Scale(lin.V3(20, 1, 20)))
		g.DrawMesh(cube, Material{BaseColor: Color{0, 1, 0, 1}, Unlit: true},
			lin.Translate(lin.V3(0, 1.2, 0)).Mul(lin.Scale(lin.V3(0.6, 0.6, 0.6))))
	}
	base := PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true}
	off := renderPost(t, g, base, scene)
	on := base
	on.Reflections = 1
	lit := renderPost(t, g, on, scene)
	// The floor below the box is where its reflection lands. Look down the
	// middle of the frame under the box for the biggest gain in green.
	best, bestY := 0, 0
	for y := 64; y < 128; y++ {
		gain := int(lit.RGBAAt(64, y).G) - int(off.RGBAAt(64, y).G)
		if gain > best {
			best, bestY = gain, y
		}
	}
	if best < 20 {
		t.Errorf("the mirror floor gained at most %d green from the reflection pass, want the box reflected in it", best)
	}
	t.Logf("brightest reflection gain %d at row %d", best, bestY)
	// A rough floor reflects nothing in screen space, so the same scene
	// with a rough material must not change.
	rough := func() {
		g.SetCamera(Camera{Position: lin.V3(0, 1.6, 5), Target: lin.V3(0, 0.6, 0)})
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Ambient: Color{}, Sky: Sky{Vacuum: 1}})
		g.DrawMesh(plane, Material{BaseColor: White, Metallic: 1, Roughness: 1}, lin.Scale(lin.V3(20, 1, 20)))
		g.DrawMesh(cube, Material{BaseColor: Color{0, 1, 0, 1}, Unlit: true},
			lin.Translate(lin.V3(0, 1.2, 0)).Mul(lin.Scale(lin.V3(0.6, 0.6, 0.6))))
	}
	roughOff := renderPost(t, g, base, rough)
	roughOn := renderPost(t, g, on, rough)
	for y := 64; y < 128; y += 8 {
		if gain := int(roughOn.RGBAAt(64, y).G) - int(roughOff.RGBAAt(64, y).G); gain > 4 {
			t.Errorf("a rough floor gained %d green at row %d; want no screen-space reflection on it", gain, y)
		}
	}
}
