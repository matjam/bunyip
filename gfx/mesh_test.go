package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestMeshDepth draws a large red cube behind a small green cube in both
// submission orders; with a working depth test the centre is green each
// time and the corners keep the clear colour.
func TestMeshDepth(t *testing.T) {
	g := newHeadless(t, 128, 128)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatalf("NewMesh: %v", err)
	}
	defer cube.Destroy()
	red := Material{BaseColor: Color{1, 0, 0, 1}}
	green := Material{BaseColor: Color{0, 1, 0, 1}}
	far := lin.Translate(lin.V3(0, 0, -4)).Mul(lin.Scale(lin.V3(6, 6, 1)))
	near := lin.Translate(lin.V3(0, 0, 0))

	for _, order := range []string{"far-first", "near-first"} {
		var img *image.RGBA
		for range 2 {
			ok, err := g.begin(Black)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			if !ok {
				continue
			}
			g.SetCamera(Camera{Position: lin.V3(0, 0, 4)})
			g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: White, Ambient: Color{0.2, 0.2, 0.2, 1}})
			if order == "far-first" {
				g.DrawMesh(cube, red, far)
				g.DrawMesh(cube, green, near)
			} else {
				g.DrawMesh(cube, green, near)
				g.DrawMesh(cube, red, far)
			}
			if img, err = g.end(true); err != nil {
				t.Fatalf("End: %v", err)
			}
		}
		// The white specular highlight adds to every channel, so compare
		// channels against each other rather than against zero.
		centre := img.RGBAAt(64, 64)
		if centre.G < 150 || int(centre.G) < int(centre.R)+50 {
			t.Errorf("%s: centre %v should be green", order, centre)
		}
		edge := img.RGBAAt(24, 64)
		if edge.R < 100 || int(edge.R) < int(edge.G)+50 {
			t.Errorf("%s: edge %v should show the red backdrop", order, edge)
		}
	}
}

// TestSpritesOverMeshes checks that 2D drawing lands on top of 3D.
func TestSpritesOverMeshes(t *testing.T) {
	g := newHeadless(t, 64, 64)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	var img *image.RGBA
	for range 2 {
		ok, err := g.begin(Black)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		g.FillRect(0, 0, 64, 64, Color{0, 0, 1, 1})
		g.SetCamera(Camera{Position: lin.V3(0, 0, 3)})
		g.DrawMesh(cube, Material{BaseColor: Color{1, 0, 0, 1}}, lin.Identity())
		if img, err = g.end(true); err != nil {
			t.Fatal(err)
		}
	}
	if c := img.RGBAAt(32, 32); c != (color.RGBA{0, 0, 255, 255}) {
		t.Errorf("centre %v, want the sprite's blue over the cube", c)
	}
}
