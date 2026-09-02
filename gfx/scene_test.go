package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

func TestFrustumMath(t *testing.T) {
	cam := Camera{Position: lin.V3(0, 0, 5), Target: lin.V3(0, 0, 0)}
	f := cam.Frustum(1)
	if !f.ContainsSphere(lin.V3(0, 0, 0), 1) {
		t.Error("the target is outside the frustum")
	}
	if f.ContainsSphere(lin.V3(0, 0, 10), 1) {
		t.Error("a sphere behind the camera is inside the frustum")
	}
	if f.ContainsSphere(lin.V3(0, 0, -2000), 1) {
		t.Error("a sphere past the far plane is inside the frustum")
	}
	if f.ContainsSphere(lin.V3(30, 0, 0), 1) {
		t.Error("a sphere far to the side is inside the frustum")
	}
	if !f.ContainsSphere(lin.V3(30, 0, 0), 40) {
		t.Error("a sphere that reaches into the view is outside")
	}
	if !f.ContainsBox(lin.V3(-1, -1, -1), lin.V3(1, 1, 1)) || f.ContainsBox(lin.V3(20, 20, 20), lin.V3(30, 30, 30)) {
		t.Error("ContainsBox disagrees with the view")
	}
	corners := FrustumCorners(cam.ViewProj(1))
	if near := corners[0].Z; near < 4.8 || near > 5 {
		t.Errorf("near corner z = %v, want just under the camera's 5", near)
	}
	if far := corners[4].Z; far > -900 {
		t.Errorf("far corner z = %v, want near -995", far)
	}
}

func TestFrustumCulling(t *testing.T) {
	g := newHeadless(t, 64, 64)
	v, i := CubeMesh()
	cube, err := g.NewMesh(v, i)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	renderMaterial(t, g, func() {
		g.DrawMesh(cube, Material{}, lin.Identity())
		g.DrawMesh(cube, Material{}, lin.Translate(lin.V3(0, 0, 20)))  // behind the camera
		g.DrawMesh(cube, Material{}, lin.Translate(lin.V3(50, 0, -5))) // far to the side
	})
	s := g.Stats()
	if s.Culled != 2 || s.Instances != 1 {
		t.Errorf("culled %d instances %d, want 2 and 1", s.Culled, s.Instances)
	}
}

func TestFog(t *testing.T) {
	g := newHeadless(t, 64, 64)
	v, i := CubeMesh()
	cube, err := g.NewMesh(v, i)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	shot := func(fog Fog) (r, b uint8) {
		img := renderMaterial(t, g, func() {
			g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: Color{1, 1, 1, 1}, Ambient: Color{0.2, 0.2, 0.2, 1}, Fog: fog})
			g.DrawMesh(cube, Material{BaseColor: Color{0.2, 0.2, 1, 1}}, lin.Identity())
		})
		p := img.RGBAAt(32, 32)
		return p.R, p.B
	}
	r0, b0 := shot(Fog{})
	if r0 > 120 || b0 < 150 {
		t.Fatalf("unfogged cube is (%d,_,%d), want blue", r0, b0)
	}
	// The cube's face sits 2.5 units away: linear fog ending there paints
	// it the fog's red, exponential fog with a large density does too, and
	// ground fog that thins above y = -10 leaves it alone.
	if r, b := shot(Fog{Color: Color{1, 0, 0, 1}, Start: 0.5, End: 2}); r < 200 || b > 60 {
		t.Errorf("linear fog gives (%d,_,%d), want red", r, b)
	}
	if r, _ := shot(Fog{Color: Color{1, 0, 0, 1}, Density: 2}); r < 200 {
		t.Errorf("exponential fog gives red %d, want full", r)
	}
	if r, b := shot(Fog{Color: Color{1, 0, 0, 1}, Start: 0.5, End: 2, Height: -10, HeightFalloff: 5}); r > 120 || b < 150 {
		t.Errorf("ground fog far below gives (%d,_,%d), want the cube's blue", r, b)
	}
}

func TestSpotLight(t *testing.T) {
	g := newHeadless(t, 64, 64)
	v, i := QuadMesh()
	quad, err := g.NewMesh(v, i)
	if err != nil {
		t.Fatal(err)
	}
	defer quad.Destroy()
	img := renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, 0, -1)}) // no sun, no ambient
		g.AddSpotLight(lin.V3(0, 0, 2), lin.V3(0, 0, -1), Color{4, 4, 4, 1}, 10, lin.Radians(10), lin.Radians(30))
		g.DrawMesh(quad, Material{Roughness: 1}, lin.Scale(lin.V3(8, 8, 1)))
	})
	if !bright(img, 32, 32) {
		t.Error("the spot's centre is dark")
	}
	if bright(img, 2, 32) {
		t.Error("outside the spot's cone is lit")
	}
	// The same light as a point light reaches the edge.
	img = renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, 0, -1)})
		g.AddPointLight(lin.V3(0, 0, 2), Color{4, 4, 4, 1}, 10)
		g.DrawMesh(quad, Material{Roughness: 1}, lin.Scale(lin.V3(8, 8, 1)))
	})
	if !bright(img, 2, 32) {
		t.Error("the point light leaves the edge dark")
	}
}

func TestManyLights(t *testing.T) {
	g := newHeadless(t, 64, 64)
	v, i := QuadMesh()
	quad, err := g.NewMesh(v, i)
	if err != nil {
		t.Fatal(err)
	}
	defer quad.Destroy()
	// Thirty dim lights far away and the thirty-first over the quad: the
	// old limit of eight would have dropped it.
	img := renderMaterial(t, g, func() {
		g.SetLight(Light{Direction: lin.V3(0, 0, -1)})
		for k := range 30 {
			g.AddPointLight(lin.V3(100+float32(k), 0, 0), Color{1, 1, 1, 1}, 1)
		}
		g.AddPointLight(lin.V3(0, 0, 1), Color{4, 4, 4, 1}, 10)
		g.DrawMesh(quad, Material{Roughness: 1}, lin.Scale(lin.V3(8, 8, 1)))
	})
	if !bright(img, 32, 32) {
		t.Error("the thirty-first light was dropped")
	}
}

func TestBillboard(t *testing.T) {
	g := newHeadless(t, 64, 64)
	for _, upright := range []bool{false, true} {
		img := renderMaterial(t, g, func() {
			g.SetCamera(Camera{Position: lin.V3(0, 2, 3), Target: lin.V3(0, 0, 0)})
			g.DrawBillboard(Billboard{Color: Color{0, 1, 0, 1}, Size: lin.V2(1, 1), Upright: upright})
		})
		p := img.RGBAAt(32, 32)
		if p.G < 150 || p.R > 60 {
			t.Errorf("upright %v: billboard's centre is %v, want green", upright, p)
		}
	}
	// Offset (0, 0.5) stands the quad on its position: nothing below it.
	img := renderMaterial(t, g, func() {
		g.DrawBillboard(Billboard{Color: Color{0, 1, 0, 1}, Size: lin.V2(1, 1), Offset: lin.V2(0, 0.5)})
	})
	if p := img.RGBAAt(32, 40); p.G > 60 {
		t.Errorf("a quad standing on the origin paints below it: %v", p)
	}
	if p := img.RGBAAt(32, 24); p.G < 150 {
		t.Errorf("a quad standing on the origin is missing above it: %v", p)
	}
	if s := g.Stats(); s.Instances != 1 {
		t.Errorf("a billboard is %d instances", s.Instances)
	}
}

func TestLOD(t *testing.T) {
	a, b, c := &Mesh{}, &Mesh{}, &Mesh{}
	l := NewLOD([]*Mesh{a, b, c, nil}, []float32{10, 20, 40})
	for _, tc := range []struct {
		d    float32
		want *Mesh
	}{{0, a}, {9.9, a}, {10, b}, {25, c}, {41, nil}} {
		if got := l.Pick(tc.d); got != tc.want {
			t.Errorf("Pick(%v) picked the wrong level", tc.d)
		}
	}
	if NewLOD([]*Mesh{a}, nil).Pick(1e6) != a {
		t.Error("a single level does not serve every distance")
	}
}

func TestMeshUpdate(t *testing.T) {
	g := newHeadless(t, 64, 64)
	v, i := CubeMesh()
	// Start off screen, then move the geometry to the centre.
	m, err := g.NewMesh(TransformVertices(v, lin.Translate(lin.V3(50, 0, 0))), i)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Destroy()
	img := renderMaterial(t, g, func() { g.DrawMesh(m, Material{}, lin.Identity()) })
	if bright(img, 32, 32) {
		t.Fatal("the off-screen cube is visible")
	}
	if err := m.Update(v, i); err != nil {
		t.Fatal(err)
	}
	img = renderMaterial(t, g, func() { g.DrawMesh(m, Material{}, lin.Identity()) })
	if !bright(img, 32, 32) {
		t.Error("the updated cube is not visible")
	}
	if m.Min.X != -0.5 || len(m.Vertices()) != len(v) || len(m.Indices()) != len(i) {
		t.Error("bounds or geometry not updated")
	}
	// Updating mid-frame keeps the old buffers until the frame is done.
	renderMaterial(t, g, func() {
		g.DrawMesh(m, Material{}, lin.Identity())
		if err := m.Update(TransformVertices(v, lin.Scale(lin.V3(2, 2, 2))), i); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPrimitives(t *testing.T) {
	heights := make([]float32, 16)
	heights[5] = 1
	shapes := map[string]func() ([]Vertex, []uint32){
		"quad":        QuadMesh,
		"plane":       func() ([]Vertex, []uint32) { return PlaneMesh(4) },
		"heightfield": func() ([]Vertex, []uint32) { return HeightfieldMesh(heights, 4, 4, 1) },
		"cylinder":    func() ([]Vertex, []uint32) { return CylinderMesh(12) },
		"cone":        func() ([]Vertex, []uint32) { return ConeMesh(12) },
		"capsule":     func() ([]Vertex, []uint32) { return CapsuleMesh(4, 12, 0.5) },
		"torus":       func() ([]Vertex, []uint32) { return TorusMesh(0.25, 16, 8) },
	}
	for name, mk := range shapes {
		v, idx := mk()
		if len(v) == 0 || len(idx) == 0 || len(idx)%3 != 0 {
			t.Errorf("%s: %d vertices, %d indices", name, len(v), len(idx))
			continue
		}
		for _, i := range idx {
			if int(i) >= len(v) {
				t.Errorf("%s: index %d out of range", name, i)
				break
			}
		}
		for k, vert := range v {
			if l := vert.Normal.Len(); l < 0.99 || l > 1.01 {
				t.Errorf("%s: vertex %d normal length %v", name, k, l)
				break
			}
		}
		// Every triangle winds outward: its face normal agrees with its
		// vertices' normals on closed shapes.
		if name != "quad" && name != "plane" && name != "heightfield" {
			bad := 0
			for i := 0; i+2 < len(idx); i += 3 {
				a, b, c := v[idx[i]], v[idx[i+1]], v[idx[i+2]]
				fn := b.Pos.Sub(a.Pos).Cross(c.Pos.Sub(a.Pos))
				if fn.Len() > 1e-6 && fn.Dot(a.Normal.Add(b.Normal).Add(c.Normal)) < 0 { // poles are degenerate
					bad++
				}
			}
			if bad > 0 {
				t.Errorf("%s: %d of %d triangles wind inward", name, bad, len(idx)/3)
			}
		}
	}
	v, _ := HeightfieldMesh(heights, 4, 4, 1)
	// The peak's normal points straight up by symmetry; its neighbour's
	// leans away from it.
	if len(v) != 16 || v[5].Pos.Y != 1 || v[5].Normal.Y < 0.99 || v[4].Normal.Y >= 0.99 || v[4].Normal.X >= 0 {
		t.Errorf("heightfield peak %+v, neighbour %+v", v[5], v[4])
	}
	if v[0].Pos.X != -1.5 || v[15].Pos.Z != 1.5 {
		t.Errorf("heightfield is not centred: %v .. %v", v[0].Pos, v[15].Pos)
	}
	cv, _ := CapsuleMesh(4, 12, 0.5)
	for _, vert := range cv {
		if vert.Pos.Y > 1.501 || vert.Pos.Y < -1.501 {
			t.Errorf("capsule vertex at y = %v", vert.Pos.Y)
			break
		}
	}
	// Flat shading unshares vertices; merging offsets indices.
	fv, fi := FlatShaded(CubeMesh())
	if len(fv) != 36 || len(fi) != 36 {
		t.Errorf("flat cube has %d vertices, %d indices", len(fv), len(fi))
	}
	qv, qi := QuadMesh()
	mv, mi := AppendMesh(qv, qi, qv, qi)
	if len(mv) != 8 || mi[len(mi)-1] != qi[len(qi)-1]+4 {
		t.Errorf("merged quads: %d vertices, last index %d", len(mv), mi[len(mi)-1])
	}
}
