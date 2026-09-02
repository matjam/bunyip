package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestVertexColorsAndUVTransform(t *testing.T) {
	g := newHeadless(t, 64, 64)
	// A quad whose left vertices are red and right vertices blue, unlit
	// so the colours come through untouched.
	verts := []Vertex{
		{Pos: lin.V3(-1, -1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(0, 1), Color: RGB(255, 0, 0)},
		{Pos: lin.V3(1, -1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(1, 1), Color: RGB(0, 0, 255)},
		{Pos: lin.V3(1, 1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(1, 0), Color: RGB(0, 0, 255)},
		{Pos: lin.V3(-1, 1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(0, 0), Color: RGB(255, 0, 0)},
	}
	quad, err := g.NewMesh(verts, []uint32{0, 1, 2, 0, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	defer quad.Destroy()
	img := renderMaterial(t, g, func() {
		g.DrawMesh(quad, Material{Unlit: true}, lin.Identity())
	})
	left, right := img.RGBAAt(15, 32), img.RGBAAt(49, 32) // the quad covers x 14..50
	if left.R < 150 || left.R < left.B*2 || right.B < 150 || right.B < right.R*2 {
		t.Errorf("vertex colours not applied: left %v right %v", left, right)
	}
	// A texture that is white on the left half and black on the right;
	// a UV transform that shifts it by half a texture swaps the halves.
	tex := image.NewRGBA(image.Rect(0, 0, 2, 1))
	tex.SetRGBA(0, 0, color.RGBA{255, 255, 255, 255})
	tex.SetRGBA(1, 0, color.RGBA{0, 0, 0, 255})
	tx, err := g.NewTexture(tex, TextureOptions{Repeat: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Destroy()
	plain := facingQuad(t, g)
	img = renderMaterial(t, g, func() {
		g.DrawMesh(plain, Material{Texture: tx, Unlit: true}, lin.Translate(lin.V3(-1, 0, 0)).Mul(lin.Scale(lin.V3(0.9, 0.9, 1))))
		g.DrawMesh(plain, Material{Texture: tx, Unlit: true, UVTransform: lin.Translate2(0.5, 0)}, lin.Translate(lin.V3(1, 0, 0)).Mul(lin.Scale(lin.V3(0.9, 0.9, 1))))
	})
	if !bright(img, 10, 32) || bright(img, 26, 32) {
		t.Errorf("untransformed quad should be white on its left and black on its right")
	}
	if bright(img, 38, 32) || !bright(img, 54, 32) {
		t.Errorf("shifted quad should be black on its left and white on its right")
	}
}

func TestClearcoatSheenSubsurface(t *testing.T) {
	g := newHeadless(t, 64, 64)
	sv, si := SphereMesh(16, 32)
	sphere, err := g.NewMesh(sv, si)
	if err != nil {
		t.Fatal(err)
	}
	defer sphere.Destroy()
	// Lit head-on: a clearcoat adds a highlight in the middle, sheen adds
	// light at the rim, subsurface lets a back light through. Each must
	// change the picture in the expected place versus the plain material.
	render := func(m Material, dir lin.Vec3) *image.RGBA {
		return renderMaterial(t, g, func() {
			g.SetLight(Light{Direction: dir, Color: Color{2, 2, 2, 1}, Ambient: Color{0.05, 0.05, 0.05, 1}})
			g.DrawMesh(sphere, m, lin.Scale(lin.V3(1.3, 1.3, 1.3)))
		})
	}
	base := Material{BaseColor: RGB(60, 60, 60), Roughness: 0.9}
	sum := func(img *image.RGBA, x, y int) uint32 {
		r, gg, b, _ := img.At(x, y).RGBA()
		return (r + gg + b) >> 8
	}
	plain := render(base, lin.V3(0, 0, -1))
	coat := base
	coat.Clearcoat = 1
	if c := render(coat, lin.V3(0, 0, -1)); sum(c, 32, 32) <= sum(plain, 32, 32) {
		t.Errorf("clearcoat highlight missing: %d vs %d", sum(c, 32, 32), sum(plain, 32, 32))
	}
	velvet := base
	velvet.Sheen = RGB(255, 255, 255)
	if v := render(velvet, lin.V3(0, 0, -1)); sum(v, 8, 32) <= sum(plain, 8, 32) {
		t.Errorf("sheen rim light missing: %d vs %d", sum(v, 8, 32), sum(plain, 8, 32))
	}
	leaf := Material{BaseColor: RGB(60, 200, 60), Roughness: 0.9, Subsurface: 1}
	backlit := render(leaf, lin.V3(0, 0, 1)) // light from behind the sphere
	plainBack := render(Material{BaseColor: RGB(60, 200, 60), Roughness: 0.9}, lin.V3(0, 0, 1))
	if sum(backlit, 32, 32) <= sum(plainBack, 32, 32) {
		t.Errorf("subsurface should let the back light through: %d vs %d", sum(backlit, 32, 32), sum(plainBack, 32, 32))
	}
}
