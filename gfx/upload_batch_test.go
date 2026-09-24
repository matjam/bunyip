package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestUploadsBeforeFirstFrame creates 200 meshes and 100 textures before
// any frame, the way a game's Init does, and checks that doing so waits
// for the GPU not once and that the first frames draw every one of them.
// Each mesh is a quad in its own cell of a grid, and each texture a solid
// colour of its own, so a resource whose upload went missing leaves its
// cell black or the wrong colour.
func TestUploadsBeforeFirstFrame(t *testing.T) {
	const w, h, cell = 160, 80, 8
	g := newHeadless(t, w, h)
	const meshes, textures = 200, 100
	before := g.r.Device.Waits()
	ms := make([]*Mesh, meshes)
	for i := range ms {
		// One world unit is one pixel under the orthographic camera below,
		// centred on the target.
		x0 := float32(-w/2 + (i%(w/cell))*cell + 1)
		y0 := float32(h/2 - (i/(w/cell))*cell - 1)
		x1, y1 := x0+cell-2, y0-(cell-2)
		white := Color{1, 1, 1, 1}
		n := lin.V3(0, 0, 1)
		v := []Vertex{
			{Pos: lin.V3(x0, y0, 0), Normal: n, Color: white},
			{Pos: lin.V3(x0, y1, 0), Normal: n, Color: white},
			{Pos: lin.V3(x1, y1, 0), Normal: n, Color: white},
			{Pos: lin.V3(x1, y0, 0), Normal: n, Color: white},
		}
		m, err := g.NewMesh(v, []uint32{0, 1, 2, 0, 2, 3})
		if err != nil {
			t.Fatal(err)
		}
		ms[i] = m
	}
	texColor := func(i int) color.RGBA { return color.RGBA{uint8(40 + i*2), uint8(250 - i*2), uint8(i * 37 % 256), 255} }
	ts := make([]*Texture, textures)
	for i := range ts {
		img := image.NewRGBA(image.Rect(0, 0, cell, cell))
		for p := 0; p < len(img.Pix); p += 4 {
			c := texColor(i)
			img.Pix[p], img.Pix[p+1], img.Pix[p+2], img.Pix[p+3] = c.R, c.G, c.B, c.A
		}
		tex, err := g.NewTexture(img, TextureOptions{NoMipmaps: true})
		if err != nil {
			t.Fatal(err)
		}
		ts[i] = tex
	}
	if waits := g.r.Device.Waits() - before; waits != 0 {
		t.Errorf("creating %d meshes and %d textures waited for the GPU %d times, want 0", meshes, textures, waits)
	}
	// A texture destroyed before it was ever drawn frees its image only
	// once the batch that uploads it has run, which the validation layers
	// would report otherwise.
	for range 5 {
		tex, err := g.NewTexture(image.NewRGBA(image.Rect(0, 0, 4, 4)), TextureOptions{})
		if err != nil {
			t.Fatal(err)
		}
		tex.Destroy()
	}

	got := renderMaterial(t, g, func() {
		g.SetCamera(Camera{Position: lin.V3(0, 0, 5), Target: lin.V3(0, 0, 0), Ortho: h / 2})
		for _, m := range ms {
			g.DrawMesh(m, Material{BaseColor: White, Unlit: true}, lin.Identity())
		}
	})
	for i := range ms {
		x, y := (i%(w/cell))*cell+cell/2, (i/(w/cell))*cell+cell/2
		if c := got.RGBAAt(x, y); c.R < 200 || c.G < 200 || c.B < 200 {
			t.Errorf("mesh %d: pixel (%d, %d) is %v, want white", i, x, y, c)
		}
	}
	got = renderMaterial(t, g, func() {
		for i, tex := range ts {
			g.DrawTexture(tex, float32((i%(w/cell))*cell), float32((i/(w/cell))*cell))
		}
	})
	for i := range ts {
		x, y := (i%(w/cell))*cell+cell/2, (i/(w/cell))*cell+cell/2
		want, c := texColor(i), got.RGBAAt(x, y)
		if absDiff(c.R, want.R) > 2 || absDiff(c.G, want.G) > 2 || absDiff(c.B, want.B) > 2 {
			t.Errorf("texture %d: pixel (%d, %d) is %v, want %v", i, x, y, c, want)
		}
	}
}
