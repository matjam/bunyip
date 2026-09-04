package render

import (
	"fmt"
	"image"

	"github.com/matjam/bunyip/internal/vk"
)

// Image is a VkImage with allocator memory and a view over every level.
// A depth image with a stencil aspect has two views: View covers depth
// alone, for sampling, and AttachView covers both aspects, for rendering.
type Image struct {
	Handle     vk.VkImage
	View       vk.VkImageView
	AttachView vk.VkImageView
	Format     vk.VkFormat
	Extent     vk.VkExtent2D
	Mips       uint32
	mem        allocation
	dev        *Device
}

// HasStencil reports whether a depth format carries a stencil aspect.
func HasStencil(f vk.VkFormat) bool {
	return f == vk.VK_FORMAT_D32_SFLOAT_S8_UINT || f == vk.VK_FORMAT_D24_UNORM_S8_UINT || f == vk.VK_FORMAT_D16_UNORM_S8_UINT
}

// depthAspect is the aspect mask a depth format's whole image uses in
// barriers: depth, plus stencil when present.
func depthAspect(f vk.VkFormat) vk.VkImageAspectFlags {
	if HasStencil(f) {
		return vk.VK_IMAGE_ASPECT_DEPTH_BIT | vk.VK_IMAGE_ASPECT_STENCIL_BIT
	}
	return vk.VK_IMAGE_ASPECT_DEPTH_BIT
}

// depthLayout is the attachment layout for a depth format.
func depthLayout(f vk.VkFormat) vk.VkImageLayout {
	if HasStencil(f) {
		return vk.VK_IMAGE_LAYOUT_DEPTH_STENCIL_ATTACHMENT_OPTIMAL
	}
	return vk.VK_IMAGE_LAYOUT_DEPTH_ATTACHMENT_OPTIMAL
}

// NewImage creates a 2D image with one mip level, binds memory and makes a view.
func (d *Device) NewImage(extent vk.VkExtent2D, format vk.VkFormat, usage vk.VkImageUsageFlags, aspect vk.VkImageAspectFlags) (*Image, error) {
	return d.NewImageMips(extent, format, usage, aspect, 1)
}

// MipLevels is the full mip chain length for an extent.
func MipLevels(extent vk.VkExtent2D) uint32 {
	n := uint32(1)
	for w, h := extent.Width, extent.Height; w > 1 || h > 1; w, h = max(w/2, 1), max(h/2, 1) {
		n++
	}
	return n
}

// NewImageMips creates a 2D image with mips levels.
func (d *Device) NewImageMips(extent vk.VkExtent2D, format vk.VkFormat, usage vk.VkImageUsageFlags, aspect vk.VkImageAspectFlags, mips uint32) (*Image, error) {
	img := &Image{Format: format, Extent: extent, Mips: mips, dev: d}
	info := vk.VkImageCreateInfo{
		SType:         vk.VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,
		ImageType:     vk.VK_IMAGE_TYPE_2D,
		Format:        format,
		Extent:        vk.VkExtent3D{Width: extent.Width, Height: extent.Height, Depth: 1},
		MipLevels:     mips,
		ArrayLayers:   1,
		Samples:       vk.VK_SAMPLE_COUNT_1_BIT,
		Tiling:        vk.VK_IMAGE_TILING_OPTIMAL,
		Usage:         usage,
		SharingMode:   vk.VK_SHARING_MODE_EXCLUSIVE,
		InitialLayout: vk.VK_IMAGE_LAYOUT_UNDEFINED,
	}
	if err := vk.Check("vkCreateImage", vk.VkCreateImage(d.Handle, &info, nil, &img.Handle)); err != nil {
		return nil, err
	}
	var req vk.VkMemoryRequirements
	vk.VkGetImageMemoryRequirements(d.Handle, img.Handle, &req)
	var err error
	if img.mem, err = d.alloc.allocate(req, vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT, false); err != nil {
		img.Destroy()
		return nil, err
	}
	if err := vk.Check("vkBindImageMemory", vk.VkBindImageMemory(d.Handle, img.Handle, img.mem.Memory, img.mem.Offset)); err != nil {
		img.Destroy()
		return nil, err
	}
	if img.View, err = d.newImageViewMips(img.Handle, format, aspect, mips); err != nil {
		img.Destroy()
		return nil, err
	}
	img.AttachView = img.View
	if aspect == vk.VK_IMAGE_ASPECT_DEPTH_BIT && HasStencil(format) {
		if img.AttachView, err = d.newImageViewMips(img.Handle, format, depthAspect(format), mips); err != nil {
			img.Destroy()
			return nil, err
		}
	}
	return img, nil
}

// NewSampledImage creates an empty image for a texture, with a full mip
// chain when mipmaps is set. Its contents are undefined until
// RecordImageUpload fills them.
func (d *Device) NewSampledImage(extent vk.VkExtent2D, format vk.VkFormat, mipmaps bool) (*Image, error) {
	mips := uint32(1)
	// Transfer source so the texture can be read back or blitted later.
	usage := vk.VkImageUsageFlags(vk.VK_IMAGE_USAGE_SAMPLED_BIT | vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT | vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT)
	if mipmaps {
		mips = MipLevels(extent)
	}
	return d.NewImageMips(extent, format, usage, vk.VK_IMAGE_ASPECT_COLOR_BIT, mips)
}

// RecordImageUpload records the first fill of a sampled image from a
// staging buffer at offset: the whole of level 0, then the mip chain,
// leaving every level in shader-read-only layout. The image must still
// be in undefined layout, which is how NewSampledImage leaves it.
func RecordImageUpload(cb vk.VkCommandBuffer, img *Image, staging *Buffer, offset vk.VkDeviceSize) {
	mips := max(img.Mips, 1)
	imageBarrierLevels(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT, 0, mips,
		vk.VK_IMAGE_LAYOUT_UNDEFINED, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT, 0,
		vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
	region := vk.VkBufferImageCopy{
		BufferOffset:     offset,
		ImageSubresource: vk.VkImageSubresourceLayers{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, LayerCount: 1},
		ImageExtent:      vk.VkExtent3D{Width: img.Extent.Width, Height: img.Extent.Height, Depth: 1},
	}
	vk.VkCmdCopyBufferToImage(cb, staging.Handle, img.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &region)
	generateMips(cb, img)
}

// NewTextureImage uploads RGBA pixels (row-major, 4 bytes per pixel) into a
// sampled image, generates a full mip chain when mipmaps is set, and leaves
// it in shader-read-only layout. It waits for the queue, so it is for
// setup; inside a frame use NewSampledImage and RecordImageUpload.
func (d *Device) NewTextureImage(extent vk.VkExtent2D, format vk.VkFormat, pixels []byte, mipmaps bool) (*Image, error) {
	img, err := d.NewSampledImage(extent, format, mipmaps)
	if err != nil {
		return nil, err
	}
	staging, err := d.NewBuffer(vk.VkDeviceSize(len(pixels)), vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
		vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
	if err != nil {
		img.Destroy()
		return nil, err
	}
	defer staging.Destroy()
	if err := staging.Write(0, pixels); err != nil {
		img.Destroy()
		return nil, err
	}
	if err := d.OneShot(func(cb vk.VkCommandBuffer) { RecordImageUpload(cb, img, staging, 0) }); err != nil {
		img.Destroy()
		return nil, err
	}
	return img, nil
}

// WriteImage replaces a rectangle of a sampled image's level 0 with RGBA
// pixels (row-major, 4 bytes per pixel, w*h*4 bytes) and rebuilds any mip
// chain. The image must not be in use by a frame in flight.
func (d *Device) WriteImage(img *Image, x, y, w, h int, pixels []byte) error {
	if len(pixels) < w*h*4 {
		return fmt.Errorf("render: %d bytes for a %dx%d write", len(pixels), w, h)
	}
	staging, err := d.NewBuffer(vk.VkDeviceSize(w*h*4), vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
		vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
	if err != nil {
		return err
	}
	defer staging.Destroy()
	if err := staging.Write(0, pixels[:w*h*4]); err != nil {
		return err
	}
	return d.OneShot(func(cb vk.VkCommandBuffer) { RecordImageWrite(cb, img, x, y, w, h, staging, 0) })
}

// RecordImageWrite records a copy of a staging buffer, from offset, into
// a rectangle of a sampled image, with the barriers that order it after
// earlier reads on the queue and before later ones, and rebuilds the mip
// chain. The image must be in shader-read-only layout, which is where
// RecordImageUpload leaves it.
func RecordImageWrite(cb vk.VkCommandBuffer, img *Image, x, y, w, h int, staging *Buffer, offset vk.VkDeviceSize) {
	mips := max(img.Mips, 1)
	imageBarrierLevels(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT, 0, mips,
		vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT,
		vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
	region := vk.VkBufferImageCopy{
		BufferOffset:     offset,
		ImageSubresource: vk.VkImageSubresourceLayers{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, LayerCount: 1},
		ImageOffset:      vk.VkOffset3D{X: int32(x), Y: int32(y)},
		ImageExtent:      vk.VkExtent3D{Width: uint32(w), Height: uint32(h), Depth: 1},
	}
	vk.VkCmdCopyBufferToImage(cb, staging.Handle, img.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &region)
	if mips > 1 {
		generateMips(cb, img)
		return
	}
	imageBarrier(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
		vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
		vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
}

// ReadImage copies level 0 of an image that is in shader-read-only layout
// back to the host as RGBA, swizzling BGRA formats. The image must not be
// in use by a frame in flight.
func (d *Device) ReadImage(img *Image) (*image.RGBA, error) {
	w, h := int(img.Extent.Width), int(img.Extent.Height)
	buf, err := d.NewBuffer(vk.VkDeviceSize(w*h*4), vk.VK_BUFFER_USAGE_TRANSFER_DST_BIT,
		vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
	if err != nil {
		return nil, err
	}
	defer buf.Destroy()
	err = d.OneShot(func(cb vk.VkCommandBuffer) {
		imageBarrier(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
			vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL, vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT|vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT,
			vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT|vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT,
			vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_READ_BIT)
		region := vk.VkBufferImageCopy{
			ImageSubresource: vk.VkImageSubresourceLayers{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, LayerCount: 1},
			ImageExtent:      vk.VkExtent3D{Width: uint32(w), Height: uint32(h), Depth: 1},
		}
		vk.VkCmdCopyImageToBuffer(cb, img.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, buf.Handle, 1, &region)
		imageBarrier(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
			vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_READ_BIT,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
	})
	if err != nil {
		return nil, err
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(out.Pix, buf.Bytes()[:w*h*4])
	switch img.Format {
	case vk.VK_FORMAT_B8G8R8A8_SRGB, vk.VK_FORMAT_B8G8R8A8_UNORM:
		for i := 0; i+3 < len(out.Pix); i += 4 {
			out.Pix[i], out.Pix[i+2] = out.Pix[i+2], out.Pix[i]
		}
	}
	return out, nil
}

func (i *Image) Destroy() {
	if i.AttachView != 0 && i.AttachView != i.View {
		vk.VkDestroyImageView(i.dev.Handle, i.AttachView, nil)
	}
	i.AttachView = 0
	if i.View != 0 {
		vk.VkDestroyImageView(i.dev.Handle, i.View, nil)
		i.View = 0
	}
	if i.Handle != 0 {
		vk.VkDestroyImage(i.dev.Handle, i.Handle, nil)
		i.Handle = 0
	}
	i.dev.alloc.free(i.mem)
	i.mem = allocation{}
}

// generateMips blits each level from the one above and leaves the whole
// chain in shader-read-only layout. Level 0 must be in transfer-dst layout.
func generateMips(cb vk.VkCommandBuffer, img *Image) {
	w, h := int32(img.Extent.Width), int32(img.Extent.Height)
	for level := uint32(1); level < img.Mips; level++ {
		imageBarrierLevels(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT, level-1, 1,
			vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_BLIT_BIT|vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
			vk.VK_PIPELINE_STAGE_2_BLIT_BIT, vk.VK_ACCESS_2_TRANSFER_READ_BIT)
		nw, nh := max(w/2, 1), max(h/2, 1)
		blit := vk.VkImageBlit{
			SrcSubresource: vk.VkImageSubresourceLayers{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, MipLevel: level - 1, LayerCount: 1},
			DstSubresource: vk.VkImageSubresourceLayers{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, MipLevel: level, LayerCount: 1},
		}
		blit.SrcOffsets[1] = vk.VkOffset3D{X: w, Y: h, Z: 1}
		blit.DstOffsets[1] = vk.VkOffset3D{X: nw, Y: nh, Z: 1}
		vk.VkCmdBlitImage(cb, img.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, img.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &blit, vk.VK_FILTER_LINEAR)
		imageBarrierLevels(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT, level-1, 1,
			vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_BLIT_BIT, vk.VK_ACCESS_2_TRANSFER_READ_BIT,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
		w, h = nw, nh
	}
	imageBarrierLevels(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT, img.Mips-1, 1,
		vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_BLIT_BIT|vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
		vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
}

// NewSampler makes a clamp-to-edge sampler, nearest or linear. Linear
// samplers filter across the mip chain with anisotropy when the GPU has it.
func (d *Device) NewSampler(linear bool) (vk.VkSampler, error) {
	return d.NewSamplerRepeat(linear, false)
}

// NewSamplerRepeat is NewSampler with a choice of edge behaviour.
func (d *Device) NewSamplerRepeat(linear, repeat bool) (vk.VkSampler, error) {
	filter := vk.VkFilter(vk.VK_FILTER_NEAREST)
	mip := vk.VkSamplerMipmapMode(vk.VK_SAMPLER_MIPMAP_MODE_NEAREST)
	if linear {
		filter = vk.VK_FILTER_LINEAR
		mip = vk.VK_SAMPLER_MIPMAP_MODE_LINEAR
	}
	address := vk.VkSamplerAddressMode(vk.VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE)
	if repeat {
		address = vk.VK_SAMPLER_ADDRESS_MODE_REPEAT
	}
	info := vk.VkSamplerCreateInfo{
		SType:        vk.VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO,
		MagFilter:    filter,
		MinFilter:    filter,
		MipmapMode:   mip,
		AddressModeU: address,
		AddressModeV: address,
		AddressModeW: address,
		MaxLod:       vk.VK_LOD_CLAMP_NONE,
	}
	if linear && d.anisotropy > 1 {
		info.AnisotropyEnable = vk.VK_TRUE
		info.MaxAnisotropy = d.anisotropy
	}
	var s vk.VkSampler
	err := vk.Check("vkCreateSampler", vk.VkCreateSampler(d.Handle, &info, nil, &s))
	return s, err
}

// NewShadowSampler makes a comparison sampler for shadow maps: linear
// filtering of the comparison result gives free 2x2 percentage-closer
// filtering, and clamp-to-border white keeps everything outside the map lit.
func (d *Device) NewShadowSampler() (vk.VkSampler, error) {
	info := vk.VkSamplerCreateInfo{
		SType:         vk.VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO,
		MagFilter:     vk.VK_FILTER_LINEAR,
		MinFilter:     vk.VK_FILTER_LINEAR,
		MipmapMode:    vk.VK_SAMPLER_MIPMAP_MODE_NEAREST,
		AddressModeU:  vk.VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_BORDER,
		AddressModeV:  vk.VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_BORDER,
		AddressModeW:  vk.VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_BORDER,
		CompareEnable: vk.VK_TRUE,
		CompareOp:     vk.VK_COMPARE_OP_LESS_OR_EQUAL,
		BorderColor:   vk.VK_BORDER_COLOR_FLOAT_OPAQUE_WHITE,
		MaxLod:        1,
	}
	var s vk.VkSampler
	err := vk.Check("vkCreateSampler", vk.VkCreateSampler(d.Handle, &info, nil, &s))
	return s, err
}
