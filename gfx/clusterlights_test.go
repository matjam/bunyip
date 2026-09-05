package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestClusteredLights lights a floor with three hundred point lights, more
// than any fixed array could hold: none is dropped, the red one under
// the middle of the view colours the floor beneath it, and the green one
// off to the side colours the floor there, so lights reach the clusters
// across the whole view rather than one corner of it.
func TestClusteredLights(t *testing.T) {
	g := newHeadless(t, 128, 128)
	pv, pi := PlaneMesh(1)
	floor, err := g.NewMesh(pv, pi)
	if err != nil {
		t.Fatal(err)
	}
	defer floor.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	red, green := lin.V3(0, 0.4, 0), lin.V3(4, 0.4, 0)
	var redX, redY, greenX, greenY float32
	img := frames(t, g, func() {
		g.SetCamera(Camera{Position: lin.V3(0, 12, 0.01), Target: lin.V3(0, 0, 0)})
		g.SetLight(Light{Direction: lin.V3(0, -1, 0)}) // no sun, no ambient
		// A field of dim lights over the floor, then the two the test
		// looks for.
		for i := range 298 {
			x := float32(i%20)*0.8 - 8
			z := float32(i/20)*0.8 - 6
			g.AddPointLight(lin.V3(x, 0.4, z), Color{0.05, 0.05, 0.05, 1}, 0.5)
		}
		g.AddPointLight(red, Color{20, 0, 0, 1}, 3)
		g.AddPointLight(green, Color{0, 20, 0, 1}, 3)
		g.DrawMesh(floor, Material{Roughness: 1}, lin.Scale(lin.V3(30, 1, 30)))
		redX, redY, _ = g.Project(red.Sub(lin.V3(0, 0.4, 0)))
		greenX, greenY, _ = g.Project(green.Sub(lin.V3(0, 0.4, 0)))
	})
	if s := g.Stats(); s.Lights != 300 || s.LightsDropped != 0 {
		t.Fatalf("the frame kept %d lights and dropped %d, want 300 and 0", s.Lights, s.LightsDropped)
	}
	under := img.RGBAAt(int(redX), int(redY))
	if under.R < 60 || int(under.R) < int(under.G)+40 {
		t.Errorf("the floor under the red light is %v, want a red pixel", under)
	}
	beside := img.RGBAAt(int(greenX), int(greenY))
	if beside.G < 60 || int(beside.G) < int(beside.R)+40 {
		t.Errorf("the floor under the green light is %v, want a green pixel", beside)
	}
}
