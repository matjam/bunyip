package render

import "github.com/matjam/bunyip/internal/vk"

// Target is an offscreen render target: a colour image and optionally a
// depth image, both sampleable afterwards. Scene rendering goes to an HDR
// target before post-processing; shadow maps are depth-only targets.
//
// A multisampled target keeps the attachments a pass renders into in
// MSColor and MSDepth and resolves them into Color and Depth when the
// pass ends, so everything downstream reads the same single-sample
// images whatever the sample count.
type Target struct {
	Color   *Image // nil for depth-only targets; the resolve destination when multisampled
	Depth   *Image
	MSColor *Image                   // the multisampled colour attachment, nil at one sample
	MSDepth *Image                   // the multisampled depth attachment, nil at one sample
	Samples vk.VkSampleCountFlagBits // one unless the target is multisampled
	Extent  vk.VkExtent2D
	dev     *Device
}

// TargetDesc describes an offscreen target. The zero value of every
// field is the plain case: a single-sample target with no extra usage.
type TargetDesc struct {
	Extent      vk.VkExtent2D
	ColorFormat vk.VkFormat              // VK_FORMAT_UNDEFINED for a depth-only target
	DepthFormat vk.VkFormat              // VK_FORMAT_UNDEFINED for no depth attachment
	Samples     vk.VkSampleCountFlagBits // zero or one renders without multisampling
	ColorUsage  vk.VkImageUsageFlags     // added to the single-sample colour image
	DepthUsage  vk.VkImageUsageFlags     // added to the depth image
}

// NewTarget creates a colour (and depth) target of the given formats; pass
// VK_FORMAT_UNDEFINED to omit either image.
func (d *Device) NewTarget(extent vk.VkExtent2D, colorFormat, depthFormat vk.VkFormat) (*Target, error) {
	return d.NewTargetDesc(TargetDesc{Extent: extent, ColorFormat: colorFormat, DepthFormat: depthFormat})
}

// NewTargetSampled is NewTarget with a colour image that can also be
// cleared by transfer, for render textures that may be sampled before
// their first pass.
func (d *Device) NewTargetSampled(extent vk.VkExtent2D, colorFormat, depthFormat vk.VkFormat) (*Target, error) {
	return d.NewTargetDesc(TargetDesc{Extent: extent, ColorFormat: colorFormat, DepthFormat: depthFormat,
		ColorUsage: vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT})
}

// NewTargetReadable is NewTargetSampled with a colour image that can also
// be read back to the host with ReadImage.
func (d *Device) NewTargetReadable(extent vk.VkExtent2D, colorFormat, depthFormat vk.VkFormat) (*Target, error) {
	return d.NewTargetDesc(TargetDesc{Extent: extent, ColorFormat: colorFormat, DepthFormat: depthFormat,
		ColorUsage: vk.VK_IMAGE_USAGE_SAMPLED_BIT | vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT})
}

// NewTargetCopyable is NewTarget with a colour image that transfers can
// read, for snapshots taken with CopyColorForSampling.
func (d *Device) NewTargetCopyable(extent vk.VkExtent2D, colorFormat, depthFormat vk.VkFormat) (*Target, error) {
	return d.NewTargetDesc(TargetDesc{Extent: extent, ColorFormat: colorFormat, DepthFormat: depthFormat,
		ColorUsage: vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT})
}

// NewTargetDesc creates a target from a full description.
func (d *Device) NewTargetDesc(desc TargetDesc) (*Target, error) {
	samples := desc.Samples
	if samples == 0 {
		samples = vk.VK_SAMPLE_COUNT_1_BIT
	}
	t := &Target{Extent: desc.Extent, Samples: samples, dev: d}
	ms := samples != vk.VK_SAMPLE_COUNT_1_BIT
	var err error
	fail := func(err error) (*Target, error) {
		t.Destroy()
		return nil, err
	}
	if desc.ColorFormat != vk.VK_FORMAT_UNDEFINED {
		// Transfer destination so ClearColorForSampling can clear it
		// before its first pass.
		t.Color, err = d.NewImage(desc.Extent, desc.ColorFormat,
			vk.VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT|vk.VK_IMAGE_USAGE_SAMPLED_BIT|vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT|desc.ColorUsage,
			vk.VK_IMAGE_ASPECT_COLOR_BIT)
		if err != nil {
			return fail(err)
		}
		if ms {
			// Nothing samples or copies the multisampled attachment; the
			// resolve into Color is what the rest of the frame reads.
			t.MSColor, err = d.NewImageSamples(desc.Extent, desc.ColorFormat,
				vk.VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT, vk.VK_IMAGE_ASPECT_COLOR_BIT, 1, samples)
			if err != nil {
				return fail(err)
			}
		}
	}
	if desc.DepthFormat != vk.VK_FORMAT_UNDEFINED {
		t.Depth, err = d.NewImage(desc.Extent, desc.DepthFormat,
			vk.VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT|vk.VK_IMAGE_USAGE_SAMPLED_BIT|vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT|desc.DepthUsage,
			vk.VK_IMAGE_ASPECT_DEPTH_BIT)
		if err != nil {
			return fail(err)
		}
		if ms {
			t.MSDepth, err = d.NewImageSamples(desc.Extent, desc.DepthFormat,
				vk.VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT, vk.VK_IMAGE_ASPECT_DEPTH_BIT, 1, samples)
			if err != nil {
				return fail(err)
			}
		}
	}
	return t, nil
}

func (t *Target) Destroy() {
	for _, img := range []**Image{&t.Color, &t.Depth, &t.MSColor, &t.MSDepth} {
		if *img != nil {
			(*img).Destroy()
			*img = nil
		}
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
	// Extra are colour attachments after the target's own, in the order
	// a pipeline's ExtraColor lists them, and at most three. Each is the
	// target's extent, is cleared to the colour at the same index in
	// ExtraClear, and ends readable by shaders like the target's own.
	// They are single-sample and carry no resolve, so a target with them
	// must be single-sample too: every attachment of a pass shares one
	// sample count.
	Extra      []*Image
	ExtraClear [][4]float32
	// Depth is a depth image to render into instead of the target's own,
	// for a pass that shares another target's depth. Nil uses the
	// target's, and LoadDepth applies to whichever is used.
	Depth *Image
}

// depthImage is the depth attachment the pass renders into.
func (p PassDesc) depthImage() *Image {
	if p.Depth != nil {
		return p.Depth
	}
	return p.Target.Depth
}

// The rendering attachment infos are handed to Vulkan by pointer and are
// only read while the call runs. Passes are recorded from the goroutine
// that owns the device, so one set for the process is enough and a local
// slice is not forced onto the heap once a pass.
var colorAttachScratch [4]vk.VkRenderingAttachmentInfo

// BeginTargetPass transitions the target's images for rendering and opens a
// dynamic-rendering pass. The colour image ends in colour-attachment layout
// and the depth image in depth-attachment layout; EndTargetPass moves both
// to shader-read-only so the next pass can sample them.
//
// A multisampled target renders into MSColor and MSDepth and resolves
// into Color and Depth as the pass ends: colour by averaging the
// samples, depth by taking sample zero, which every device supports and
// which costs nothing beyond the resolve the colour attachment already
// needs. The passes that read the scene depth afterwards (ambient
// occlusion, decals, the transmission snapshot) therefore see one exact
// sample per pixel rather than a blend of unrelated depths.
func BeginTargetPass(cb vk.VkCommandBuffer, p PassDesc) {
	t := p.Target
	var depth vk.VkRenderingAttachmentInfo
	info := vk.VkRenderingInfo{
		SType:      vk.VK_STRUCTURE_TYPE_RENDERING_INFO,
		RenderArea: vk.VkRect2D{Extent: t.Extent},
		LayerCount: 1,
	}
	if t.Color != nil {
		n := uint32(0)
		// img is the single-sample image, ms the multisampled attachment
		// that resolves into it, nil when the attachment is not
		// multisampled.
		attach := func(img, ms *Image, clearTo [4]float32) {
			// Loading keeps the contents, so the transition must start from
			// the layout the last pass left rather than discarding with
			// UNDEFINED.
			was := vk.VkImageLayout(vk.VK_IMAGE_LAYOUT_UNDEFINED)
			if p.LoadColor {
				was = vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL
			}
			imageBarrier(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
				was, vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
				vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT,
				vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT)
			view := img.View
			if ms != nil {
				// The multisampled attachment is never sampled, so it stays
				// in colour-attachment layout between passes and only needs
				// a barrier against the pass before it.
				msWas := vk.VkImageLayout(vk.VK_IMAGE_LAYOUT_UNDEFINED)
				if p.LoadColor {
					msWas = vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL
				}
				imageBarrier(cb, ms.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
					msWas, vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
					vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT,
					vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT)
				view = ms.View
			}
			var clear vk.VkClearValue
			*clear.Color().Float32() = clearTo
			colorAttachScratch[n] = vk.VkRenderingAttachmentInfo{
				SType:       vk.VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO,
				ImageView:   view,
				ImageLayout: vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
				LoadOp:      vk.VK_ATTACHMENT_LOAD_OP_CLEAR,
				StoreOp:     vk.VK_ATTACHMENT_STORE_OP_STORE,
				ClearValue:  clear,
			}
			if ms != nil {
				colorAttachScratch[n].ResolveMode = vk.VK_RESOLVE_MODE_AVERAGE_BIT
				colorAttachScratch[n].ResolveImageView = img.View
				colorAttachScratch[n].ResolveImageLayout = vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL
			}
			if p.LoadColor {
				colorAttachScratch[n].LoadOp = vk.VK_ATTACHMENT_LOAD_OP_LOAD
			}
			n++
		}
		attach(t.Color, t.MSColor, p.ClearColor)
		for i, img := range p.Extra {
			var clearTo [4]float32
			if i < len(p.ExtraClear) {
				clearTo = p.ExtraClear[i]
			}
			attach(img, nil, clearTo)
		}
		info.ColorAttachmentCount = n
		info.PColorAttachments = &colorAttachScratch[0]
	}
	var stencil vk.VkRenderingAttachmentInfo
	if d := p.depthImage(); d != nil && !p.NoDepth {
		format := d.Format
		// Only the target's own depth may be multisampled. A pass given a
		// depth image of its own borrows a single-sample one, which is how
		// the order-independent transparency pass tests against the
		// resolved depth of the multisampled scene.
		var msDepth *Image
		if p.Depth == nil {
			msDepth = t.MSDepth
		}
		was := vk.VkImageLayout(vk.VK_IMAGE_LAYOUT_UNDEFINED)
		if p.LoadDepth {
			was = vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL
		}
		imageBarrier(cb, d.Handle, depthAspect(format),
			was, depthLayout(format),
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT,
			vk.VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT|vk.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT,
			vk.VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT)
		view := d.AttachView
		if msDepth != nil {
			view = msDepth.AttachView
		}
		var clear vk.VkClearValue
		*clear.DepthStencil() = vk.VkClearDepthStencilValue{Depth: p.ClearDepth}
		depth = vk.VkRenderingAttachmentInfo{
			SType:       vk.VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO,
			ImageView:   view,
			ImageLayout: depthLayout(format),
			LoadOp:      vk.VK_ATTACHMENT_LOAD_OP_CLEAR,
			StoreOp:     vk.VK_ATTACHMENT_STORE_OP_STORE,
			ClearValue:  clear,
		}
		if p.LoadDepth {
			depth.LoadOp = vk.VK_ATTACHMENT_LOAD_OP_LOAD
		}
		if msDepth != nil {
			msWas := vk.VkImageLayout(vk.VK_IMAGE_LAYOUT_UNDEFINED)
			if p.LoadDepth {
				msWas = depthLayout(format)
			}
			imageBarrier(cb, msDepth.Handle, depthAspect(format),
				msWas, depthLayout(format),
				vk.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT, vk.VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT,
				vk.VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT|vk.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT,
				vk.VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT)
			// Sample zero rather than an average: averaging depths across
			// an edge invents a surface that is in neither triangle, and
			// every device supports this mode for depth and stencil alike.
			depth.ResolveMode = vk.VK_RESOLVE_MODE_SAMPLE_ZERO_BIT
			depth.ResolveImageView = d.AttachView
			depth.ResolveImageLayout = depthLayout(format)
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
		readable := func(img *Image) {
			imageBarrier(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
				vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
				vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT,
				vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
		}
		readable(t.Color)
		for _, img := range p.Extra {
			readable(img)
		}
	}
	if d := p.depthImage(); d != nil && !p.NoDepth {
		format := d.Format
		imageBarrier(cb, d.Handle, depthAspect(format),
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
