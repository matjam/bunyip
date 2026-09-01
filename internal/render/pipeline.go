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
	Blend            bool // premultiplied alpha blending
	DepthTest        bool
	DepthWrite       bool
	PushConstantSize uint32 // bytes visible to all stages, 0 for none
	SetLayouts       []vk.VkDescriptorSetLayout
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
		FrontFace:   vk.VK_FRONT_FACE_COUNTER_CLOCKWISE,
		LineWidth:   1,
	}
	multisample := vk.VkPipelineMultisampleStateCreateInfo{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO, RasterizationSamples: vk.VK_SAMPLE_COUNT_1_BIT}
	depth := vk.VkPipelineDepthStencilStateCreateInfo{
		SType:            vk.VK_STRUCTURE_TYPE_PIPELINE_DEPTH_STENCIL_STATE_CREATE_INFO,
		DepthTestEnable:  boolean(desc.DepthTest),
		DepthWriteEnable: boolean(desc.DepthWrite),
		DepthCompareOp:   vk.VK_COMPARE_OP_LESS_OR_EQUAL,
	}
	blendAttachment := vk.VkPipelineColorBlendAttachmentState{
		ColorWriteMask: vk.VK_COLOR_COMPONENT_R_BIT | vk.VK_COLOR_COMPONENT_G_BIT | vk.VK_COLOR_COMPONENT_B_BIT | vk.VK_COLOR_COMPONENT_A_BIT,
	}
	if desc.Blend {
		blendAttachment.BlendEnable = vk.VK_TRUE
		blendAttachment.SrcColorBlendFactor = vk.VK_BLEND_FACTOR_ONE
		blendAttachment.DstColorBlendFactor = vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA
		blendAttachment.ColorBlendOp = vk.VK_BLEND_OP_ADD
		blendAttachment.SrcAlphaBlendFactor = vk.VK_BLEND_FACTOR_ONE
		blendAttachment.DstAlphaBlendFactor = vk.VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA
		blendAttachment.AlphaBlendOp = vk.VK_BLEND_OP_ADD
	}
	blend := vk.VkPipelineColorBlendStateCreateInfo{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO, AttachmentCount: 1, PAttachments: &blendAttachment}
	dynamicStates := []vk.VkDynamicState{vk.VK_DYNAMIC_STATE_VIEWPORT, vk.VK_DYNAMIC_STATE_SCISSOR}
	dynamic := vk.VkPipelineDynamicStateCreateInfo{SType: vk.VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO, DynamicStateCount: 2, PDynamicStates: &dynamicStates[0]}
	rendering := vk.VkPipelineRenderingCreateInfo{
		SType:                   vk.VK_STRUCTURE_TYPE_PIPELINE_RENDERING_CREATE_INFO,
		ColorAttachmentCount:    1,
		PColorAttachmentFormats: &desc.ColorFormat,
		DepthAttachmentFormat:   desc.DepthFormat,
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

func (p *Pipeline) Destroy() {
	if p.Handle != 0 {
		vk.VkDestroyPipeline(p.dev.Handle, p.Handle, nil)
		vk.VkDestroyPipelineLayout(p.dev.Handle, p.Layout, nil)
		p.Handle = 0
	}
}

// SetViewport records a full-extent viewport and scissor. Vulkan's viewport
// is flipped here so that +Y is down, matching 2D screen conventions.
func SetViewport(cb vk.VkCommandBuffer, extent vk.VkExtent2D) {
	viewport := vk.VkViewport{Width: float32(extent.Width), Height: float32(extent.Height), MaxDepth: 1}
	scissor := vk.VkRect2D{Extent: extent}
	vk.VkCmdSetViewport(cb, 0, 1, &viewport)
	vk.VkCmdSetScissor(cb, 0, 1, &scissor)
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
