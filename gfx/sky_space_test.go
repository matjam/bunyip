package gfx

import (
	"math"
	"testing"

	"github.com/matjam/bunyip/lin"
)

func uniformSpace(t *testing.T, g *Graphics, c Color, intensity float32) *Environment {
	t.Helper()
	p := &HDRImage{Width: 32, Height: 16, Pix: make([]float32, 32*16*3)}
	for i := 0; i < len(p.Pix); i += 3 {
		p.Pix[i], p.Pix[i+1], p.Pix[i+2] = c.R, c.G, c.B
	}
	env, err := g.NewEnvironmentHDR(p, EnvironmentOptions{Size: 16, Intensity: intensity})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(env.Destroy)
	return env
}

func TestSkySpaceBackgroundAndAtmosphere(t *testing.T) {
	g := newHeadless(t, 64, 64)
	env := uniformSpace(t, g, Color{.2, .1, .05, 1}, 2)
	cam := Camera{Target: lin.V3(0, .5, -1), Up: lin.V3(0, 1, 0)}
	for _, altitude := range []float32{2, 400} {
		light := Light{Direction: lin.V3(0, -.5, -.866), Color: White, Sky: Sky{Space: env, Atmosphere: Atmosphere{Height: 60, Altitude: altitude}}}
		img := skyShot(t, g, cam, light)
		s := light.Sky.resolved(light)
		for _, pixel := range [][2]int{{8, 8}, {32, 6}, {56, 8}, {16, 24}, {48, 24}} {
			d := skyDir(cam, 64, 64, pixel[0], pixel[1])
			r, gg, b := s.radiance(d)
			p := img.RGBAAt(pixel[0], pixel[1])
			for i, want := range []float32{r, gg, b} {
				got := unpost([]uint8{p.R, p.G, p.B}[i])
				if math.Abs(got-float64(want)) > .02+.15*float64(want) {
					t.Fatalf("altitude%v pixel%v channel%d: GPU%v CPU%v", altitude, pixel, i, got, want)
				}
			}
		}
	}
	// An explicit image environment retains replacement precedence, even with
	// both a bright procedural sky and a second space environment present.
	replacement := uniformSpace(t, g, Color{0, 0, .3, 1}, 1)
	img := skyShot(t, g, cam, Light{Environment: replacement, Sky: Sky{Space: env, Zenith: Color{3, 0, 0, 1}}})
	p := img.RGBAAt(32, 32)
	if p.R > 5 || p.G > 5 || p.B < 100 {
		t.Fatalf("image replacement precedence lost: %v", p)
	}
}

func TestSkySpaceLightsDiffuseAndReflections(t *testing.T) {
	g := newHeadless(t, 64, 64)
	env := uniformSpace(t, g, Color{.25, .05, .01, 1}, 1)
	quad := facingQuad(t, g)
	for _, metallic := range []float32{0, 1} {
		img := renderMaterial(t, g, func() {
			g.SetLight(Light{Direction: lin.V3(0, -1, 0), Sky: Sky{Vacuum: 1, Space: env}})
			g.DrawMesh(quad, Material{BaseColor: White, Roughness: .1, Metallic: metallic}, lin.Identity())
		})
		p := img.RGBAAt(32, 32)
		if p.R < 80 || p.R < p.B+30 {
			t.Fatalf("metallic%v did not receive space lighting: %v", metallic, p)
		}
	}
	// Destroying a borrowed space image must invalidate the sky cache and stop
	// binding its retired descriptor while the Sky value itself remains usable.
	env.Destroy()
	img := skyShot(t, g, Camera{Target: lin.V3(0, 0, -1)}, Light{Sky: Sky{Vacuum: 1, Space: env}})
	if p := img.RGBAAt(32, 32); p.R > 5 || p.G > 5 || p.B > 5 {
		t.Fatalf("destroyed space remains visible: %v", p)
	}
}

func TestSkySpaceOpticalDepth(t *testing.T) {
	s := Sky{Atmosphere: Atmosphere{Height: 60, Altitude: 2}}.resolved(Light{})
	up := s.spaceTransmittance(lin.V3(0, 1, 0))
	if !(up.X > up.Z && up.Z > 0 && up.X < 1) {
		t.Fatalf("expected wavelength-dependent air attenuation: %v", up)
	}
	if down := s.spaceTransmittance(lin.V3(0, -1, 0)); down.Len() != 0 {
		t.Fatalf("planet did not occlude space: %v", down)
	}
	s.Atmosphere.Altitude = 400
	if outward := s.spaceTransmittance(lin.V3(0, 1, 0)); outward != lin.V3(1, 1, 1) {
		t.Fatalf("vacuum outward ray attenuated: %v", outward)
	}
}

func BenchmarkSkySpaceEnvironment512(b *testing.B) {
	g := benchHeadless(b, 32, 32)
	p := &HDRImage{Width: 2048, Height: 1024, Pix: make([]float32, 2048*1024*3)}
	for i := range p.Pix {
		p.Pix[i] = .01
	}
	b.ReportAllocs()
	for b.Loop() {
		env, err := g.NewEnvironmentHDR(p, EnvironmentOptions{Size: 512})
		if err != nil {
			b.Fatal(err)
		}
		env.Destroy()
	}
}
