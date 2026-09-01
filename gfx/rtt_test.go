package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestRenderTexture draws a lit red cube into a render texture, then draws
// that texture as a sprite on the left half of the screen next to a blue
// rectangle, and checks both halves.
func TestRenderTexture(t *testing.T) {
	g := newHeadless(t, 128, 64)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	rt, err := g.NewRenderTexture(64, 64)
	if err != nil {
		t.Fatalf("NewRenderTexture: %v", err)
	}
	defer rt.Destroy()
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
		g.DrawTo(rt, Color{0, 0, 0, 1}, func() {
			g.SetCamera(Camera{Position: lin.V3(0, 0, 2.5)})
			g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{3, 3, 3, 1}, Ambient: Color{0.2, 0.2, 0.2, 1}})
			g.DrawMesh(cube, Material{BaseColor: Color{1, 0, 0, 1}, Roughness: 1}, lin.Identity())
		})
		g.DrawTexture(rt.Texture(), 0, 0)
		g.FillRect(64, 0, 64, 64, Color{0, 0, 1, 1})
		if img, err = g.End(true); err != nil {
			t.Fatal(err)
		}
	}
	left := img.RGBAAt(32, 32)
	if left.R < 150 || left.G > 80 {
		t.Errorf("left %v: expected the red cube from the render texture", left)
	}
	if right := img.RGBAAt(96, 32); right.B < 200 || right.R > 20 {
		t.Errorf("right %v: expected the blue rectangle", right)
	}
	if corner := img.RGBAAt(2, 2); corner.R > 20 {
		t.Errorf("corner %v: render texture background should be black", corner)
	}
}

// TestPicking casts rays from screen points at a cube and checks hits and misses.
func TestPicking(t *testing.T) {
	g := newHeadless(t, 100, 100)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	ok, err := g.Begin(Black)
	if err != nil || !ok {
		t.Fatal(err)
	}
	g.SetCamera(Camera{Position: lin.V3(0, 0, 5), Target: lin.V3(0, 0, 0)})
	model := lin.Translate(lin.V3(1, 0, 0))
	centre := g.ScreenRay(50, 50)
	if _, hit := cube.Intersect(model, centre); hit {
		t.Error("ray through the screen centre should miss a cube offset to x=1")
	}
	// The cube at x=1 sits right of centre; at z=5 with 60 degrees the
	// half-width is 2.9 units, so x=1 is 17 px right of centre.
	right := g.ScreenRay(67, 50)
	h, hit := cube.Intersect(model, right)
	if !hit {
		t.Fatal("ray at the cube's centre should hit")
	}
	if h.Distance < 4.3 || h.Distance > 4.7 || h.Normal.Z < 0.9 {
		t.Errorf("hit %+v: expected the front face about 4.5 away", h)
	}
	if _, err := g.End(false); err != nil {
		t.Fatal(err)
	}
}
