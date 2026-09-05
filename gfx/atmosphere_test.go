package gfx

import (
	"image"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// skyShot renders the sky background alone through a camera.
func skyShot(t *testing.T, g *Graphics, cam Camera, l Light) *image.RGBA {
	t.Helper()
	l.Background = true
	return renderMaterial(t, g, func() {
		g.SetCamera(cam)
		g.SetLight(l)
	})
}

// skyDir is the direction the background shader looks along for a pixel,
// the same unprojection skyparam.frag does.
func skyDir(cam Camera, w, h, x, y int) lin.Vec3 {
	inv := cam.ViewProj(float32(w) / float32(h)).Inverse()
	nx := 2*(float32(x)+0.5)/float32(w) - 1
	ny := 2*(float32(y)+0.5)/float32(h) - 1
	near := inv.MulVec4(lin.V4(nx, ny, 0, 1))
	far := inv.MulVec4(lin.V4(nx, ny, 1, 1))
	return far.Vec3().Mul(1 / far.W).Sub(near.Vec3().Mul(1 / near.W)).Norm()
}

// unpost turns a composited pixel back into the radiance the scene pass
// wrote: sRGB decoding, then the inverse of the ACES curve in post.frag,
// with an exposure of 1. It is well conditioned up to about 0.6 and
// loses precision as the curve flattens above that.
func unpost(v uint8) float64 {
	s := float64(v) / 255
	y := s / 12.92
	if s > 0.04045 {
		y = math.Pow((s+0.055)/1.055, 2.4)
	}
	// y = x(ax+b) / (x(cx+d)+e), so (a-yc)x² + (b-yd)x - ye = 0.
	const a, b, c, d, e = 2.51, 0.03, 2.43, 0.59, 0.14
	qa, qb, qc := a-y*c, b-y*d, -y*e
	return (-qb + math.Sqrt(qb*qb-4*qa*qc)) / (2 * qa)
}

// TestAtmosphereBlocksMatch checks that the scattering model is the same
// text in the mesh prelude and the background shader. The two are
// compiled separately, so nothing but this catches a change to one.
func TestAtmosphereBlocksMatch(t *testing.T) {
	block := func(path string) string {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s := string(src)
		i, j := strings.Index(s, "// ATMOSPHERE."), strings.Index(s, "// END ATMOSPHERE.")
		if i < 0 || j <= i {
			t.Fatalf("%s has no atmosphere block", path)
		}
		return s[i:j]
	}
	a := block("shaders/prelude_mesh.glsl")
	b := block("shaders/skyparam.frag")
	if a == b {
		return
	}
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := range max(len(al), len(bl)) {
		x, y := "", ""
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			t.Fatalf("the atmosphere block differs at line %d of the block:\nprelude_mesh.glsl: %q\nskyparam.frag:     %q", i+1, x, y)
		}
	}
}

// TestAtmosphereThinsWithAltitude checks the model against what a climb
// looks like: the sky dims as the camera leaves the air behind, and in
// space only the limb still scatters.
func TestAtmosphereThinsWithAltitude(t *testing.T) {
	sky := func(alt float32) Sky {
		s := Sky{Atmosphere: Atmosphere{Height: 60, Altitude: alt}}
		return s.resolved(Light{Direction: lin.V3(0, -1, 0), Color: White})
	}
	up := lin.V3(0, 1, 0)
	var last float32
	for i, alt := range []float32{0, 10, 30, 120} {
		_, g, _ := sky(alt).radiance(up)
		if i > 0 && g >= last {
			t.Errorf("looking up from %v the sky is %v, want dimmer than the %v below it", alt, g, last)
		}
		last = g
	}
	// From orbit the sky overhead is empty and the limb still glows: with
	// the ground 6000 units away and the camera 400 above it the planet's
	// edge sits about twenty degrees below the horizontal.
	high := sky(400)
	_, zenith, _ := high.radiance(up)
	_, limb, _ := high.radiance(lin.V3(0, -0.34, 0.94).Norm())
	if zenith > 1e-6 {
		t.Errorf("from 400 units up the sky overhead is %v, want nothing", zenith)
	}
	if limb < 0.005 {
		t.Errorf("from 400 units up the planet's limb is %v, want the air along it lit", limb)
	}
}

// TestAtmosphereReddensTheHorizon checks the colours the model exists
// for: with the sun low the horizon is red and the sky overhead stays
// blue, and with the sun overhead the horizon is no longer red.
func TestAtmosphereReddensTheHorizon(t *testing.T) {
	g := newHeadless(t, 64, 64)
	air := Sky{Atmosphere: Atmosphere{Height: 60}}
	// Looking along +z, with the sun in that half of the sky. The horizon
	// is sampled a quarter of the frame from the sun, clear of its glow.
	side := Camera{Position: lin.V3(0, 0, -5), Target: lin.V3(0, 0, 5)}
	overhead := Camera{Position: lin.V3(0, 0, 0), Target: lin.V3(0, 1, 0), Up: lin.V3(0, 0, 1)}
	low := lin.V3(0, -0.07, 1).Norm()  // the sun four degrees up
	high := lin.V3(0, -1, 0.05).Norm() // the sun overhead
	ratio := func(c image.RGBA, x, y int) float64 {
		p := c.RGBAAt(x, y)
		return float64(p.R+1) / float64(p.B+1)
	}
	dusk := skyShot(t, g, side, Light{Direction: low, Color: White, Sky: air})
	duskUp := skyShot(t, g, overhead, Light{Direction: low, Color: White, Sky: air})
	noon := skyShot(t, g, side, Light{Direction: high, Color: White, Sky: air})
	horizon, zenith, noonHorizon := ratio(*dusk, 4, 31), ratio(*duskUp, 32, 32), ratio(*noon, 4, 31)
	if horizon <= 1 {
		t.Errorf("with the sun four degrees up the horizon is %v red over blue, want more red than blue", horizon)
	}
	if zenith >= 1 {
		t.Errorf("with the sun four degrees up the sky overhead is %v red over blue, want more blue than red", zenith)
	}
	if horizon <= 2*zenith {
		t.Errorf("the horizon is %v red over blue and the zenith %v, want the horizon much redder", horizon, zenith)
	}
	if noonHorizon >= horizon {
		t.Errorf("with the sun overhead the horizon is %v red over blue and at dusk %v, want dusk redder", noonHorizon, horizon)
	}
}

// TestAtmosphereMatchesGo checks the scattering the background shader
// draws against Sky.radiance, which is what the ambient harmonics are
// projected from. The two are separate implementations of one model, and
// a scene lit by one and drawn with the other has to agree.
func TestAtmosphereMatchesGo(t *testing.T) {
	g := newHeadless(t, 64, 64)
	// The sun sits behind the camera, so no pixel carries the disc's glow,
	// which the Go side leaves out.
	dir := lin.V3(0, -0.5, 0.866).Norm()
	light := Light{Direction: dir, Color: White, Sky: Sky{Atmosphere: Atmosphere{Height: 60, Altitude: 2}}}
	cam := Camera{Position: lin.V3(0, 0, 0), Target: lin.V3(0, 0, 5)}
	img := skyShot(t, g, cam, light)
	sky := light.Sky.resolved(light)
	for _, p := range [][2]int{{8, 8}, {32, 6}, {56, 8}, {16, 24}, {48, 24}, {32, 30}} {
		d := skyDir(cam, 64, 64, p[0], p[1])
		wr, wg, wb := sky.radiance(d)
		px := img.RGBAAt(p[0], p[1])
		gr, gg, gb := unpost(px.R), unpost(px.G), unpost(px.B)
		for _, c := range []struct {
			name     string
			want, is float64
		}{{"red", float64(wr), gr}, {"green", float64(wg), gg}, {"blue", float64(wb), gb}} {
			if math.Abs(c.is-c.want) > 0.02+0.15*c.want {
				t.Errorf("pixel %v along %v: the shader's %s is %.4f, Sky.radiance says %.4f", p, d, c.name, c.is, c.want)
			}
		}
	}
}
