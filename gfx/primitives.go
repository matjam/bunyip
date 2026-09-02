package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// QuadMesh returns a unit square in the xy plane centred on the origin,
// facing +z, its UVs running from the top-left: the shape of a billboard
// or a flat sprite in a 3D scene.
func QuadMesh() ([]Vertex, []uint32) {
	n := lin.V3(0, 0, 1)
	return []Vertex{
		{Pos: lin.V3(-0.5, 0.5, 0), Normal: n, UV: lin.V2(0, 0)},
		{Pos: lin.V3(0.5, 0.5, 0), Normal: n, UV: lin.V2(1, 0)},
		{Pos: lin.V3(0.5, -0.5, 0), Normal: n, UV: lin.V2(1, 1)},
		{Pos: lin.V3(-0.5, -0.5, 0), Normal: n, UV: lin.V2(0, 1)},
	}, []uint32{0, 2, 1, 0, 3, 2}
}

// PlaneMesh returns a unit square in the xz plane centred on the origin,
// facing +y, divided into segments by segments quads so a vertex shader
// can ripple it: ground, water, a tabletop. UVs run 0..1 across it.
func PlaneMesh(segments int) ([]Vertex, []uint32) {
	segments = max(segments, 1)
	heights := make([]float32, (segments+1)*(segments+1))
	return HeightfieldMesh(heights, segments+1, segments+1, 1/float32(segments))
}

// HeightfieldMesh returns terrain from a grid of heights, cols across (x)
// by rows deep (z), cell world units apart and centred on the origin,
// with smooth normals and UVs running 0..1 across the grid. Heights are
// read row by row, so heights[z*cols+x] is the height at column x of row
// z. Pair it with a MeshShape collider of the same vertices.
func HeightfieldMesh(heights []float32, cols, rows int, cell float32) ([]Vertex, []uint32) {
	if cols < 2 || rows < 2 || len(heights) < cols*rows {
		return nil, nil
	}
	verts := make([]Vertex, 0, cols*rows)
	w, d := float32(cols-1)*cell, float32(rows-1)*cell
	for z := range rows {
		for x := range cols {
			verts = append(verts, Vertex{
				Pos: lin.V3(float32(x)*cell-w/2, heights[z*cols+x], float32(z)*cell-d/2),
				UV:  lin.V2(float32(x)/float32(cols-1), float32(z)/float32(rows-1)),
			})
		}
	}
	idx := make([]uint32, 0, (cols-1)*(rows-1)*6)
	for z := range rows - 1 {
		for x := range cols - 1 {
			a := uint32(z*cols + x)
			b, c, e := a+1, a+uint32(cols), a+uint32(cols)+1
			idx = append(idx, a, e, b, a, c, e)
		}
	}
	ComputeNormals(verts, idx)
	return verts, idx
}

// CylinderMesh returns a cylinder of radius 1 from y = -1 to y = 1 with
// flat caps, in segments around: pillars, barrels, wheels, the shape of
// a capsule collider's middle.
func CylinderMesh(segments int) ([]Vertex, []uint32) {
	segments = max(segments, 3)
	var verts []Vertex
	var idx []uint32
	// The side, with a seam vertex so the UVs wrap.
	for s := 0; s <= segments; s++ {
		a := 2 * math.Pi * float64(s) / float64(segments)
		n := lin.V3(float32(math.Cos(a)), 0, float32(math.Sin(a)))
		u := float32(s) / float32(segments)
		verts = append(verts, Vertex{Pos: n.Add(lin.V3(0, 1, 0)), Normal: n, UV: lin.V2(u, 0)}, Vertex{Pos: n.Add(lin.V3(0, -1, 0)), Normal: n, UV: lin.V2(u, 1)})
	}
	for s := range segments {
		a := uint32(2 * s)
		idx = append(idx, a, a+2, a+1, a+1, a+2, a+3)
	}
	for _, y := range []float32{1, -1} {
		centre := uint32(len(verts))
		n := lin.V3(0, y, 0)
		verts = append(verts, Vertex{Pos: n, Normal: n, UV: lin.V2(0.5, 0.5)})
		for s := 0; s <= segments; s++ {
			a := 2 * math.Pi * float64(s) / float64(segments)
			c, sn := float32(math.Cos(a)), float32(math.Sin(a))
			verts = append(verts, Vertex{Pos: lin.V3(c, y, sn), Normal: n, UV: lin.V2(0.5+c/2, 0.5+sn/2)})
		}
		for s := range segments {
			a := centre + 1 + uint32(s)
			if y > 0 {
				idx = append(idx, centre, a+1, a)
			} else {
				idx = append(idx, centre, a, a+1)
			}
		}
	}
	return verts, idx
}

// ConeMesh returns a cone of radius 1 at y = -1 rising to a point at
// y = 1, in segments around: spikes, trees, arrow heads, spot light
// gizmos.
func ConeMesh(segments int) ([]Vertex, []uint32) {
	segments = max(segments, 3)
	var verts []Vertex
	var idx []uint32
	slant := float32(1 / math.Sqrt2)
	for s := range segments {
		a0 := 2 * math.Pi * float64(s) / float64(segments)
		a1 := 2 * math.Pi * float64(s+1) / float64(segments)
		am := (a0 + a1) / 2
		p0 := lin.V3(float32(math.Cos(a0)), -1, float32(math.Sin(a0)))
		p1 := lin.V3(float32(math.Cos(a1)), -1, float32(math.Sin(a1)))
		n0 := lin.V3(float32(math.Cos(a0))*slant, slant, float32(math.Sin(a0))*slant)
		n1 := lin.V3(float32(math.Cos(a1))*slant, slant, float32(math.Sin(a1))*slant)
		nm := lin.V3(float32(math.Cos(am))*slant, slant, float32(math.Sin(am))*slant)
		base := uint32(len(verts))
		verts = append(verts,
			Vertex{Pos: lin.V3(0, 1, 0), Normal: nm, UV: lin.V2(float32(s)/float32(segments)+0.5/float32(segments), 0)},
			Vertex{Pos: p0, Normal: n0, UV: lin.V2(float32(s)/float32(segments), 1)},
			Vertex{Pos: p1, Normal: n1, UV: lin.V2(float32(s+1)/float32(segments), 1)})
		idx = append(idx, base, base+2, base+1)
	}
	centre := uint32(len(verts))
	down := lin.V3(0, -1, 0)
	verts = append(verts, Vertex{Pos: down, Normal: down, UV: lin.V2(0.5, 0.5)})
	for s := 0; s <= segments; s++ {
		a := 2 * math.Pi * float64(s) / float64(segments)
		c, sn := float32(math.Cos(a)), float32(math.Sin(a))
		verts = append(verts, Vertex{Pos: lin.V3(c, -1, sn), Normal: down, UV: lin.V2(0.5+c/2, 0.5+sn/2)})
	}
	for s := range segments {
		a := centre + 1 + uint32(s)
		idx = append(idx, centre, a, a+1)
	}
	return verts, idx
}

// CapsuleMesh returns a capsule of radius 1 whose straight middle runs
// from y = -halfHeight to y = halfHeight, with rings across each cap and
// segments around: the shape of a Capsule collider and of most
// characters' bodies.
func CapsuleMesh(rings, segments int, halfHeight float32) ([]Vertex, []uint32) {
	rings, segments = max(rings, 2), max(segments, 3)
	halfHeight = max(halfHeight, 0)
	var verts []Vertex
	total := 2 + 2*halfHeight
	// Rows of latitude: the top hemisphere raised by halfHeight, then the
	// bottom one lowered by it, so the rows either side of the equator
	// span the straight middle.
	ring := func(phi float64, off float32) {
		y := float32(math.Cos(phi))
		for s := 0; s <= segments; s++ {
			theta := 2 * math.Pi * float64(s) / float64(segments)
			n := lin.V3(float32(math.Sin(phi)*math.Cos(theta)), y, float32(math.Sin(phi)*math.Sin(theta)))
			p := n.Add(lin.V3(0, off, 0))
			verts = append(verts, Vertex{Pos: p, Normal: n, UV: lin.V2(float32(s)/float32(segments), (1+halfHeight-p.Y)/total)})
		}
	}
	for r := 0; r <= rings; r++ {
		ring(math.Pi/2*float64(r)/float64(rings), halfHeight)
	}
	for r := 0; r <= rings; r++ {
		ring(math.Pi/2+math.Pi/2*float64(r)/float64(rings), -halfHeight)
	}
	var idx []uint32
	stride := uint32(segments + 1)
	rows := 2 * (rings + 1)
	for r := 0; r < rows-1; r++ {
		for s := 0; s < segments; s++ {
			a := uint32(r)*stride + uint32(s)
			b := a + stride
			idx = append(idx, a, a+1, b, a+1, b+1, b)
		}
	}
	return verts, idx
}

// TorusMesh returns a ring of radius 1 with a tube of radius tube, in
// rings around the ring and segments around the tube: rings, tyres,
// selection circles.
func TorusMesh(tube float32, rings, segments int) ([]Vertex, []uint32) {
	rings, segments = max(rings, 3), max(segments, 3)
	var verts []Vertex
	for r := 0; r <= rings; r++ {
		a := 2 * math.Pi * float64(r) / float64(rings)
		centre := lin.V3(float32(math.Cos(a)), 0, float32(math.Sin(a)))
		for s := 0; s <= segments; s++ {
			b := 2 * math.Pi * float64(s) / float64(segments)
			n := centre.Mul(float32(math.Cos(b))).Add(lin.V3(0, float32(math.Sin(b)), 0))
			verts = append(verts, Vertex{Pos: centre.Add(n.Mul(tube)), Normal: n, UV: lin.V2(float32(r)/float32(rings), float32(s)/float32(segments))})
		}
	}
	var idx []uint32
	stride := uint32(segments + 1)
	for r := 0; r < rings; r++ {
		for s := 0; s < segments; s++ {
			a := uint32(r)*stride + uint32(s)
			b := a + stride
			idx = append(idx, a, a+1, b, a+1, b+1, b)
		}
	}
	return verts, idx
}

// ComputeNormals sets every vertex's normal to the average of its
// triangles' face normals, for geometry built without them: terrain,
// marching cubes, meshes edited in code.
func ComputeNormals(verts []Vertex, indices []uint32) {
	for i := range verts {
		verts[i].Normal = lin.Vec3{}
	}
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := indices[i], indices[i+1], indices[i+2]
		if int(a) >= len(verts) || int(b) >= len(verts) || int(c) >= len(verts) {
			continue
		}
		n := verts[b].Pos.Sub(verts[a].Pos).Cross(verts[c].Pos.Sub(verts[a].Pos))
		verts[a].Normal = verts[a].Normal.Add(n)
		verts[b].Normal = verts[b].Normal.Add(n)
		verts[c].Normal = verts[c].Normal.Add(n)
	}
	for i := range verts {
		if verts[i].Normal.Len() > 0 {
			verts[i].Normal = verts[i].Normal.Norm()
		} else {
			verts[i].Normal = lin.V3(0, 1, 0)
		}
	}
}

// FlatShaded returns a copy of a mesh with no shared vertices, each
// triangle's vertices carrying its face normal: the faceted look of
// low-poly art and voxel worlds.
func FlatShaded(verts []Vertex, indices []uint32) ([]Vertex, []uint32) {
	out := make([]Vertex, 0, len(indices))
	idx := make([]uint32, 0, len(indices))
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := verts[indices[i]], verts[indices[i+1]], verts[indices[i+2]]
		n := b.Pos.Sub(a.Pos).Cross(c.Pos.Sub(a.Pos))
		if n.Len() > 0 {
			n = n.Norm()
		}
		a.Normal, b.Normal, c.Normal = n, n, n
		base := uint32(len(out))
		out = append(out, a, b, c)
		idx = append(idx, base, base+1, base+2)
	}
	return out, idx
}

// TransformVertices returns a copy of the vertices moved by a matrix,
// normals turned with it, for placing parts before merging them.
func TransformVertices(verts []Vertex, m lin.Mat4) []Vertex {
	out := make([]Vertex, len(verts))
	nm := m.NormalMatrix()
	for i, v := range verts {
		v.Pos = m.MulPoint(v.Pos)
		v.Normal = nm.MulVec(v.Normal)
		if v.Normal.Len() > 0 {
			v.Normal = v.Normal.Norm()
		}
		out[i] = v
	}
	return out
}

// AppendMesh adds a second mesh's geometry to the first, offsetting its
// indices, so a chunk, a building or a compound shape becomes one mesh
// and one draw.
func AppendMesh(verts []Vertex, indices []uint32, moreVerts []Vertex, moreIndices []uint32) ([]Vertex, []uint32) {
	base := uint32(len(verts))
	verts = append(verts, moreVerts...)
	for _, i := range moreIndices {
		indices = append(indices, base+i)
	}
	return verts, indices
}
