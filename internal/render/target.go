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
	return d.newTarget(extent, colorFormat, depthFormat, 0)
}

// NewTargetSampled is NewTarget with a colour image that can also be
// cleared by transfer, for render textures that may be sampled before
// their first pass.
func (d *Device) NewTargetSampled(extent vk.VkExtent2D, colorFormat, depthFormat vk.VkFormat) (*Target, error) {
	return d.newTarget(extent, colorFormat, depthFormat, vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT)
}

// NewTargetReadable is NewTargetSampled with a colour image that can also
// be read back to the host with ReadImage.
func (d *Device) NewTargetReadable(extent vk.VkExtent2D, colorFormat, depthFormat vk.VkFormat) (*Target, error) {
	return d.newTarget(extent, colorFormat, depthFormat, vk.VkImageUsageFlags(vk.VK_IMAGE_USAGE_SAMPLED_BIT|vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT))
}

// NewTargetCopyable is NewTarget with a colour image that transfers can
// read, for snapshots taken with CopyColorForSampling.
func (d *Device) NewTargetCopyable(extent vk.VkExtent2D, colorFormat, depthFormat vk.VkFormat) (*Target, error) {
	return d.newTarget(extent, colorFormat, depthFormat, vk.VkImageUsageFlags(vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT))
}

func (d *Device) newTarget(extent vk.VkExtent2D, colorFormat, depthFormat vk.VkFormat, extra vk.VkImageUsageFlags) (*Target, error) {
	t := &Target{Extent: extent, dev: d}
	var err error
	if colorFormat != vk.VK_FORMAT_UNDEFINED {
		t.Color, err = d.NewImage(extent, colorFormat,
			vk.VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|vk.VK_IMAGE_USAGE_SAMPLED_BIT|extra, vk.VK_IMAGE_ASPECT_COLOR_BIT)
		if err != nil {
			return nil, err
		}
	}
	if depthFormat != vk.VK_FORMAT_UNDEFINED {
		t.Depth, err = d.NewImage(extent, depthFormat,
			vk.VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT|vk.VK_IMAGE_USAGE_SAMPLED_BIT|vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT, vk.VK_IMAGE_ASPECT_DEPTH_BIT)
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
	LoadDepth  bool    // keep existing depth (and stencil) instead of clearing
	NoDepth    bool    // render without the depth attachment, leaving the depth image readable
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
		// Loading keeps the contents, so the transition must start from the
		// layout the last pass left rather than discarding with UNDEFINED.
		was := vk.VkImageLayout(vk.VK_IMAGE_LAYOUT_UNDEFINED)
		if p.LoadColor {
			was = vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL
		}
		imageBarrier(cb, t.Color.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
			was, vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
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
	var stencil vk.VkRenderingAttachmentInfo
	if t.Depth != nil && !p.NoDepth {
		format := t.Depth.Format
		was := vk.VkImageLayout(vk.VK_IMAGE_LAYOUT_UNDEFINED)
		if p.LoadDepth {
			was = vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL
		}
		imageBarrier(cb, t.Depth.Handle, depthAspect(format),
			was, depthLayout(format),
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT,
			vk.VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT|vk.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT,
			vk.VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT)
		var clear vk.VkClearValue
		*clear.DepthStencil() = vk.VkClearDepthStencilValue{Depth: p.ClearDepth}
		depth = vk.VkRenderingAttachmentInfo{
			SType:       vk.VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO,
			ImageView:   t.Depth.AttachView,
			ImageLayout: depthLayout(format),
			LoadOp:      vk.VK_ATTACHMENT_LOAD_OP_CLEAR,
			StoreOp:     vk.VK_ATTACHMENT_STORE_OP_STORE,
			ClearValue:  clear,
		}
		if p.LoadDepth {
			depth.LoadOp = vk.VK_ATTACHMENT_LOAD_OP_LOAD
		}
		info.PDepthAttachment = &depth
		if HasStencil(format) {
			stencil = depth
			info.PStencilAttachment = &stencil
		}
	}
	vk.VkCmdBeginRendering(cb, &info)
	SetViewport(cb, t.Extent)
}

// EndTargetPass closes the pass and makes the images readable by shaders.
func EndTargetPass(cb vk.VkCommandBuffer, t *Target) {
	EndTargetPassDesc(cb, PassDesc{Target: t})
}

// EndTargetPassDesc is EndTargetPass for a pass begun with the same
// description, so a pass without a depth attachment leaves the depth
// image as it was.
func EndTargetPassDesc(cb vk.VkCommandBuffer, p PassDesc) {
	t := p.Target
	vk.VkCmdEndRendering(cb)
	if t.Color != nil {
		imageBarrier(cb, t.Color.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
			vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
	}
	if t.Depth != nil && !p.NoDepth {
		format := t.Depth.Format
		imageBarrier(cb, t.Depth.Handle, depthAspect(format),
			depthLayout(format), vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT, vk.VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
	}
}

// ClearDepthForSampling clears a depth image to 1 (nothing occluded) and
// leaves it in shader-read-only layout, for shadow maps that are bound
// before any shadow pass has run.
func ClearDepthForSampling(cb vk.VkCommandBuffer, depth *Image) {
	aspect := depthAspect(depth.Format)
	imageBarrier(cb, depth.Handle, aspect,
		vk.VK_IMAGE_LAYOUT_UNDEFINED, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT, 0,
		vk.VK_PIPELINE_STAGE_2_CLEAR_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
	value := vk.VkClearDepthStencilValue{Depth: 1}
	rng := vk.VkImageSubresourceRange{AspectMask: aspect, LevelCount: 1, LayerCount: 1}
	vk.VkCmdClearDepthStencilImage(cb, depth.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, &value, 1, &rng)
	imageBarrier(cb, depth.Handle, aspect,
		vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_CLEAR_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
		vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
}

// ClearColorForSampling clears a colour image (every mip level) to black
// and leaves it in shader-read-only layout, so it can be sampled before
// its first pass.
func ClearColorForSampling(cb vk.VkCommandBuffer, img *Image) {
	mips := max(img.Mips, 1)
	imageBarrierLevels(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT, 0, mips,
		vk.VK_IMAGE_LAYOUT_UNDEFINED, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT, 0,
		vk.VK_PIPELINE_STAGE_2_CLEAR_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
	var value vk.VkClearColorValue
	rng := vk.VkImageSubresourceRange{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, LevelCount: mips, LayerCount: 1}
	vk.VkCmdClearColorImage(cb, img.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, &value, 1, &rng)
	imageBarrierLevels(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT, 0, mips,
		vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_CLEAR_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
		vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
}

// CopyColorForSampling copies a colour image that a finished pass left in
// shader-read-only layout into dst, fills dst's mip chain with blurred
// copies, and leaves both readable by shaders. dst may be a different
// size; the copy is a linear blit.
func CopyColorForSampling(cb vk.VkCommandBuffer, src, dst *Image) {
	imageBarrier(cb, src.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
		vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL, vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT,
		vk.VK_PIPELINE_STAGE_2_BLIT_BIT, vk.VK_ACCESS_2_TRANSFER_READ_BIT)
	mips := max(dst.Mips, 1)
	imageBarrierLevels(cb, dst.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT, 0, mips,
		vk.VK_IMAGE_LAYOUT_UNDEFINED, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT,
		vk.VK_PIPELINE_STAGE_2_BLIT_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
	blit := vk.VkImageBlit{
		SrcSubresource: vk.VkImageSubresourceLayers{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, LayerCount: 1},
		DstSubresource: vk.VkImageSubresourceLayers{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, LayerCount: 1},
	}
	blit.SrcOffsets[1] = vk.VkOffset3D{X: int32(src.Extent.Width), Y: int32(src.Extent.Height), Z: 1}
	blit.DstOffsets[1] = vk.VkOffset3D{X: int32(dst.Extent.Width), Y: int32(dst.Extent.Height), Z: 1}
	vk.VkCmdBlitImage(cb, src.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, dst.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &blit, vk.VK_FILTER_LINEAR)
	imageBarrier(cb, src.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
		vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_BLIT_BIT, vk.VK_ACCESS_2_TRANSFER_READ_BIT,
		vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT|vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT,
		vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT|vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT)
	generateMips(cb, dst)
}
