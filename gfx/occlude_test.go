package gfx

import (
	"math/rand"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestOcclusionCulling stands a wall between the camera and a box. The
// box is culled while it hides behind the wall and drawn once it steps
// out from behind it, and the frame's counts say which happened.
func TestOcclusionCulling(t *testing.T) {
	g := newHeadless(t, 64, 64)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	// The camera looks down -z from z = 10; the wall stands at z = 0 and
	// the box at z = -4, either behind the wall or beside it.
	wall := lin.Translate(lin.V3(0, 0, 0)).Mul(lin.Scale(lin.V3(8, 8, 0.5)))
	run := func(x float32) FrameStats {
		frames(t, g, func() {
			g.SetCamera(Camera{Position: lin.V3(0, 0, 10), Target: lin.V3(0, 0, 0)})
			g.SetLight(Light{Direction: lin.V3(0, -1, -0.3), Color: White})
			g.AddOccluder3D(cube, wall)
			g.DrawMesh(cube, Material{Roughness: 1}, wall)
			g.DrawMesh(cube, Material{Roughness: 1}, lin.Translate(lin.V3(x, 0, -4)))
		})
		return g.Stats()
	}
	behind := run(0)
	if behind.Occluded != 1 {
		t.Errorf("box behind the wall: Occluded = %d, want 1", behind.Occluded)
	}
	if behind.Culled != 1 {
		t.Errorf("box behind the wall: Culled = %d, want 1", behind.Culled)
	}
	beside := run(6)
	if beside.Occluded != 0 {
		t.Errorf("box beside the wall: Occluded = %d, want 0", beside.Occluded)
	}
	if beside.Culled != 0 {
		t.Errorf("box beside the wall: Culled = %d, want 0", beside.Culled)
	}
}

// TestOcclusionKeepsNearDraws checks the two ways the buffer must not
// cull: a draw in front of the occluder, and every draw when the frame
// added no occluder at all.
func TestOcclusionKeepsNearDraws(t *testing.T) {
	g := newHeadless(t, 64, 64)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	wall := lin.Translate(lin.V3(0, 0, 0)).Mul(lin.Scale(lin.V3(8, 8, 0.5)))
	run := func(z float32, occlude bool) FrameStats {
		frames(t, g, func() {
			g.SetCamera(Camera{Position: lin.V3(0, 0, 10), Target: lin.V3(0, 0, 0)})
			g.SetLight(Light{Direction: lin.V3(0, -1, -0.3), Color: White})
			if occlude {
				g.AddOccluder3D(cube, wall)
			}
			g.DrawMesh(cube, Material{Roughness: 1}, wall)
			g.DrawMesh(cube, Material{Roughness: 1}, lin.Translate(lin.V3(0, 0, z)))
		})
		return g.Stats()
	}
	if s := run(5, true); s.Occluded != 0 {
		t.Errorf("box in front of the wall: Occluded = %d, want 0", s.Occluded)
	}
	if s := run(-4, false); s.Occluded != 0 || s.Culled != 0 {
		t.Errorf("no occluder: Occluded = %d, Culled = %d, want 0 and 0", s.Occluded, s.Culled)
	}
}

// TestOcclusionBuffer exercises the rasteriser and the box test without a
// GPU: a wall quad written into the buffer hides what is behind it and
// not what is beside or in front of it.
func TestOcclusionBuffer(t *testing.T) {
	cam := Camera{Position: lin.V3(0, 0, 10), Target: lin.V3(0, 0, 0)}
	viewProj := cam.ViewProj(16.0 / 9)
	verts, idx := QuadMesh()
	wall := &Mesh{verts: verts, indices: idx, IndexCount: uint32(len(idx))}
	for i := range verts {
		verts[i].Pos = verts[i].Pos.Mul(8)
	}
	var o occlusionBuffer
	o.begin()
	o.rasterise(wall, viewProj)
	o.on = true

	cases := []struct {
		name   string
		centre lin.Vec3
		radius float32
		want   bool
	}{
		{"behind the wall", lin.V3(0, 0, -4), 1, true},
		{"beside the wall", lin.V3(6, 0, -4), 1, false},
		{"in front of the wall", lin.V3(0, 0, 5), 1, false},
		// The wall's edge at x = 4, z = 0 projects where x = 5.6 does at
		// z = -4, so this sphere hangs half out from behind it.
		{"straddling the wall's edge", lin.V3(5.6, 0, -4), 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := o.hides(viewProj, c.centre, c.radius); got != c.want {
				t.Errorf("hides = %v, want %v", got, c.want)
			}
		})
	}
	// Nothing written means nothing hidden, whatever the geometry.
	var empty occlusionBuffer
	empty.begin()
	empty.on = true
	if empty.hides(viewProj, lin.V3(0, 0, -4), 1) {
		t.Error("an empty buffer hid a draw")
	}
}

// BenchmarkOcclusion measures the cost the brief asks about: rasterising
// fifty box occluders and testing a thousand draws against them.
func BenchmarkOcclusion(b *testing.B) {
	cam := Camera{Position: lin.V3(0, 20, 90), Target: lin.V3(0, 0, 0)}
	viewProj := cam.ViewProj(16.0 / 9)
	cv, ci := CubeMesh()
	box := &Mesh{verts: cv, indices: ci, IndexCount: uint32(len(ci))}
	r := rand.New(rand.NewSource(1))
	occluders := make([]lin.Mat4, 50)
	for i := range occluders {
		p := lin.V3((r.Float32()-0.5)*80, 5, (r.Float32()-0.5)*60)
		occluders[i] = lin.Translate(p).Mul(lin.Scale(lin.V3(8, 10, 2)))
	}
	type probe struct {
		centre lin.Vec3
		radius float32
	}
	probes := make([]probe, 1000)
	for i := range probes {
		probes[i] = probe{centre: lin.V3((r.Float32()-0.5)*100, 2, -20-r.Float32()*60), radius: 1}
	}
	var o occlusionBuffer
	fill := func() {
		o.begin()
		for _, m := range occluders {
			o.rasterise(box, viewProj.Mul(m))
		}
		o.on = true
	}
	b.Run("whole frame", func(b *testing.B) {
		hidden := 0
		for range b.N {
			fill()
			for _, p := range probes {
				if o.hides(viewProj, p.centre, p.radius) {
					hidden++
				}
			}
		}
		b.ReportMetric(float64(hidden)/float64(b.N), "hidden/frame")
	})
	b.Run("rasterise", func(b *testing.B) {
		for range b.N {
			fill()
		}
	})
	b.Run("test", func(b *testing.B) {
		fill()
		for range b.N {
			for _, p := range probes {
				_ = o.hides(viewProj, p.centre, p.radius)
			}
		}
	})
}
