package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// facingQuad is a unit quad in the x-y plane facing +z, drawn at the
// origin so a camera on +z sees it fill the middle of the frame.
func facingQuad(t *testing.T, g *Graphics) *Mesh {
	t.Helper()
	verts := []Vertex{
		{Pos: lin.V3(-1, -1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(0, 1)},
		{Pos: lin.V3(1, -1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(1, 1)},
		{Pos: lin.V3(1, 1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(1, 0)},
		{Pos: lin.V3(-1, 1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(0, 0)},
	}
	m, err := g.NewMesh(verts, []uint32{0, 1, 2, 0, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Destroy)
	return m
}

// renderMaterial draws meshes through a fixed camera with post-processing
// off and returns the frame.
func renderMaterial(t *testing.T, g *Graphics, draw func()) *image.RGBA {
	t.Helper()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	if ok, err := g.Begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	g.SetCamera(Camera{Position: lin.V3(0, 0, 3), Target: lin.V3(0, 0, 0)})
	g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{1, 1, 1, 1}, Ambient: Color{0.2, 0.2, 0.2, 1}})
	draw()
	img, err := g.End(true)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func bright(img *image.RGBA, x, y int) bool {
	r, gg, b, _ := img.At(x, y).RGBA()
	return r+gg+b > 3*0x3000
}

func TestAlphaCutout(t *testing.T) {
	g := newHeadless(t, 64, 64)
	// Left half of the texture is opaque, right half transparent.
	tex := image.NewRGBA(image.Rect(0, 0, 2, 1))
	tex.SetRGBA(0, 0, color.RGBA{255, 255, 255, 255})
	tex.SetRGBA(1, 0, color.RGBA{255, 255, 255, 30})
	tx, err := g.NewTexture(tex, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Destroy()
	quad := facingQuad(t, g)
	img := renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{Texture: tx, AlphaCutoff: 0.5}, lin.Identity())
	})
	if !bright(img, 20, 32) {
		t.Errorf("opaque half of the cutout quad is missing")
	}
	if bright(img, 44, 32) {
		t.Errorf("transparent half of the cutout quad was drawn")
	}
}

func TestUnlitMaterial(t *testing.T) {
	g := newHeadless(t, 64, 64)
	quad := facingQuad(t, g)
	// Lit from behind with a dim ambient: a lit quad is dark, an unlit one
	// shows its base colour exactly.
	img := renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, 0, 1), Color: Color{1, 1, 1, 1}, Ambient: Color{0.02, 0.02, 0.02, 1}})
		g.DrawMesh(quad, Material{BaseColor: RGB(0, 200, 0), Unlit: true}, lin.Translate(lin.V3(-0.8, 0, 0)).Mul(lin.Scale(lin.V3(0.5, 0.5, 1))))
		g.DrawMesh(quad, Material{BaseColor: RGB(0, 200, 0)}, lin.Translate(lin.V3(0.8, 0, 0)).Mul(lin.Scale(lin.V3(0.5, 0.5, 1))))
	})
	_, unlitG, _, _ := img.At(20, 32).RGBA()
	_, litG, _, _ := img.At(44, 32).RGBA()
	if unlitG < 0x9000 {
		t.Errorf("unlit quad green is %d, want its base colour", unlitG>>8)
	}
	if litG > 0x3000 {
		t.Errorf("lit quad facing away from the light is %d green, want dark", litG>>8)
	}
}

func TestDoubleSidedAndDepthState(t *testing.T) {
	g := newHeadless(t, 64, 64)
	quad := facingQuad(t, g)
	back := lin.Rotate(lin.Radians(180), lin.V3(0, 1, 0)) // faces away from the camera
	img := renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{BaseColor: RGB(255, 255, 255)}, lin.Translate(lin.V3(-0.8, 0, 0)).Mul(back).Mul(lin.Scale(lin.V3(0.5, 0.5, 1))))
		g.DrawMesh(quad, Material{BaseColor: RGB(255, 255, 255), DoubleSided: true}, lin.Translate(lin.V3(0.8, 0, 0)).Mul(back).Mul(lin.Scale(lin.V3(0.5, 0.5, 1))))
	})
	if bright(img, 20, 32) {
		t.Errorf("single-sided back face was drawn")
	}
	if !bright(img, 44, 32) {
		t.Errorf("double-sided back face was culled")
	}
	// A far quad drawn after a near one shows through with NoDepthTest.
	img = renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{BaseColor: RGB(255, 0, 0)}, lin.Translate(lin.V3(0, 0, 1)))
		g.DrawMesh(quad, Material{BaseColor: RGB(0, 0, 255), NoDepthTest: true}, lin.Translate(lin.V3(0, 0, -1)).Mul(lin.Scale(lin.V3(0.5, 0.5, 1))))
	})
	r, _, b, _ := img.At(32, 32).RGBA()
	if b < r {
		t.Errorf("NoDepthTest quad behind the red one did not draw over it: r %d b %d", r>>8, b>>8)
	}
}
