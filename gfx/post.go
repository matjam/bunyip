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
}

// DefaultPost is the starting PostSettings.
func DefaultPost() PostSettings {
	return PostSettings{Exposure: 1, Bloom: 0.25, BloomThreshold: 1, Saturation: 1, Contrast: 1}
}

// SetPost replaces the post-processing settings.
func (g *Graphics) SetPost(p PostSettings) { g.post.settings = p }

// Post returns the current settings.
func (g *Graphics) Post() PostSettings { return g.post.settings }

// postPass owns the HDR scene target, bloom targets and the fullscreen pipelines.
type postPass struct {
	settings  PostSettings
	hdr       *render.Target
	bloomA    *render.Target
	bloomB    *render.Target
	composite *render.Pipeline
	bright    *render.Pipeline
	blur      *render.Pipeline
	singles   *render.DescriptorSets // one-sampler sets for bright/blur inputs
	pairs     *render.DescriptorSets // scene+bloom for the composite
	hdrSet    vk.VkDescriptorSet
	bloomASet vk.VkDescriptorSet
	bloomBSet vk.VkDescriptorSet
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
	dev := g.R.Device
	var err error
	if p.singles, err = dev.NewSamplerDescriptors(1, 8); err != nil {
		return err
	}
	if p.pairs, err = dev.NewSamplerDescriptors(2, 4); err != nil {
		return err
	}
	push := uint32(unsafe.Sizeof(postPush{}))
	if p.bright, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.BrightFrag, ColorFormat: hdrFormat,
		PushConstantSize: push, SetLayouts: []vk.VkDescriptorSetLayout{p.singles.Layout},
	}); err != nil {
		return err
	}
	if p.blur, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.BlurFrag, ColorFormat: hdrFormat,
		PushConstantSize: push, SetLayouts: []vk.VkDescriptorSetLayout{p.singles.Layout},
	}); err != nil {
		return err
	}
	if p.composite, err = dev.NewPipeline(render.PipelineDesc{
		Vert: shaders.PostVert, Frag: shaders.PostFrag,
		ColorFormat: g.R.Swapchain.Format, DepthFormat: g.R.DepthFormat,
		PushConstantSize: push, SetLayouts: []vk.VkDescriptorSetLayout{p.pairs.Layout},
	}); err != nil {
		return err
	}
	if err := g.createPostTargets(g.R.Swapchain.Extent); err != nil {
		return err
	}
	g.R.OnResize(g.createPostTargets)
	return nil
}

// createPostTargets (re)builds the scene-sized targets and their sets.
func (g *Graphics) createPostTargets(extent vk.VkExtent2D) error {
	p := &g.post
	dev := g.R.Device
	if err := dev.WaitIdle(); err != nil {
		return err
	}
	g.destroyPostTargets()
	var err error
	if p.hdr, err = dev.NewTarget(extent, hdrFormat, g.R.DepthFormat); err != nil {
		return err
	}
	half := vk.VkExtent2D{Width: max(extent.Width/2, 1), Height: max(extent.Height/2, 1)}
	if p.bloomA, err = dev.NewTarget(half, hdrFormat, vk.VK_FORMAT_UNDEFINED); err != nil {
		return err
	}
	if p.bloomB, err = dev.NewTarget(half, hdrFormat, vk.VK_FORMAT_UNDEFINED); err != nil {
		return err
	}
	if p.hdrSet, err = p.singles.Allocate(p.hdr.Color.View, g.linear); err != nil {
		return err
	}
	if p.bloomASet, err = p.singles.Allocate(p.bloomA.Color.View, g.linear); err != nil {
		return err
	}
	if p.bloomBSet, err = p.singles.Allocate(p.bloomB.Color.View, g.linear); err != nil {
		return err
	}
	if p.finalSet, err = p.pairs.AllocateMany([]render.SamplerBinding{
		{View: p.hdr.Color.View, Sampler: g.linear}, {View: p.bloomA.Color.View, Sampler: g.linear},
	}); err != nil {
		return err
	}
	if p.blackSet, err = p.pairs.AllocateMany([]render.SamplerBinding{
		{View: p.hdr.Color.View, Sampler: g.linear}, {View: g.meshes.black.img.View, Sampler: g.linear},
	}); err != nil {
		return err
	}
	return nil
}

func (g *Graphics) destroyPostTargets() {
	p := &g.post
	for _, t := range []*render.Target{p.hdr, p.bloomA, p.bloomB} {
		if t != nil {
			t.Destroy()
		}
	}
	p.hdr, p.bloomA, p.bloomB = nil, nil, nil
	for _, set := range []vk.VkDescriptorSet{p.hdrSet, p.bloomASet, p.bloomBSet} {
		if set != 0 {
			p.singles.Free(set)
		}
	}
	for _, set := range []vk.VkDescriptorSet{p.finalSet, p.blackSet} {
		if set != 0 {
			p.pairs.Free(set)
		}
	}
	p.hdrSet, p.bloomASet, p.bloomBSet, p.finalSet, p.blackSet = 0, 0, 0, 0, 0
}

// fullscreen records one fullscreen triangle with the pipeline and set.
func fullscreen(cb vk.VkCommandBuffer, pipe *render.Pipeline, set vk.VkDescriptorSet, push postPush) {
	vk.VkCmdBindPipeline(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Handle)
	vk.VkCmdBindDescriptorSets(cb, vk.VK_PIPELINE_BIND_POINT_GRAPHICS, pipe.Layout, 0, 1, &set, 0, nil)
	vk.VkCmdPushConstants(cb, pipe.Layout, vk.VK_SHADER_STAGE_VERTEX_BIT|vk.VK_SHADER_STAGE_FRAGMENT_BIT, 0, uint32(unsafe.Sizeof(push)), unsafe.Pointer(&push))
	vk.VkCmdDraw(cb, 3, 1, 0, 0)
}

// renderBloom runs bright pass and two blur passes; the result ends in bloomA.
func (g *Graphics) renderBloom(fr *render.Frame) {
	p := &g.post
	cb := fr.CB
	s := p.settings
	render.BeginTargetPass(cb, render.PassDesc{Target: p.bloomA})
	fullscreen(cb, p.bright, p.hdrSet, postPush{a: [4]float32{s.BloomThreshold, 0.5, 0, 0}})
	render.EndTargetPass(cb, p.bloomA)
	step := [2]float32{1 / float32(p.bloomA.Extent.Width), 1 / float32(p.bloomA.Extent.Height)}
	render.BeginTargetPass(cb, render.PassDesc{Target: p.bloomB})
	fullscreen(cb, p.blur, p.bloomASet, postPush{a: [4]float32{step[0] * 1.5, 0, 0, 0}})
	render.EndTargetPass(cb, p.bloomB)
	render.BeginTargetPass(cb, render.PassDesc{Target: p.bloomA})
	fullscreen(cb, p.blur, p.bloomBSet, postPush{a: [4]float32{0, step[1] * 1.5, 0, 0}})
	render.EndTargetPass(cb, p.bloomA)
}

// composite writes the tone-mapped scene into the current swapchain pass.
func (g *Graphics) composite(fr *render.Frame, bloom bool) {
	p := &g.post
	s := p.settings
	set := p.finalSet
	strength := s.Bloom
	if !bloom {
		set = p.blackSet
		strength = 0
	}
	fullscreen(fr.CB, p.composite, set, postPush{
		a: [4]float32{s.Exposure, strength, s.Vignette, s.Saturation},
		b: [4]float32{s.Contrast, 0, 0, 0},
	})
}

func (p *postPass) destroy(g *Graphics) {
	g.destroyPostTargets()
	for _, pipe := range []*render.Pipeline{p.composite, p.bright, p.blur} {
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
