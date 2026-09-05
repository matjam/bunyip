package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// velocityFormat holds a screen-space motion vector per pixel, in texture
// coordinates, signed and small.
const velocityFormat = vk.VK_FORMAT_R16G16_SFLOAT

// velocityPush is the velocity pass's block: the projection it rasterises
// with, which is the jittered one the scene pass used, and the previous
// frame's projection, which both positions are measured through.
type velocityPush struct {
	viewProj     lin.Mat4
	prevViewProj lin.Mat4
}

// velocityVertexLayout is the velocity programs' own vertex input: the
// position, the model matrix's rows and the previous frame's rows, plus
// the joints for the skinned variant. It declares fewer attributes than
// the lit pass because the programs read nothing else, over the same
// buffers and the same strides.
func velocityVertexLayout(skinned bool) ([]vk.VkVertexInputBindingDescription, []vk.VkVertexInputAttributeDescription) {
	stride := uint32(vertexSize)
	if skinned {
		stride = skinVertexSize
	}
	bindings := []vk.VkVertexInputBindingDescription{
		{Binding: 0, Stride: stride, InputRate: vk.VK_VERTEX_INPUT_RATE_VERTEX},
		{Binding: 1, Stride: meshInstanceSize, InputRate: vk.VK_VERTEX_INPUT_RATE_INSTANCE},
	}
	const f32x4 = vk.VK_FORMAT_R32G32B32A32_SFLOAT
	attrs := []vk.VkVertexInputAttributeDescription{
		{Location: 0, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 0},
		{Location: 1, Binding: 1, Format: f32x4, Offset: 0},
		{Location: 2, Binding: 1, Format: f32x4, Offset: 16},
		{Location: 3, Binding: 1, Format: f32x4, Offset: 32},
		{Location: 4, Binding: 1, Format: f32x4, Offset: 192},
		{Location: 5, Binding: 1, Format: f32x4, Offset: 208},
		{Location: 6, Binding: 1, Format: f32x4, Offset: 224},
	}
	if skinned {
		attrs = append(attrs,
			vk.VkVertexInputAttributeDescription{Location: 7, Binding: 1, Format: f32x4, Offset: 80}, // extra, x = joint base
			vk.VkVertexInputAttributeDescription{Location: 8, Binding: 0, Format: vk.VK_FORMAT_R8G8B8A8_UINT, Offset: 44},
			vk.VkVertexInputAttributeDescription{Location: 9, Binding: 0, Format: f32x4, Offset: 48},
		)
	}
	return bindings, attrs
}

// initVelocity builds the two velocity pipelines. They test depth against
// the scene pass's buffer without writing it, so a moved mesh behind
// something else leaves no motion vector on top of it.
func (g *Graphics) initVelocity() error {
	p := &g.post
	dev := g.r.Device
	desc := render.PipelineDesc{
		Vert: shaders.VelocityVert, Frag: shaders.VelocityFrag,
		ColorFormat: velocityFormat, DepthFormat: g.r.DepthFormat,
		CullMode: vk.VK_CULL_MODE_BACK_BIT, DepthTest: true,
		PushConstantSize: uint32(unsafe.Sizeof(velocityPush{})),
	}
	desc.Bindings, desc.Attributes = velocityVertexLayout(false)
	var err error
	if p.velocity, err = dev.NewPipeline(desc); err != nil {
		return err
	}
	desc.Vert = shaders.VelocitySkinVert
	desc.Bindings, desc.Attributes = velocityVertexLayout(true)
	desc.SetLayouts = []vk.VkDescriptorSetLayout{g.meshes.jointLayout.Layout}
	p.velocitySkin, err = dev.NewPipeline(desc)
	return err
}

// renderVelocity draws the moved meshes into the velocity image. A draw
// the game gave no previous transform leaves zero there, which is what
// the resolve passes take to mean "moved with the camera alone".
func (g *Graphics) renderVelocity(cb vk.VkCommandBuffer, fr *render.Frame, q *drawQueue, t *sceneTargets, draws drawList) {
	p := &g.post
	p.vel = velocityPush{viewProj: q.viewProjJ, prevViewProj: q.prevViewProj}
	pass := render.PassDesc{Target: &t.velPass, LoadDepth: true}
	render.BeginTargetPass(cb, pass)
	rec := &g.rec
	rec.offset = 0
	if q.hasMoved && q.inst.buffers[q.inst.slot] != nil {
		vk.CmdBindVertexBuffers(cb, 1, 1, &q.inst.buffers[q.inst.slot].Handle, &rec.offset)
		var bound *render.Pipeline
		for i := range draws.len() {
			d := draws.at(i)
			if !d.moved {
				continue
			}
			pipe := p.velocity
			if d.skinned {
				pipe = p.velocitySkin
			}
			if pipe != bound {
				bound = pipe
				vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
				// A pipeline switch changes the layout, so the block is
				// pushed again with it.
				vk.CmdPushConstants(cb, pipe.Layout, meshStages, 0, uint32(unsafe.Sizeof(p.vel)), unsafe.Pointer(&p.vel))
				if d.skinned {
					vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &q.jointBuf.Sets[fr.Slot], 0, nil)
				}
			}
			vk.CmdBindVertexBuffers(cb, 0, 1, &d.mesh.vbuf.Handle, &rec.offset)
			vk.CmdBindIndexBuffer(cb, d.mesh.ibuf.Handle, 0, vk.VK_INDEX_TYPE_UINT32)
			vk.CmdDrawIndexed(cb, d.mesh.IndexCount, 1, 0, 0, uint32(i))
			g.stats.Draws3D++
		}
	}
	render.EndTargetPassDesc(cb, pass)
}
