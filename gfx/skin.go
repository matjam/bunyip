package gfx

import (
	"fmt"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// SkinVertex is a vertex with up to four joint influences.
type SkinVertex struct {
	Pos     lin.Vec3
	Normal  lin.Vec3
	UV      lin.Vec2
	Joints  [4]uint8
	Weights [4]float32
}

const skinVertexSize = 52

// NewSkinnedMesh uploads skinned geometry; draw it with DrawSkinned or
// through an animated model.
func (g *Graphics) NewSkinnedMesh(verts []SkinVertex, indices []uint32) (*Mesh, error) {
	if len(verts) == 0 || len(indices) == 0 || len(indices)%3 != 0 {
		return nil, fmt.Errorf("gfx: skinned mesh needs vertices and a whole number of triangles")
	}
	plain := make([]Vertex, len(verts))
	for i, v := range verts {
		plain[i] = Vertex{Pos: v.Pos, Normal: v.Normal, UV: v.UV}
	}
	m, err := g.newMesh(plain, indices, unsafe.Slice((*byte)(unsafe.Pointer(&verts[0])), len(verts)*skinVertexSize))
	if err != nil {
		return nil, err
	}
	m.skinned = true
	return m, nil
}

// skinVertexLayout is the skinned binding 0 plus the shared instance binding.
func skinVertexLayout() ([]vk.VkVertexInputBindingDescription, []vk.VkVertexInputAttributeDescription) {
	bindings, attrs := meshVertexLayout()
	bindings[0].Stride = skinVertexSize
	attrs = append(attrs,
		vk.VkVertexInputAttributeDescription{Location: 10, Binding: 0, Format: vk.VK_FORMAT_R8G8B8A8_UINT, Offset: 32},
		vk.VkVertexInputAttributeDescription{Location: 11, Binding: 0, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 36},
	)
	return bindings, attrs
}

// DrawSkinned draws a skinned mesh with explicit joint matrices (one per
// joint, already multiplied by the inverse bind matrices).
func (g *Graphics) DrawSkinned(m *Mesh, mat Material, model lin.Mat4, joints []lin.Mat4) {
	if !m.skinned || len(joints) == 0 {
		g.DrawMesh(m, mat, model)
		return
	}
	if mat.BaseColor == (Color{}) {
		mat.BaseColor = White
	}
	if mat.Roughness == 0 {
		mat.Roughness = 0.6
	}
	q := g.cur
	base := len(q.joints)
	q.joints = append(q.joints, joints...)
	q.draws = append(q.draws, meshDraw{mesh: m, mat: mat, model: model, jointBase: base, skinned: true})
}
