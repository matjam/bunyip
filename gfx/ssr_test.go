package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// renderPost is renderMaterial with the game's own post settings, for the
// passes a test has to turn on.
func renderPost(t *testing.T, g *Graphics, post PostSettings, draw func()) *image.RGBA {
	t.Helper()
	g.SetPost(post)
	if ok, err := g.begin(Black); err != nil || !ok {
		t.Fatal(err)
	}
	draw()
	img, err := g.end(true)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestScreenSpaceReflections puts a bright green box over a mirror floor
// under a black sky, where the only green the floor can show is the box's
// reflection, and checks the reflection pass puts it there.
func TestScreenSpaceReflections(t *testing.T) {
	g := newHeadless(t, 128, 128)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	pv, pi := PlaneMesh(1)
	plane, err := g.NewMesh(pv, pi)
	if err != nil {
		t.Fatal(err)
	}
	defer plane.Destroy()
	scene := func() {
		g.SetCamera(Camera{Position: lin.V3(0, 1.6, 5), Target: lin.V3(0, 0.6, 0)})
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Ambient: Color{}, Sky: Sky{Vacuum: 1}})
		g.DrawMesh(plane, Material{BaseColor: White, Metallic: 1, Roughness: 0.05}, lin.Scale(lin.V3(20, 1, 20)))
		g.DrawMesh(cube, Material{BaseColor: Color{0, 1, 0, 1}, Unlit: true},
			lin.Translate(lin.V3(0, 1.2, 0)).Mul(lin.Scale(lin.V3(0.6, 0.6, 0.6))))
	}
	base := PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true}
	off := renderPost(t, g, base, scene)
	on := base
	on.Reflections = 1
	lit := renderPost(t, g, on, scene)
	// The floor below the box is where its reflection lands. Look down the
	// middle of the frame under the box for the biggest gain in green.
	best, bestY := 0, 0
	for y := 64; y < 128; y++ {
		gain := int(lit.RGBAAt(64, y).G) - int(off.RGBAAt(64, y).G)
		if gain > best {
			best, bestY = gain, y
		}
	}
	if best < 20 {
		t.Errorf("the mirror floor gained at most %d green from the reflection pass, want the box reflected in it", best)
	}
	t.Logf("brightest reflection gain %d at row %d", best, bestY)
	// A rough floor reflects nothing in screen space, so the same scene
	// with a rough material must not change.
	rough := func() {
		g.SetCamera(Camera{Position: lin.V3(0, 1.6, 5), Target: lin.V3(0, 0.6, 0)})
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Ambient: Color{}, Sky: Sky{Vacuum: 1}})
		g.DrawMesh(plane, Material{BaseColor: White, Metallic: 1, Roughness: 1}, lin.Scale(lin.V3(20, 1, 20)))
		g.DrawMesh(cube, Material{BaseColor: Color{0, 1, 0, 1}, Unlit: true},
			lin.Translate(lin.V3(0, 1.2, 0)).Mul(lin.Scale(lin.V3(0.6, 0.6, 0.6))))
	}
	roughOff := renderPost(t, g, base, rough)
	roughOn := renderPost(t, g, on, rough)
	for y := 64; y < 128; y += 8 {
		if gain := int(roughOn.RGBAAt(64, y).G) - int(roughOff.RGBAAt(64, y).G); gain > 4 {
			t.Errorf("a rough floor gained %d green at row %d; want no screen-space reflection on it", gain, y)
		}
	}
}

// TestScreenSpaceReflectionPaths renders the mirror floor and the green
// box through each way the reflections reach the scene: applied after an
// uninterrupted scene pass, blended into the pass that resumes it for a
// translucent draw, and both again multisampled, with decals and
// particles drawn over the result. Every path must put the box's
// reflection under it, and the validation layers must stay quiet.
func TestScreenSpaceReflectionPaths(t *testing.T) {
	g := newHeadless(t, 128, 128)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	pv, pi := PlaneMesh(1)
	plane, err := g.NewMesh(pv, pi)
	if err != nil {
		t.Fatal(err)
	}
	defer plane.Destroy()
	tex, err := g.NewTexture(image.NewRGBA(image.Rect(0, 0, 4, 4)), TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	scene := func(glass bool) func() {
		return func() {
			g.SetCamera(Camera{Position: lin.V3(0, 1.6, 5), Target: lin.V3(0, 0.6, 0)})
			g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Ambient: Color{}, Sky: Sky{Vacuum: 1}})
			g.DrawMesh(plane, Material{BaseColor: White, Metallic: 1, Roughness: 0.05}, lin.Scale(lin.V3(20, 1, 20)))
			g.DrawMesh(cube, Material{BaseColor: Color{0, 1, 0, 1}, Unlit: true},
				lin.Translate(lin.V3(0, 1.2, 0)).Mul(lin.Scale(lin.V3(0.6, 0.6, 0.6))))
			if glass {
				// A faint blended box off to one side, away from the
				// reflection, so the pass has translucent draws after it.
				g.DrawMesh(cube, Material{BaseColor: Color{1, 0, 0, 0.2}, Blend: true, Unlit: true},
					lin.Translate(lin.V3(-2.5, 0.5, 0)).Mul(lin.Scale(lin.V3(0.3, 0.3, 0.3))))
			}
			// A transparent decal and a transparent particle exercise the
			// pass over the finished scene without changing it.
			g.DrawDecal(tex, lin.Translate(lin.V3(2.5, 0, 0)), White)
			g.DrawParticles3D(nil, []ParticleQuad{{Pos: lin.V3(2.5, 0.5, 0), Size: lin.V2(0.1, 0.1)}}, Particles3D{})
		}
	}
	for _, c := range []struct {
		name    string
		glass   bool
		samples int
	}{
		{"applied", false, 1},
		{"blended", true, 1},
		{"applied multisampled", false, 4},
		{"blended multisampled", true, 4},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.samples > g.MaxSamples() {
				t.Skipf("the device takes at most %d samples", g.MaxSamples())
			}
			base := PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true, Samples: c.samples}
			on := base
			on.Reflections = 1
			renderPost(t, g, base, scene(c.glass)) // the sample count takes effect on the next frame
			off := renderPost(t, g, base, scene(c.glass))
			renderPost(t, g, on, scene(c.glass))
			lit := renderPost(t, g, on, scene(c.glass))
			best := 0
			for y := 64; y < 128; y++ {
				best = max(best, int(lit.RGBAAt(64, y).G)-int(off.RGBAAt(64, y).G))
			}
			if best < 20 {
				t.Errorf("the mirror floor gained at most %d green, want the box reflected in it", best)
			}
		})
	}
}
