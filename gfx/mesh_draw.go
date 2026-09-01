package gfx

import (
	"slices"
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
	pbrPipe       *render.Pipeline
	blendPipe     *render.Pipeline
	shadowPipe    *render.Pipeline
	skinPipe      *render.Pipeline
	skinBlendPipe *render.Pipeline
	skinShadow    *render.Pipeline
	jointLayout   *render.StorageSets
	uniformLayout *render.UniformSets // owns the layout the pipelines were built against
	shadow        *render.Target
	shadowSet     vk.VkDescriptorSet
	shadowDesc    *render.DescriptorSets
	shadowSamp    vk.VkSampler
	materials     *render.DescriptorSets
	matSets       map[[4]*Texture]vk.VkDescriptorSet
	flatNormal    *Texture
	black         *Texture
}

const meshStages = vk.VK_SHADER_STAGE_VERTEX_BIT | vk.VK_SHADER_STAGE_FRAGMENT_BIT

var frameUniformsSize = vk.VkDeviceSize(unsafe.Sizeof(frameUniforms{}))

func defaultLight() Light {
	return Light{Direction: lin.V3(-0.5, -1, -0.3), Color: Color{1, 1, 1, 1}, Ambient: Color{0.15, 0.15, 0.18, 1}}
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

func (g *Graphics) initMeshPass() error {
	mp := &g.meshes
	mp.matSets = map[[4]*Texture]vk.VkDescriptorSet{}
	dev := g.r.Device
	var err error
	layout, err := dev.NewUniformSets(frameUniformsSize, meshStages)
	if err != nil {
		return err
	}
	mp.uniformLayout = layout
	if mp.jointLayout, err = dev.NewStorageSets(64, vk.VK_SHADER_STAGE_VERTEX_BIT); err != nil {
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
	if mp.shadow, err = dev.NewTarget(vk.VkExtent2D{Width: shadowMapSize, Height: shadowMapSize}, vk.VK_FORMAT_UNDEFINED, g.r.DepthFormat); err != nil {
		return err
	}
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
	bindings, attrs := meshVertexLayout()
	common := render.PipelineDesc{
		Vert: shaders.PBRVert, Frag: shaders.PBRFrag,
		ColorFormat: hdrFormat, DepthFormat: g.r.DepthFormat,
		Bindings: bindings, Attributes: attrs,
		CullMode: vk.VK_CULL_MODE_BACK_BIT, DepthTest: true, DepthWrite: true,
		SetLayouts: []vk.VkDescriptorSetLayout{mp.materials.Layout, mp.uniformLayout.Layout, mp.shadowDesc.Layout},
	}
	if mp.pbrPipe, err = dev.NewPipeline(common); err != nil {
		return err
	}
	blend := common
	blend.Blend, blend.DepthWrite, blend.CullMode = true, false, vk.VK_CULL_MODE_NONE
	if mp.blendPipe, err = dev.NewPipeline(blend); err != nil {
		return err
	}
	shadow := render.PipelineDesc{
		Vert: shaders.ShadowVert, Frag: shaders.ShadowFrag,
		NoColor: true, DepthFormat: g.r.DepthFormat,
		Bindings: bindings, Attributes: attrs[:7], // the depth pass reads no material attributes
		CullMode: vk.VK_CULL_MODE_NONE, DepthTest: true, DepthWrite: true,
		DepthBias: 1.5, DepthSlopeBias: 2.0,
		SetLayouts: []vk.VkDescriptorSetLayout{mp.materials.Layout, mp.uniformLayout.Layout},
	}
	if mp.shadowPipe, err = dev.NewPipeline(shadow); err != nil {
		return err
	}
	// Skinned variants read joints and weights from binding 0 and joint
	// matrices from a storage buffer in set 3.
	sbind, sattrs := skinVertexLayout()
	skinSets := []vk.VkDescriptorSetLayout{mp.materials.Layout, mp.uniformLayout.Layout, mp.shadowDesc.Layout, mp.jointLayout.Layout}
	skin := common
	skin.Vert, skin.Bindings, skin.Attributes, skin.SetLayouts = shaders.PBRSkinVert, sbind, sattrs, skinSets
	if mp.skinPipe, err = dev.NewPipeline(skin); err != nil {
		return err
	}
	skinBlend := blend
	skinBlend.Vert, skinBlend.Bindings, skinBlend.Attributes, skinBlend.SetLayouts = shaders.PBRSkinVert, sbind, sattrs, skinSets
	if mp.skinBlendPipe, err = dev.NewPipeline(skinBlend); err != nil {
		return err
	}
	skinShadow := shadow
	skinShadow.Vert, skinShadow.Bindings = shaders.ShadowSkinVert, sbind
	skinShadow.Attributes = append(append([]vk.VkVertexInputAttributeDescription{}, sattrs[:7]...), sattrs[9:]...)
	skinShadow.SetLayouts = skinSets
	mp.skinShadow, err = dev.NewPipeline(skinShadow)
	return err
}

// meshVertexLayout is the per-vertex binding 0 and per-instance binding 1.
func meshVertexLayout() ([]vk.VkVertexInputBindingDescription, []vk.VkVertexInputAttributeDescription) {
	bindings := []vk.VkVertexInputBindingDescription{
		{Binding: 0, Stride: vertexSize, InputRate: vk.VK_VERTEX_INPUT_RATE_VERTEX},
		{Binding: 1, Stride: meshInstanceSize, InputRate: vk.VK_VERTEX_INPUT_RATE_INSTANCE},
	}
	attrs := []vk.VkVertexInputAttributeDescription{
		{Location: 0, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 0},
		{Location: 1, Binding: 0, Format: vk.VK_FORMAT_R32G32B32_SFLOAT, Offset: 12},
		{Location: 2, Binding: 0, Format: vk.VK_FORMAT_R32G32_SFLOAT, Offset: 24},
		{Location: 3, Binding: 1, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 0},
		{Location: 4, Binding: 1, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 16},
		{Location: 5, Binding: 1, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 32},
		{Location: 6, Binding: 1, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 48},
		{Location: 7, Binding: 1, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 64},
		{Location: 8, Binding: 1, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 80},
		{Location: 9, Binding: 1, Format: vk.VK_FORMAT_R32G32B32A32_SFLOAT, Offset: 96},
	}
	return bindings, attrs
}

// SetCamera sets the camera for this frame's meshes.
func (g *Graphics) SetCamera(c Camera) { g.cur.camera, g.cur.hasCam = c, true }

// SetLight sets the directional light, ambient term and shadow settings.
func (g *Graphics) SetLight(l Light) { g.cur.light = l }

// AddPointLight adds a point light for this frame (at most 8 are used).
func (g *Graphics) AddPointLight(pos lin.Vec3, c Color, rng float32) {
	if len(g.cur.points) < maxPointLights {
		g.cur.points = append(g.cur.points, pointLight{pos, c, rng})
	}
}

// DrawMesh queues a mesh with a material and a model matrix. Draws that
// share a mesh and material become one instanced draw call; blended
// materials draw after everything opaque, farthest first.
func (g *Graphics) DrawMesh(m *Mesh, mat Material, model lin.Mat4) {
	if mat.BaseColor == (Color{}) {
		mat.BaseColor = White
	}
	if mat.Roughness == 0 {
		mat.Roughness = 0.6
	}
	g.cur.draws = append(g.cur.draws, meshDraw{mesh: m, mat: mat, model: model})
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
		bindings[i] = render.SamplerBinding{View: t.img.View, Sampler: g.sampler(!t.nearest, t.repeat)}
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
func (q *drawQueue) lightMatrix() lin.Mat4 {
	r := q.light.ShadowRadius
	if r <= 0 {
		r = 25
	}
	dir := q.light.Direction.Norm()
	center := q.camera.Target
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

// writeUniforms fills the queue's frame block for the slot.
func (q *drawQueue) writeUniforms(slot int, aspect float32) error {
	if !q.hasCam {
		q.camera = Camera{Position: lin.V3(0, 0, 5)}
	}
	l := q.light
	strength := l.ShadowStrength
	if strength == 0 {
		strength = 1
	}
	u := frameUniforms{
		viewProj:      q.camera.ViewProj(aspect),
		lightViewProj: q.lightMatrix(),
		camPos:        q.camera.Position.Vec4(1),
		lightDir:      l.Direction.Norm().Vec4(0),
		lightColor:    lin.V4(l.Color.R, l.Color.G, l.Color.B, strength),
		ambient:       lin.V4(l.Ambient.R, l.Ambient.G, l.Ambient.B, float32(len(q.points))),
		params:        lin.V4(shadowMapSize, boolFloat(l.Shadows), 0, 0),
	}
	for i, p := range q.points {
		u.pointPos[i] = p.pos.Vec4(p.rng)
		u.pointColor[i] = lin.V4(p.color.R, p.color.G, p.color.B, 1)
	}
	return q.uniforms.Write(slot, unsafe.Slice((*byte)(unsafe.Pointer(&u)), unsafe.Sizeof(u)))
}

func boolFloat(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// prepareDraws resolves material sets, sorts opaque draws for instancing and
// blended draws back to front, and uploads the instance stream.
func (g *Graphics) prepareDraws(q *drawQueue, slot int) (opaque, blended []meshDraw, err error) {
	view := q.camera.viewMatrix()
	for i := range q.draws {
		d := &q.draws[i]
		if d.set, err = g.materialSet(d.mat); err != nil {
			return nil, nil, err
		}
		pos := d.model.MulPoint(d.mesh.Min.Add(d.mesh.Max).Mul(0.5))
		d.depth = -view.MulPoint(pos).Z
	}
	slices.SortStableFunc(q.draws, func(a, b meshDraw) int {
		switch {
		case a.mat.Blend != b.mat.Blend:
			if a.mat.Blend {
				return 1
			}
			return -1
		case a.mat.Blend: // farthest first
			if a.depth > b.depth {
				return -1
			}
			if a.depth < b.depth {
				return 1
			}
			return 0
		case a.skinned != b.skinned:
			if a.skinned {
				return 1
			}
			return -1
		case a.set != b.set:
			if a.set < b.set {
				return -1
			}
			return 1
		case a.mesh != b.mesh:
			if uintptr(unsafe.Pointer(a.mesh)) < uintptr(unsafe.Pointer(b.mesh)) {
				return -1
			}
			return 1
		}
		return 0
	})
	q.inst.reset()
	for _, d := range q.draws {
		m := d.mat
		q.inst.add(meshInstance{
			model:     d.model,
			baseColor: [4]float32{m.BaseColor.R, m.BaseColor.G, m.BaseColor.B, m.BaseColor.A},
			material:  [4]float32{orOne(m.Metallic, m.MetalRoughTexture != nil), m.Roughness, m.Emissive, boolFloat(m.NormalTexture != nil)},
			extra:     [4]float32{float32(d.jointBase), 0, 0, 0},
		})
	}
	if err := q.inst.upload(g.r.Device, slot); err != nil {
		return nil, nil, err
	}
	if len(q.joints) > 0 {
		data := unsafe.Slice((*byte)(unsafe.Pointer(&q.joints[0])), len(q.joints)*64)
		if err := q.jointBuf.Write(slot, data); err != nil {
			return nil, nil, err
		}
	}
	split := len(q.draws)
	for i, d := range q.draws {
		if d.mat.Blend {
			split = i
			break
		}
	}
	return q.draws[:split], q.draws[split:], nil
}

// orOne returns the metallic factor; with a metal-rough texture and a zero
// factor the texture drives it, matching glTF's default factor of 1.
func orOne(metallic float32, hasTexture bool) float32 {
	if hasTexture && metallic == 0 {
		return 1
	}
	return metallic
}

// drawRuns records draws as instanced runs of identical mesh and material.
// first is the index of draws[0] in the instance stream. Skinned draws use
// the skinned variant of pipe and are never merged, since each has its own
// joint matrices.
func (g *Graphics) drawRuns(cb vk.VkCommandBuffer, fr *render.Frame, q *drawQueue, pipe, skinPipe *render.Pipeline, draws []meshDraw, first uint32, withMaterials bool) {
	var offset vk.VkDeviceSize
	vk.VkCmdBindVertexBuffers(cb, 1, 1, &q.inst.buffers[q.inst.slot].Handle, &offset)
	var bound *render.Pipeline
	for i := 0; i < len(draws); {
		d := draws[i]
		run := 1
		if !d.skinned {
			for i+run < len(draws) && !draws[i+run].skinned && draws[i+run].mesh == d.mesh && draws[i+run].set == d.set {
				run++
			}
		}
		p := pipe
		if d.skinned {
			p = skinPipe
		}
		if p != bound {
			bound = p
			vk.VkCmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Handle)
			vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 1, 1, &q.uniforms.Sets[fr.Slot], 0, nil)
			if withMaterials {
				vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 2, 1, &g.meshes.shadowSet, 0, nil)
			}
			if d.skinned {
				vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 3, 1, &q.jointBuf.Sets[fr.Slot], 0, nil)
			}
		}
		if withMaterials {
			vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 0, 1, &d.set, 0, nil)
		}
		vk.VkCmdBindVertexBuffers(cb, 0, 1, &d.mesh.vbuf.Handle, &offset)
		vk.VkCmdBindIndexBuffer(cb, d.mesh.ibuf.Handle, 0, vk.VK_INDEX_TYPE_UINT32)
		vk.VkCmdDrawIndexed(cb, d.mesh.IndexCount, uint32(run), 0, 0, first+uint32(i))
		i += run
	}
}

// renderScene runs the shadow and lit passes of a queue into the targets'
// HDR image.
func (g *Graphics) renderScene(fr *render.Frame, q *drawQueue, t *sceneTargets) error {
	mp := &g.meshes
	cb := fr.CB
	aspect := float32(t.extent.Width) / float32(t.extent.Height)
	if err := q.writeUniforms(fr.Slot, aspect); err != nil {
		return err
	}
	opaque, blended, err := g.prepareDraws(q, fr.Slot)
	if err != nil {
		return err
	}
	if q.light.Shadows {
		render.BeginTargetPass(cb, render.PassDesc{Target: mp.shadow, ClearDepth: 1})
		g.drawRuns(cb, fr, q, mp.shadowPipe, mp.skinShadow, opaque, 0, false)
		render.EndTargetPass(cb, mp.shadow)
	}
	c := q.clear.premultiplied()
	render.BeginTargetPass(cb, render.PassDesc{Target: t.hdr, ClearColor: c, ClearDepth: 1})
	g.drawRuns(cb, fr, q, mp.pbrPipe, mp.skinPipe, opaque, 0, true)
	g.drawRuns(cb, fr, q, mp.blendPipe, mp.skinBlendPipe, blended, uint32(len(opaque)), true)
	render.EndTargetPass(cb, t.hdr)
	return nil
}

func (mp *meshPass) destroy(g *Graphics) {
	dev := g.r.Device.Handle
	for _, p := range []*render.Pipeline{mp.pbrPipe, mp.blendPipe, mp.shadowPipe, mp.skinPipe, mp.skinBlendPipe, mp.skinShadow} {
		if p != nil {
			p.Destroy()
		}
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
	if mp.uniformLayout != nil {
		mp.uniformLayout.Destroy()
	}
	if mp.jointLayout != nil {
		mp.jointLayout.Destroy()
	}
}
