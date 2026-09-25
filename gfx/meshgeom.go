package gfx

import (
	"math"

	"github.com/matjam/bunyip/lin"
)

// KeepGeometry is how much of a mesh's geometry stays in main memory after
// it is uploaded, for Intersect, AddOccluder3D, Vertices and Indices. The
// GPU's copy is what draws; this one only answers questions on the
// processor.
type KeepGeometry uint8

const (
	// KeepVertices keeps the vertex and index slices the mesh was given,
	// every field as uploaded, which is what Vertices then returns. It
	// refers to the slices rather than copying them, so it costs nothing
	// while the caller keeps them too, and 60 bytes a vertex once only the
	// mesh does. It is what NewMesh does and the zero value.
	KeepVertices KeepGeometry = iota
	// KeepPositions keeps a copy of each vertex's position, 12 bytes, and
	// of the triangle indices, two bytes each while the mesh has at most
	// 65536 vertices and four beyond that: all that picking and occlusion
	// read. Vertices then returns the positions with the other fields
	// zero. The meshes of a Model, a skinned mesh and a Terrain's chunks
	// keep this.
	KeepPositions
	// KeepNone keeps nothing, for meshes a game never picks: Intersect
	// reports no hit, AddOccluder3D ignores the mesh, and Vertices and
	// Indices return nil.
	KeepNone
)

// MeshOptions are the choices NewMeshWith takes. The zero value is what
// NewMesh uses.
type MeshOptions struct {
	// Keep is the geometry the mesh keeps in main memory; the zero value
	// is KeepVertices.
	Keep KeepGeometry
}

// meshGeometry is what a mesh keeps of its geometry in main memory.
type meshGeometry struct {
	keep  KeepGeometry
	full  []Vertex   // KeepVertices: the slice the mesh was given
	pos   []lin.Vec3 // KeepPositions: each vertex's position
	idx16 []uint16   // KeepPositions with at most 65536 vertices
	idx32 []uint32   // KeepVertices: the slice the mesh was given; KeepPositions beyond 65536 vertices: a copy
}

// set replaces the kept geometry, reusing the storage the last geometry
// had when it is large enough, so a mesh updated every frame with the
// same size allocates nothing here.
func (k *meshGeometry) set(keep KeepGeometry, verts []Vertex, indices []uint32) {
	k.keep = keep
	switch keep {
	case KeepNone:
		*k = meshGeometry{keep: KeepNone}
	case KeepVertices:
		k.full, k.idx32 = verts, indices
		k.pos, k.idx16 = nil, nil
	default:
		k.full = nil
		k.pos = grow(k.pos, len(verts))
		for i, v := range verts {
			k.pos[i] = v.Pos
		}
		if len(verts) <= math.MaxUint16+1 {
			k.idx16 = grow(k.idx16, len(indices))
			for i, x := range indices {
				k.idx16[i] = uint16(x)
			}
			k.idx32 = nil
		} else {
			k.idx32 = grow(k.idx32, len(indices))
			copy(k.idx32, indices)
			k.idx16 = nil
		}
	}
}

// vertexCount is how many vertices are kept.
func (k *meshGeometry) vertexCount() int {
	if k.keep == KeepVertices {
		return len(k.full)
	}
	return len(k.pos)
}

// indexCount is how many indices are kept.
func (k *meshGeometry) indexCount() int {
	if k.idx16 != nil {
		return len(k.idx16)
	}
	return len(k.idx32)
}

// index returns the i'th kept index.
func (k *meshGeometry) index(i int) uint32 {
	if k.idx16 != nil {
		return uint32(k.idx16[i])
	}
	return k.idx32[i]
}

// position returns the kept position of vertex i.
func (k *meshGeometry) position(i uint32) lin.Vec3 {
	if k.keep == KeepVertices {
		return k.full[i].Pos
	}
	return k.pos[i]
}

// vertices is what Mesh.Vertices returns: the slice the mesh was given,
// or a new one built from the kept positions.
func (k *meshGeometry) vertices() []Vertex {
	switch {
	case k.keep == KeepVertices:
		return k.full
	case len(k.pos) == 0:
		return nil
	}
	out := make([]Vertex, len(k.pos))
	for i, p := range k.pos {
		out[i].Pos = p
	}
	return out
}

// indices is what Mesh.Indices returns: the slice the mesh was given or
// kept, or a new one widened from the two-byte indices.
func (k *meshGeometry) indices() []uint32 {
	if k.idx16 == nil {
		return k.idx32
	}
	out := make([]uint32, len(k.idx16))
	for i, x := range k.idx16 {
		out[i] = uint32(x)
	}
	return out
}

// intersect finds the nearest triangle a ray in mesh space meets, at a
// positive distance along d. The triangles are tested in index order
// with rayTriangle whatever the kept form, so every form finds the same
// hit.
func (k *meshGeometry) intersect(o, d lin.Vec3) (best float32, normal lin.Vec3, found bool) {
	switch {
	case k.keep == KeepVertices:
		return intersectFull(k.full, k.idx32, o, d)
	case k.idx16 != nil:
		return intersectIndexed(k.pos, k.idx16, o, d)
	default:
		return intersectIndexed(k.pos, k.idx32, o, d)
	}
}

// intersectIndexed is intersect over kept positions and indices of
// either width.
func intersectIndexed[I uint16 | uint32](pos []lin.Vec3, idx []I, o, d lin.Vec3) (float32, lin.Vec3, bool) {
	best := float32(math.MaxFloat32)
	var bestN lin.Vec3
	found := false
	for i := 0; i+2 < len(idx); i += 3 {
		a, b, c := pos[idx[i]], pos[idx[i+1]], pos[idx[i+2]]
		if t, ok := rayTriangle(o, d, a, b, c); ok && t < best && t > 0 {
			best, found = t, true
			bestN = b.Sub(a).Cross(c.Sub(a))
		}
	}
	return best, bestN, found
}

// intersectFull is intersect over the vertices a mesh was given.
func intersectFull(verts []Vertex, idx []uint32, o, d lin.Vec3) (float32, lin.Vec3, bool) {
	best := float32(math.MaxFloat32)
	var bestN lin.Vec3
	found := false
	for i := 0; i+2 < len(idx); i += 3 {
		a, b, c := verts[idx[i]].Pos, verts[idx[i+1]].Pos, verts[idx[i+2]].Pos
		if t, ok := rayTriangle(o, d, a, b, c); ok && t < best && t > 0 {
			best, found = t, true
			bestN = b.Sub(a).Cross(c.Sub(a))
		}
	}
	return best, bestN, found
}
