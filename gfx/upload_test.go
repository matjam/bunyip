package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestFrameUploadsDoNotWait updates a mesh and writes a texture inside
// every frame for sixty frames and checks that none of it stalled the
// GPU: the upload arena and the retire ring must carry all of it. The
// pixel read back each frame checks the data drawn is the frame's own,
// not the one before it.
func TestFrameUploadsDoNotWait(t *testing.T) {
	g := newHeadless(t, 64, 64)
	quad := facingQuad(t, g)
	tex, err := g.NewBlankTexture(2, 2, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()

	// The quad is drawn unlit through both the mesh's vertex colours and
	// the texture, and the two alternate between white and nearly black.
	// Frame to frame the pixel read back swings, so an upload the GPU saw
	// a frame late shows up inverted.
	indices := []uint32{0, 1, 2, 0, 2, 3}
	frame := func(lit bool) *image.RGBA {
		t.Helper()
		c, v := Color{0.02, 0.02, 0.02, 1}, uint8(20)
		if lit {
			c, v = White, 255
		}
		verts := []Vertex{
			{Pos: lin.V3(-1, -1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(0, 1), Color: c},
			{Pos: lin.V3(1, -1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(1, 1), Color: c},
			{Pos: lin.V3(1, 1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(1, 0), Color: c},
			{Pos: lin.V3(-1, 1, 0), Normal: lin.V3(0, 0, 1), UV: lin.V2(0, 0), Color: c},
		}
		src := image.NewRGBA(image.Rect(0, 0, 2, 2))
		for y := range 2 {
			for x := range 2 {
				src.SetRGBA(x, y, color.RGBA{v, v, v, 255})
			}
		}
		return renderMaterial(t, g, func() {
			if err := quad.Update(verts, indices); err != nil {
				t.Fatal(err)
			}
			if err := tex.Write(0, 0, src); err != nil {
				t.Fatal(err)
			}
			g.DrawMesh(quad, Material{Texture: tex, Unlit: true}, lin.Identity())
		})
	}

	// Warm up: the first frames allocate the arena, the instance stream
	// and the material pipelines, which may wait.
	for range 4 {
		frame(true)
	}
	before := g.r.Device.Waits()
	for i := range 60 {
		lit := i%2 == 0
		img := frame(lit)
		if s := g.Stats(); s.Waits != 0 {
			t.Fatalf("frame %d waited for the GPU %d times", i, s.Waits)
		}
		if bright(img, 32, 32) != lit {
			t.Fatalf("frame %d drew the previous frame's upload (wanted lit=%v)", i, lit)
		}
	}
	if after := g.r.Device.Waits(); after != before {
		t.Errorf("sixty frames of uploads waited for the GPU %d times", after-before)
	}
}

// TestDestroyInFrameDoesNotWait destroys a texture and a mesh while a
// frame is open and checks nothing stalled; the retire ring frees them
// when the slot comes round.
func TestDestroyInFrameDoesNotWait(t *testing.T) {
	g := newHeadless(t, 32, 32)
	// Warm up so pipeline and target creation is not counted.
	renderMaterial(t, g, func() {})
	before := g.r.Device.Waits()
	for range 4 {
		renderMaterial(t, g, func() {
			src := image.NewRGBA(image.Rect(0, 0, 4, 4))
			tex, err := g.NewTexture(src, TextureOptions{})
			if err != nil {
				t.Fatal(err)
			}
			mesh := facingQuad(t, g)
			g.DrawMesh(mesh, Material{Texture: tex}, lin.Identity())
			tex.Destroy()
			mesh.Destroy()
		})
		if s := g.Stats(); s.Waits != 0 {
			t.Fatalf("a frame that created and destroyed resources waited %d times", s.Waits)
		}
	}
	if after := g.r.Device.Waits(); after != before {
		t.Errorf("creating and destroying inside a frame waited %d times", after-before)
	}
}
