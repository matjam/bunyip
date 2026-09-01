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
}

// ModelPart is one primitive placed by one node.
type ModelPart struct {
	Mesh     *Mesh
	Material Material
	World    lin.Mat4
}

// LoadModel uploads a parsed glTF document.
func (g *Graphics) LoadModel(doc *gltf.Document) (*Model, error) {
	m := &Model{}
	m.Min, m.Max = doc.Bounds()
	for _, img := range doc.Images {
		tex, err := g.NewTexture(img, TextureOptions{Linear: true})
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
				verts := make([]Vertex, len(p.Positions))
				for i := range verts {
					verts[i] = Vertex{Pos: p.Positions[i], Normal: p.Normals[i], UV: p.UVs[i]}
				}
				var err error
				if mesh, err = g.NewMesh(verts, p.Indices); err != nil {
					m.Destroy()
					return nil, err
				}
				uploaded[k] = mesh
				m.meshes = append(m.meshes, mesh)
			}
			mat := Material{BaseColor: White}
			if p.Material >= 0 {
				src := doc.Materials[p.Material]
				mat.BaseColor = Color{src.BaseColor[0], src.BaseColor[1], src.BaseColor[2], src.BaseColor[3]}
				if src.Image >= 0 && src.Image < len(m.textures) {
					mat.Texture = m.textures[src.Image]
				}
			}
			m.Parts = append(m.Parts, ModelPart{Mesh: mesh, Material: mat, World: inst.World})
		}
	}
	return m, nil
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
