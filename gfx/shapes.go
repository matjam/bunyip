package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// CubeMesh returns a unit cube centred on the origin with flat normals and
// a full UV square on each face.
func CubeMesh() ([]Vertex, []uint32) {
	type face struct{ n, u, v lin.Vec3 }
	faces := []face{
		{lin.V3(0, 0, 1), lin.V3(1, 0, 0), lin.V3(0, 1, 0)},
		{lin.V3(0, 0, -1), lin.V3(-1, 0, 0), lin.V3(0, 1, 0)},
		{lin.V3(1, 0, 0), lin.V3(0, 0, -1), lin.V3(0, 1, 0)},
		{lin.V3(-1, 0, 0), lin.V3(0, 0, 1), lin.V3(0, 1, 0)},
		{lin.V3(0, 1, 0), lin.V3(1, 0, 0), lin.V3(0, 0, -1)},
		{lin.V3(0, -1, 0), lin.V3(1, 0, 0), lin.V3(0, 0, 1)},
	}
	var verts []Vertex
	var idx []uint32
	for _, f := range faces {
		base := uint32(len(verts))
		c := f.n.Mul(0.5)
		for _, uv := range [][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
			p := c.Add(f.u.Mul(0.5 * uv[0])).Add(f.v.Mul(0.5 * uv[1]))
			verts = append(verts, Vertex{Pos: p, Normal: f.n, UV: lin.V2((uv[0]+1)/2, (1-uv[1])/2)})
		}
		idx = append(idx, base, base+1, base+2, base, base+2, base+3)
	}
	return verts, idx
}

// SphereMesh returns a UV sphere of radius 1 with the given resolution.
func SphereMesh(rings, segments int) ([]Vertex, []uint32) {
	var verts []Vertex
	for r := 0; r <= rings; r++ {
		phi := math.Pi * float64(r) / float64(rings)
		for s := 0; s <= segments; s++ {
			theta := 2 * math.Pi * float64(s) / float64(segments)
			n := lin.V3(float32(math.Sin(phi)*math.Cos(theta)), float32(math.Cos(phi)), float32(math.Sin(phi)*math.Sin(theta)))
			verts = append(verts, Vertex{Pos: n, Normal: n, UV: lin.V2(float32(s)/float32(segments), float32(r)/float32(rings))})
		}
	}
	var idx []uint32
	for r := 0; r < rings; r++ {
		for s := 0; s < segments; s++ {
			a := uint32(r*(segments+1) + s)
			b := a + uint32(segments) + 1
			idx = append(idx, a, a+1, b, a+1, b+1, b)
		}
	}
	return verts, idx
}
