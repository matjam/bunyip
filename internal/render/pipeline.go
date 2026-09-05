package render

import (
	"fmt"
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
// when (stencil & 0xff) Compare Ref, and passing fragments apply Pass
// to the stored value when Write is set.
type StencilState struct {
	Compare vk.VkCompareOp
	Ref     uint32
	Write   bool
	Pass    vk.VkStencilOp
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
type Pipeline struct {
	Handle vk.VkPipeline
	Layout vk.VkPipelineLayout
	dev    *Device
}

func (d *Device) newShaderModule(spirv []byte) (vk.VkShaderModule, error) {
	if len(spirv) == 0 || len(spirv)%4 != 0 {
		return 0, fmt.Errorf("render: SPIR-V of %d bytes is not a whole number of words", len(spirv))
	}
	info := vk.VkShaderModuleCreateInfo{
		SType:    vk.VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO,
		CodeSize: uintptr(len(spirv)),
		PCode:    (*uint32)(unsafe.Pointer(&spirv[0])),
	}
	var mod vk.VkShaderModule
	err := vk.Check("vkCreateShaderModule", vk.VkCreateShaderModule(d.Handle, &info, nil, &mod))
	return mod, err
}

// NewPipeline builds a graphics pipeline from desc.
func (d *Device) NewPipeline(desc PipelineDesc) (*Pipeline, error) {
	vert, err := d.newShaderModule(desc.Vert)
	if err != nil {
		return nil, err
	}
	defer vk.VkDestroyShaderModule(d.Handle, vert, nil)
	frag, err := d.newShaderModule(desc.Frag)
	if err != nil {
		return nil, err
	}
	defer vk.VkDestroyShaderModule(d.Handle, frag, nil)
	if desc.FrontFace == 0 {
		desc.FrontFace = vk.VK_FRONT_FACE_COUNTER_CLOCKWISE
	}

	p := &Pipeline{dev: d}
	entry, keep := vk.CString("main")
	defer func() { _ = keep }()
	stages := []vk.VkPipelineShaderStageCreateInfo{
		{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, Stage: vk.VK_SHADER_STAGE_VERTEX_BIT, Module: vert, PName: entry},
		{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, Stage: vk.VK_SHADER_STAGE_FRAGMENT_BIT, Module: frag, PName: entry},
	}
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
			FailOp: vk.VK_STENCIL_OP_KEEP, PassOp: s.Pass, DepthFailOp: vk.VK_STENCIL_OP_KEEP,
			CompareOp: s.Compare, CompareMask: 0xff, Reference: s.Ref,
		}
		if s.Write {
			op.WriteMask = 0xff
		}
		depth.StencilTestEnable = vk.VK_TRUE
		depth.Front, depth.Back = op, op
	}
	attachments := []vk.VkPipelineColorBlendAttachmentState{blendState(desc.Blend, desc.Factors)}
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
	if err := vk.Check("vkCreateGraphicsPipelines", vk.VkCreateGraphicsPipelines(d.Handle, 0, 1, &info, nil, &p.Handle)); err != nil {
		vk.VkDestroyPipelineLayout(d.Handle, p.Layout, nil)
		return nil, err
	}
	return p, nil
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

func (p *Pipeline) Destroy() {
	if p.Handle != 0 {
		vk.VkDestroyPipeline(p.dev.Handle, p.Handle, nil)
		vk.VkDestroyPipelineLayout(p.dev.Handle, p.Layout, nil)
		p.Handle = 0
	}
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
