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
		ok, err := g.begin(Black)
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
		if img, err = g.end(true); err != nil {
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
	ok, err := g.begin(Black)
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
	if _, err := g.end(false); err != nil {
		t.Fatal(err)
	}
}

// drawIntoTexture renders two frames of body into a fresh render texture
// with the given options and reads it back.
func drawIntoTexture(t *testing.T, g *Graphics, opts RenderTextureOptions, body func()) (*RenderTexture, *image.RGBA) {
	t.Helper()
	rt, err := g.NewRenderTextureOptions(64, 64, opts)
	if err != nil {
		t.Fatalf("NewRenderTextureOptions: %v", err)
	}
	t.Cleanup(rt.Destroy)
	for range 2 {
		ok, err := g.begin(Black)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if !ok {
			continue
		}
		g.DrawTo(rt, Black, body)
		if _, err := g.end(false); err != nil {
			t.Fatalf("end: %v", err)
		}
	}
	img, err := rt.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return rt, img
}

// TestRenderTextureFormats draws the same white rectangle into a surface
// of each colour format and reads each one back.
func TestRenderTextureFormats(t *testing.T) {
	g := newHeadless(t, 64, 64)
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1})
	white := func() { g.FillRect(16, 16, 32, 32, White) }
	cases := []struct {
		name string
		opts RenderTextureOptions
	}{
		{"screen", RenderTextureOptions{}},
		{"hdr", RenderTextureOptions{Format: ColorHDR}},
		{"mask", RenderTextureOptions{Format: ColorMask}},
		{"no depth", RenderTextureOptions{NoDepth: true}},
		{"mask without depth", RenderTextureOptions{Format: ColorMask, NoDepth: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, img := drawIntoTexture(t, g, c.opts, white)
			if v := img.RGBAAt(32, 32).R; v < 200 {
				t.Errorf("centre red %d: the rectangle should be white", v)
			}
			if v := img.RGBAAt(2, 2).R; v > 20 {
				t.Errorf("corner red %d: outside the rectangle should be black", v)
			}
		})
	}
}

// TestRenderTextureNoDepth checks that leaving out the surface's depth
// buffer still draws a 3D scene, which has a depth buffer of its own.
func TestRenderTextureNoDepth(t *testing.T) {
	g := newHeadless(t, 64, 64)
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1})
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	scene := func() {
		g.SetCamera(Camera{Position: lin.V3(0, 0, 2.5)})
		g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{3, 3, 3, 1}, Ambient: Color{0.2, 0.2, 0.2, 1}})
		g.DrawMesh(cube, Material{BaseColor: Color{1, 0, 0, 1}, Roughness: 1}, lin.Identity())
	}
	_, img := drawIntoTexture(t, g, RenderTextureOptions{NoDepth: true}, scene)
	if c := img.RGBAAt(32, 32); c.R < 150 || c.G > 80 {
		t.Errorf("centre %v: expected the red cube", c)
	}
}

// TestRenderTextureReadDepth reads back the depth a 3D scene left in a
// render texture: nearer where the cube stands than where it does not.
func TestRenderTextureReadDepth(t *testing.T) {
	g := newHeadless(t, 64, 64)
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1})
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	rt, _ := drawIntoTexture(t, g, RenderTextureOptions{}, func() {
		g.SetCamera(Camera{Position: lin.V3(0, 0, 2.5)})
		g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{3, 3, 3, 1}})
		g.DrawMesh(cube, Material{BaseColor: White, Roughness: 1}, lin.Identity())
	})
	depth, err := rt.ReadDepth()
	if err != nil {
		t.Fatalf("ReadDepth: %v", err)
	}
	if len(depth) != 64*64 {
		t.Fatalf("ReadDepth gave %d values, want %d", len(depth), 64*64)
	}
	centre, corner := depth[32*64+32], depth[2*64+2]
	if centre <= 0 || centre >= 1 {
		t.Errorf("centre depth %v: the cube should lie between the planes", centre)
	}
	if corner < 0.999 {
		t.Errorf("corner depth %v: nothing was drawn there, so it should be the far plane", corner)
	}
	if centre >= corner {
		t.Errorf("centre depth %v should be nearer than the corner %v", centre, corner)
	}
}
