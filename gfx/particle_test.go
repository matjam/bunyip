package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// quad makes one ParticleQuad covering a square of side units centred on
// (x, y), showing the whole of its texture.
func quad(x, y, side float32, c Color) ParticleQuad {
	return ParticleQuad{Pos: lin.V3(x, y, 0), Size: lin.V2(side, side), UV1: lin.V2(1, 1), Color: c}
}

// TestDrawParticles draws a batch of instanced quads and checks that each
// one landed where its position and size say it should, with its own
// tint, and that the space between them was left alone.
func TestDrawParticles(t *testing.T) {
	g := newHeadless(t, 128, 128)
	quads := []ParticleQuad{
		quad(32, 32, 24, RGB(255, 0, 0)),
		quad(96, 32, 24, RGB(0, 255, 0)),
		quad(32, 96, 24, RGB(0, 0, 255)),
	}
	var img *image.RGBA
	ok, err := g.begin(Black)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !ok {
		t.Skip("no frame")
	}
	g.DrawParticles(nil, quads)
	if img, err = g.end(true); err != nil {
		t.Fatalf("end: %v", err)
	}
	cases := []struct {
		name string
		x, y int
		want color.RGBA
	}{
		{"red", 32, 32, color.RGBA{255, 0, 0, 255}},
		{"green", 96, 32, color.RGBA{0, 255, 0, 255}},
		{"blue", 32, 96, color.RGBA{0, 0, 255, 255}},
		{"gap", 64, 64, color.RGBA{0, 0, 0, 255}},
		{"empty corner", 96, 96, color.RGBA{0, 0, 0, 255}},
		{"outside the quad", 32, 12, color.RGBA{0, 0, 0, 255}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := img.RGBAAt(c.x, c.y); !closeColor(got, c.want) {
				t.Errorf("pixel (%d,%d) = %v, want %v", c.x, c.y, got, c.want)
			}
		})
	}
}

// TestDrawParticlesAlpha checks that a translucent tint blends over what
// is already there, which is what says the fragment stage premultiplies
// the instance colour rather than passing it through.
func TestDrawParticlesAlpha(t *testing.T) {
	g := newHeadless(t, 64, 64)
	ok, err := g.begin(Black)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !ok {
		t.Skip("no frame")
	}
	g.FillRect(0, 0, 64, 64, RGB(0, 0, 255))
	g.DrawParticles(nil, []ParticleQuad{quad(32, 32, 32, Color{R: 1, A: 0.5})})
	img, err := g.end(true)
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	got := img.RGBAAt(32, 32)
	// Half red over blue, blended in linear light and written to an sRGB
	// image: both channels come out near the sRGB value of 0.5.
	if got.R < 150 || got.B < 150 || got.G > 40 {
		t.Errorf("blended pixel = %v, want red and blue both near half", got)
	}
}

// TestDrawParticlesLayers checks that instanced particles interleave with
// the sprite stream by layer instead of always drawing over it: the
// sprite on the higher layer must cover the particle under it.
func TestDrawParticlesLayers(t *testing.T) {
	g := newHeadless(t, 64, 64)
	ok, err := g.begin(Black)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !ok {
		t.Skip("no frame")
	}
	// A particle on layer 1 under a sprite on layer 2, and another on
	// layer 3 over a sprite on layer 2.
	g.SetLayer(1)
	g.DrawParticles(nil, []ParticleQuad{quad(16, 32, 24, RGB(255, 0, 0))})
	g.SetLayer(2)
	g.FillRect(0, 0, 64, 64, RGB(0, 255, 0))
	g.SetLayer(3)
	g.DrawParticles(nil, []ParticleQuad{quad(48, 32, 24, RGB(0, 0, 255))})
	g.SetLayer(0)
	img, err := g.end(true)
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if got := img.RGBAAt(16, 32); !closeColor(got, color.RGBA{0, 255, 0, 255}) {
		t.Errorf("particle under the sprite showed through: %v", got)
	}
	if got := img.RGBAAt(48, 32); !closeColor(got, color.RGBA{0, 0, 255, 255}) {
		t.Errorf("particle over the sprite was covered: %v", got)
	}
}

// TestDrawParticles3D draws billboards in the scene and checks that one
// in front of the camera is visible while one behind a wall is hidden,
// which is the depth test the fragment program does by hand.
func TestDrawParticles3D(t *testing.T) {
	g := newHeadless(t, 128, 128)
	verts, idx := QuadMesh()
	mesh, err := g.NewMesh(verts, idx)
	if err != nil {
		t.Fatalf("NewMesh: %v", err)
	}
	defer mesh.Destroy()
	ok, err := g.begin(Black)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !ok {
		t.Skip("no frame")
	}
	g.SetCamera(Camera{Position: lin.V3(0, 0, 6), Target: lin.Vec3{}})
	g.SetLight(Light{Direction: lin.V3(0, -1, -1), Color: White})
	// A wall of two units across the left half of the view, at z = 0.
	g.DrawMesh(mesh, Material{BaseColor: RGB(20, 20, 20), Unlit: true, DoubleSided: true},
		lin.TRS(lin.V3(-1.2, 0, 0), lin.Quat{W: 1}, lin.V3(2, 4, 1)))
	g.DrawParticles3D(nil, []ParticleQuad{
		{Pos: lin.V3(-1.2, 0, -1), Size: lin.V2(1, 1), UV1: lin.V2(1, 1), Color: RGB(255, 0, 0)}, // behind the wall
		{Pos: lin.V3(1.2, 0, 0), Size: lin.V2(1, 1), UV1: lin.V2(1, 1), Color: RGB(0, 255, 0)},   // clear of it
	}, Particles3D{})
	img, err := g.end(true)
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	// The camera looks down -z, so world +x is to the right of the view.
	// At six units and a sixty degree field of view the view is 3.46
	// units each side of the centre, so x = 1.2 is 22 pixels right of it.
	if got := img.RGBAAt(86, 64); got.G < 120 || got.R > 80 {
		t.Errorf("particle clear of the wall = %v, want green", got)
	}
	if got := img.RGBAAt(42, 64); got.R > 80 {
		t.Errorf("particle behind the wall showed through: %v", got)
	}
}

// TestDrawParticles3DSoft checks that the soft fade thins a particle as
// it approaches the surface behind it: the same quad drawn against a
// wall just behind it is dimmer with Soft set than without.
func TestDrawParticles3DSoft(t *testing.T) {
	g := newHeadless(t, 64, 64)
	verts, idx := QuadMesh()
	mesh, err := g.NewMesh(verts, idx)
	if err != nil {
		t.Fatalf("NewMesh: %v", err)
	}
	defer mesh.Destroy()
	render := func(soft float32) color.RGBA {
		ok, err := g.begin(Black)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if !ok {
			t.Skip("no frame")
		}
		g.SetCamera(Camera{Position: lin.V3(0, 0, 6), Target: lin.Vec3{}})
		g.DrawMesh(mesh, Material{BaseColor: RGB(10, 10, 10), Unlit: true, DoubleSided: true},
			lin.TRS(lin.Vec3{}, lin.Quat{W: 1}, lin.V3(6, 6, 1)))
		g.DrawParticles3D(nil, []ParticleQuad{
			{Pos: lin.V3(0, 0, 0.25), Size: lin.V2(2, 2), UV1: lin.V2(1, 1), Color: RGB(255, 255, 255)},
		}, Particles3D{Soft: soft})
		img, err := g.end(true)
		if err != nil {
			t.Fatalf("end: %v", err)
		}
		return img.RGBAAt(32, 32)
	}
	hard := render(0)
	faded := render(2)
	if hard.R < 200 {
		t.Fatalf("hard-edged particle = %v, want near white", hard)
	}
	if faded.R >= hard.R {
		t.Errorf("soft particle = %v, hard = %v; the fade did not dim it", faded, hard)
	}
}
