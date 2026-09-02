package gfx

import (
	"math"
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
	shadowCascades = 3
	maxPointLights = 8
)

// meshPass owns the 3D pipelines, targets and per-frame uniforms.
type meshPass struct {
	defaultShader *Shader // the standard material, whose pipelines are its variants
	shadowPipe    *render.Pipeline
	skinShadow    *render.Pipeline
	jointLayout   *render.StorageSets
	uniformLayout *render.UniformSets // owns the layout the pipelines were built against
	shadow        [shadowCascades]*render.Target
	shadowSet     vk.VkDescriptorSet
	shadowDesc    *render.DescriptorSets
	shadowSamp    vk.VkSampler
	materials     *render.DescriptorSets // four material textures plus a shader's image0..3
	matSets       map[[8]*Texture]vk.VkDescriptorSet
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
	view          lin.Mat4
	lightViewProj [shadowCascades]lin.Mat4
	camPos        lin.Vec4
	lightDir      lin.Vec4
	lightColor    lin.Vec4
	sky           lin.Vec4
	ground        lin.Vec4
	params        lin.Vec4
	splits        lin.Vec4
	radii         lin.Vec4
	pointPos      [maxPointLights]lin.Vec4
	pointColor    [maxPointLights]lin.Vec4
}

func (g *Graphics) initMeshPass() error {
	mp := &g.meshes
	mp.matSets = map[[8]*Texture]vk.VkDescriptorSet{}
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
	if mp.materials, err = dev.NewSamplerDescriptors(8, 1024); err != nil {
		return err
	}
	if mp.shadowSamp, err = dev.NewShadowSampler(); err != nil {
		return err
	}
	if mp.shadowDesc, err = dev.NewImmutableSamplerDescriptors(shadowCascades, 4, mp.shadowSamp); err != nil {
		return err
	}
	shadowBindings := make([]render.SamplerBinding, shadowCascades)
	for c := range mp.shadow {
		if mp.shadow[c], err = dev.NewTarget(vk.VkExtent2D{Width: shadowMapSize, Height: shadowMapSize}, vk.VK_FORMAT_UNDEFINED, g.r.DepthFormat); err != nil {
			return err
		}
		depth := mp.shadow[c].Depth
		if err := dev.OneShot(func(cb vk.VkCommandBuffer) { render.ClearDepthForSampling(cb, depth) }); err != nil {
			return err
		}
		shadowBindings[c] = render.SamplerBinding{View: depth.View, Sampler: mp.shadowSamp}
	}
	if mp.shadowSet, err = mp.shadowDesc.AllocateMany(shadowBindings); err != nil {
		return err
	}
	if mp.flatNormal, err = g.newTexture(1, 1, []byte{128, 128, 255, 255}, TextureOptions{Data: true}); err != nil {
		return err
	}
	if mp.black, err = g.newTexture(1, 1, []byte{0, 0, 0, 255}, TextureOptions{Data: true}); err != nil {
		return err
	}
	bindings, attrs := meshVertexLayout()
	shadow := render.PipelineDesc{
		Vert: shaders.ShadowVert, Frag: shaders.ShadowFrag,
		NoColor: true, DepthFormat: g.r.DepthFormat,
		Bindings: bindings, Attributes: attrs[:7], // the depth pass reads no material attributes
		CullMode: vk.VK_CULL_MODE_NONE, DepthTest: true, DepthWrite: true,
		DepthBias: 1.5, DepthSlopeBias: 2.0,
		PushConstantSize: 4, // cascade index
		SetLayouts:       []vk.VkDescriptorSetLayout{mp.materials.Layout, mp.uniformLayout.Layout},
	}
	if mp.shadowPipe, err = dev.NewPipeline(shadow); err != nil {
		return err
	}
	// Skinned variants read joints and weights from binding 0 and joint
	// matrices from a storage buffer in set 3.
	sbind, sattrs := skinVertexLayout()
	skinShadow := shadow
	skinShadow.Vert, skinShadow.Bindings = shaders.ShadowSkinVert, sbind
	skinShadow.Attributes = append(append([]vk.VkVertexInputAttributeDescription{}, sattrs[:7]...), sattrs[9:]...)
	skinShadow.SetLayouts = []vk.VkDescriptorSetLayout{mp.materials.Layout, mp.uniformLayout.Layout, mp.shadowDesc.Layout, mp.jointLayout.Layout}
	if mp.skinShadow, err = dev.NewPipeline(skinShadow); err != nil {
		return err
	}
	mp.defaultShader = &Shader{g: g, frag: shaders.PBRFrag, mesh: true, pipes: map[pipeKey]*render.Pipeline{}}
	for _, key := range []pipeKey{{blend: BlendReplace}, {blend: BlendAlpha}} {
		if _, err := mp.defaultShader.pipeline(key); err != nil {
			return err
		}
	}
	return nil
}

// pipelineDesc is the lit pass pipeline for static or skinned meshes,
// without a fragment program: each mesh shader supplies its own.
func (mp *meshPass) pipelineDesc(skinned bool) render.PipelineDesc {
	g := mp.defaultShader.g
	bindings, attrs := meshVertexLayout()
	desc := render.PipelineDesc{
		Vert:        shaders.PBRVert,
		ColorFormat: hdrFormat, DepthFormat: g.r.DepthFormat,
		Bindings: bindings, Attributes: attrs[:9], // static meshes have no joint base
		CullMode: vk.VK_CULL_MODE_BACK_BIT, DepthTest: true, DepthWrite: true,
		SetLayouts: []vk.VkDescriptorSetLayout{mp.materials.Layout, mp.uniformLayout.Layout, mp.shadowDesc.Layout, mp.jointLayout.Layout, g.uniforms.Layout},
	}
	if skinned {
		desc.Vert = shaders.PBRSkinVert
		desc.Bindings, desc.Attributes = skinVertexLayout()
	}
	return desc
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
	g.queueMesh(meshDraw{mesh: m, mat: mat, model: model})
}

// queueMesh fills a draw's defaults, captures its shader's uniforms, and
// adds it to the current queue.
func (g *Graphics) queueMesh(d meshDraw) {
	if d.mat.BaseColor == (Color{}) {
		d.mat.BaseColor = White
	}
	if d.mat.Roughness == 0 {
		d.mat.Roughness = 0.6
	}
	d.shader = d.mat.Shader
	if d.shader == nil {
		d.shader = g.meshes.defaultShader
	} else if !d.shader.mesh {
		panic("gfx: Material.Shader wants a mesh shader from NewMeshShader")
	}
	d.uniform = d.shader.uniformOffset()
	g.cur.draws = append(g.cur.draws, d)
}

// materialSet returns the descriptor set for a material's textures and
// its shader's images.
func (g *Graphics) materialSet(mat Material) (vk.VkDescriptorSet, error) {
	mp := &g.meshes
	key := [8]*Texture{orTex(mat.Texture, g.white), orTex(mat.MetalRoughTexture, g.white), orTex(mat.NormalTexture, mp.flatNormal), orTex(mat.EmissiveTexture, mp.black)}
	if mat.Shader != nil {
		for i, t := range mat.Shader.images {
			key[4+i] = t
		}
	}
	if set, ok := mp.matSets[key]; ok {
		return set, nil
	}
	bindings := make([]render.SamplerBinding, len(key))
	for i, t := range key {
		if t == nil {
			t = g.white
		}
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

// forgetTexture drops cached descriptor sets that reference a destroyed texture.
func (g *Graphics) forgetTexture(t *Texture) {
	for key, set := range g.meshes.matSets {
		if slices.Contains(key[:], t) {
			g.meshes.materials.Free(set)
			delete(g.meshes.matSets, key)
		}
	}
	for key, set := range g.imageSets {
		if slices.Contains(key[:], t) {
			g.descriptors.Free(set)
			delete(g.imageSets, key)
		}
	}
}

// cascades fits one orthographic light frustum to each slice of the
// camera frustum out to the shadow distance, returning the matrices and
// the view-space depth where each cascade ends.
func (q *drawQueue) cascades(aspect float32) ([shadowCascades]lin.Mat4, lin.Vec4, lin.Vec4) {
	var mats [shadowCascades]lin.Mat4
	var splits, radii lin.Vec4
	far := q.light.ShadowDistance
	if far <= 0 {
		far = q.light.ShadowRadius
	}
	if far <= 0 {
		far = 60
	}
	_, _, near, _ := q.camera.defaults()
	dir := q.light.Direction.Norm()
	lightUp := lin.V3(0, 1, 0)
	if abs32(dir.Y) > 0.95 {
		lightUp = lin.V3(0, 0, 1)
	}
	invVP := q.camera.Projection(aspect).Mul(q.camera.viewMatrix()).Inverse()
	// Practical split scheme, weighted towards logarithmic.
	ends := [shadowCascades]float32{}
	for i := range ends {
		f := float32(i+1) / shadowCascades
		logSplit := near * float32(math.Pow(float64(far/near), float64(f)))
		linSplit := near + (far-near)*f
		ends[i] = 0.7*logSplit + 0.3*linSplit
	}
	splits = lin.V4(ends[0], ends[1], ends[2], 0)
	prev := near
	proj := q.camera.Projection(aspect)
	for i := range mats {
		// Slice corners in world space: unproject the 8 corners at the
		// slice's near and far depths.
		var corners [8]lin.Vec3
		k := 0
		for _, z := range [2]float32{prev, ends[i]} {
			clipZ := proj.MulVec4(lin.V4(0, 0, -z, 1))
			ndcZ := clipZ.Z / clipZ.W
			for _, x := range [2]float32{-1, 1} {
				for _, y := range [2]float32{-1, 1} {
					p := invVP.MulVec4(lin.V4(x, y, ndcZ, 1))
					corners[k] = p.Vec3().Mul(1 / p.W)
					k++
				}
			}
		}
		var centre lin.Vec3
		for _, c := range corners {
			centre = centre.Add(c)
		}
		centre = centre.Mul(1.0 / 8)
		radius := float32(0)
		for _, c := range corners {
			radius = max(radius, c.Sub(centre).Len())
		}
		// Snap the centre to shadow-map texels so edges do not swim.
		texel := radius * 2 / shadowMapSize
		view := lin.LookAt(centre.Sub(dir.Mul(radius*2)), centre, lightUp)
		c := view.MulPoint(centre)
		c.X = float32(math.Floor(float64(c.X/texel))) * texel
		c.Y = float32(math.Floor(float64(c.Y/texel))) * texel
		centre = view.Inverse().MulPoint(c)
		view = lin.LookAt(centre.Sub(dir.Mul(radius*2)), centre, lightUp)
		mats[i] = lin.Ortho(-radius, radius, -radius, radius, 0.1, radius*4).Mul(view)
		switch i {
		case 0:
			radii.X = radius
		case 1:
			radii.Y = radius
		default:
			radii.Z = radius
		}
		prev = ends[i]
	}
	return mats, splits, radii
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// writeUniforms fills the queue's frame block for the slot.
func (q *drawQueue) writeUniforms(slot int, aspect, time float32) error {
	if !q.hasCam {
		q.camera = Camera{Position: lin.V3(0, 0, 5)}
	}
	l := q.light
	strength := l.ShadowStrength
	if strength == 0 {
		strength = 1
	}
	sky, ground := l.Sky, l.Ground
	if sky == (Color{}) {
		sky = l.Ambient
	}
	if ground == (Color{}) {
		ground = l.Ambient
	}
	mats, splits, radii := q.cascades(aspect)
	u := frameUniforms{
		viewProj:      q.camera.ViewProj(aspect),
		view:          q.camera.viewMatrix(),
		lightViewProj: mats,
		camPos:        q.camera.Position.Vec4(1),
		lightDir:      l.Direction.Norm().Vec4(0),
		lightColor:    lin.V4(l.Color.R, l.Color.G, l.Color.B, strength),
		sky:           lin.V4(sky.R, sky.G, sky.B, 1),
		ground:        lin.V4(ground.R, ground.G, ground.B, 1),
		params:        lin.V4(shadowMapSize, boolFloat(l.Shadows), float32(len(q.points)), time),
		splits:        splits,
		radii:         radii,
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
		case a.shader != b.shader:
			if uintptr(unsafe.Pointer(a.shader)) < uintptr(unsafe.Pointer(b.shader)) {
				return -1
			}
			return 1
		case a.uniform != b.uniform:
			return int(a.uniform - b.uniform)
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

// drawRuns records draws as instanced runs of identical mesh, material
// and shader state. first is the index of draws[0] in the instance
// stream. In the shadow pass (cascade set) the depth-only pipelines are
// used; otherwise each draw's shader picks its lit pipeline. Skinned
// draws are never merged, since each has its own joint matrices.
func (g *Graphics) drawRuns(cb vk.VkCommandBuffer, fr *render.Frame, q *drawQueue, draws []meshDraw, first uint32, cascade *int32) error {
	mp := &g.meshes
	var offset vk.VkDeviceSize
	vk.VkCmdBindVertexBuffers(cb, 1, 1, &q.inst.buffers[q.inst.slot].Handle, &offset)
	var bound *render.Pipeline
	boundUniform := int32(-2)
	for i := 0; i < len(draws); {
		d := draws[i]
		run := 1
		if !d.skinned {
			for i+run < len(draws) {
				n := draws[i+run]
				if n.skinned || n.mesh != d.mesh || n.set != d.set || n.shader != d.shader || n.uniform != d.uniform {
					break
				}
				run++
			}
		}
		var p *render.Pipeline
		if cascade != nil {
			p = mp.shadowPipe
			if d.skinned {
				p = mp.skinShadow
			}
		} else {
			key := pipeKey{blend: BlendReplace, skinned: d.skinned}
			if d.mat.Blend {
				key.blend = BlendAlpha
			}
			var err error
			if p, err = d.shader.pipeline(key); err != nil {
				return err
			}
		}
		if p != bound {
			bound = p
			boundUniform = -2
			vk.VkCmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Handle)
			vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 1, 1, &q.uniforms.Sets[fr.Slot], 0, nil)
			if cascade != nil {
				vk.VkCmdPushConstants(cb, p.Layout, meshStages, 0, 4, unsafe.Pointer(cascade))
			} else {
				vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 2, 1, &g.meshes.shadowSet, 0, nil)
			}
			if d.skinned {
				vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 3, 1, &q.jointBuf.Sets[fr.Slot], 0, nil)
			}
		}
		if cascade == nil {
			vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 0, 1, &d.set, 0, nil)
			if d.uniform >= 0 && d.uniform != boundUniform {
				boundUniform = d.uniform
				dyn := uint32(d.uniform)
				vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.Layout, 4, 1, &g.uniforms.Sets[fr.Slot], 1, &dyn)
			}
		}
		vk.VkCmdBindVertexBuffers(cb, 0, 1, &d.mesh.vbuf.Handle, &offset)
		vk.VkCmdBindIndexBuffer(cb, d.mesh.ibuf.Handle, 0, vk.VK_INDEX_TYPE_UINT32)
		vk.VkCmdDrawIndexed(cb, d.mesh.IndexCount, uint32(run), 0, 0, first+uint32(i))
		i += run
	}
	return nil
}

// renderScene runs the shadow and lit passes of a queue into the targets'
// HDR image.
func (g *Graphics) renderScene(fr *render.Frame, q *drawQueue, t *sceneTargets) error {
	mp := &g.meshes
	cb := fr.CB
	aspect := float32(t.extent.Width) / float32(t.extent.Height)
	if err := q.writeUniforms(fr.Slot, aspect, g.time); err != nil {
		return err
	}
	opaque, blended, err := g.prepareDraws(q, fr.Slot)
	if err != nil {
		return err
	}
	if q.light.Shadows {
		for c := range mp.shadow {
			render.BeginTargetPass(cb, render.PassDesc{Target: mp.shadow[c], ClearDepth: 1})
			cascade := int32(c)
			if err := g.drawRuns(cb, fr, q, opaque, 0, &cascade); err != nil {
				return err
			}
			render.EndTargetPass(cb, mp.shadow[c])
		}
	}
	c := q.clear.premultiplied()
	render.BeginTargetPass(cb, render.PassDesc{Target: t.hdr, ClearColor: c, ClearDepth: 1})
	if err := g.drawRuns(cb, fr, q, opaque, 0, nil); err != nil {
		return err
	}
	if err := g.drawRuns(cb, fr, q, blended, uint32(len(opaque)), nil); err != nil {
		return err
	}
	render.EndTargetPass(cb, t.hdr)
	return nil
}

func (mp *meshPass) destroy(g *Graphics) {
	dev := g.r.Device.Handle
	if mp.defaultShader != nil {
		mp.defaultShader.Destroy()
	}
	for _, p := range []*render.Pipeline{mp.shadowPipe, mp.skinShadow} {
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
	for _, t := range mp.shadow {
		if t != nil {
			t.Destroy()
		}
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
