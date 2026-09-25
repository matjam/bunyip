package gfx

import (
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// referenceIntersect is Mesh.Intersect as it was when a mesh kept the
// vertices it was given, triangle loop and all, which the kept forms are
// compared against.
func referenceIntersect(verts []Vertex, indices []uint32, lo, hi lin.Vec3, model lin.Mat4, r Ray) (Hit, bool) {
	inv := model.Inverse()
	o := inv.MulPoint(r.Origin)
	d := inv.MulVec4(r.Dir.Vec4(0)).Vec3()
	if !rayBox(o, d, lo, hi) {
		return Hit{}, false
	}
	best := float32(math.MaxFloat32)
	var bestN lin.Vec3
	found := false
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := verts[indices[i]].Pos, verts[indices[i+1]].Pos, verts[indices[i+2]].Pos
		if t, ok := rayTriangle(o, d, a, b, c); ok && t < best && t > 0 {
			best, found = t, true
			bestN = b.Sub(a).Cross(c.Sub(a))
		}
	}
	if !found {
		return Hit{}, false
	}
	local := o.Add(d.Mul(best))
	world := model.MulPoint(local)
	n := model.MulVec4(bestN.Vec4(0)).Vec3().Norm()
	return Hit{Distance: world.Sub(r.Origin).Len(), Point: world, Normal: n}, true
}

// gridMesh is an n by n grid of vertices over the unit square, bumped so
// rays meet it at varied heights.
func gridMesh(n int) ([]Vertex, []uint32) {
	verts := make([]Vertex, 0, n*n)
	for y := range n {
		for x := range n {
			fx, fy := float32(x)/float32(n-1)-0.5, float32(y)/float32(n-1)-0.5
			verts = append(verts, Vertex{Pos: lin.V3(fx, 0.1*float32(math.Sin(float64(fx*17))*math.Cos(float64(fy*13))), fy)})
		}
	}
	var idx []uint32
	for y := range n - 1 {
		for x := range n - 1 {
			a := uint32(y*n + x)
			idx = append(idx, a, a+uint32(n), a+1, a+1, a+uint32(n), a+uint32(n)+1)
		}
	}
	return verts, idx
}

// keptMesh builds a mesh on the processor alone, keeping its geometry as
// keep says, as newMesh would.
func keptMesh(verts []Vertex, indices []uint32, keep KeepGeometry) *Mesh {
	m := &Mesh{IndexCount: uint32(len(indices)), vertexCount: len(verts)}
	m.Min, m.Max = vertexBounds(verts)
	m.geom.set(keep, verts, indices)
	return m
}

// TestIntersectKeptGeometry casts random rays at meshes that keep their
// geometry in each form and checks every hit, and every miss, is exactly
// what the full vertices gave: two-byte indices for a small mesh, four
// for one past 65536 vertices.
func TestIntersectKeptGeometry(t *testing.T) {
	sphere, sphereIdx := SphereMesh(32, 64)
	grid, gridIdx := gridMesh(300) // 90000 vertices: four-byte indices
	for _, tc := range []struct {
		name    string
		verts   []Vertex
		indices []uint32
		wide    bool
	}{{"sphere", sphere, sphereIdx, false}, {"grid", grid, gridIdx, true}} {
		t.Run(tc.name, func(t *testing.T) {
			full := keptMesh(tc.verts, tc.indices, KeepVertices)
			compact := keptMesh(tc.verts, tc.indices, KeepPositions)
			if wide := compact.geom.idx32 != nil; wide != tc.wide {
				t.Fatalf("four-byte indices = %v, want %v", wide, tc.wide)
			}
			r := rand.New(rand.NewSource(7))
			hits := 0
			for i := range 2000 {
				model := lin.Translate(lin.V3(r.Float32()-0.5, r.Float32()-0.5, r.Float32()-0.5)).
					Mul(lin.Rotate(r.Float32()*6, lin.V3(0.3, 1, 0.2).Norm())).Mul(lin.Scale(lin.V3(1+r.Float32(), 1+r.Float32(), 1+r.Float32())))
				origin := lin.V3(r.Float32()*6-3, r.Float32()*6-3, r.Float32()*6-3)
				ray := Ray{Origin: origin, Dir: lin.V3(r.Float32()-0.5, r.Float32()-0.5, r.Float32()-0.5).Sub(origin.Mul(0.3)).Norm()}
				want, wantOK := referenceIntersect(tc.verts, tc.indices, full.Min, full.Max, model, ray)
				for _, m := range []*Mesh{full, compact} {
					got, ok := m.Intersect(model, ray)
					if ok != wantOK || got != want {
						t.Fatalf("ray %d with %v: got %+v %v, want %+v %v", i, m.geom.keep, got, ok, want, wantOK)
					}
				}
				if wantOK {
					hits++
				}
			}
			if hits < 100 {
				t.Fatalf("only %d of 2000 rays hit; the test needs more", hits)
			}
		})
	}
}

// TestKeptGeometryForms checks what each form keeps and returns: the
// given slices, positions with compact indices, or nothing, and that
// ReleaseGeometry leaves a mesh no picking data.
func TestKeptGeometryForms(t *testing.T) {
	verts, idx := SphereMesh(8, 16)
	full := keptMesh(verts, idx, KeepVertices)
	if got := full.Vertices(); &got[0] != &verts[0] || !slices.Equal(full.Indices(), idx) {
		t.Fatal("KeepVertices does not return the slices it was given")
	}
	compact := keptMesh(verts, idx, KeepPositions)
	got := compact.Vertices()
	for i, v := range got {
		if v != (Vertex{Pos: verts[i].Pos}) {
			t.Fatalf("vertex %d: %+v, want the position alone", i, v)
		}
	}
	if !slices.Equal(compact.Indices(), idx) {
		t.Fatal("compact indices differ")
	}
	if n, want := len(compact.geom.pos)*12+len(compact.geom.idx16)*2, len(verts)*12+len(idx)*2; n != want {
		t.Fatalf("compact geometry is %d bytes, want %d", n, want)
	}
	ray := Ray{Origin: lin.V3(0, 0, 5), Dir: lin.V3(0, 0, -1)}
	if _, ok := compact.Intersect(lin.Identity(), ray); !ok {
		t.Fatal("a ray through the sphere's centre missed it")
	}
	none := keptMesh(verts, idx, KeepNone)
	if none.Vertices() != nil || none.Indices() != nil {
		t.Fatal("KeepNone kept geometry")
	}
	if _, ok := none.Intersect(lin.Identity(), ray); ok {
		t.Fatal("a mesh that keeps nothing reported a hit")
	}
	compact.ReleaseGeometry()
	if _, ok := compact.Intersect(lin.Identity(), ray); ok || compact.Vertices() != nil {
		t.Fatal("ReleaseGeometry kept the geometry")
	}
}
