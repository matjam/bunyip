package gfx

import (
	"fmt"
	"image"
	"image/color"
	"slices"

	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

// Model is a glTF document uploaded to the GPU: one Mesh per primitive,
// one Texture per image, and the placements to draw.
type Model struct {
	Parts    []ModelPart
	Min, Max lin.Vec3
	meshes   []*Mesh
	textures []*Texture
	nodes    []gltf.Node
	skins    []gltf.Skin
	clips    []gltf.Animation
	order    []int // node indices, parents before children
	morphs   []*morphMesh
	g        *Graphics
}

// morphMesh is a primitive with morph targets: the rest geometry stays on
// the CPU and a weighted blend of the targets is uploaded when the
// weights change.
type morphMesh struct {
	mesh    *Mesh
	node    int
	names   []string
	targets []gltf.MorphTarget
	base    []Vertex     // rest vertices of a plain mesh
	skin    []SkinVertex // rest vertices of a skinned mesh
	indices []uint32
	rest    []float32 // the file's default weights
	weights []float32 // the weights last uploaded
}

// apply blends the targets by the weights and uploads the result when
// the weights differ from the last upload.
func (mm *morphMesh) apply(weights []float32) error {
	if len(weights) == len(mm.weights) && slices.Equal(weights, mm.weights) {
		return nil
	}
	mm.weights = append(mm.weights[:0], weights...)
	blend := func(i int, pos, normal lin.Vec3) (lin.Vec3, lin.Vec3) {
		for ti, w := range weights {
			if ti >= len(mm.targets) || w == 0 {
				continue
			}
			t := mm.targets[ti]
			pos = pos.Add(t.Positions[i].Mul(w))
			if t.Normals != nil {
				normal = normal.Add(t.Normals[i].Mul(w))
			}
		}
		return pos, normal.Norm()
	}
	if mm.skin != nil {
		verts := make([]SkinVertex, len(mm.skin))
		for i, v := range mm.skin {
			v.Pos, v.Normal = blend(i, v.Pos, v.Normal)
			verts[i] = v
		}
		return mm.mesh.UpdateSkinned(verts, mm.indices)
	}
	verts := make([]Vertex, len(mm.base))
	for i, v := range mm.base {
		v.Pos, v.Normal = blend(i, v.Pos, v.Normal)
		verts[i] = v
	}
	return mm.mesh.Update(verts, mm.indices)
}

// NodeCount is the number of nodes in the model's hierarchy.
func (m *Model) NodeCount() int { return len(m.nodes) }

// NodeName returns a node's name; an unknown index gives "".
func (m *Model) NodeName(node int) string {
	if node < 0 || node >= len(m.nodes) {
		return ""
	}
	return m.nodes[node].Name
}

// NodeIndex returns the index of the first node with the name, or -1.
func (m *Model) NodeIndex(name string) int {
	for i, n := range m.nodes {
		if n.Name == name {
			return i
		}
	}
	return -1
}

// NodeParent returns a node's parent index, or -1 for a root.
func (m *Model) NodeParent(node int) int {
	if node < 0 || node >= len(m.nodes) {
		return -1
	}
	return m.nodes[node].Parent
}

// NodeMatrix returns a node's rest-pose world matrix in model space, for
// a socket on a model that is not animated: a lamp's bulb, a turret's
// muzzle. An animated model's current pose comes from AnimPlayer.NodeMatrix.
// An unknown index gives the identity.
func (m *Model) NodeMatrix(node int) lin.Mat4 {
	if node < 0 || node >= len(m.nodes) {
		return lin.Identity()
	}
	mat := m.nodes[node].Local()
	for p := m.nodes[node].Parent; p >= 0 && p < len(m.nodes); p = m.nodes[p].Parent {
		mat = m.nodes[p].Local().Mul(mat)
	}
	return mat
}

// NodePosition returns a node's rest-pose position in model space.
func (m *Model) NodePosition(node int) lin.Vec3 {
	mat := m.NodeMatrix(node)
	return lin.V3(mat[12], mat[13], mat[14])
}

// AnimMask is the set of nodes an animation layer affects, one flag per
// node; nil means every node. Build one with MaskNodes or MaskSubtree.
type AnimMask []bool

// MaskNodes makes a mask of exactly the named nodes; unknown names are
// ignored.
func (m *Model) MaskNodes(names ...string) AnimMask {
	mask := make(AnimMask, len(m.nodes))
	for _, name := range names {
		if i := m.NodeIndex(name); i >= 0 {
			mask[i] = true
		}
	}
	return mask
}

// MaskSubtree makes a mask of the named nodes and everything under them:
// "Spine1" for the upper body, "Head" for the head and its children.
func (m *Model) MaskSubtree(names ...string) AnimMask {
	mask := make(AnimMask, len(m.nodes))
	for _, name := range names {
		if i := m.NodeIndex(name); i >= 0 {
			m.markSubtree(mask, i, 0)
		}
	}
	return mask
}

func (m *Model) markSubtree(mask AnimMask, node, depth int) {
	if node < 0 || node >= len(m.nodes) || depth > 64 {
		return
	}
	mask[node] = true
	for _, c := range m.nodes[node].Children {
		m.markSubtree(mask, c, depth+1)
	}
}

// MorphTargets names the morph targets of the node's mesh, blank where
// the file names none; nil when the node has no morph targets.
func (m *Model) MorphTargets(node int) []string {
	for _, mm := range m.morphs {
		if mm.node == node {
			return mm.names
		}
	}
	return nil
}

// MorphWeights returns the morph target weights the node's mesh was last
// uploaded with; nil when the node has no morph targets.
func (m *Model) MorphWeights(node int) []float32 {
	for _, mm := range m.morphs {
		if mm.node == node {
			return mm.weights
		}
	}
	return nil
}

// SetMorphWeights blends the node's morph targets by the weights (one per
// target, 0 for none and 1 for the full shape) and uploads the result:
// a facial expression, a wind-bent plant. A player's weights channels do
// the same through DrawModelAnimated. Blending runs on the CPU and
// costs one pass over the mesh's vertices per target with a non-zero
// weight, plus the upload, each time the weights change; unchanged
// weights cost nothing.
func (m *Model) SetMorphWeights(node int, weights []float32) error {
	found := false
	for _, mm := range m.morphs {
		if mm.node != node {
			continue
		}
		found = true
		if err := mm.apply(weights); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("gfx: node %d has no morph targets", node)
	}
	return nil
}

// ModelPart is one primitive placed by one node.
type ModelPart struct {
	Mesh     *Mesh
	Material Material
	World    lin.Mat4
	node     int
	skin     int
}

// LoadModel uploads a parsed glTF document.
func (g *Graphics) LoadModel(doc *gltf.Document) (*Model, error) {
	m := &Model{nodes: doc.Nodes, skins: doc.Skins, clips: doc.Animations, g: g}
	m.Min, m.Max = doc.Bounds()
	m.order = topoOrder(doc.Nodes)
	// glTF keeps thickness in a texture's green channel; the material
	// reads red, so those images are swizzled once here.
	thickness := map[int]bool{}
	for _, mat := range doc.Materials {
		if mat.ThicknessImage >= 0 {
			thickness[mat.ThicknessImage] = true
		}
	}
	for i, img := range doc.Images {
		if thickness[i] {
			img = greenToRed(img)
		}
		tex, err := g.NewTexture(img, TextureOptions{Linear: true, Data: doc.IsDataImage(i)})
		if err != nil {
			m.Destroy()
			return nil, err
		}
		m.textures = append(m.textures, tex)
	}
	type key struct{ mesh, prim int }
	uploaded := map[key]*Mesh{}
	for _, inst := range doc.Instances {
		src := doc.Meshes[inst.Mesh]
		for pi, p := range src.Primitives {
			k := key{inst.Mesh, pi}
			mesh, ok := uploaded[k]
			// A primitive with morph targets is uploaded once per instance,
			// since each instance blends its own copy.
			if !ok || len(p.Targets) > 0 {
				var err error
				vertex := func(i int) Vertex {
					v := Vertex{Pos: p.Positions[i], Normal: p.Normals[i], UV: p.UVs[i]}
					if p.UVs2 != nil {
						v.UV2 = p.UVs2[i]
					}
					if p.Colors != nil {
						c := p.Colors[i]
						v.Color = Color{c.X, c.Y, c.Z, c.W}
					}
					return v
				}
				var mm *morphMesh
				if len(p.Targets) > 0 {
					mm = &morphMesh{node: inst.Node, targets: p.Targets, indices: p.Indices, names: make([]string, len(p.Targets))}
					copy(mm.names, src.TargetNames)
				}
				if p.Skinned() && inst.Skin >= 0 {
					verts := make([]SkinVertex, len(p.Positions))
					for i := range verts {
						v := vertex(i)
						verts[i] = SkinVertex{Pos: v.Pos, Normal: v.Normal, UV: v.UV, UV2: v.UV2, Color: v.Color, Joints: p.Joints[i], Weights: p.Weights[i]}
					}
					mesh, err = g.NewSkinnedMesh(verts, p.Indices)
					if mm != nil {
						mm.skin = verts
					}
				} else {
					verts := make([]Vertex, len(p.Positions))
					for i := range verts {
						verts[i] = vertex(i)
					}
					mesh, err = g.NewMesh(verts, p.Indices)
					if mm != nil {
						mm.base = verts
					}
				}
				if err != nil {
					m.Destroy()
					return nil, err
				}
				uploaded[k] = mesh
				m.meshes = append(m.meshes, mesh)
				if mm != nil {
					mm.mesh = mesh
					m.morphs = append(m.morphs, mm)
					// The upload is the rest geometry: every weight zero.
					mm.rest = m.restWeights(inst.Node, src)
					mm.weights = make([]float32, len(mm.rest))
					if err := mm.apply(mm.rest); err != nil {
						m.Destroy()
						return nil, err
					}
				}
			}
			mat := Material{BaseColor: White, Roughness: 0.6}
			if p.Material >= 0 {
				src := doc.Materials[p.Material]
				mat.BaseColor = Color{src.BaseColor[0], src.BaseColor[1], src.BaseColor[2], src.BaseColor[3]}
				mat.Metallic, mat.Roughness = src.Metallic, max(src.Roughness, 0.04)
				mat.Texture = m.texture(src.Image)
				mat.MetalRoughTexture = m.texture(src.MetalRoughImage)
				mat.NormalTexture = m.texture(src.NormalImage)
				mat.EmissiveTexture = m.texture(src.EmissiveImage)
				mat.Emissive = max(src.Emissive[0], src.Emissive[1], src.Emissive[2])
				mat.OcclusionTexture = m.texture(src.OcclusionImage)
				mat.OcclusionStrength = src.OcclusionStrength
				mat.OcclusionUV2 = src.OcclusionUV2
				mat.DoubleSided = src.DoubleSided
				mat.Unlit = src.Unlit
				mat.Clearcoat, mat.ClearcoatRoughness = src.Clearcoat, src.ClearcoatRoughness
				mat.Sheen = Color{src.SheenColor[0], src.SheenColor[1], src.SheenColor[2], 1}
				if src.SheenColor == [3]float32{} {
					mat.Sheen = Color{}
				}
				mat.SheenRoughness = src.SheenRoughness
				if src.Transmission > 0 {
					mat.Transmission, mat.IOR, mat.Thickness = src.Transmission, src.IOR, src.Thickness
					mat.AttenuationDistance = src.AttenuationDistance
					mat.AttenuationColor = Color{src.AttenuationColor[0], src.AttenuationColor[1], src.AttenuationColor[2], 1}
					mat.TransmissionTexture = m.texture(src.TransmissionImage)
					mat.ThicknessTexture = m.texture(src.ThicknessImage)
				}
				if src.UVOffset != [2]float32{} || src.UVRotation != 0 || src.UVScale != [2]float32{1, 1} {
					// glTF: uv' = T · R · S · uv.
					mat.UVTransform = lin.Translate2(src.UVOffset[0], src.UVOffset[1]).Mul(lin.Rotate2(-src.UVRotation)).Mul(lin.Scale2(src.UVScale[0], src.UVScale[1]))
				}
				switch src.AlphaMode {
				case gltf.AlphaMask:
					mat.AlphaCutoff = src.AlphaCutoff
				case gltf.AlphaBlend:
					mat.Blend = true
				}
			}
			m.Parts = append(m.Parts, ModelPart{Mesh: mesh, Material: mat, World: inst.World, node: inst.Node, skin: inst.Skin})
		}
	}
	g.track(m, Resource{Kind: ResourceModel, Parts: len(m.Parts)})
	return m, nil
}

// restWeights returns the morph weights a node starts with: its own,
// else its mesh's defaults, else zeros, one per target.
func (m *Model) restWeights(node int, mesh gltf.Mesh) []float32 {
	w := make([]float32, mesh.TargetCount())
	if node >= 0 && node < len(m.nodes) && len(m.nodes[node].Weights) > 0 {
		copy(w, m.nodes[node].Weights)
	} else {
		copy(w, mesh.Weights)
	}
	return w
}

// greenToRed copies an image with its green channel in red, for glTF
// thickness maps, which store thickness in green.
func greenToRed(src image.Image) image.Image {
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, g, _, _ := src.At(x, y).RGBA()
			v := uint8(g >> 8)
			out.SetRGBA(x-b.Min.X, y-b.Min.Y, color.RGBA{v, v, v, 255})
		}
	}
	return out
}

func (m *Model) texture(i int) *Texture {
	if i < 0 || i >= len(m.textures) {
		return nil
	}
	return m.textures[i]
}

// topoOrder lists nodes so that every parent precedes its children.
func topoOrder(nodes []gltf.Node) []int {
	order := make([]int, 0, len(nodes))
	var visit func(int, int)
	visit = func(i, depth int) {
		if i < 0 || i >= len(nodes) || depth > 64 {
			return
		}
		order = append(order, i)
		for _, c := range nodes[i].Children {
			visit(c, depth+1)
		}
	}
	for i, n := range nodes {
		if n.Parent < 0 {
			visit(i, 0)
		}
	}
	return order
}

// DrawModel queues every part of the model under a world transform.
func (g *Graphics) DrawModel(m *Model, world lin.Mat4) {
	for _, p := range m.Parts {
		g.DrawMesh(p.Mesh, p.Material, world.Mul(p.World))
	}
}

// Destroy frees the model's meshes and textures.
func (m *Model) Destroy() {
	if m.g != nil {
		m.g.forget(m)
	}
	for _, mesh := range m.meshes {
		mesh.Destroy()
	}
	for _, t := range m.textures {
		t.Destroy()
	}
	m.meshes, m.textures, m.Parts, m.morphs = nil, nil, nil, nil
}
