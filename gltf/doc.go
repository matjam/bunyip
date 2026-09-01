// Package gltf loads glTF 2.0 models (.gltf with external or embedded
// buffers, and .glb) into plain Go slices: positions, normals, UVs, indices,
// materials, decoded images and the flattened node hierarchy with world
// matrices. It has no GPU dependency; gfx.LoadModel uploads a Document.
package gltf

import (
	"image"

	"github.com/matjam/bunyip/lin"
)

// Document is a decoded model.
type Document struct {
	Meshes    []Mesh
	Materials []Material
	Images    []image.Image
	Instances []Instance // every mesh placement in the default scene, flattened
}

// Mesh is one glTF mesh: a set of primitives drawn together.
type Mesh struct {
	Name       string
	Primitives []Primitive
}

// Primitive is a triangle list with one material.
type Primitive struct {
	Positions []lin.Vec3
	Normals   []lin.Vec3 // computed from the triangles when the file has none
	UVs       []lin.Vec2 // zero-filled when the file has none
	Indices   []uint32
	Material  int // index into Document.Materials, or -1
}

// Material is the subset of the PBR material the renderer uses.
type Material struct {
	Name      string
	BaseColor [4]float32 // linear RGBA factor
	Image     int        // index into Document.Images, or -1
	Linear    bool       // sampler asks for linear filtering
}

// Instance places a mesh in the world.
type Instance struct {
	Name  string
	Mesh  int
	World lin.Mat4
}

// Bounds returns the axis-aligned box around every placed primitive.
func (d *Document) Bounds() (lo, hi lin.Vec3) {
	first := true
	for _, inst := range d.Instances {
		for _, p := range d.Meshes[inst.Mesh].Primitives {
			for _, pos := range p.Positions {
				w := inst.World.MulPoint(pos)
				if first {
					lo, hi, first = w, w, false
					continue
				}
				lo = lin.V3(min(lo.X, w.X), min(lo.Y, w.Y), min(lo.Z, w.Z))
				hi = lin.V3(max(hi.X, w.X), max(hi.Y, w.Y), max(hi.Z, w.Z))
			}
		}
	}
	return lo, hi
}
