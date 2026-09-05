package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// tiltedQuad is a flat unlit quad, drawn turned so that its edges cross
// the pixel grid diagonally: the case multisampling exists for.
func tiltedQuad(t *testing.T, g *Graphics) *Mesh {
	t.Helper()
	verts := []Vertex{
		{Pos: lin.V3(-1, -1, 0), Normal: lin.V3(0, 0, 1)},
		{Pos: lin.V3(1, -1, 0), Normal: lin.V3(0, 0, 1)},
		{Pos: lin.V3(1, 1, 0), Normal: lin.V3(0, 0, 1)},
		{Pos: lin.V3(-1, 1, 0), Normal: lin.V3(0, 0, 1)},
	}
	mesh, err := g.NewMesh(verts, []uint32{0, 1, 2, 0, 2, 3})
	if err != nil {
		t.Fatalf("NewMesh: %v", err)
	}
	t.Cleanup(mesh.Destroy)
	return mesh
}

// edgePixels counts the pixels whose red channel lies strictly between
// the background and the quad's own colour: the partly covered ones only
// a multisampled edge produces.
func edgePixels(img *image.RGBA) (edges int, solid uint8) {
	for i := 0; i+3 < len(img.Pix); i += 4 {
		solid = max(solid, img.Pix[i])
	}
	if solid < 32 {
		return 0, solid
	}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		if v := img.Pix[i]; v > 16 && v < solid-16 {
			edges++
		}
	}
	return edges, solid
}

// renderTilted draws the tilted quad at a sample count and returns the
// frame.
func renderTilted(t *testing.T, g *Graphics, mesh *Mesh, samples int) *image.RGBA {
	t.Helper()
	post := DefaultPost()
	post.Bloom, post.AmbientOcclusion = 0, 0
	post.NoAntiAlias = true // isolate multisampling from FXAA
	post.Samples = samples
	g.SetPost(post)
	var img *image.RGBA
	for range 2 {
		ok, err := g.begin(Black)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if !ok {
			continue
		}
		g.SetCamera(Camera{Position: lin.V3(0, 0, 3)})
		g.DrawMesh(mesh, Material{BaseColor: White, Unlit: true, DoubleSided: true},
			lin.Rotate(lin.Radians(30), lin.V3(0, 0, 1)))
		if img, err = g.end(true); err != nil {
			t.Fatalf("end: %v", err)
		}
	}
	return img
}

// TestMultisampling renders one tilted quad with and without
// multisampling. The single-sample frame has hard edges: every pixel is
// either background or quad. Four samples give the edge pixels
// intermediate values, and nothing else about the picture changes.
func TestMultisampling(t *testing.T) {
	g := newHeadless(t, 128, 128)
	if g.MaxSamples() < 4 {
		t.Skipf("device supports at most %d samples", g.MaxSamples())
	}
	mesh := tiltedQuad(t, g)

	plain := renderTilted(t, g, mesh, 1)
	flat, solid := edgePixels(plain)
	if solid < 32 {
		t.Fatalf("the quad did not draw: brightest pixel %d", solid)
	}
	multi := renderTilted(t, g, mesh, 4)
	smooth, solid4 := edgePixels(multi)
	if solid4 < 32 {
		t.Fatalf("the multisampled quad did not draw: brightest pixel %d", solid4)
	}
	if smooth <= flat*4 || smooth < 50 {
		t.Errorf("4 samples gave %d partly covered pixels, 1 sample gave %d; expected far more", smooth, flat)
	}
	// The interior is unchanged: multisampling smooths edges, it does not
	// dim or brighten what it covers.
	if d := int(solid4) - int(solid); d < -2 || d > 2 {
		t.Errorf("solid colour moved from %d to %d with multisampling", solid, solid4)
	}
	// The middle of the quad is still solid in both.
	if v := multi.RGBAAt(64, 64).R; v < solid4-2 {
		t.Errorf("centre pixel %d is not the quad's solid colour %d", v, solid4)
	}
}

// TestMultisampledRenderTexture multisamples a render texture's own
// surface, so 2D drawing into it is anti-aliased as well.
func TestMultisampledRenderTexture(t *testing.T) {
	g := newHeadless(t, 64, 64)
	if g.MaxSamples() < 4 {
		t.Skipf("device supports at most %d samples", g.MaxSamples())
	}
	draw := func(samples int) *image.RGBA {
		rt, err := g.NewRenderTextureOptions(64, 64, RenderTextureOptions{Samples: samples})
		if err != nil {
			t.Fatalf("NewRenderTextureOptions: %v", err)
		}
		defer rt.Destroy()
		for range 2 {
			ok, err := g.begin(Black)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			if !ok {
				continue
			}
			g.DrawTo(rt, Black, func() {
				// A triangle with two diagonal edges, drawn as 2D geometry.
				g.DrawTriangles(nil, []Vertex2D{
					{Pos: lin.V2(32, 4), Color: White},
					{Pos: lin.V2(60, 60), Color: White},
					{Pos: lin.V2(4, 60), Color: White},
				})
			})
			if _, err := g.end(false); err != nil {
				t.Fatalf("end: %v", err)
			}
		}
		img, err := rt.Read()
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		return img
	}
	flat, _ := edgePixels(draw(1))
	smooth, solid := edgePixels(draw(4))
	if solid < 32 {
		t.Fatalf("the triangle did not draw: brightest pixel %d", solid)
	}
	if smooth <= flat*4 || smooth < 30 {
		t.Errorf("4 samples gave %d partly covered pixels, 1 sample gave %d; expected far more", smooth, flat)
	}
}
