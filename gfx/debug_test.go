package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestDebugLinesAndText(t *testing.T) {
	g := newHeadless(t, 96, 96)
	// A red line across the middle of the view, looking down -z from z=3,
	// and debug text in the corner.
	img := renderMaterial(t, g, func() {
		g.DrawLine3D(lin.V3(-1, 0, 0), lin.V3(1, 0, 0), RGB(255, 0, 0))
		g.DebugText(2, 70, "hello")
	})
	found := false
	for y := 44; y < 53 && !found; y++ {
		for x := 30; x < 66; x++ {
			c := img.RGBAAt(x, y)
			if c.R > 150 && c.G < 80 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("no red debug line pixels along the middle row")
	}
	lit := 0
	for y := 70; y < 90; y++ {
		for x := 2; x < 50; x++ {
			if bright(img, x, y) {
				lit++
			}
		}
	}
	if lit < 10 {
		t.Errorf("debug text lit %d pixels, want some", lit)
	}
}

func TestProjectAndOrtho(t *testing.T) {
	g := newHeadless(t, 96, 96)
	if ok, err := g.Begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	g.SetCamera(Camera{Position: lin.V3(0, 0, 3), Target: lin.V3(0, 0, 0)})
	x, y, ok := g.Project(lin.V3(0, 0, 0))
	if !ok || x < 47 || x > 49 || y < 47 || y > 49 {
		t.Errorf("origin projects to %v %v %v, want the centre", x, y, ok)
	}
	if _, _, ok := g.Project(lin.V3(0, 0, 10)); ok {
		t.Error("a point behind the camera projected")
	}
	if x, y, _ := g.Project(lin.V3(0, 1, 0)); y >= 48 || x < 47 {
		t.Errorf("a point above the origin projects to %v %v, want higher on screen", x, y)
	}
	// An orthographic camera of half-height 1 puts y = 1 at the top edge.
	g.SetCamera(Camera{Position: lin.V3(0, 0, 3), Target: lin.V3(0, 0, 0), Ortho: 1})
	if _, y, _ := g.Project(lin.V3(0, 1, 0)); y > 1 {
		t.Errorf("ortho top edge projects to y %v, want 0", y)
	}
	if _, err := g.End(false); err != nil {
		t.Fatal(err)
	}
	// Under an orthographic camera a sphere near and far draw the same size.
	sv, si := SphereMesh(16, 32)
	sphere, err := g.NewMesh(sv, si)
	if err != nil {
		t.Fatal(err)
	}
	defer sphere.Destroy()
	width := func(z float32) int {
		if ok, err := g.Begin(Black); err != nil || !ok {
			t.Fatal(err)
		}
		g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
		g.SetCamera(Camera{Position: lin.V3(0, 0, 10), Target: lin.V3(0, 0, 0), Ortho: 2})
		g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: White, Ambient: Color{0.5, 0.5, 0.5, 1}})
		g.DrawMesh(sphere, Material{Unlit: true, BaseColor: White}, lin.Translate(lin.V3(0, 0, z)))
		img, err := g.End(true)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for x := 0; x < 96; x++ {
			if bright(img, x, 48) {
				n++
			}
		}
		return n
	}
	near, far := width(4), width(-4)
	if near < 40 || near != far {
		t.Errorf("ortho sphere widths near %d far %d, want equal", near, far)
	}
}

func TestTextureWriteAndRead(t *testing.T) {
	g := newHeadless(t, 32, 32)
	tex, err := g.NewBlankTexture(4, 4, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	patch := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := range 2 {
		for x := range 2 {
			patch.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	if err := tex.Write(2, 2, patch); err != nil {
		t.Fatal(err)
	}
	back, err := tex.Read()
	if err != nil {
		t.Fatal(err)
	}
	if c := back.RGBAAt(3, 3); c.R != 255 || c.A != 255 {
		t.Errorf("written pixel reads back as %v", c)
	}
	if c := back.RGBAAt(0, 0); c.A != 0 {
		t.Errorf("untouched pixel reads back as %v, want transparent", c)
	}
	// Writes past the edge are clipped, not an error.
	if err := tex.Write(3, 3, patch); err != nil {
		t.Fatal(err)
	}
	// A render texture reads back what was drawn into it.
	rt, err := g.NewRenderTexture(16, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Destroy()
	if ok, err := g.Begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	g.DrawTo(rt, RGB(0, 0, 255), func() {})
	if _, err := g.End(false); err != nil {
		t.Fatal(err)
	}
	img, err := rt.Read()
	if err != nil {
		t.Fatal(err)
	}
	if c := img.RGBAAt(8, 8); c.B < 200 || c.R > 30 {
		t.Errorf("render texture reads back %v, want blue", c)
	}
}
