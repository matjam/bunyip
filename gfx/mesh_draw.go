package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

const (
	hdrFormat      = vk.VK_FORMAT_R16G16B16A16_SFLOAT
	shadowMapSize  = 2048
	maxPointLights = 8
)

// meshPass owns the 3D pipelines, targets and per-frame uniforms.
type meshPass struct {
	pbrPipe    *render.Pipeline
	shadowPipe *render.Pipeline
	uniforms   *render.UniformSets
	shadow     *render.Target
	shadowSet  vk.VkDescriptorSet
	shadowDesc *render.DescriptorSets
	shadowSamp vk.VkSampler
	materials  *render.DescriptorSets
	matSets    map[[4]*Texture]vk.VkDescriptorSet
	flatNormal *Texture
	black      *Texture

	draws  []meshDraw
	camera Camera
	light  Light
	hasCam bool
	points []pointLight
}

type pointLight struct {
	pos   lin.Vec3
	color Color
	rng   float32
}

// frameUniforms mirrors the Frame block in pbr.vert (std140).
type frameUniforms struct {
	viewProj      lin.Mat4
	lightViewProj lin.Mat4
	camPos        lin.Vec4
	lightDir      lin.Vec4
	lightColor    lin.Vec4
	ambient       lin.Vec4
	params        lin.Vec4
	pointPos      [maxPointLights]lin.Vec4
	pointColor    [maxPointLights]lin.Vec4
}

// meshPush mirrors the PC block in pbr.vert.
type meshPush struct {
	model     lin.Mat4
	baseColor [4]float32
	material  [4]float32
}

func (g *Graphics) initMeshPass() error {
	mp := &g.meshes
	mp.matSets = map[[4]*Texture]vk.VkDescriptorSet{}
	dev := g.R.Device
	var err error
	if mp.uniforms, err = dev.NewUniformSets(vk.VkDeviceSize(unsafe.Sizeof(frameUniforms{})),
		vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT); err != nil {
		return err
	}
	if mp.materials, err = dev.NewSamplerDescriptors(4, 1024); err != nil {
		return err
	}
	if mp.shadowSamp, err = dev.NewShadowSampler(); err != nil {
		return err
	}
	if mp.shadowDesc, err = dev.NewImmutableSamplerDescriptors(1, 4, mp.shadowSamp); err != nil {
		return err
	}
	if mp.shadow, err = dev.NewTarget(vk.VkExtent2D{Width: shadowMapSize, Height: shadowMapSize}, vk.VK_FORMAT_UNDEFINED, g.R.DepthFormat); err != nil {
		return err
	}
	// Frames without a shadow pass still bind the map, so it must already be
	// in the layout the descriptor promises.
	if err := dev.OneShot(func(cb vk.VkCommandBuffer) { render.ClearDepthForSampling(cb, mp.shadow.Depth) }); err != nil {
		return err
	}
	if mp.shadowSet, err = mp.shadowDesc.Allocate(mp.shadow.Depth.View, mp.shadowSamp); err != nil {
		return err
	}
	if mp.flatNormal, err = g.newTexture(1, 1, []byte{128, 128, 255, 255}, TextureOptions{Data: true}); err != nil {
		return err
	}
	if mp.black, err = g.newTexture(1, 1, []byte{0, 0, 0, 255}, TextureOptions{Data: true}); err != nil {
		return err
	}
	bindings := []vk.VkVertexInputBindingDescription{{Binding: 0, Stride: vertexSize, InputRate: vk.VK_VERTEX_INPUT_RATE_VERTEX}}
	attrs := []vk.VkVertexInputAttributeDescription{
		{Location: 0, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 0},
		{Location: 1, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 12},
		{Location: 2, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 24},
	}
	push := uint32(unsafe.Sizeof(meshPush{}))
	mp.pbrPipe, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PBRVert, Frag: shaders.PBRFrag,
		ColorFormat: hdrFormat, DepthFormat: g.R.DepthFormat,
		Bindings: bindings, Attributes: attrs,
		CullMode: vk.VK_CULL_MODE_BACK_BIT, DepthTest: true, DepthWrite: true,
		PushConstantSize: push,
		SetLayouts:       []vk.VkDescriptorSetLayout{mp.materials.Layout, mp.uniforms.Layout, mp.shadowDesc.Layout},
	})
	if err != nil {
		return err
	}
	mp.shadowPipe, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.ShadowVert, Frag: shaders.ShadowFrag,
		NoColor: true, DepthFormat: g.R.DepthFormat,
		Bindings: bindings, Attributes: attrs,
		CullMode: vk.VK_CULL_MODE_NONE, DepthTest: true, DepthWrite: true,
		DepthBias: 1.5, DepthSlopeBias: 2.0,
		PushConstantSize: push,
		SetLayouts:       []vk.VkDescriptorSetLayout{mp.materials.Layout, mp.uniforms.Layout},
	})
	if err != nil {
		return err
	}
	mp.light = Light{Direction: lin.V3(-0.5, -1, -0.3), Color: Color{1, 1, 1, 1}, Ambient: Color{0.15, 0.15, 0.18, 1}}
	return nil
}

// SetCamera sets the camera for this frame's meshes.
func (g *Graphics) SetCamera(c Camera) { g.meshes.camera, g.meshes.hasCam = c, true }

// SetLight sets the directional light, ambient term and shadow settings.
func (g *Graphics) SetLight(l Light) { g.meshes.light = l }

// AddPointLight adds a point light for this frame (at most 8 are used).
func (g *Graphics) AddPointLight(pos lin.Vec3, c Color, rng float32) {
	if len(g.meshes.points) < maxPointLights {
		g.meshes.points = append(g.meshes.points, pointLight{pos, c, rng})
	}
}

// DrawMesh queues a mesh with a material and a model matrix. Meshes are
// drawn depth-tested and post-processed before any sprites, so 2D always
// overlays 3D.
func (g *Graphics) DrawMesh(m *Mesh, mat Material, model lin.Mat4) {
	if mat.BaseColor == (Color{}) {
		mat.BaseColor = White
	}
	if mat.Roughness == 0 {
		mat.Roughness = 0.6
	}
	g.meshes.draws = append(g.meshes.draws, meshDraw{mesh: m, mat: mat, model: model})
}

// materialSet returns the descriptor set for a material's textures.
func (g *Graphics) materialSet(mat Material) (vk.VkDescriptorSet, error) {
	mp := &g.meshes
	key := [4]*Texture{orTex(mat.Texture, g.white), orTex(mat.MetalRoughTexture, g.white), orTex(mat.NormalTexture, mp.flatNormal), orTex(mat.EmissiveTexture, mp.black)}
	if set, ok := mp.matSets[key]; ok {
		return set, nil
	}
	bindings := make([]render.SamplerBinding, 4)
	for i, t := range key {
		sampler := g.linear
		if t.nearest {
			sampler = g.nearest
		}
		bindings[i] = render.SamplerBinding{View: t.img.View, Sampler: sampler}
	}
	set, err := mp.materials.AllocateMany(bindings)
	if err != nil {
		return 0, err
	}
	mp.matSets[key] = set
	return set, nil
}

func orTex(t, fallback *Texture) *Texture {
	if t == nil {
		return fallback
	}
	return t
}

// forgetTexture drops cached material sets that reference a destroyed texture.
func (g *Graphics) forgetTexture(t *Texture) {
	for key, set := range g.meshes.matSets {
		if key[0] == t || key[1] == t || key[2] == t || key[3] == t {
			g.meshes.materials.Free(set)
			delete(g.meshes.matSets, key)
		}
	}
}

// lightMatrix fits an orthographic shadow frustum around the camera target.
func (mp *meshPass) lightMatrix() lin.Mat4 {
	r := mp.light.ShadowRadius
	if r <= 0 {
		r = 25
	}
	dir := mp.light.Direction.Norm()
	center := mp.camera.Target
	eye := center.Sub(dir.Mul(r * 2))
	up := lin.V3(0, 1, 0)
	if abs32(dir.Y) > 0.95 {
		up = lin.V3(0, 0, 1)
	}
	return lin.Ortho(-r, r, -r, r, 0.1, r*4).Mul(lin.LookAt(eye, center, up))
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// writeUniforms fills the frame block for the slot.
func (mp *meshPass) writeUniforms(slot int, aspect float32) error {
	if !mp.hasCam {
		mp.camera = Camera{Position: lin.V3(0, 0, 5)}
	}
	l := mp.light
	strength := l.ShadowStrength
	if strength == 0 {
		strength = 1
	}
	u := frameUniforms{
		viewProj:      mp.camera.ViewProj(aspect),
		lightViewProj: mp.lightMatrix(),
		camPos:        mp.camera.Position.Vec4(1),
		lightDir:      l.Direction.Norm().Vec4(0),
		lightColor:    lin.V4(l.Color.R, l.Color.G, l.Color.B, strength),
		ambient:       lin.V4(l.Ambient.R, l.Ambient.G, l.Ambient.B, float32(len(mp.points))),
		params:        lin.V4(shadowMapSize, boolFloat(l.Shadows), 0, 0),
	}
	for i, p := range mp.points {
		u.pointPos[i] = p.pos.Vec4(p.rng)
		u.pointColor[i] = lin.V4(p.color.R, p.color.G, p.color.B, 1)
	}
	return mp.uniforms.Write(slot, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u)))
}

func boolFloat(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// drawAll records every queued mesh with the given pipeline.
func (g *Graphics) drawAll(cb vk.VkCommandBuffer, pipe *render.Pipeline, withMaterials bool) error {
	mp := &g.meshes
	var offset vk.VkDeviceSize
	for _, d := range mp.draws {
		if withMaterials {
			set, err := g.materialSet(d.mat)
			if err != nil {
				return err
			}
			vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &set, 0, nil)
		}
		m := d.mat
		push := meshPush{
			model:     d.model,
			baseColor: [4]float32{m.BaseColor.R, m.BaseColor.G, m.BaseColor.B, m.BaseColor.A},
			material:  [4]float32{orOne(m.Metallic, m.MetalRoughTexture != nil), m.Roughness, m.Emissive, boolFloat(m.NormalTexture != nil)},
		}
		vk.VkCmdPushConstants(cb, pipe.Layout, vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT, 0, uint32(unsafe.Sizeof(push)), unsafe.Pointer(&push))
		vk.VkCmdBindVertexBuffers(cb, 0, 1, &d.mesh.vbuf.Handle, &offset)
		vk.VkCmdBindIndexBuffer(cb, d.mesh.ibuf.Handle, 0, vk.VK_INDEX_TYPE_UINT32)
		vk.VkCmdDrawIndexed(cb, d.mesh.IndexCount, 1, 0, 0, 0)
	}
	return nil
}

// orOne returns the metallic factor; with a metal-rough texture and a zero
// factor the texture drives it, matching glTF's default factor of 1.
func orOne(metallic float32, hasTexture bool) float32 {
	if hasTexture && metallic == 0 {
		return 1
	}
	return metallic
}

// renderScene runs the shadow and lit passes into the HDR target.
func (g *Graphics) renderScene(fr *render.Frame) error {
	mp := &g.meshes
	cb := fr.CB
	aspect := float32(fr.Extent.Width) / float32(fr.Extent.Height)
	if err := mp.writeUniforms(fr.Slot, aspect); err != nil {
		return err
	}
	if mp.light.Shadows {
		render.BeginTargetPass(cb, render.PassDesc{Target: mp.shadow, ClearDepth: 1})
		vk.VkCmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.shadowPipe.Handle)
		vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.shadowPipe.Layout, 1, 1, &mp.uniforms.Sets[fr.Slot], 0, nil)
		if err := g.drawAll(cb, mp.shadowPipe, false); err != nil {
			return err
		}
		render.EndTargetPass(cb, mp.shadow)
	}
	c := g.clear.premultiplied()
	render.BeginTargetPass(cb, render.PassDesc{Target: g.post.hdr, ClearColor: c, ClearDepth: 1})
	vk.VkCmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.pbrPipe.Handle)
	vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.pbrPipe.Layout, 1, 1, &mp.uniforms.Sets[fr.Slot], 0, nil)
	vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, mp.pbrPipe.Layout, 2, 1, &mp.shadowSet, 0, nil)
	if err := g.drawAll(cb, mp.pbrPipe, true); err != nil {
		return err
	}
	render.EndTargetPass(cb, g.post.hdr)
	return nil
}

func (mp *meshPass) reset() {
	mp.draws = mp.draws[:0]
	mp.points = mp.points[:0]
	mp.hasCam = false
}

func (mp *meshPass) destroy(g *Graphics) {
	dev := g.R.Device.Handle
	if mp.pbrPipe != nil {
		mp.pbrPipe.Destroy()
	}
	if mp.shadowPipe != nil {
		mp.shadowPipe.Destroy()
	}
	if mp.flatNormal != nil {
		mp.flatNormal.Destroy()
	}
	if mp.black != nil {
		mp.black.Destroy()
	}
	if mp.shadow != nil {
		mp.shadow.Destroy()
	}
	if mp.shadowSamp != 0 {
		vk.VkDestroySampler(dev, mp.shadowSamp, nil)
	}
	if mp.shadowDesc != nil {
		mp.shadowDesc.Destroy()
	}
	if mp.materials != nil {
		mp.materials.Destroy()
	}
	if mp.uniforms != nil {
		mp.uniforms.Destroy()
	}
}
