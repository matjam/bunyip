package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
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
}

// DefaultPost is the starting PostSettings.
func DefaultPost() PostSettings {
	return PostSettings{Exposure: 1, Bloom: 0.25, BloomThreshold: 1, Saturation: 1, Contrast: 1}
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
	singles   *render.DescriptorSets
	pairs     *render.DescriptorSets
	main      *sceneTargets
}

// sceneTargets are the offscreen images one output needs: HDR scene,
// bloom ping-pong at half size, and the LDR image FXAA reads.
type sceneTargets struct {
	extent    vk.VkExtent2D
	hdr       *render.Target
	bloomA    *render.Target
	bloomB    *render.Target
	ldr       *render.Target
	hdrSet    vk.VkDescriptorSet
	bloomASet vk.VkDescriptorSet
	bloomBSet vk.VkDescriptorSet
	ldrSet    vk.VkDescriptorSet
	finalSet  vk.VkDescriptorSet
	blackSet  vk.VkDescriptorSet
}

type postPush struct {
	a [4]float32
	b [4]float32
}

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
	g.r.OnResize(func(extent vk.VkExtent2D) error {
		if err := g.r.Device.WaitIdle(); err != nil {
			return err
		}
		p.main.destroy(g)
		var err error
		p.main, err = g.newSceneTargets(extent)
		return err
	})
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
	if t.hdr, err = dev.NewTarget(extent, hdrFormat, g.r.DepthFormat); err != nil {
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
	if t.hdrSet, err = p.singles.Allocate(t.hdr.Color.View, g.linear); err != nil {
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
	if t.finalSet, err = p.pairs.AllocateMany([]render.SamplerBinding{
		{View: t.hdr.Color.View, Sampler: g.linear}, {View: t.bloomA.Color.View, Sampler: g.linear},
	}); err != nil {
		return fail(err)
	}
	if t.blackSet, err = p.pairs.AllocateMany([]render.SamplerBinding{
		{View: t.hdr.Color.View, Sampler: g.linear}, {View: g.meshes.black.img.View, Sampler: g.linear},
	}); err != nil {
		return fail(err)
	}
	return t, nil
}

func (t *sceneTargets) destroy(g *Graphics) {
	p := &g.post
	for _, set := range []vk.VkDescriptorSet{t.hdrSet, t.bloomASet, t.bloomBSet, t.ldrSet} {
		if set != 0 {
			p.singles.Free(set)
		}
	}
	for _, set := range []vk.VkDescriptorSet{t.finalSet, t.blackSet} {
		if set != 0 {
			p.pairs.Free(set)
		}
	}
	for _, tg := range []*render.Target{t.hdr, t.bloomA, t.bloomB, t.ldr} {
		if tg != nil {
			tg.Destroy()
		}
	}
	*t = sceneTargets{}
}

// fullscreen records one fullscreen triangle with the pipeline and set.
func fullscreen(cb vk.VkCommandBuffer, pipe *render.Pipeline, set vk.VkDescriptorSet, push postPush) {
	vk.VkCmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
	vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &set, 0, nil)
	vk.VkCmdPushConstants(cb, pipe.Layout, vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT, 0, uint32(unsafe.Sizeof(push)), unsafe.Pointer(&push))
	vk.VkCmdDraw(cb, 3, 1, 0, 0)
}

// renderBloom runs bright pass and two blur passes; the result ends in bloomA.
func (g *Graphics) renderBloom(cb vk.VkCommandBuffer, t *sceneTargets) {
	p := &g.post
	s := p.settings
	render.BeginTargetPass(cb, render.PassDesc{Target: t.bloomA})
	fullscreen(cb, p.bright, t.hdrSet, postPush{a: [4]float32{s.BloomThreshold, 0.5, 0, 0}})
	render.EndTargetPass(cb, t.bloomA)
	step := [2]float32{1 / float32(t.bloomA.Extent.Width), 1 / float32(t.bloomA.Extent.Height)}
	render.BeginTargetPass(cb, render.PassDesc{Target: t.bloomB})
	fullscreen(cb, p.blur, t.bloomASet, postPush{a: [4]float32{step[0] * 1.5, 0, 0, 0}})
	render.EndTargetPass(cb, t.bloomB)
	render.BeginTargetPass(cb, render.PassDesc{Target: t.bloomA})
	fullscreen(cb, p.blur, t.bloomBSet, postPush{a: [4]float32{0, step[1] * 1.5, 0, 0}})
	render.EndTargetPass(cb, t.bloomA)
}

// composite writes the tone-mapped scene into the current pass.
func (g *Graphics) composite(cb vk.VkCommandBuffer, t *sceneTargets, bloom bool) {
	p := &g.post
	s := p.settings
	set := t.finalSet
	strength := s.Bloom
	if !bloom {
		set = t.blackSet
		strength = 0
	}
	fullscreen(cb, p.composite, set, postPush{
		a: [4]float32{s.Exposure, strength, s.Vignette, s.Saturation},
		b: [4]float32{s.Contrast, 0, 0, 0},
	})
}

// antiAlias resolves the LDR image into the current pass with FXAA.
func (g *Graphics) antiAlias(cb vk.VkCommandBuffer, t *sceneTargets) {
	fullscreen(cb, g.post.fxaa, t.ldrSet, postPush{a: [4]float32{1 / float32(t.extent.Width), 1 / float32(t.extent.Height), 0, 0}})
}

func (p *postPass) destroy(g *Graphics) {
	if p.main != nil {
		p.main.destroy(g)
	}
	for _, pipe := range []*render.Pipeline{p.composite, p.bright, p.blur, p.fxaa} {
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
}
