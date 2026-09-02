package gfx

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestSkyHarmonics(t *testing.T) {
	// A uniform sky integrates to an irradiance of 1 in every direction.
	white := Color{1, 1, 1, 1}
	sh := Sky{Zenith: white, Horizon: white, Ground: white}.sh()
	for _, n := range []lin.Vec3{lin.V3(0, 1, 0), lin.V3(1, 0, 0), lin.V3(0, -1, 0), lin.V3(0.6, 0, 0.8)} {
		b := shBasis(n)
		var e float64
		for i := range 9 {
			e += float64(sh[i].X) * b[i]
		}
		if math.Abs(e-1) > 0.02 {
			t.Errorf("uniform sky irradiance at %v = %v, want 1", n, e)
		}
	}
	// Blue above and red below: the up normal sees blue, the down normal red,
	// and turning the axis over swaps them.
	blue, red := Color{0, 0, 1, 1}, Color{1, 0, 0, 1}
	for _, up := range []lin.Vec3{lin.V3(0, 1, 0), lin.V3(0, -1, 0)} {
		sh := Sky{Zenith: blue, Horizon: blue, Ground: red, Up: up}.sh()
		b := shBasis(up)
		var r, bl float64
		for i := range 9 {
			r += float64(sh[i].X) * b[i]
			bl += float64(sh[i].Z) * b[i]
		}
		if bl < 0.6 || r > 0.4 {
			t.Errorf("normal along up %v sees red %v blue %v, want mostly blue", up, r, bl)
		}
	}
}

func TestSkyBackground(t *testing.T) {
	g := newHeadless(t, 64, 64)
	// Looking along -z from +z, the top of the frame sees above the
	// horizon and the bottom sees below it.
	shot := func(sky Sky) (top, bottom lin.Vec3) {
		img := renderMaterial(t, g, func() {
			g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{1, 1, 1, 1}, Sky: sky, Background: true})
		})
		a, b := img.RGBAAt(32, 3), img.RGBAAt(32, 60)
		return lin.V3(float32(a.R), float32(a.G), float32(a.B)), lin.V3(float32(b.R), float32(b.G), float32(b.B))
	}
	top, bottom := shot(Sky{Zenith: Color{0, 0, 1, 1}, Horizon: Color{0, 0, 1, 1}, Ground: Color{1, 0, 0, 1}})
	if top.Z < 150 || top.X > 60 {
		t.Errorf("sky above the horizon is %v, want blue", top)
	}
	// The haze band under the horizon mixes some sky in, so red dominates
	// rather than being pure.
	if bottom.X < 150 || bottom.Z > bottom.X-30 {
		t.Errorf("ground below the horizon is %v, want mostly red", bottom)
	}
	// In a vacuum the sky is black but the ground below still shows.
	top, bottom = shot(Sky{Zenith: Color{0, 0, 1, 1}, Ground: Color{1, 0, 0, 1}, Vacuum: 1})
	if top.Len() > 30 {
		t.Errorf("sky in a vacuum is %v, want black", top)
	}
	if bottom.X < 150 {
		t.Errorf("ground in a vacuum is %v, want red", bottom)
	}
	// Turning the axis over puts the ground on top.
	top, _ = shot(Sky{Zenith: Color{0, 0, 1, 1}, Ground: Color{1, 0, 0, 1}, Up: lin.V3(0, -1, 0)})
	if top.X < 150 || top.Z > top.X-30 {
		t.Errorf("with up pointing down the top of the frame is %v, want the red ground", top)
	}
}

func TestSkyLightsMeshes(t *testing.T) {
	g := newHeadless(t, 64, 64)
	sv, si := SphereMesh(24, 48)
	sphere, err := g.NewMesh(sv, si)
	if err != nil {
		t.Fatal(err)
	}
	defer sphere.Destroy()
	// No direct light: a white rough sphere under a blue sky over red
	// ground is blue on top and red underneath.
	img := renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Sky: Sky{Zenith: Color{0, 0, 1, 1}, Ground: Color{1, 0, 0, 1}}})
		g.DrawMesh(sphere, Material{Roughness: 1}, lin.Identity())
	})
	top, bottom := img.RGBAAt(32, 14), img.RGBAAt(32, 50)
	if top.B < top.R+30 {
		t.Errorf("top of the sphere is %v, want bluer than red", top)
	}
	if bottom.R < bottom.B+30 {
		t.Errorf("underside of the sphere is %v, want redder than blue", bottom)
	}
}
