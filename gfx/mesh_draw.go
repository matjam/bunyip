package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// meshPass owns the 3D pipeline and per-frame uniforms.
type meshPass struct {
	pipe     *render.Pipeline
	uniforms *render.UniformSets
	draws    []meshDraw
	camera   Camera
	light    Light
	hasCam   bool
}

func (g *Graphics) initMeshPass() error {
	mp := &g.meshes
	var err error
	if mp.uniforms, err = g.R.Device.NewUniformSets(vk.VkDeviceSize(unsafe.Sizeof(frameUniforms{})),
		vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT); err != nil {
		return err
	}
	bindings := []vk.VkVertexInputBindingDescription{{Binding: 0, Stride: vertexSize, InputRate: vk.VK_VERTEX_INPUT_RATE_VERTEX}}
	attrs := []vk.VkVertexInputAttributeDescription{
		{Location: 0, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 0},
		{Location: 1, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 12},
		{Location: 2, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 24},
	}
	mp.pipe, err = g.R.Device.NewPipeline(render.PipelineDesc{
		Vert: shaders.MeshVert, Frag: shaders.MeshFrag,
		ColorFormat: g.R.Swapchain.Format, DepthFormat: g.R.DepthFormat,
		Bindings: bindings, Attributes: attrs,
		CullMode:         vk.VK_CULL_MODE_BACK_BIT,
		DepthTest:        true,
		DepthWrite:       true,
		PushConstantSize: uint32(unsafe.Sizeof(meshPush{})),
		SetLayouts:       []vk.VkDescriptorSetLayout{g.descriptors.Layout, mp.uniforms.Layout},
	})
	if err != nil {
		mp.uniforms.Destroy()
		return err
	}
	mp.light = Light{Direction: lin.V3(-0.5, -1, -0.3), Color: Color{1, 1, 1, 1}, Ambient: Color{0.15, 0.15, 0.18, 1}}
	return nil
}

// SetCamera sets the camera for this frame's meshes.
func (g *Graphics) SetCamera(c Camera) { g.meshes.camera, g.meshes.hasCam = c, true }

// SetLight sets the directional light and ambient term.
func (g *Graphics) SetLight(l Light) { g.meshes.light = l }

// DrawMesh queues a mesh with a material and a model matrix. Meshes are
// drawn depth-tested before any sprites, so 2D always overlays 3D.
func (g *Graphics) DrawMesh(m *Mesh, mat Material, model lin.Mat4) {
	if mat.BaseColor == (Color{}) {
		mat.BaseColor = White
	}
	g.meshes.draws = append(g.meshes.draws, meshDraw{mesh: m, mat: mat, model: model})
}

func (g *Graphics) flushMeshes(fr *render.Frame) error {
	mp := &g.meshes
	if len(mp.draws) == 0 {
		return nil
	}
	if !mp.hasCam {
		mp.camera = Camera{Position: lin.V3(0, 0, 5)}
	}
	aspect := float32(fr.Extent.Width) / float32(fr.Extent.Height)
	u := frameUniforms{
		viewProj:   mp.camera.ViewProj(aspect),
		camPos:     mp.camera.Position.Vec4(1),
		lightDir:   mp.light.Direction.Norm().Vec4(0),
		lightColor: lin.V4(mp.light.Color.R, mp.light.Color.G, mp.light.Color.B, 1),
		ambient:    lin.V4(mp.light.Ambient.R, mp.light.Ambient.G, mp.light.Ambient.B, 1),
	}
	if err := mp.uniforms.Write(fr.Slot, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u))); err != nil {
		return err
	}
	cb := fr.CB
	vk.VkCmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.pipe.Handle)
	vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.pipe.Layout, 1, 1, &mp.uniforms.Sets[fr.Slot], 0, nil)
	var offset vk.VkDeviceSize
	for _, d := range mp.draws {
		tex := d.mat.Texture
		if tex == nil {
			tex = g.white
		}
		vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.pipe.Layout, 0, 1, &tex.set, 0, nil)
		push := meshPush{model: d.model, baseColor: [4]float32{d.mat.BaseColor.R, d.mat.BaseColor.G, d.mat.BaseColor.B, d.mat.BaseColor.A}}
		vk.VkCmdPushConstants(cb, mp.pipe.Layout, vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT, 0, uint32(unsafe.Sizeof(push)), unsafe.Pointer(&push))
		vk.VkCmdBindVertexBuffers(cb, 0, 1, &d.mesh.vbuf.Handle, &offset)
		vk.VkCmdBindIndexBuffer(cb, d.mesh.ibuf.Handle, 0, vk.VK_INDEX_TYPE_UINT32)
		vk.VkCmdDrawIndexed(cb, d.mesh.IndexCount, 1, 0, 0, 0)
	}
	return nil
}

func (mp *meshPass) reset() {
	mp.draws = mp.draws[:0]
	mp.hasCam = false
}

func (mp *meshPass) destroy() {
	if mp.pipe != nil {
		mp.pipe.Destroy()
	}
	if mp.uniforms != nil {
		mp.uniforms.Destroy()
	}
}
