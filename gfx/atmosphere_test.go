package gfx

import (
	"image"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/matjam/bunyip/internal/render"
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
// the same unprojection skyparam.frag.wgsl does.
func skyDir(cam Camera, w, h, x, y int) lin.Vec3 {
	inv := cam.ViewProj(float32(w) / float32(h)).Inverse()
	nx := 2*(float32(x)+0.5)/float32(w) - 1
	ny := 2*(float32(y)+0.5)/float32(h) - 1
	near := inv.MulVec4(lin.V4(nx, ny, 0, 1))
	far := inv.MulVec4(lin.V4(nx, ny, 1, 1))
	return far.Vec3().Mul(1 / far.W).Sub(near.Vec3().Mul(1 / near.W)).Norm()
}

// unpost turns a composited pixel back into the radiance the scene pass
// wrote: sRGB decoding, then the inverse of the ACES curve in post.frag.wgsl,
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

// TestAtmosphereBlocksMatch checks that the atmosphere is the same text
// wherever it is compiled: the mapping from a ray to where the lookup
// tables keep it in the shader that builds them and in the two that
// read them, and the lookups in the mesh prelude and the background
// shader. The three are compiled separately, so nothing but this
// catches a change to one.
func TestAtmosphereBlocksMatch(t *testing.T) {
	block := func(path, name string) string {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s := string(src)
		i, j := strings.Index(s, "// "+name+"."), strings.Index(s, "// END "+name+".")
		if i < 0 || j <= i {
			t.Fatalf("%s has no %s block", path, name)
		}
		return s[i:j]
	}
	same := func(name string, paths ...string) {
		first := block(paths[0], name)
		for _, path := range paths[1:] {
			other := block(path, name)
			if other == first {
				continue
			}
			al, bl := strings.Split(first, "\n"), strings.Split(other, "\n")
			for i := range max(len(al), len(bl)) {
				x, y := "", ""
				if i < len(al) {
					x = al[i]
				}
				if i < len(bl) {
					y = bl[i]
				}
				if x != y {
					t.Fatalf("the %s block differs at line %d of the block:\n%s: %q\n%s: %q", name, i+1, paths[0], x, path, y)
				}
			}
		}
	}
	same("ATMOSPHERE MAPPING", "shaders/atmoslut.frag.wgsl", "shaders/prelude_mesh.wgsl", "shaders/skyparam.frag.wgsl")
	same("ATMOSPHERE LOOKUP", "shaders/prelude_mesh.wgsl", "shaders/skyparam.frag.wgsl")
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

// The mapping from a texel of the atmosphere's tables to the ray it
// holds, as the ATMOSPHERE MAPPING block in the shaders computes it, for
// TestAtmosphereTablesMatchGo to integrate the same rays in Go.

// tableUnit is atmosUnit: a texel coordinate to 0..1 over n texels.
func tableUnit(t, n float64) float64 { return math.Min(math.Max((t-0.5)/(n-1), 0), 1) }

// transRay is atmosTransRay: the radius and the cosine with the zenith a
// transmittance texel stands for.
func transRay(x, y, radius, top float64) (r, mu float64) {
	span := math.Sqrt(top*top - radius*radius)
	rho := span * tableUnit(y, atmosTransH)
	r = math.Sqrt(rho*rho + radius*radius)
	dMin, dMax := top-r, rho+span
	d := dMin + tableUnit(x, atmosTransW)*(dMax-dMin)
	mu = 1
	if d > 0 {
		mu = math.Min(math.Max((span*span-rho*rho-d*d)/(2*r*d), -1), 1)
	}
	return r, mu
}

// viewDir is atmosViewDir: the direction a texel of a view table w by h
// stands for.
func viewDir(x, y, w, h float64, up, side lin.Vec3, first, grazing float64) lin.Vec3 {
	rows := h / 2
	var cosZ, sinZ float64
	if y < rows {
		c := 1 - tableUnit(y, rows)
		p := grazing - (grazing-first)*c*c
		cosZ, sinZ = (1-p*p)/(1+p*p), 2*p/(1+p*p)
	} else {
		c := tableUnit(y-rows, rows)
		q := (1 - c*c) / grazing
		cosZ, sinZ = -(1-q*q)/(1+q*q), 2*q/(1+q*q)
	}
	cosA := 1 - 2*tableUnit(x, w)
	sinA := math.Sqrt(math.Max(1-cosA*cosA, 0))
	across := up.Cross(side)
	return up.Mul(float32(cosZ)).Add(side.Mul(float32(cosA * sinZ))).Add(across.Mul(float32(sinA * sinZ))).Norm()
}

// TestAtmosphereTablesMatchGo reads the atmosphere's lookup tables back
// after a frame and checks texels of each against the Go model at the
// ray the texel stands for: the transmittance table against
// Sky.opticalDepth, and the sky view and the aerial perspective against
// Sky.scatterTerms, which is Sky.scatter without its phase functions.
// The shader that builds them and the Go side are separate
// implementations of one model; this is what keeps them one.
func TestAtmosphereTablesMatchGo(t *testing.T) {
	g := newHeadless(t, 64, 64)
	sunDir := lin.V3(0.3, -0.25, 0.9).Norm() // the sun about fourteen degrees up
	light := Light{Direction: sunDir, Color: White, Background: true,
		Sky: Sky{Atmosphere: Atmosphere{Height: 60, Altitude: 2}}}
	renderMaterial(t, g, func() {
		g.SetCamera(Camera{Position: lin.V3(0, 2, 0), Target: lin.V3(0, 2, 5)})
		g.SetLight(light)
	})
	tables := &g.meshes.atmos
	if !tables.haveTrans || !tables.haveView {
		t.Fatal("a frame with an atmosphere built no tables")
	}
	sky := light.Sky.resolved(light)
	a := sky.Atmosphere
	radius, top := float64(a.PlanetRadius), float64(a.PlanetRadius+a.Height)
	hR, hM := a.Height/rayleighFalloff, a.Height/mieFalloff
	read := func(tg *render.Target) (int, [][4]float32) {
		raw, err := g.r.Device.ReadImageRaw(tg.Color, 8)
		if err != nil {
			t.Fatal(err)
		}
		px := make([][4]float32, len(raw)/8)
		for i := range px {
			for c := range 4 {
				px[i][c] = f16ToF32(uint16(raw[8*i+2*c]) | uint16(raw[8*i+2*c+1])<<8)
			}
		}
		return int(tg.Extent.Width), px
	}
	// agree reports whether a table's value is the Go model's, within the
	// half float the table stores and the interpolation of the
	// transmittance table the sky view reads.
	var worst float32 // the largest error seen, as a share of the tolerance
	agree := func(is, want, rel float32) bool {
		allowed := 2e-4 + rel*abs32(want)
		worst = max(worst, abs32(is-want)/allowed)
		return abs32(is-want) <= allowed
	}
	defer func() { t.Logf("the largest difference used %.0f%% of the tolerance", 100*worst) }()

	w, trans := read(tables.trans)
	for y := 0; y < atmosTransH; y += 7 {
		for x := 0; x < atmosTransW; x += 23 {
			r, mu := transRay(float64(x)+0.5, float64(y)+0.5, radius, top)
			p := lin.V3(0, float32(r), 0)
			d := lin.V3(float32(math.Sqrt(math.Max(1-mu*mu, 0))), float32(mu), 0)
			r4, m4 := sky.opticalDepth(p, d, 4)
			r8, m8 := sky.opticalDepth(p, d, 8)
			texel := trans[y*w+x]
			for i, want := range [4]float32{r4 / hR, m4 / hM, r8 / hR, m8 / hM} {
				if !agree(texel[i], want, 0.01) {
					t.Errorf("transmittance texel (%d, %d), height %.3f and cosine %.4f: channel %d is %.4f, Go says %.4f", x, y, r-radius, mu, i, texel[i], want)
				}
			}
		}
	}

	up := sky.Up
	side := atmosSunSide(up, sky.sun)
	first, grazing := atmosHorizon(a.PlanetRadius+a.Altitude, a.PlanetRadius, a.PlanetRadius+a.Height)
	// Rows at the ground's horizon hold rays that graze it, which the two
	// sides may place either side of it; they are left out.
	skip := func(row, rows int) bool { return row >= rows/2-2 && row <= rows/2+1 }

	w, view := read(tables.sky)
	for y := 0; y < 2*atmosSkyH; y += 5 {
		row := y % atmosSkyH
		if skip(row, atmosSkyH) {
			continue
		}
		for x := 0; x < atmosSkyW; x += 9 {
			d := viewDir(float64(x)+0.5, float64(row)+0.5, atmosSkyW, atmosSkyH, up, side, float64(first), float64(grazing))
			ray, mie, _, _ := sky.scatterTerms(d, 1e9, atmosphereViewSteps, atmosphereSunSteps)
			want := ray
			if y >= atmosSkyH {
				want = mie
			}
			texel := view[y*w+x]
			for i, c := range [3]float32{want.X, want.Y, want.Z} {
				if !agree(texel[i], c, 0.01) {
					t.Errorf("sky view texel (%d, %d) along %v: channel %d is %.5f, Go says %.5f", x, y, d, i, texel[i], c)
				}
			}
		}
	}

	w, aerial := read(tables.aerial)
	r := a.PlanetRadius + a.Altitude
	for y := 0; y < 2*atmosAerialH; y += 5 {
		row := y % atmosAerialH
		if skip(row, atmosAerialH) {
			continue
		}
		for x := 0; x < atmosAerialW*atmosAerialD; x += 37 {
			k, col := x/atmosAerialW, x%atmosAerialW
			d := viewDir(float64(col)+0.5, float64(row)+0.5, atmosAerialW, atmosAerialH, up, side, float64(first), float64(grazing))
			// The slice's distance: its fraction of the ray's run through
			// the air, as atmosSpan and atmosSliceFraction find it.
			near, far := raySphere(up.Mul(r), d, a.PlanetRadius+a.Height)
			t0, t1 := max(near, 0), far
			if g0, g1 := raySphere(up.Mul(r), d, a.PlanetRadius); g1 > 0 && g0 > 0 {
				t1 = min(t1, g0)
			}
			u := float32(k) / (atmosAerialD - 1)
			f := 2 * u * u
			if u >= 0.5 {
				f = 1 - 2*(1-u)*(1-u)
			}
			ray, mie, odR, odM := sky.scatterTerms(d, max(t1-t0, 0)*f, 4, 2)
			want := [4]float32{ray.X, ray.Y, ray.Z, odR / hR}
			if y >= atmosAerialH {
				want = [4]float32{mie.X, mie.Y, mie.Z, odM / hM}
			}
			texel := aerial[y*w+x]
			for i, c := range want {
				if !agree(texel[i], c, 0.01) {
					t.Errorf("aerial perspective texel (%d, %d), slice %d along %v: channel %d is %.5f, Go says %.5f", x, y, k, d, i, texel[i], c)
				}
			}
		}
	}
}

// TestAtmosphereTablesRebuild checks when the view tables are built: on
// the first frame with an atmosphere, not again while nothing moves or
// the camera's altitude moves by less than atmosAltitudeStep of the
// air's height, and again when the altitude moves further, the sun moves
// or the air changes.
func TestAtmosphereTablesRebuild(t *testing.T) {
	g := newHeadless(t, 32, 32)
	tables := &g.meshes.atmos
	frame := func(l Light) {
		t.Helper()
		l.Background = true
		renderMaterial(t, g, func() { g.SetLight(l) })
	}
	base := Light{Direction: lin.V3(0.2, -0.5, 0.8), Color: White, Sky: Sky{Atmosphere: Atmosphere{Height: 100, Altitude: 10}}}
	frame(Light{Direction: base.Direction, Color: White})
	if tables.builds != 0 {
		t.Fatalf("a frame without an atmosphere built the tables %d times", tables.builds)
	}
	steps := []struct {
		name  string
		light func() Light
		want  int
	}{
		{"the first frame", func() Light { return base }, 1},
		{"the same frame again", func() Light { return base }, 1},
		{"a small climb", func() Light { l := base; l.Sky.Atmosphere.Altitude += 0.05; return l }, 1},
		{"a climb", func() Light { l := base; l.Sky.Atmosphere.Altitude += 1; return l }, 2},
		{"the sun moving", func() Light { l := base; l.Direction = lin.V3(0.2, -0.51, 0.8); return l }, 3},
		{"brighter sunlight", func() Light { l := base; l.Sky.Atmosphere.Intensity = 30; return l }, 4},
	}
	for _, s := range steps {
		frame(s.light())
		if tables.builds != s.want {
			t.Errorf("after %s the view tables were built %d times, want %d", s.name, tables.builds, s.want)
		}
	}
}

// TestAtmospherePlanReadsBuiltAltitude checks that a frame reusing the
// view tables reads them at the altitude and sun they were built for,
// so the lookup and the table agree while the camera drifts within a
// step, and at its own once they are built again.
func TestAtmospherePlanReadsBuiltAltitude(t *testing.T) {
	var tables atmosTables
	u := frameUniforms{
		atmos: lin.V4(10000, 100, 100/rayleighFalloff, 100/mieFalloff),
		betaR: lin.V4(0.003, 0.008, 0.02, 22),
		betaM: lin.V4(0.01, 0.76, 10, 1),
		skyUp: lin.V4(0, 1, 0, 0),
		sun:   lin.V4(0, 0.5, 0.866, 0.0047),
	}
	tables.plan(&u)
	tables.built, tables.haveTrans, tables.haveView = tables.want, true, true
	drift := u
	drift.betaM.Z = 10.05
	drift.sun = lin.V4(0.0001, 0.5, 0.866, 0.0047)
	tables.plan(&drift)
	if drift.betaM.Z != 10 {
		t.Errorf("drifting a twentieth of a unit within a %v step, the frame reads the tables at altitude %v, want 10", 100*atmosAltitudeStep, drift.betaM.Z)
	}
	if want := atmosSunSide(lin.V3(0, 1, 0), lin.V3(0, 0.5, 0.866)).Vec4(10010); drift.atmosView != want {
		t.Errorf("drifting within a step, the frame measures azimuth from %v, want the built sun's %v", drift.atmosView, want)
	}
	climb := u
	climb.betaM.Z = 12
	tables.plan(&climb)
	if climb.betaM.Z != 12 {
		t.Errorf("climbing two units, the frame reads the tables at altitude %v, want its own 12", climb.betaM.Z)
	}
}
