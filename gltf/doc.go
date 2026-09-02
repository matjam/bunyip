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
	Meshes     []Mesh
	Materials  []Material
	Images     []image.Image
	Instances  []Instance // every mesh placement in the default scene, flattened
	Nodes      []Node     // the node hierarchy, for animation
	Skins      []Skin
	Animations []Animation
}

// Node is one node of the hierarchy with its rest-pose local transform.
type Node struct {
	Name        string
	Parent      int // -1 for a root
	Children    []int
	Translation lin.Vec3
	Rotation    lin.Quat
	Scale       lin.Vec3
	Mesh        int // -1 none
	Skin        int // -1 none
}

// Local returns the node's rest-pose local matrix.
func (n Node) Local() lin.Mat4 { return lin.TRS(n.Translation, n.Rotation, n.Scale) }

// Skin binds joints (node indices) with their inverse bind matrices.
type Skin struct {
	Name        string
	Joints      []int
	InverseBind []lin.Mat4
}

// Animation is a named clip of node channels.
type Animation struct {
	Name     string
	Duration float32
	Channels []Channel
}

// ChannelPath says which node property a channel animates.
type ChannelPath uint8

const (
	PathTranslation ChannelPath = iota
	PathRotation
	PathScale
)

// Channel is one animated node property with keyframes.
type Channel struct {
	Node   int
	Path   ChannelPath
	Times  []float32
	Values []lin.Vec4 // xyz for translation and scale, xyzw for rotation
	Step   bool       // STEP interpolation; otherwise linear (cubic falls back to linear)
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
	Material  int        // index into Document.Materials, or -1
	Joints    [][4]uint8 // per vertex, when skinned
	Weights   [][4]float32
}

// Skinned reports whether the primitive carries joint weights.
func (p *Primitive) Skinned() bool { return len(p.Joints) == len(p.Positions) && len(p.Positions) > 0 }

// Material is the metallic-roughness material.
type Material struct {
	Name      string
	BaseColor [4]float32 // linear RGBA factor
	Image     int        // albedo image index into Document.Images, or -1
	Linear    bool       // sampler asks for linear filtering

	Metallic          float32 // factor; default 1
	Roughness         float32 // factor; default 1
	MetalRoughImage   int     // G roughness, B metallic; -1 none
	NormalImage       int     // -1 none
	EmissiveImage     int     // -1 none
	Emissive          [3]float32
	OcclusionImage    int     // R occlusion; -1 none
	OcclusionStrength float32 // default 1

	AlphaMode   AlphaMode
	AlphaCutoff float32 // for AlphaMask; default 0.5
	DoubleSided bool
	Unlit       bool // KHR_materials_unlit: draw the base colour without lighting
}

// AlphaMode is how a material's alpha is used.
type AlphaMode uint8

const (
	AlphaOpaque AlphaMode = iota
	AlphaMask             // fragments below AlphaCutoff are discarded
	AlphaBlend            // alpha-blended
)

// IsDataImage reports whether an image holds non-colour data (metallic-
// roughness or normals) and must not be decoded as sRGB.
func (d *Document) IsDataImage(i int) bool {
	for _, m := range d.Materials {
		if m.MetalRoughImage == i || m.NormalImage == i || m.OcclusionImage == i {
			return true
		}
	}
	return false
}

// Instance places a mesh in the world.
type Instance struct {
	Name  string
	Mesh  int
	Node  int // node index, for animation
	Skin  int // -1 when not skinned
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
