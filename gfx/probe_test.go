package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestCubeFacesSample checks the inverse of cubeDir: a direction through
// the middle of a face reads that face, and an off-centre direction reads
// the texel cubeDir would have written it to.
func TestCubeFacesSample(t *testing.T) {
	const side = 4
	c := &cubeFaces{side: side}
	// Each face is a flat colour with its own red channel, plus one texel
	// per face marked in green, so orientation shows up as well as order.
	for face := range 6 {
		pix := make([]byte, side*side*8)
		for y := range side {
			for x := range side {
				i := (y*side + x) * 8
				putF16(pix[i:], float32(face+1))
				putF16(pix[i+2:], 0)
				putF16(pix[i+4:], 0)
				putF16(pix[i+6:], 1)
			}
		}
		c.pix[face] = pix
	}
	for face := range 6 {
		r, _, _ := c.sample(cubeDir(face, 0, 0).Norm())
		if int(r+0.5) != face+1 {
			t.Errorf("direction through face %d reads face %d", face, int(r+0.5)-1)
		}
	}
	// One corner texel of every face is marked, and the direction cubeDir
	// maps that texel's centre to must read it back, mirroring included.
	const x, y = 0, side - 1
	u := 2*(float32(x)+0.5)/side - 1
	v := 2*(float32(y)+0.5)/side - 1
	for face := range 6 {
		i := ((y * side) + (side - 1 - x)) * 8
		putF16(c.pix[face][i+2:], 9)
	}
	for face := range 6 {
		_, g, _ := c.sample(cubeDir(face, u, v).Norm())
		if g < 8.5 {
			t.Errorf("face %d texel at u %v v %v reads green %v, want the marked texel", face, u, v, g)
		}
	}
}

// TestBakeProbeOrientation bakes a probe in a room with one red wall and
// checks the environment it produced is red in that direction and dark in
// the other, which fails if a face is rendered turned or mirrored.
func TestBakeProbeOrientation(t *testing.T) {
	g := newHeadless(t, 32, 32)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	probe := &ReflectionProbe{Position: lin.V3(0, 0, 0), Extent: lin.V3(4, 4, 4), Resolution: 32}
	defer probe.Destroy()
	// One bright red slab three units along +X, nothing anywhere else.
	err = g.BakeProbe(probe, func() {
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Ambient: Color{}, Sky: Sky{Vacuum: 1}})
		g.DrawMesh(cube, Material{BaseColor: Color{1, 0, 0, 1}, Unlit: true},
			lin.Translate(lin.V3(3, 0, 0)).Mul(lin.Scale(lin.V3(0.5, 2, 2))))
	})
	if err != nil {
		t.Fatal(err)
	}
	env := probe.Environment()
	if env == nil {
		t.Fatal("BakeProbe left no environment")
	}
	irradiance := func(n lin.Vec3) (r, g, b float32) {
		basis := shBasis(n)
		var out [3]float64
		for i := range 9 {
			out[0] += float64(env.sh[i].X) * basis[i]
			out[1] += float64(env.sh[i].Y) * basis[i]
			out[2] += float64(env.sh[i].Z) * basis[i]
		}
		return float32(out[0]), float32(out[1]), float32(out[2])
	}
	toward, _, _ := irradiance(lin.V3(1, 0, 0))
	away, _, _ := irradiance(lin.V3(-1, 0, 0))
	if toward <= away*2 || toward < 0.05 {
		t.Errorf("baked irradiance is %.3f towards the red slab and %.3f away; want much more towards it", toward, away)
	}
	up, _, _ := irradiance(lin.V3(0, 1, 0))
	if up > toward {
		t.Errorf("baked irradiance is %.3f upwards and %.3f towards the slab; the faces are turned", up, toward)
	}
}

// TestReflectionProbeRoom bakes a probe inside a red room under a blue
// sky and checks a mirror sphere inside the probe's volume reflects the
// room, while the same sphere outside every probe reflects the sky.
func TestReflectionProbeRoom(t *testing.T) {
	g := newHeadless(t, 96, 96)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	sv, si := SphereMesh(24, 48)
	sphere, err := g.NewMesh(sv, si)
	if err != nil {
		t.Fatal(err)
	}
	defer sphere.Destroy()
	blue := Sky{Zenith: Color{0, 0, 1, 1}, Horizon: Color{0, 0, 1, 1}, Ground: Color{0, 0, 1, 1}}
	// A cube scaled by a negative factor turns inside out, so its faces
	// are the walls of a room seen from within.
	room := func() {
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Sky: blue})
		g.DrawMesh(cube, Material{BaseColor: Color{1, 0, 0, 1}, Unlit: true}, lin.Scale(lin.V3(-6, -6, -6)))
	}
	probe := &ReflectionProbe{Position: lin.V3(0, 0, 0), Extent: lin.V3(5, 5, 5), Resolution: 48}
	defer probe.Destroy()
	if err := g.BakeProbe(probe, room); err != nil {
		t.Fatal(err)
	}
	mirror := Material{BaseColor: White, Metallic: 1, Roughness: 0.05}
	img := renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Sky: blue})
		g.AddProbe(probe)
		g.DrawMesh(sphere, mirror, lin.Scale(lin.V3(1.2, 1.2, 1.2)))
	})
	inside := img.RGBAAt(48, 48)
	if inside.R < inside.B+40 {
		t.Errorf("a mirror sphere inside the red room's probe is %v, want a red reflection", inside)
	}
	img = renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Sky: blue})
		g.DrawMesh(sphere, mirror, lin.Scale(lin.V3(1.2, 1.2, 1.2)))
	})
	outside := img.RGBAAt(48, 48)
	if outside.B < outside.R+40 {
		t.Errorf("a mirror sphere outside every probe is %v, want the blue sky reflected", outside)
	}
	// A draw beyond the probe's volume keeps the sky even while the probe
	// is in the frame.
	img = renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, -1, 0), Color: Color{}, Sky: blue})
		g.AddProbe(&ReflectionProbe{Position: lin.V3(20, 0, 0), Extent: lin.V3(1, 1, 1), Resolution: 16, env: probe.env})
		g.DrawMesh(sphere, mirror, lin.Scale(lin.V3(1.2, 1.2, 1.2)))
	})
	far := img.RGBAAt(48, 48)
	if far.B < far.R+40 {
		t.Errorf("a mirror sphere outside a distant probe's volume is %v, want the blue sky reflected", far)
	}
}
