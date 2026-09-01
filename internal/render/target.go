package render

import "github.com/matjam/bunyip/internal/vk"

// Target is an offscreen render target: a colour image and optionally a
// depth image, both sampleable afterwards. Scene rendering goes to an HDR
// target before post-processing; shadow maps are depth-only targets.
type Target struct {
	Color  *Image // nil for depth-only targets
	Depth  *Image
	Extent vk.VkExtent2D
	dev    *Device
}

// NewTarget creates a colour (and depth) target of the given formats; pass
// VK_FORMAT_UNDEFINED to omit either image.
func (d *Device) NewTarget(extent vk.VkExtent2D, colorFormat, depthFormat vk.VkFormat) (*Target, error) {
	t := &Target{Extent: extent, dev: d}
	var err error
	if colorFormat != vk.VK_FORMAT_UNDEFINED {
		t.Color, err = d.NewImage(extent, colorFormat,
			vk.VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|vk.VK_IMAGE_USAGE_SAMPLED_BIT, vk.VK_IMAGE_ASPECT_COLOR_BIT)
		if err != nil {
			return nil, err
		}
	}
	if depthFormat != vk.VK_FORMAT_UNDEFINED {
		t.Depth, err = d.NewImage(extent, depthFormat,
			vk.VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT|vk.VK_IMAGE_USAGE_SAMPLED_BIT, vk.VK_IMAGE_ASPECT_DEPTH_BIT)
		if err != nil {
			t.Destroy()
			return nil, err
		}
	}
	return t, nil
}

func (t *Target) Destroy() {
	if t.Color != nil {
		t.Color.Destroy()
		t.Color = nil
	}
	if t.Depth != nil {
		t.Depth.Destroy()
		t.Depth = nil
	}
}

// PassDesc describes one dynamic-rendering pass into a target.
type PassDesc struct {
	Target     *Target
	ClearColor [4]float32
	ClearDepth float32 // 1 for a normal depth pass
	LoadColor  bool    // keep existing colour instead of clearing
}

// BeginTargetPass transitions the target's images for rendering and opens a
// dynamic-rendering pass. The colour image ends in colour-attachment layout
// and the depth image in depth-attachment layout; EndTargetPass moves both
// to shader-read-only so the next pass can sample them.
func BeginTargetPass(cb vk.VkCommandBuffer, p PassDesc) {
	t := p.Target
	var color vk.VkRenderingAttachmentInfo
	var depth vk.VkRenderingAttachmentInfo
	info := vk.VkRenderingInfo{
		SType:      vk.VK_STRUCTURE_TYPE_RENDERING_INFO,
		RenderArea: vk.VkRect2D{Extent: t.Extent},
		LayerCount: 1,
	}
	if t.Color != nil {
		imageBarrier(cb, t.Color.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
			vk.VK_IMAGE_LAYOUT_UNDEFINED, vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT,
			vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT)
		var clear vk.VkClearValue
		*clear.Color().Float32() = p.ClearColor
		color = vk.VkRenderingAttachmentInfo{
			SType:       vk.VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO,
			ImageView:   t.Color.View,
			ImageLayout: vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
			LoadOp:      vk.VK_ATTACHMENT_LOAD_OP_CLEAR,
			StoreOp:     vk.VK_ATTACHMENT_STORE_OP_STORE,
			ClearValue:  clear,
		}
		if p.LoadColor {
			color.LoadOp = vk.VK_ATTACHMENT_LOAD_OP_LOAD
		}
		info.ColorAttachmentCount = 1
		info.PColorAttachments = &color
	}
	if t.Depth != nil {
		imageBarrier(cb, t.Depth.Handle, vk.VK_IMAGE_ASPECT_DEPTH_BIT,
			vk.VK_IMAGE_LAYOUT_UNDEFINED, vk.VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT,
			vk.VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT|vk.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT,
			vk.VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT)
		var clear vk.VkClearValue
		*clear.DepthStencil() = vk.VkClearDepthStencilValue{Depth: p.ClearDepth}
		depth = vk.VkRenderingAttachmentInfo{
			SType:       vk.VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO,
			ImageView:   t.Depth.View,
			ImageLayout: vk.VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL,
			LoadOp:      vk.VK_ATTACHMENT_LOAD_OP_CLEAR,
			StoreOp:     vk.VK_ATTACHMENT_STORE_OP_STORE,
			ClearValue:  clear,
		}
		info.PDepthAttachment = &depth
	}
	vk.VkCmdBeginRendering(cb, &info)
	SetViewport(cb, t.Extent)
}

// EndTargetPass closes the pass and makes the images readable by shaders.
func EndTargetPass(cb vk.VkCommandBuffer, t *Target) {
	vk.VkCmdEndRendering(cb)
	if t.Color != nil {
		imageBarrier(cb, t.Color.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
			vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
	}
	if t.Depth != nil {
		imageBarrier(cb, t.Depth.Handle, vk.VK_IMAGE_ASPECT_DEPTH_BIT,
			vk.VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT, vk.VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
	}
}
