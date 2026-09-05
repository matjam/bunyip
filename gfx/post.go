package gfx

import (
	"image"
	"image/color"
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// PostSettings controls the post-processing applied to 3D scenes.
type PostSettings struct {
	Exposure       float32 // scene multiplier before tone mapping; default 1
	Bloom          float32 // bloom strength; default 0.25, 0 disables the passes
	BloomThreshold float32 // luminance where bloom starts; default 1
	Vignette       float32 // 0..1 edge darkening; default 0
	Saturation     float32 // default 1
	Contrast       float32 // default 1
	NoAntiAlias    bool    // skip the FXAA pass on the main frame
	// AmbientOcclusion is the strength of screen-space ambient occlusion,
	// 0 (off) to 1; default 0.6. It darkens creases and contact points.
	AmbientOcclusion float32
	// OcclusionRadius is the occlusion kernel size in world units; default 1.
	OcclusionRadius float32
	// ShowOcclusion displays the occlusion buffer instead of the scene, for tuning.
	ShowOcclusion bool
	// OrderIndependent composites blended materials without sorting them.
	// Each pixel's translucent fragments accumulate with a weight that
	// favours the nearest, and one pass resolves them, so meshes that
	// intersect or overlap themselves no longer pick a single order for
	// the whole draw (weighted blended order-independent transparency,
	// McGuire and Bavoil). It costs two more images the size of the frame
	// and one pass. Zero keeps the sorted path, and transmissive
	// materials stay on it either way, because they read the scene behind
	// them and so must draw in order.
	OrderIndependent bool
	// LUT grades the final colours through a lookup table: a strip of n
	// slices of n by n, n by n squared pixels wide, as NeutralLUT lays it
	// out and image editors export it after grading a screenshot. Load it
	// with NewLUT; nil grades nothing. LUTStrength blends towards the
	// graded colour; zero means 1.
	LUT         *Texture
	LUTStrength float32
}

// NeutralLUT returns an identity colour lookup table of n slices (16 or
// 32 are usual): grade a screenshot with it pasted in the corner, crop
// it back out, and every frame gets the same grade through
// PostSettings.LUT.
func NeutralLUT(n int) *image.RGBA {
	n = max(n, 2)
	img := image.NewRGBA(image.Rect(0, 0, n*n, n))
	for b := range n {
		for g := range n {
			for r := range n {
				v := func(i int) uint8 { return uint8(i * 255 / (n - 1)) }
				img.SetRGBA(b*n+r, g, color.RGBA{v(r), v(g), v(b), 255})
			}
		}
	}
	return img
}

// NewLUT uploads a colour lookup table for PostSettings.LUT: linear
// filtering, no colour-space conversion.
func (g *Graphics) NewLUT(img image.Image) (*Texture, error) {
	return g.NewTexture(img, TextureOptions{Linear: true, Data: true})
}

// DefaultPost is the starting PostSettings.
func DefaultPost() PostSettings {
	return PostSettings{Exposure: 1, Bloom: 0.25, BloomThreshold: 1, Saturation: 1, Contrast: 1, AmbientOcclusion: 0.6, OcclusionRadius: 1}
}

// SetPost replaces the post-processing settings.
func (g *Graphics) SetPost(p PostSettings) { g.post.settings = p }

// Post returns the current settings.
func (g *Graphics) Post() PostSettings { return g.post.settings }

// postPass owns the fullscreen pipelines and descriptor pools shared by
// every scene target set.
type postPass struct {
	settings  PostSettings
	composite *render.Pipeline
	bright    *render.Pipeline
	blur      *render.Pipeline
	fxaa      *render.Pipeline
	ssao      *render.Pipeline
	aoBlur    *render.Pipeline
	oit       *render.Pipeline // resolves the order-independent transparency targets
	singles   *render.DescriptorSets
	pairs     *render.DescriptorSets // accumulation and revealage together
	triples   *render.DescriptorSets
	main      *sceneTargets
	// The recording commands take their block and set by pointer, and a
	// pointer to a local is forced onto the heap once per call. These
	// live with the pass and are filled in place instead.
	push postPush
	ao   ssaoPush
	set  vk.VkDescriptorSet
	lut  vk.VkDescriptorSet
}

// sceneTargets are the offscreen images one output needs: HDR scene,
// bloom ping-pong at half size, and the LDR image FXAA reads.
type sceneTargets struct {
	extent    vk.VkExtent2D
	hdr       *render.Target
	bloomA    *render.Target
	bloomB    *render.Target
	ldr       *render.Target
	aoA       *render.Target
	aoB       *render.Target
	scene     *render.Image  // the opaque scene with blurred mips, for transmission
	accum     *render.Target // weighted translucent colour, made on first use
	reveal    *render.Target // what those fragments leave showing
	oitSet    vk.VkDescriptorSet
	hdrSet    vk.VkDescriptorSet
	depthSet  vk.VkDescriptorSet
	bloomASet vk.VkDescriptorSet
	bloomBSet vk.VkDescriptorSet
	ldrSet    vk.VkDescriptorSet
	aoASet    vk.VkDescriptorSet
	finalSet  vk.VkDescriptorSet // scene, bloom, ao
	noBloom   vk.VkDescriptorSet // scene, black, ao
}

type postPush struct {
	a [4]float32
	b [4]float32
}

// ssaoPush carries the projection both ways for depth reconstruction.
type ssaoPush struct {
	proj, invProj lin.Mat4
}

const aoFormat = vk.VK_FORMAT_R8_UNORM

// revealFormat holds the product of one minus each translucent
// fragment's alpha. It is a half float rather than a byte because the
// product falls away quickly and the composite divides by what is left.
const revealFormat = vk.VK_FORMAT_R16_SFLOAT

func (g *Graphics) initPost() error {
	p := &g.post
	p.settings = DefaultPost()
	dev := g.r.Device
	var err error
	if p.singles, err = dev.NewSamplerDescriptors(1, 64); err != nil {
		return err
	}
	if p.pairs, err = dev.NewSamplerDescriptors(2, 32); err != nil {
		return err
	}
	if p.triples, err = dev.NewSamplerDescriptors(3, 32); err != nil {
		return err
	}
	push := uint32(unsafe.Sizeof(postPush{}))
	single := []vk.VkDescriptorSetLayout{p.singles.Layout}
	if p.bright, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.BrightFrag, ColorFormat: hdrFormat, PushConstantSize: push, SetLayouts: single,
	}); err != nil {
		return err
	}
	if p.blur, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.BlurFrag, ColorFormat: hdrFormat, PushConstantSize: push, SetLayouts: single,
	}); err != nil {
		return err
	}
	// The composite and FXAA passes write swapchain-format images with the
	// frame's depth attachment present, on screen and in render textures.
	if p.composite, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.PostFrag,
		ColorFormat: g.r.Swapchain.Format, DepthFormat: g.r.DepthFormat,
		PushConstantSize: push, SetLayouts: []vk.VkDescriptorSetLayout{p.triples.Layout, g.descriptors.Layout},
	}); err != nil {
		return err
	}
	// Decals read the scene depth through the same single-sampler layout.
	decalBindings, decalAttrs := meshVertexLayout()
	if g.meshes.decalPipe, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.DecalVert, Frag: shaders.DecalFrag, ColorFormat: hdrFormat,
		Bindings: decalBindings[:1], Attributes: decalAttrs[:1],
		CullMode: vk.VK_CULL_MODE_FRONT_BIT, Blend: true,
		PushConstantSize: uint32(unsafe.Sizeof(decalPush{})),
		SetLayouts:       []vk.VkDescriptorSetLayout{p.singles.Layout, g.meshes.uniformLayout.Layout, g.descriptors.Layout},
	}); err != nil {
		return err
	}
	if p.ssao, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.SSAOFrag, ColorFormat: aoFormat,
		PushConstantSize: uint32(unsafe.Sizeof(ssaoPush{})), SetLayouts: single,
	}); err != nil {
		return err
	}
	if p.aoBlur, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.AOBlurFrag, ColorFormat: aoFormat, PushConstantSize: push, SetLayouts: single,
	}); err != nil {
		return err
	}
	// Resolving transparency draws over the scene with the revealage in
	// alpha: the source keeps what the fragments covered and the
	// destination keeps what they left.
	if p.oit, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.OITFrag,
		ColorFormat: hdrFormat, DepthFormat: g.r.DepthFormat,
		Blend: true, Factors: &render.BlendFactors{
			SrcColor: vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA, DstColor: vk.VK_BLEND_FACTOR_SRC_ALPHA, ColorOp: vk.VK_BLEND_OP_ADD,
			SrcAlpha: vk.VK_BLEND_FACTOR_ZERO, DstAlpha: vk.VK_BLEND_FACTOR_ONE, AlphaOp: vk.VK_BLEND_OP_ADD,
		},
		PushConstantSize: push, SetLayouts: []vk.VkDescriptorSetLayout{p.pairs.Layout},
	}); err != nil {
		return err
	}
	if p.fxaa, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.FXAAFrag,
		ColorFormat: g.r.Swapchain.Format, DepthFormat: g.r.DepthFormat,
		PushConstantSize: push, SetLayouts: single,
	}); err != nil {
		return err
	}
	if p.main, err = g.newSceneTargets(g.r.Swapchain.Extent); err != nil {
		return err
	}
	g.r.OnResize(func(vk.VkExtent2D) error { return g.rebuildMain() })
	return nil
}

// newSceneTargets builds the images and descriptor sets for one extent.
func (g *Graphics) newSceneTargets(extent vk.VkExtent2D) (*sceneTargets, error) {
	p := &g.post
	dev := g.r.Device
	t := &sceneTargets{extent: extent}
	var err error
	fail := func(err error) (*sceneTargets, error) {
		t.destroy(g)
		return nil, err
	}
	if t.hdr, err = dev.NewTargetCopyable(extent, hdrFormat, g.r.DepthFormat); err != nil {
		return fail(err)
	}
	half := vk.VkExtent2D{Width: max(extent.Width/2, 1), Height: max(extent.Height/2, 1)}
	if t.bloomA, err = dev.NewTarget(half, hdrFormat, vk.VK_FORMAT_UNDEFINED); err != nil {
		return fail(err)
	}
	if t.bloomB, err = dev.NewTarget(half, hdrFormat, vk.VK_FORMAT_UNDEFINED); err != nil {
		return fail(err)
	}
	if t.ldr, err = dev.NewTarget(extent, g.r.Swapchain.Format, g.r.DepthFormat); err != nil {
		return fail(err)
	}
	if t.aoA, err = dev.NewTargetSampled(half, aoFormat, vk.VK_FORMAT_UNDEFINED); err != nil {
		return fail(err)
	}
	if t.aoB, err = dev.NewTargetSampled(half, aoFormat, vk.VK_FORMAT_UNDEFINED); err != nil {
		return fail(err)
	}
	// Transmissive materials read the opaque scene through this copy; six
	// levels give enough blur for the roughest glass.
	sceneUsage := vk.VkImageUsageFlags(vk.VK_IMAGE_USAGE_SAMPLED_BIT | vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT | vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT)
	if t.scene, err = dev.NewImageMips(extent, hdrFormat, sceneUsage, vk.VK_IMAGE_ASPECT_COLOR_BIT, min(render.MipLevels(extent), 6)); err != nil {
		return fail(err)
	}
	// The composite always binds the AO image; give it white until a pass
	// runs. Every material set binds the scene copy, so it must be readable
	// before the first transmissive draw.
	if err := g.setup(func(cb vk.VkCommandBuffer) {
		render.ClearColorForSampling(cb, t.aoB.Color)
		render.ClearColorForSampling(cb, t.scene)
	}); err != nil {
		return fail(err)
	}
	if t.hdrSet, err = p.singles.Allocate(t.hdr.Color.View, g.linear); err != nil {
		return fail(err)
	}
	if t.depthSet, err = p.singles.Allocate(t.hdr.Depth.View, g.nearest); err != nil {
		return fail(err)
	}
	if t.aoASet, err = p.singles.Allocate(t.aoA.Color.View, g.linear); err != nil {
		return fail(err)
	}
	if t.bloomASet, err = p.singles.Allocate(t.bloomA.Color.View, g.linear); err != nil {
		return fail(err)
	}
	if t.bloomBSet, err = p.singles.Allocate(t.bloomB.Color.View, g.linear); err != nil {
		return fail(err)
	}
	if t.ldrSet, err = p.singles.Allocate(t.ldr.Color.View, g.linear); err != nil {
		return fail(err)
	}
	if t.finalSet, err = p.triples.AllocateMany([]render.SamplerBinding{
		{View: t.hdr.Color.View, Sampler: g.linear}, {View: t.bloomA.Color.View, Sampler: g.linear}, {View: t.aoB.Color.View, Sampler: g.linear},
	}); err != nil {
		return fail(err)
	}
	if t.noBloom, err = p.triples.AllocateMany([]render.SamplerBinding{
		{View: t.hdr.Color.View, Sampler: g.linear}, {View: g.meshes.black.img.View, Sampler: g.linear}, {View: t.aoB.Color.View, Sampler: g.linear},
	}); err != nil {
		return fail(err)
	}
	return t, nil
}

// orderIndependent makes the accumulation and revealage images for an
// output the first time a frame asks for order-independent transparency.
// They are the size of the frame again in half floats, so an output that
// never uses them never pays for them. Nothing samples them before the
// pass that clears and fills them, so they need no clear here.
func (g *Graphics) orderIndependent(t *sceneTargets) error {
	if t.accum != nil {
		return nil
	}
	dev := g.r.Device
	accum, err := dev.NewTarget(t.extent, hdrFormat, vk.VK_FORMAT_UNDEFINED)
	if err != nil {
		return err
	}
	reveal, err := dev.NewTarget(t.extent, revealFormat, vk.VK_FORMAT_UNDEFINED)
	if err != nil {
		accum.Destroy()
		return err
	}
	set, err := g.post.pairs.AllocateMany([]render.SamplerBinding{
		{View: accum.Color.View, Sampler: g.linear}, {View: reveal.Color.View, Sampler: g.linear},
	})
	if err != nil {
		accum.Destroy()
		reveal.Destroy()
		return err
	}
	t.accum, t.reveal, t.oitSet = accum, reveal, set
	return nil
}

func (t *sceneTargets) destroy(g *Graphics) {
	p := &g.post
	for _, set := range []vk.VkDescriptorSet{t.hdrSet, t.depthSet, t.bloomASet, t.bloomBSet, t.ldrSet, t.aoASet} {
		if set != 0 {
			p.singles.Free(set)
		}
	}
	if t.oitSet != 0 {
		p.pairs.Free(t.oitSet)
	}
	for _, set := range []vk.VkDescriptorSet{t.finalSet, t.noBloom} {
		if set != 0 {
			p.triples.Free(set)
		}
	}
	for _, tg := range []*render.Target{t.hdr, t.bloomA, t.bloomB, t.ldr, t.aoA, t.aoB, t.accum, t.reveal} {
		if tg != nil {
			tg.Destroy()
		}
	}
	if t.scene != nil {
		g.forgetScene(t.scene)
		t.scene.Destroy()
	}
	*t = sceneTargets{}
}

// fullscreen records one fullscreen triangle with the pipeline and set.
func (p *postPass) fullscreen(cb vk.VkCommandBuffer, pipe *render.Pipeline, set vk.VkDescriptorSet, push postPush) {
	p.set, p.push = set, push
	vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &p.set, 0, nil)
	vk.CmdPushConstants(cb, pipe.Layout, vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT, 0, uint32(unsafe.Sizeof(p.push)), unsafe.Pointer(&p.push))
	vk.CmdDraw(cb, 3, 1, 0, 0)
}

// renderBloom runs bright pass and two blur passes; the result ends in bloomA.
func (g *Graphics) renderBloom(cb vk.VkCommandBuffer, t *sceneTargets) {
	p := &g.post
	s := p.settings
	render.BeginTargetPass(cb, render.PassDesc{Target: t.bloomA})
	p.fullscreen(cb, p.bright, t.hdrSet, postPush{a: [4]float32{s.BloomThreshold, 0.5, 0, 0}})
	render.EndTargetPass(cb, t.bloomA)
	step := [2]float32{1 / float32(t.bloomA.Extent.Width), 1 / float32(t.bloomA.Extent.Height)}
	render.BeginTargetPass(cb, render.PassDesc{Target: t.bloomB})
	p.fullscreen(cb, p.blur, t.bloomASet, postPush{a: [4]float32{step[0] * 1.5, 0, 0, 0}})
	render.EndTargetPass(cb, t.bloomB)
	render.BeginTargetPass(cb, render.PassDesc{Target: t.bloomA})
	p.fullscreen(cb, p.blur, t.bloomBSet, postPush{a: [4]float32{0, step[1] * 1.5, 0, 0}})
	render.EndTargetPass(cb, t.bloomA)
}

// renderAO computes half-resolution ambient occlusion from the scene depth
// and blurs it into aoB.
func (g *Graphics) renderAO(cb vk.VkCommandBuffer, q *drawQueue, t *sceneTargets) {
	p := &g.post
	aspect := float32(t.extent.Width) / float32(t.extent.Height)
	proj := q.camera.Projection(aspect)
	p.ao = ssaoPush{proj: proj, invProj: proj.Inverse()}
	radius := p.settings.OcclusionRadius
	if radius <= 0 {
		radius = 1
	}
	p.ao.proj[15] = radius // see ssao.frag
	render.BeginTargetPass(cb, render.PassDesc{Target: t.aoA})
	vk.CmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.ssao.Handle)
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.ssao.Layout, 0, 1, &t.depthSet, 0, nil)
	vk.CmdPushConstants(cb, p.ssao.Layout, meshStages, 0, uint32(unsafe.Sizeof(p.ao)), unsafe.Pointer(&p.ao))
	vk.CmdDraw(cb, 3, 1, 0, 0)
	render.EndTargetPass(cb, t.aoA)
	render.BeginTargetPass(cb, render.PassDesc{Target: t.aoB})
	p.fullscreen(cb, p.aoBlur, t.aoASet, postPush{a: [4]float32{1 / float32(t.aoA.Extent.Width), 1 / float32(t.aoA.Extent.Height), 0, 0}})
	render.EndTargetPass(cb, t.aoB)
}

// composite writes the tone-mapped scene into the current pass.
func (g *Graphics) composite(cb vk.VkCommandBuffer, t *sceneTargets, bloom, ao bool) {
	p := &g.post
	s := p.settings
	set := t.finalSet
	strength := s.Bloom
	if !bloom {
		set = t.noBloom
		strength = 0
	}
	aoStrength := float32(0)
	if ao {
		aoStrength = s.AmbientOcclusion
	}
	lut, lutStrength := g.white, float32(0)
	if s.LUT != nil && s.LUT.set != 0 {
		lut = s.LUT
		lutStrength = s.LUTStrength
		if lutStrength <= 0 {
			lutStrength = 1
		}
	}
	p.lut = lut.set
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, p.composite.Layout, 1, 1, &p.lut, 0, nil)
	p.fullscreen(cb, p.composite, set, postPush{
		a: [4]float32{s.Exposure, strength, s.Vignette, s.Saturation},
		b: [4]float32{s.Contrast, aoStrength, boolFloat(s.ShowOcclusion), lutStrength},
	})
}

// antiAlias resolves the LDR image into the current pass with FXAA.
func (g *Graphics) antiAlias(cb vk.VkCommandBuffer, t *sceneTargets) {
	g.post.fullscreen(cb, g.post.fxaa, t.ldrSet, postPush{a: [4]float32{1 / float32(t.extent.Width), 1 / float32(t.extent.Height), 0, 0}})
}

func (p *postPass) destroy(g *Graphics) {
	if p.main != nil {
		p.main.destroy(g)
	}
	for _, pipe := range []*render.Pipeline{p.composite, p.bright, p.blur, p.fxaa, p.ssao, p.aoBlur, p.oit} {
		if pipe != nil {
			pipe.Destroy()
		}
	}
	if p.singles != nil {
		p.singles.Destroy()
	}
	if p.pairs != nil {
		p.pairs.Destroy()
	}
	if p.triples != nil {
		p.triples.Destroy()
	}
}
