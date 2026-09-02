package gfx

import (
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
	m := &Model{nodes: doc.Nodes, skins: doc.Skins, clips: doc.Animations}
	m.Min, m.Max = doc.Bounds()
	m.order = topoOrder(doc.Nodes)
	for i, img := range doc.Images {
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
		for pi, p := range doc.Meshes[inst.Mesh].Primitives {
			k := key{inst.Mesh, pi}
			mesh, ok := uploaded[k]
			if !ok {
				var err error
				if p.Skinned() && inst.Skin >= 0 {
					verts := make([]SkinVertex, len(p.Positions))
					for i := range verts {
						verts[i] = SkinVertex{Pos: p.Positions[i], Normal: p.Normals[i], UV: p.UVs[i], Joints: p.Joints[i], Weights: p.Weights[i]}
					}
					mesh, err = g.NewSkinnedMesh(verts, p.Indices)
				} else {
					verts := make([]Vertex, len(p.Positions))
					for i := range verts {
						verts[i] = Vertex{Pos: p.Positions[i], Normal: p.Normals[i], UV: p.UVs[i]}
					}
					mesh, err = g.NewMesh(verts, p.Indices)
				}
				if err != nil {
					m.Destroy()
					return nil, err
				}
				uploaded[k] = mesh
				m.meshes = append(m.meshes, mesh)
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
				mat.DoubleSided = src.DoubleSided
				mat.Unlit = src.Unlit
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
	return m, nil
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
	for _, mesh := range m.meshes {
		mesh.Destroy()
	}
	for _, t := range m.textures {
		t.Destroy()
	}
	m.meshes, m.textures, m.Parts = nil, nil, nil
}
