// Package gltf loads glTF 2.0 models (.gltf with external or embedded
// buffers, and .glb) into plain Go slices: positions, normals, UVs,
// vertex colours, joints and weights, indices, materials with their
// textures and the KHR extensions the renderer supports (clearcoat,
// sheen, transmission, volume, emissive strength, texture transforms),
// decoded images, morph targets with their default weights, skins,
// animation clips (node transforms and morph weights) and the flattened
// node hierarchy with world matrices. Sparse accessors, which store only
// the elements a morph target moves, decode over the base data or over
// zeros when the accessor has no buffer view.
//
// Load reads a file, Parse a byte slice; both return a Document. The
// package has no GPU dependency, so a tool or a server can read models
// too; gfx.LoadModel uploads a Document into meshes and materials, and
// phys can take a mesh's triangles as a static collider. Parse errors
// name the offending buffer, accessor or image.
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
	// Weights are the node's own morph target weights, overriding the
	// mesh's defaults; nil means the mesh's.
	Weights []float32
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
	PathWeights // the morph target weights of the node's mesh
)

// Channel is one animated node property with keyframes.
type Channel struct {
	Node   int
	Path   ChannelPath
	Times  []float32
	Values []lin.Vec4 // xyz for translation and scale, xyzw for rotation; nil for weights
	// Weights holds a PathWeights channel's keys: one weight per morph
	// target for each time, in time order.
	Weights []float32
	Step    bool // STEP interpolation; otherwise linear (cubic falls back to linear)
}

// WeightCount is the number of morph target weights per key of a
// PathWeights channel; zero for other paths.
func (c *Channel) WeightCount() int {
	if len(c.Times) == 0 {
		return 0
	}
	return len(c.Weights) / len(c.Times)
}

// Mesh is one glTF mesh: a set of primitives drawn together.
type Mesh struct {
	Name       string
	Primitives []Primitive
	// Weights are the default morph target weights, one per target; nil
	// means every target at zero.
	Weights []float32
	// TargetNames names the morph targets when the file carries them in
	// the mesh's extras; nil otherwise.
	TargetNames []string
}

// TargetCount is the number of morph targets, from the first primitive.
func (m *Mesh) TargetCount() int {
	if len(m.Primitives) == 0 {
		return 0
	}
	return len(m.Primitives[0].Targets)
}

// MorphTarget is one blend shape of a primitive: offsets added to each
// vertex's position, and to its normal when the target has normals,
// scaled by the target's weight.
type MorphTarget struct {
	Positions []lin.Vec3 // one per vertex
	Normals   []lin.Vec3 // nil when the target has none
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
	Colors    []lin.Vec4 // COLOR_0, linear RGBA; nil when the file has none
	UVs2      []lin.Vec2 // TEXCOORD_1; nil when the file has none
	Targets   []MorphTarget
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

	OcclusionUV2 bool // the occlusion texture uses TEXCOORD_1
	// UVOffset, UVRotation and UVScale are the base colour texture's
	// KHR_texture_transform; Scale is 1,1 without one.
	UVOffset   [2]float32
	UVRotation float32
	UVScale    [2]float32

	Clearcoat           float32    // KHR_materials_clearcoat factor
	ClearcoatRoughness  float32    // default 0
	SheenColor          [3]float32 // KHR_materials_sheen; zero for none
	SheenRoughness      float32
	Transmission        float32    // KHR_materials_transmission factor; zero for opaque
	TransmissionImage   int        // R scales the factor; -1 none
	IOR                 float32    // KHR_materials_ior; default 1.5
	Thickness           float32    // KHR_materials_volume thickness factor
	ThicknessImage      int        // G scales the thickness, as glTF stores it; -1 none
	AttenuationDistance float32    // zero for no absorption
	AttenuationColor    [3]float32 // default white

	// Specular is KHR_materials_specular: SpecularFactor scales a
	// dielectric's reflection (default 1) and SpecularColor tints it
	// (default white). SpecularImage holds the strength in its alpha and
	// SpecularColorImage the tint in its RGB; each is -1 when the file
	// gives none.
	SpecularFactor     float32
	SpecularColor      [3]float32
	SpecularImage      int
	SpecularColorImage int

	// Iridescence is KHR_materials_iridescence, a thin film over the
	// surface: IridescenceFactor is its strength (default 0),
	// IridescenceIOR its index of refraction (default 1.3) and the film
	// is between IridescenceThicknessMin and IridescenceThicknessMax
	// nanometres thick (defaults 100 and 400). IridescenceImage scales
	// the strength by its red channel and IridescenceThicknessImage
	// places the thickness between the two by its green channel; each is
	// -1 when the file gives none.
	IridescenceFactor         float32
	IridescenceIOR            float32
	IridescenceThicknessMin   float32
	IridescenceThicknessMax   float32
	IridescenceImage          int
	IridescenceThicknessImage int

	// Anisotropy is KHR_materials_anisotropy, a highlight stretched along
	// the surface: AnisotropyStrength is how far (default 0) and
	// AnisotropyRotation which way, in radians. AnisotropyImage holds a
	// direction in red and green and a strength in blue, or -1.
	AnisotropyStrength float32
	AnisotropyRotation float32
	AnisotropyImage    int

	// SpecGloss reports that the material came from
	// KHR_materials_pbrSpecularGlossiness, which the loader converts to
	// metallic-roughness: the factors above are the converted ones.
	// SpecGlossImage is the extension's specular-glossiness image, whose
	// alpha is the glossiness, or -1; the renderer turns it into a
	// metallic-roughness map. Its RGB, the specular colour per texel, is
	// not used, so a file whose specular colour varies across one
	// material loads with the converted factors alone.
	SpecGloss      bool
	SpecGlossImage int
	// Glossiness is the extension's glossiness factor, which scales the
	// image's alpha; 1 without the extension.
	Glossiness float32
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
		if m.MetalRoughImage == i || m.NormalImage == i || m.OcclusionImage == i || m.TransmissionImage == i || m.ThicknessImage == i ||
			m.IridescenceImage == i || m.IridescenceThicknessImage == i || m.AnisotropyImage == i || m.SpecularImage == i {
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
