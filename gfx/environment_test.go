package gfx

import (
	"image"
	"image/color"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestEnvironmentReflection lights a mirror-like metal sphere with an
// environment that is red above and blue below, with no direct light:
// the top of the sphere must reflect red and the bottom blue, and with
// Background set the corners of the frame show the sky.
func TestEnvironmentReflection(t *testing.T) {
	g := newHeadless(t, 96, 96)
	pano := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		c := color.RGBA{230, 30, 30, 255}
		if y >= 16 {
			c = color.RGBA{30, 30, 230, 255}
		}
		for x := range 64 {
			pano.SetRGBA(x, y, c)
		}
	}
	env, err := g.NewEnvironment(pano, EnvironmentOptions{Size: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer env.Destroy()
	sv, si := SphereMesh(24, 48)
	sphere, err := g.NewMesh(sv, si)
	if err != nil {
		t.Fatal(err)
	}
	defer sphere.Destroy()
	img := renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Environment: env, Background: true})
		g.DrawMesh(sphere, Material{BaseColor: White, Metallic: 1, Roughness: 0.05}, lin.Scale(lin.V3(1.2, 1.2, 1.2)))
	})
	top := img.RGBAAt(48, 30)
	bottom := img.RGBAAt(48, 66)
	if top.R < top.B+40 {
		t.Errorf("top of the metal sphere is %v, want a red reflection", top)
	}
	if bottom.B < bottom.R+40 {
		t.Errorf("bottom of the metal sphere is %v, want a blue reflection", bottom)
	}
	corner := img.RGBAAt(4, 4)
	if corner.R < 100 {
		t.Errorf("corner of the frame is %v, want the red sky as background", corner)
	}
	// A rough dielectric under the same environment is tinted by the
	// irradiance: reddish on top, bluish below, and lit at all.
	img = renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Environment: env})
		g.DrawMesh(sphere, Material{BaseColor: White, Roughness: 1}, lin.Scale(lin.V3(1.2, 1.2, 1.2)))
	})
	top, bottom = img.RGBAAt(48, 30), img.RGBAAt(48, 66)
	if top.R <= top.B || bottom.B <= bottom.R || top.R < 40 {
		t.Errorf("diffuse sphere top %v bottom %v; want the environment's tint from irradiance", top, bottom)
	}
}

func TestEnvironmentSH(t *testing.T) {
	// A uniform white panorama has irradiance 1 everywhere: only the
	// constant term survives and evaluates to 1 for any normal.
	src := newRadianceMap(32, 16)
	for y := range 16 {
		for x := range 32 {
			src.set(x, y, 1, 1, 1)
		}
	}
	sh := irradianceSH(src)
	for _, n := range []lin.Vec3{{X: 0, Y: 1, Z: 0}, {X: 1, Y: 0, Z: 0}, {X: 0, Y: 0, Z: -1}} {
		basis := shBasis(n)
		var e float64
		for i := range 9 {
			e += float64(sh[i].X) * basis[i]
		}
		if e < 0.97 || e > 1.03 {
			t.Errorf("irradiance for normal %v is %.3f, want 1", n, e)
		}
	}
}
