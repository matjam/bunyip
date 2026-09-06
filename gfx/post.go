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
	// Samples multisamples the 3D scene pass: 1 (the default), 2, 4 or 8,
	// clamped to what the GPU supports. Every triangle edge is then
	// resolved from that many coverage samples, which is the one form of
	// anti-aliasing that does not blur the picture, at the cost of that
	// many times the scene's colour and depth memory and bandwidth. Set
	// NoAntiAlias with it: FXAA over an already resolved image only
	// softens it. TemporalAA already resolves the edges and turns FXAA off
	// by itself, so leave this at 1 when that is on. Changing it rebuilds
	// the scene targets and the pipelines that draw into them, on the next
	// frame.
	Samples int
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
	// Reflections is the strength of screen-space reflections, 0 (off, the
	// default) to 1. A smooth surface mirrors what the screen already
	// shows: a polished floor under a bright object, a wet road under a
	// sign. Where a ray leaves the screen or hits nothing the surface
	// keeps its environment or probe reflection.
	Reflections float32
	// ReflectionRoughness is the roughness a surface stops reflecting the
	// screen at, fading out over the half of the range below it; zero
	// means 0.35.
	ReflectionRoughness float32
	// ReflectionDistance is how far a reflection ray travels in world
	// units; zero means 30.
	ReflectionDistance float32
	// ReflectionSteps is how many samples a reflection ray takes along the
	// way; zero means 32. More is sharper and slower.
	ReflectionSteps int
	// LUT grades the final colours through a lookup table: a strip of n
	// slices of n by n, n by n squared pixels wide, as NeutralLUT lays it
	// out and image editors export it after grading a screenshot. Load it
	// with NewLUT; nil grades nothing. LUTStrength blends towards the
	// graded colour; zero means 1.
	LUT         *Texture
	LUTStrength float32

	// TemporalAA averages each frame with the ones before it, jittering
	// the projection by a fraction of a pixel so the average fills in the
	// steps along an edge. It replaces FXAA on the main frame while it is
	// on; default off. Moving meshes need DrawMeshMoved and its
	// companions, or they smear until the neighbourhood clamp catches up.
	// Motion vectors are written by the opaque meshes the camera sees, so
	// a moving blended or transmissive mesh reprojects as if it were
	// still whatever it was drawn with.
	TemporalAA bool
	// TemporalBlend is how much of the new frame goes into the average,
	// 0.02 to 1; zero means 0.1. Lower is steadier and softer.
	TemporalBlend float32

	// FocusDistance is how far in front of the camera the image is sharp,
	// in world units; zero turns depth of field off. FocusRange is how
	// far either side of it stays sharp before the blur grows, and how
	// far past that the blur reaches its full width; zero means a quarter
	// of FocusDistance. BokehRadius is that full width in pixels (zero
	// means 12) and BokehSamples how many taps the disc takes (zero means
	// 16).
	FocusDistance float32
	FocusRange    float32
	BokehRadius   float32
	BokehSamples  int

	// MotionBlur smears each pixel back along the way it moved since the
	// last frame, 0 (off) to 1; default 0. MotionSamples is how many taps
	// it takes along that path; zero means 8. The camera's motion is read
	// from depth, an object's from the velocity buffer, so a moving mesh
	// needs DrawMeshMoved to blur along its own path.
	MotionBlur    float32
	MotionSamples int

	// Aberration splits the red and blue channels apart towards the edge
	// of the frame, as a cheap lens does; zero means off, 1 is about three
	// pixels at the edge of a 1080-wide frame and 0.5 is a subtle fringe.
	Aberration float32
	// Distortion bends the image about the centre: positive is barrel,
	// negative pincushion; zero means off.
	Distortion float32
	// Ghosts draws the bright pass mirrored through the centre a few
	// times over, the reflections a lens makes of a bright light; zero
	// means off. It reads the bloom image, so it needs Bloom above zero.
	Ghosts float32
	// Grain adds per-pixel noise that moves each frame, as film does;
	// zero means off, 0.05 is subtle.
	Grain float32

	// GodRays is the strength of the shafts of light the directional
	// light throws past an occluder; zero means off. GodRayDecay is how
	// fast a shaft fades along its length (zero means 0.96),
	// GodRayDensity how far towards the sun each pixel walks (zero means
	// 0.6) and GodRaySamples how many steps it takes (zero means 32).
	// The pass is skipped when the sun is behind the camera.
	GodRays       float32
	GodRayDecay   float32
	GodRayDensity float32
	GodRaySamples int

	// Post2D runs the composite on a frame that has no 3D draws at all,
	// so bloom, the grade, the LUT, the lens effects and FXAA reach a 2D
	// game. Zero keeps the direct path, which draws the 2D stream
	// straight to the screen and costs nothing. Exposure and tone mapping
	// are skipped in this mode, so a 2D game with no other setting on
	// gets back the colours it drew; the effects that need depth
	// (ambient occlusion, depth of field, motion blur, temporal
	// anti-aliasing, god rays) stay off. It applies to the screen and not
	// to a render texture, whose alpha the composite would flatten.
	Post2D bool
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

// SetPost replaces the global post-processing settings for the screen
// and every render texture. Queued DrawTo calls use the final settings
// when the frame submits, so they cannot choose different post effects.
// A change to Samples
// takes effect on the next frame, which rebuilds the scene targets.
func (g *Graphics) SetPost(p PostSettings) {
	g.requireTextureOwner(p.LUT)
	g.post.settings = p
}

// Post returns the current settings.
func (g *Graphics) Post() PostSettings { return g.post.settings }

// MaxSamples is the highest sample count PostSettings.Samples and
// RenderTextureOptions.Samples accept on this GPU: 1 when it cannot
// multisample at all, and usually 8.
func (g *Graphics) MaxSamples() int { return g.r.Device.MaxSamples() }

// sceneSamples is the sample count the scene passes render at, clamped
// to what the device supports.
func (g *Graphics) sceneSamples() vk.VkSampleCountFlagBits {
	return g.r.Device.SampleCount(g.post.settings.Samples)
}

// postPass owns the fullscreen pipelines and descriptor pools shared by
// every scene target set.
type postPass struct {
	settings PostSettings
	// These three draw into the scene's own colour attachment or a render
	// texture's, so each is built per output through pipeCache. The rest
	// write fixed single-sample images of their own.
	composite    *pipeCache // the screen, or a render texture's format and samples
	oit          *pipeCache // resolves the order-independent transparency targets
	reflect      *pipeCache // screen-space reflections, see ssr.go
	bright       *render.Pipeline
	blur         *render.Pipeline
	fxaa         *render.Pipeline
	ssao         *render.Pipeline
	aoBlur       *render.Pipeline
	taa          *render.Pipeline
	dof          *render.Pipeline
	motionBlur   *render.Pipeline
	godRays      *render.Pipeline
	velocity     *render.Pipeline
	velocitySkin *render.Pipeline
	singles      *render.DescriptorSets
	pairs        *render.DescriptorSets // accumulation and revealage together
	triples      *render.DescriptorSets
	quads        *render.DescriptorSets
	main         *sceneTargets
	// The recording commands take their block and set by pointer, and a
	// pointer to a local is forced onto the heap once per call. These
	// live with the pass and are filled in place instead.
	push  postPush
	ao    ssaoPush
	depth depthPush
	vel   velocityPush
	set   vk.VkDescriptorSet
	lut   vk.VkDescriptorSet
}

// sceneTargets are the offscreen images one output needs: HDR scene,
// bloom ping-pong at half size, and the LDR image FXAA reads. Only the
// HDR target is ever multisampled; it resolves into its own colour and
// depth images, which is what every later pass reads.
type sceneTargets struct {
	extent    vk.VkExtent2D
	samples   vk.VkSampleCountFlagBits // the HDR pass's sample count, one for none
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
	// The optional images, made when a setting first asks for them and
	// kept until the target set is rebuilt. vel holds this frame's motion
	// vectors, velPass pairs it with the scene depth so the pass can test
	// against it, hist is the last resolved frame, pong is the scratch
	// image every chain pass writes before it is copied back over hdr,
	// rays is the light shafts at half size and ldr2 the second
	// swapchain-format image a 2D frame needs to composite and then
	// anti-alias.
	vel     *render.Target
	velPass render.Target
	hist    *render.Target
	pong    *render.Target
	rays    *render.Target
	ldr2    *render.Target
	ldr2Set vk.VkDescriptorSet
	taaSet  vk.VkDescriptorSet // scene, history, velocity, depth
	mbSet   vk.VkDescriptorSet // scene, velocity, depth
	dofSet  vk.VkDescriptorSet // scene, depth
	// finals holds the composite's set for each combination of bloom and
	// god rays, and finals2D the same for a 2D frame, whose scene image
	// is ldr. A missing input is bound to black.
	finals    [4]vk.VkDescriptorSet
	finals2D  [2]vk.VkDescriptorSet
	histValid bool // the history image holds a resolved frame
	// reflectSet is the screen-space reflection pass's input: the copy of
	// the opaque scene, the scene depth, and the copy again in the third
	// slot the triple layout wants.
	reflectSet vk.VkDescriptorSet
}

// index into sceneTargets.finals.
func finalIndex(bloom, rays bool) int {
	i := 0
	if bloom {
		i |= 1
	}
	if rays {
		i |= 2
	}
	return i
}

type postPush struct {
	a [4]float32
	b [4]float32
	c [4]float32
	d [4]float32
}

// depthPush is the block of the fullscreen passes that read the depth
// buffer: a matrix that turns a pixel into a position or a place in the
// previous frame, and two vectors of parameters.
type depthPush struct {
	matrix lin.Mat4
	a      [4]float32
	b      [4]float32
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
	if p.quads, err = dev.NewSamplerDescriptors(4, 32); err != nil {
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
	// The composite writes the swapchain's format with the frame's depth
	// attachment present on screen, and whatever colour format, depth and
	// sample count a render texture was made with, so it is cached per
	// output. FXAA only ever resolves onto the screen.
	if p.composite, err = newPipeCache(dev, render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.PostFrag,
		ColorFormat: g.r.Swapchain.Format, DepthFormat: g.r.DepthFormat,
		PushConstantSize: push, SetLayouts: []vk.VkDescriptorSetLayout{p.quads.Layout, g.descriptors.Layout},
	}); err != nil {
		return err
	}
	// Decals read the scene depth through the same single-sampler layout.
	decalBindings, decalAttrs := meshVertexLayout()
	if g.meshes.decalPipe, err = newPipeCache(dev, render.PipelineDesc{
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
	// destination keeps what they left. It lands in the scene's own
	// colour attachment, so it is built per sample count.
	if p.oit, err = newPipeCache(dev, render.PipelineDesc{
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
	depth := uint32(unsafe.Sizeof(depthPush{}))
	if p.taa, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.TAAFrag, ColorFormat: hdrFormat,
		PushConstantSize: depth, SetLayouts: []vk.VkDescriptorSetLayout{p.quads.Layout},
	}); err != nil {
		return err
	}
	triple := []vk.VkDescriptorSetLayout{p.triples.Layout}
	if p.dof, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.DOFFrag, ColorFormat: hdrFormat,
		PushConstantSize: depth, SetLayouts: triple,
	}); err != nil {
		return err
	}
	if p.motionBlur, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.MotionBlurFrag, ColorFormat: hdrFormat,
		PushConstantSize: depth, SetLayouts: triple,
	}); err != nil {
		return err
	}
	if p.godRays, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.GodRaysFrag, ColorFormat: hdrFormat,
		PushConstantSize: push, SetLayouts: single,
	}); err != nil {
		return err
	}
	if err := g.initVelocity(); err != nil {
		return err
	}
	if p.main, err = g.newSceneTargets(g.r.Swapchain.Extent, g.sceneSamples()); err != nil {
		return err
	}
	g.r.OnResize(func(vk.VkExtent2D) error { return g.rebuildMain() })
	return nil
}

// newSceneTargets builds the images and descriptor sets for one extent
// at one sample count.
func (g *Graphics) newSceneTargets(extent vk.VkExtent2D, samples vk.VkSampleCountFlagBits) (*sceneTargets, error) {
	p := &g.post
	dev := g.r.Device
	if samples == 0 {
		samples = vk.VK_SAMPLE_COUNT_1_BIT
	}
	t := &sceneTargets{extent: extent, samples: samples}
	var err error
	fail := func(err error) (*sceneTargets, error) {
		t.destroy(g)
		return nil, err
	}
	// The HDR target carries the multisampled attachments and resolves
	// into its own colour and depth images. Its colour is a transfer
	// source for the transmission snapshot and its depth one for
	// RenderTexture.ReadDepth.
	if t.hdr, err = dev.NewTargetDesc(render.TargetDesc{
		Extent: extent, ColorFormat: hdrFormat, DepthFormat: g.r.DepthFormat, Samples: samples,
		ColorUsage: vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT, DepthUsage: vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT,
	}); err != nil {
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
	if _, err := t.finalSet(g, true, false); err != nil {
		return fail(err)
	}
	if _, err := t.finalSet(g, false, false); err != nil {
		return fail(err)
	}
	if t.reflectSet, err = p.triples.AllocateMany([]render.SamplerBinding{
		{View: t.scene.View, Sampler: g.linear}, {View: t.hdr.Depth.View, Sampler: g.nearest}, {View: t.scene.View, Sampler: g.linear},
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
	for _, set := range []vk.VkDescriptorSet{t.hdrSet, t.depthSet, t.bloomASet, t.bloomBSet, t.ldrSet, t.aoASet, t.ldr2Set} {
		if set != 0 {
			p.singles.Free(set)
		}
	}
	if t.oitSet != 0 {
		p.pairs.Free(t.oitSet)
	}
	for _, set := range []vk.VkDescriptorSet{t.mbSet, t.dofSet, t.reflectSet} {
		if set != 0 {
			p.triples.Free(set)
		}
	}
	for _, set := range append(append([]vk.VkDescriptorSet{t.taaSet}, t.finals[:]...), t.finals2D[:]...) {
		if set != 0 {
			p.quads.Free(set)
		}
	}
	for _, tg := range []*render.Target{t.hdr, t.bloomA, t.bloomB, t.ldr, t.aoA, t.aoB,
		t.accum, t.reveal, t.vel, t.hist, t.pong, t.rays, t.ldr2} {
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

// renderBloom runs bright pass and two blur passes over the image src
// names; the result ends in bloomA.
func (g *Graphics) renderBloom(cb vk.VkCommandBuffer, t *sceneTargets, src vk.VkDescriptorSet) {
	p := &g.post
	s := p.settings
	render.BeginTargetPass(cb, render.PassDesc{Target: t.bloomA})
	p.fullscreen(cb, p.bright, src, postPush{a: [4]float32{s.BloomThreshold, 0.5, 0, 0}})
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
	// The jittered projection, because the depth buffer was rasterised
	// with it.
	proj := q.projJ
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

// composite writes the tone-mapped scene into the current pass, whose
// attachments out describes. flat is a 2D frame, whose colours are
// already displayable: exposure and tone mapping are skipped and the
// scene comes from the LDR image.
func (g *Graphics) composite(cb vk.VkCommandBuffer, t *sceneTargets, out outKey, bloom, ao, rays, flat bool) error {
	p := &g.post
	s := p.settings
	pipe, err := p.composite.at(out)
	if err != nil {
		return err
	}
	strength := s.Bloom
	if !bloom {
		strength = 0
	}
	var set vk.VkDescriptorSet
	if flat {
		set, err = t.final2DSet(g, bloom)
	} else {
		set, err = t.finalSet(g, bloom, rays)
	}
	if err != nil {
		return err
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
	ghosts := s.Ghosts
	if !bloom {
		ghosts = 0 // the ghosts are made of the bright pass
	}
	p.lut = lut.set
	vk.CmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 1, 1, &p.lut, 0, nil)
	p.fullscreen(cb, pipe, set, postPush{
		a: [4]float32{s.Exposure, strength, s.Vignette, s.Saturation},
		b: [4]float32{s.Contrast, aoStrength, boolFloat(s.ShowOcclusion), lutStrength},
		c: [4]float32{s.Aberration, s.Distortion, s.Grain, g.time},
		d: [4]float32{ghosts, boolFloat(flat), 0, 0},
	})
	return nil
}

// antiAlias resolves an LDR image into the current pass with FXAA.
func (g *Graphics) antiAlias(cb vk.VkCommandBuffer, t *sceneTargets, set vk.VkDescriptorSet) {
	g.post.fullscreen(cb, g.post.fxaa, set, postPush{a: [4]float32{1 / float32(t.extent.Width), 1 / float32(t.extent.Height), 0, 0}})
}

func (p *postPass) destroy(g *Graphics) {
	if p.main != nil {
		p.main.destroy(g)
	}
	p.composite.destroy()
	p.reflect.destroy()
	p.oit.destroy()
	for _, pipe := range []*render.Pipeline{p.bright, p.blur, p.fxaa, p.ssao, p.aoBlur,
		p.taa, p.dof, p.motionBlur, p.godRays, p.velocity, p.velocitySkin} {
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
	if p.quads != nil {
		p.quads.Destroy()
	}
}
