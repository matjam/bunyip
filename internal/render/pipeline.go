package render

import (
	"runtime"
	"slices"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// PipelineDesc describes a graphics pipeline for dynamic rendering. Viewport
// and scissor are always dynamic so a resize never rebuilds pipelines.
type PipelineDesc struct {
	Vert, Frag       []byte // SPIR-V
	ColorFormat      vk.VkFormat
	DepthFormat      vk.VkFormat // VK_FORMAT_UNDEFINED for no depth attachment
	Bindings         []vk.VkVertexInputBindingDescription
	Attributes       []vk.VkVertexInputAttributeDescription
	Topology         vk.VkPrimitiveTopology
	CullMode         vk.VkCullModeFlags
	Blend            bool          // blending on; premultiplied source-over unless Factors is set
	Factors          *BlendFactors // blend equation when Blend is set
	DepthTest        bool
	DepthWrite       bool
	DepthCompare     vk.VkCompareOp // zero means less-or-equal
	Stencil          *StencilState  // stencil test and write, when the depth format has stencil
	PushConstantSize uint32         // bytes visible to all stages, 0 for none
	SetLayouts       []vk.VkDescriptorSetLayout
	// Samples is the sample count of the attachments the pipeline renders
	// into; zero or one is no multisampling. It must match the target's,
	// so a pass that changes sample count needs its own pipelines.
	Samples        vk.VkSampleCountFlagBits
	NoColor        bool    // depth-only pass (shadow maps)
	NoColorWrite   bool    // keep the colour attachment but disable its writes
	DepthBias      float32 // constant depth bias, for shadow passes
	DepthSlopeBias float32
	// DepthClamp clamps a fragment's depth to the viewport's range instead
	// of clipping the primitive at the near and far planes, so a shadow
	// caster in front of the near plane still writes depth. It needs the
	// device's depthClamp feature and is dropped where that is missing;
	// Device.DepthClamp says whether it took effect.
	DepthClamp bool
	FrontFace  vk.VkFrontFace // zero means counter-clockwise
	// ExtraColor describes the colour attachments after the first: their
	// formats and how each blends. The first attachment stays
	// ColorFormat with Blend and Factors. A pass drawing with the
	// pipeline binds the same attachments in the same order, through
	// PassDesc.Extra.
	ExtraColor []ColorAttachment
}

// ColorAttachment is one of a pipeline's colour attachments past the
// first: the format it writes and how it blends. The fragment shader
// writes it from the output at the matching location.
type ColorAttachment struct {
	Format  vk.VkFormat
	Blend   bool          // blending on; premultiplied source-over unless Factors is set
	Factors *BlendFactors // blend equation when Blend is set
}

// BlendFactors is a fixed-function blend equation: colour and alpha are
// each src·SrcFactor op dst·DstFactor.
type BlendFactors struct {
	SrcColor, DstColor vk.VkBlendFactor
	ColorOp            vk.VkBlendOp
	SrcAlpha, DstAlpha vk.VkBlendFactor
	AlphaOp            vk.VkBlendOp
}

// PremultipliedOver is source-over blending for premultiplied colour.
var PremultipliedOver = BlendFactors{
	SrcColor: vk.VK_BLEND_FACTOR_ONE, DstColor: vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA, ColorOp: vk.VK_BLEND_OP_ADD,
	SrcAlpha: vk.VK_BLEND_FACTOR_ONE, DstAlpha: vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA, AlphaOp: vk.VK_BLEND_OP_ADD,
}

// StencilState is a stencil test applied to both faces: fragments pass
// using Vulkan's comparison of masked Ref against masked stored value.
// Pass, Fail and DepthFail update the stored value when Write is set.
// Zero masks mean all eight bits, preserving the ordinary full-mask case.
type StencilState struct {
	Compare             vk.VkCompareOp
	Ref                 uint32
	Write               bool
	Pass                vk.VkStencilOp
	Fail, DepthFail     vk.VkStencilOp
	ReadMask, WriteMask uint32 // zero means all eight bits; Write controls updates
}

// StencilWrite marks every fragment drawn with ref.
func StencilWrite(ref uint32) *StencilState {
	return &StencilState{Compare: vk.VK_COMPARE_OP_ALWAYS, Ref: ref, Write: true, Pass: vk.VK_STENCIL_OP_REPLACE}
}

// StencilNotEqual passes only where the stored value differs from ref.
func StencilNotEqual(ref uint32) *StencilState {
	return &StencilState{Compare: vk.VK_COMPARE_OP_NOT_EQUAL, Ref: ref, Pass: vk.VK_STENCIL_OP_KEEP}
}

// Pipeline is a graphics pipeline and its layout.
//
// A pipeline started with StartPipeline, or built while a PipelineBatch
// is collecting, is compiled on a worker goroutine. Its Layout is valid at
// once; its Handle is valid only after Wait has returned nil.
type Pipeline struct {
	Handle vk.VkPipeline
	Layout vk.VkPipelineLayout
	dev    *Device
	mods   [2]*shaderModule // the vertex and fragment modules it was built from
	done   chan struct{}    // closed when a pipeline built on a worker is finished; nil for one built in place
	err    error            // why a worker could not build it, valid once done is closed
}

// Wait blocks until the pipeline is built and returns the error that
// stopped it, if any. A pipeline built in place returns at once.
func (p *Pipeline) Wait() error {
	if p.done == nil {
		return nil
	}
	<-p.done
	return p.err
}

// Ready reports whether Wait would return at once.
func (p *Pipeline) Ready() bool {
	if p.done == nil {
		return true
	}
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// PipelineBatch collects the pipelines a stretch of setup code builds, so
// they compile on worker goroutines together instead of one after
// another. Between Device.BeginPipelineBatch and EndPipelineBatch every
// NewPipeline on the device returns at once with its Layout set and its
// Handle still being built. The caller must call Wait before it records a
// command with any of those handles.
type PipelineBatch struct {
	pipes []*Pipeline
}

// Wait blocks until every pipeline in the batch is built and returns the
// first error among them.
func (b *PipelineBatch) Wait() error {
	if b == nil {
		return nil
	}
	var first error
	for _, p := range b.pipes {
		if err := p.Wait(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// BeginPipelineBatch starts collecting: NewPipeline builds on workers and
// adds to the batch until EndPipelineBatch. Batches do not nest.
func (d *Device) BeginPipelineBatch() *PipelineBatch {
	b := &PipelineBatch{}
	d.batch = b
	return b
}

// EndPipelineBatch stops collecting. The batch's pipelines go on
// building; Wait on the batch, or on each pipeline, before using them.
func (d *Device) EndPipelineBatch() { d.batch = nil }

// NewPipeline builds a graphics pipeline from desc through the device's
// pipeline cache. Inside a PipelineBatch it only starts the build, as
// StartPipeline does.
func (d *Device) NewPipeline(desc PipelineDesc) (*Pipeline, error) {
	if b := d.batch; b != nil {
		p, err := d.StartPipeline(desc)
		if err == nil {
			b.pipes = append(b.pipes, p)
		}
		return p, err
	}
	p, err := d.newLayout(desc)
	if err != nil {
		return nil, err
	}
	if err := p.build(desc); err != nil {
		vk.VkDestroyPipelineLayout(d.Handle, p.Layout, nil)
		return nil, err
	}
	return p, nil
}

// StartPipeline creates the pipeline's layout and starts building the
// pipeline itself on a worker goroutine, returning at once. Call Wait on
// the result before recording a command with its Handle. Several started
// together build in parallel, which is how a set of pipelines costs the
// time of the slowest rather than the sum.
func (d *Device) StartPipeline(desc PipelineDesc) (*Pipeline, error) {
	p, err := d.newLayout(desc)
	if err != nil {
		return nil, err
	}
	desc = desc.clone()
	p.done = make(chan struct{})
	workers := d.pipelineWorkers()
	go func() {
		workers <- struct{}{}
		defer func() { <-workers }()
		p.err = p.build(desc)
		close(p.done)
	}()
	return p, nil
}

// NewPipelines builds several pipelines in parallel and waits for them
// all. On an error the ones that were built are destroyed.
func (d *Device) NewPipelines(descs []PipelineDesc) ([]*Pipeline, error) {
	out := make([]*Pipeline, 0, len(descs))
	var first error
	for _, desc := range descs {
		p, err := d.StartPipeline(desc)
		if err != nil {
			first = err
			break
		}
		out = append(out, p)
	}
	for _, p := range out {
		if err := p.Wait(); err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		for _, p := range out {
			p.Destroy()
		}
		return nil, first
	}
	return out, nil
}

// pipelineWorkers returns the tokens that bound how many pipelines build
// at once: one fewer than the processors, so the goroutine that asked
// keeps one, and at least one.
func (d *Device) pipelineWorkers() chan struct{} {
	ps := &d.pipes
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.workers == nil {
		ps.workers = make(chan struct{}, max(runtime.NumCPU()-1, 1))
	}
	return ps.workers
}

// clone copies the slices a description refers to, so a build on a
// worker does not share them with a caller that goes on to change them.
func (desc PipelineDesc) clone() PipelineDesc {
	desc.Bindings = slices.Clone(desc.Bindings)
	desc.Attributes = slices.Clone(desc.Attributes)
	desc.SetLayouts = slices.Clone(desc.SetLayouts)
	desc.ExtraColor = slices.Clone(desc.ExtraColor)
	for i, a := range desc.ExtraColor {
		if a.Factors != nil {
			f := *a.Factors
			desc.ExtraColor[i].Factors = &f
		}
	}
	if desc.Factors != nil {
		f := *desc.Factors
		desc.Factors = &f
	}
	if desc.Stencil != nil {
		s := *desc.Stencil
		desc.Stencil = &s
	}
	return desc
}

// newLayout creates a pipeline's layout, which is cheap and which the
// caller may bind against before the pipeline is built.
func (d *Device) newLayout(desc PipelineDesc) (*Pipeline, error) {
	p := &Pipeline{dev: d}
	var pushRange vk.VkPushConstantRange
	layoutInfo := vk.VkPipelineLayoutCreateInfo{
		SType:          vk.VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO,
		SetLayoutCount: uint32(len(desc.SetLayouts)),
		PSetLayouts:    firstOrNil(desc.SetLayouts),
	}
	if desc.PushConstantSize > 0 {
		pushRange = vk.VkPushConstantRange{StageFlags: vk.VK_SHADER_STAGE_VERTEX_BIT | vk.VK_SHADER_STAGE_FRAGMENT_BIT, Size: desc.PushConstantSize}
		layoutInfo.PushConstantRangeCount = 1
		layoutInfo.PPushConstantRanges = &pushRange
	}
	if err := vk.Check("vkCreatePipelineLayout", vk.VkCreatePipelineLayout(d.Handle, &layoutInfo, nil, &p.Layout)); err != nil {
		return nil, err
	}
	return p, nil
}

// build compiles the pipeline into p.Handle through the device's cache,
// with the shared modules for its programs. It is safe to run on several
// goroutines at once for different pipelines.
func (p *Pipeline) build(desc PipelineDesc) error {
	d := p.dev
	cache, err := d.pipelineCache()
	if err != nil {
		return err
	}
	vert, err := d.shaderModule(desc.Vert)
	if err != nil {
		return err
	}
	frag, err := d.shaderModule(desc.Frag)
	if err != nil {
		d.releaseModule(vert)
		return err
	}
	p.mods = [2]*shaderModule{vert, frag}
	if desc.FrontFace == 0 {
		desc.FrontFace = vk.VK_FRONT_FACE_COUNTER_CLOCKWISE
	}
	entry, keep := vk.CString("main")
	defer func() { _ = keep }()
	stages := []vk.VkPipelineShaderStageCreateInfo{
		{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, Stage: vk.VK_SHADER_STAGE_VERTEX_BIT, Module: vert.handle, PName: entry},
		{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, Stage: vk.VK_SHADER_STAGE_FRAGMENT_BIT, Module: frag.handle, PName: entry},
	}

	vertexInput := vk.VkPipelineVertexInputStateCreateInfo{
		SType:                           vk.VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO,
		VertexBindingDescriptionCount:   uint32(len(desc.Bindings)),
		PVertexBindingDescriptions:      firstOrNil(desc.Bindings),
		VertexAttributeDescriptionCount: uint32(len(desc.Attributes)),
		PVertexAttributeDescriptions:    firstOrNil(desc.Attributes),
	}
	topology := desc.Topology
	if topology == 0 {
		topology = vk.VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST
	}
	assembly := vk.VkPipelineInputAssemblyStateCreateInfo{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO, Topology: topology}
	viewport := vk.VkPipelineViewportStateCreateInfo{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO, ViewportCount: 1, ScissorCount: 1}
	raster := vk.VkPipelineRasterizationStateCreateInfo{
		SType:       vk.VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO,
		PolygonMode: vk.VK_POLYGON_MODE_FILL,
		CullMode:    desc.CullMode,
		FrontFace:   desc.FrontFace,
		LineWidth:   1,
	}
	if desc.DepthClamp && d.depthClamp {
		raster.DepthClampEnable = vk.VK_TRUE
	}
	if desc.DepthBias != 0 || desc.DepthSlopeBias != 0 {
		raster.DepthBiasEnable = vk.VK_TRUE
		raster.DepthBiasConstantFactor = desc.DepthBias
		raster.DepthBiasSlopeFactor = desc.DepthSlopeBias
	}
	samples := desc.Samples
	if samples == 0 {
		samples = vk.VK_SAMPLE_COUNT_1_BIT
	}
	// Coverage is per sample, shading per pixel: the standard trade that
	// makes multisampling cheap. Nothing asks for sample shading.
	multisample := vk.VkPipelineMultisampleStateCreateInfo{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO, RasterizationSamples: samples}
	compare := desc.DepthCompare
	if compare == 0 {
		compare = vk.VK_COMPARE_OP_LESS_OR_EQUAL
	}
	depth := vk.VkPipelineDepthStencilStateCreateInfo{
		SType:            vk.VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO,
		DepthTestEnable:  boolean(desc.DepthTest),
		DepthWriteEnable: boolean(desc.DepthWrite),
		DepthCompareOp:   compare,
	}
	if s := desc.Stencil; s != nil && HasStencil(desc.DepthFormat) {
		op := vk.VkStencilOpState{
			FailOp: s.Fail, PassOp: s.Pass, DepthFailOp: s.DepthFail,
			CompareOp: s.Compare, CompareMask: s.ReadMask, Reference: s.Ref,
		}
		if op.CompareMask == 0 {
			op.CompareMask = 0xff
		}
		if s.Write {
			op.WriteMask = s.WriteMask
			if op.WriteMask == 0 {
				op.WriteMask = 0xff
			}
		}
		depth.StencilTestEnable = vk.VK_TRUE
		depth.Front, depth.Back = op, op
	}
	attachments := []vk.VkPipelineColorBlendAttachmentState{blendState(desc.Blend, desc.Factors)}
	if desc.NoColorWrite {
		attachments[0].ColorWriteMask = 0
	}
	formats := []vk.VkFormat{desc.ColorFormat}
	for _, a := range desc.ExtraColor {
		attachments = append(attachments, blendState(a.Blend, a.Factors))
		formats = append(formats, a.Format)
	}
	blend := vk.VkPipelineColorBlendStateCreateInfo{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO,
		AttachmentCount: uint32(len(attachments)), PAttachments: &attachments[0]}
	if desc.NoColor {
		blend.AttachmentCount = 0
		blend.PAttachments = nil
	}
	dynamicStates := []vk.VkDynamicState{vk.VK_DYNAMIC_STATE_VIEWPORT, vk.VK_DYNAMIC_STATE_SCISSOR}
	dynamic := vk.VkPipelineDynamicStateCreateInfo{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO, DynamicStateCount: 2, PDynamicStates: &dynamicStates[0]}
	rendering := vk.VkPipelineRenderingCreateInfo{
		SType:                   vk.VK_STRUCTURE_TYPE_PIPELINE_RENDERING_CREATE_INFO,
		ColorAttachmentCount:    uint32(len(formats)),
		PColorAttachmentFormats: &formats[0],
		DepthAttachmentFormat:   desc.DepthFormat,
	}
	if HasStencil(desc.DepthFormat) {
		rendering.StencilAttachmentFormat = desc.DepthFormat
	}
	if desc.NoColor {
		rendering.ColorAttachmentCount = 0
		rendering.PColorAttachmentFormats = nil
	}
	info := vk.VkGraphicsPipelineCreateInfo{
		SType:               vk.VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO,
		PNext:               unsafe.Pointer(&rendering),
		StageCount:          2,
		PStages:             &stages[0],
		PVertexInputState:   &vertexInput,
		PInputAssemblyState: &assembly,
		PViewportState:      &viewport,
		PRasterizationState: &raster,
		PMultisampleState:   &multisample,
		PDepthStencilState:  &depth,
		PColorBlendState:    &blend,
		PDynamicState:       &dynamic,
		Layout:              p.Layout,
	}
	var handle vk.VkPipeline
	if err := vk.Check("vkCreateGraphicsPipelines", vk.VkCreateGraphicsPipelines(d.Handle, cache, 1, &info, nil, &handle)); err != nil {
		d.releaseModule(vert)
		d.releaseModule(frag)
		p.mods = [2]*shaderModule{}
		return err
	}
	p.Handle = handle
	d.builtPipeline()
	return nil
}

// blendState is one attachment's colour write mask and blend equation.
func blendState(enabled bool, factors *BlendFactors) vk.VkPipelineColorBlendAttachmentState {
	st := vk.VkPipelineColorBlendAttachmentState{
		ColorWriteMask: vk.VK_COLOR_COMPONENT_R_BIT | vk.VK_COLOR_COMPONENT_G_BIT | vk.VK_COLOR_COMPONENT_B_BIT | vk.VK_COLOR_COMPONENT_A_BIT,
	}
	if !enabled {
		return st
	}
	f := PremultipliedOver
	if factors != nil {
		f = *factors
	}
	st.BlendEnable = vk.VK_TRUE
	st.SrcColorBlendFactor = f.SrcColor
	st.DstColorBlendFactor = f.DstColor
	st.ColorBlendOp = f.ColorOp
	st.SrcAlphaBlendFactor = f.SrcAlpha
	st.DstAlphaBlendFactor = f.DstAlpha
	st.AlphaBlendOp = f.AlphaOp
	return st
}

// Destroy frees the pipeline and its layout, first waiting for a build
// still running on a worker. It must not be in use by a frame in flight.
func (p *Pipeline) Destroy() {
	if p == nil || p.Layout == 0 {
		return
	}
	_ = p.Wait()
	d := p.dev
	if p.Handle != 0 {
		vk.VkDestroyPipeline(d.Handle, p.Handle, nil)
		p.Handle = 0
	}
	vk.VkDestroyPipelineLayout(d.Handle, p.Layout, nil)
	p.Layout = 0
	d.releaseModule(p.mods[0])
	d.releaseModule(p.mods[1])
	p.mods = [2]*shaderModule{}
}

// The viewport and scissor commands take a pointer to their rectangle, and
// a pointer to a local variable would be forced onto the heap once per
// call. These two live for the process instead and are filled in place.
// Commands are recorded from the goroutine that owns the device, so there
// is one recorder at a time.
var (
	viewportScratch vk.VkViewport
	scissorScratch  vk.VkRect2D
)

// SetViewport records a full-extent viewport and scissor. Vulkan's viewport
// is flipped here so that +Y is down, matching 2D screen conventions.
func SetViewport(cb vk.VkCommandBuffer, extent vk.VkExtent2D) {
	viewportScratch = vk.VkViewport{Width: float32(extent.Width), Height: float32(extent.Height), MaxDepth: 1}
	scissorScratch = vk.VkRect2D{Extent: extent}
	vk.CmdSetViewport(cb, 0, 1, &viewportScratch)
	vk.CmdSetScissor(cb, 0, 1, &scissorScratch)
}

func boolean(b bool) vk.VkBool32 {
	if b {
		return vk.VK_TRUE
	}
	return vk.VK_FALSE
}

func firstOrNil[T any](s []T) *T {
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}

// SetScissor limits rasterisation to a pixel rectangle clamped to the
// extent; full resets to the whole extent.
func SetScissor(cb vk.VkCommandBuffer, extent vk.VkExtent2D, x, y int32, w, h uint32, full bool) {
	scissorScratch = vk.VkRect2D{Extent: extent}
	if !full {
		x = max(x, 0)
		y = max(y, 0)
		w = min(w, uint32(max(int32(extent.Width)-x, 0)))
		h = min(h, uint32(max(int32(extent.Height)-y, 0)))
		scissorScratch = vk.VkRect2D{Offset: vk.VkOffset2D{X: x, Y: y}, Extent: vk.VkExtent2D{Width: w, Height: h}}
	}
	vk.CmdSetScissor(cb, 0, 1, &scissorScratch)
}

// SetViewportRect sets the viewport to a pixel rectangle.
func SetViewportRect(cb vk.VkCommandBuffer, r vk.VkRect2D) {
	viewportScratch = vk.VkViewport{X: float32(r.Offset.X), Y: float32(r.Offset.Y), Width: float32(r.Extent.Width), Height: float32(r.Extent.Height), MaxDepth: 1}
	vk.CmdSetViewport(cb, 0, 1, &viewportScratch)
}

// SetScissorRect limits rasterisation to a pixel rectangle.
func SetScissorRect(cb vk.VkCommandBuffer, r vk.VkRect2D) {
	scissorScratch = r
	vk.CmdSetScissor(cb, 0, 1, &scissorScratch)
}

// ClearRect fills a rectangle of the pass's colour attachment with a
// colour, inside the pass.
func ClearRect(cb vk.VkCommandBuffer, r vk.VkRect2D, color [4]float32) {
	var value vk.VkClearValue
	*value.Color().Float32() = color
	att := vk.VkClearAttachment{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, ColorAttachment: 0, ClearValue: value}
	rect := vk.VkClearRect{Rect: r, BaseArrayLayer: 0, LayerCount: 1}
	vk.VkCmdClearAttachments(cb, 1, &att, 1, &rect)
}
