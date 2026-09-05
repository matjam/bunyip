package gfx

import (
	"fmt"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// SkinVertex is a Vertex with up to four joint influences.
type SkinVertex struct {
	Pos     lin.Vec3
	Normal  lin.Vec3
	UV      lin.Vec2
	UV2     lin.Vec2
	Color   Color // zero means white
	Joints  [4]uint8
	Weights [4]float32
}

// gpuSkinVertex is the skinned vertex as the GPU reads it.
type gpuSkinVertex struct {
	gpuVertex
	joints  [4]uint8
	weights [4]float32
}

const skinVertexSize = 64

// NewSkinnedMesh uploads skinned geometry; draw it with DrawSkinned or
// through an animated model.
func (g *Graphics) NewSkinnedMesh(verts []SkinVertex, indices []uint32) (*Mesh, error) {
	if len(verts) == 0 || len(indices) == 0 || len(indices)%3 != 0 {
		return nil, fmt.Errorf("gfx: skinned mesh needs vertices and a whole number of triangles")
	}
	plain, packed := packSkin(verts)
	m, err := g.newMesh(plain, indices, packed)
	if err != nil {
		return nil, err
	}
	m.skinned = true
	m.setJointBounds(verts)
	g.trackMesh(m, skinVertexSize)
	return m, nil
}

// UpdateSkinned replaces a skinned mesh's geometry, as Update does for a
// plain mesh: draws already queued this frame keep the old geometry. It
// is what morph targets on a skinned model go through.
func (m *Mesh) UpdateSkinned(verts []SkinVertex, indices []uint32) error {
	if !m.skinned {
		return fmt.Errorf("gfx: UpdateSkinned on a mesh that is not skinned")
	}
	if m.vbuf == nil || m.destroyed {
		return fmt.Errorf("gfx: update of a destroyed mesh")
	}
	if len(verts) == 0 {
		return fmt.Errorf("gfx: mesh needs vertices")
	}
	plain, packed := packSkin(verts)
	fresh, err := m.g.newMesh(plain, indices, packed)
	if err != nil {
		return err
	}
	m.retire()
	m.vbuf, m.ibuf = fresh.vbuf, fresh.ibuf
	m.IndexCount, m.verts, m.indices = fresh.IndexCount, fresh.verts, fresh.indices
	if !m.boundsFixed {
		m.Min, m.Max = fresh.Min, fresh.Max
	}
	m.setJointBounds(verts)
	m.g.trackMesh(m, skinVertexSize)
	return nil
}

// packSkin splits skinned vertices into the plain vertices kept for
// picking and the bytes the GPU reads.
func packSkin(verts []SkinVertex) ([]Vertex, []byte) {
	plain := make([]Vertex, len(verts))
	packed := make([]gpuSkinVertex, len(verts))
	for i, v := range verts {
		plain[i] = Vertex{Pos: v.Pos, Normal: v.Normal, UV: v.UV, UV2: v.UV2, Color: v.Color}
		packed[i] = gpuSkinVertex{gpuVertex: plain[i].gpu(), joints: v.Joints, weights: v.Weights}
	}
	return plain, unsafe.Slice((*byte)(unsafe.Pointer(&packed[0])), len(packed)*skinVertexSize)
}

// skinVertexLayout is the skinned binding 0 plus the shared instance binding.
func skinVertexLayout() ([]vk.VkVertexInputBindingDescription, []vk.VkVertexInputAttributeDescription) {
	bindings, attrs := meshVertexLayout()
	bindings[0].Stride = skinVertexSize
	// The instance stream holds locations 5 to 16, so the joints follow it.
	attrs = append(attrs,
		vk.VkVertexInputAttributeDescription{Location: 17, Binding: 0, Format: vk.VK_FORMAT_R8G8B8A8_UINT, Offset: 44},
		vk.VkVertexInputAttributeDescription{Location: 18, Binding: 0, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 48},
	)
	return bindings, attrs
}

// DrawSkinned draws a skinned mesh with explicit joint matrices (one per
// joint, already multiplied by the inverse bind matrices).
func (g *Graphics) DrawSkinned(m *Mesh, mat Material, model lin.Mat4, joints []lin.Mat4) {
	g.DrawSkinnedMoved(m, mat, model, model, joints)
}

// DrawSkinnedMoved is DrawSkinned for a mesh that moved: prev is the
// model matrix it was drawn with last frame. The motion vectors it
// produces carry the model matrix's motion only, not the pose's, so a
// character walking across the screen reprojects correctly while an arm
// swinging in place does not.
func (g *Graphics) DrawSkinnedMoved(m *Mesh, mat Material, model, prev lin.Mat4, joints []lin.Mat4) {
	if !m.skinned || len(joints) == 0 {
		g.DrawMeshMoved(m, mat, model, prev)
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
	g.queueMesh(meshDraw{mesh: m, mat: mat, model: model, prev: prev, moved: prev != model,
		jointBase: base, jointCount: len(joints), skinned: true})
}
