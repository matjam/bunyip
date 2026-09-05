package render

import "github.com/matjam/bunyip/internal/vk"

// SupportsFormat reports whether the device can sample an image in the
// format with optimal tiling. A block-compressed format is optional in
// Vulkan, so the caller checks before uploading one and decodes on the
// processor when the answer is no.
func (d *Device) SupportsFormat(f vk.VkFormat) bool {
	var props vk.VkFormatProperties
	vk.VkGetPhysicalDeviceFormatProperties(d.gpu.handle, f, &props)
	return props.OptimalTilingFeatures&vk.VK_FORMAT_FEATURE_SAMPLED_IMAGE_BIT != 0
}

// LevelCopy is where one mip level's bytes sit in a staging buffer and
// what size that level is in texels.
type LevelCopy struct {
	Offset        vk.VkDeviceSize
	Width, Height uint32
}

// NewLevelledImage creates a sampled image with the mip levels given,
// for a texture whose levels come ready made rather than being blitted
// from level zero. A compressed format cannot be blitted, so this is the
// only way to fill one.
func (d *Device) NewLevelledImage(extent vk.VkExtent2D, format vk.VkFormat, mips uint32) (*Image, error) {
	usage := vk.VkImageUsageFlags(vk.VK_IMAGE_USAGE_SAMPLED_BIT | vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT)
	return d.NewImageMips(extent, format, usage, vk.VK_IMAGE_ASPECT_COLOR_BIT, max(mips, 1))
}

// RecordLevelsUpload records the fill of every mip level of an image
// from one staging buffer, leaving the whole chain in shader-read-only
// layout. The image must still be in undefined layout, which is how
// NewLevelledImage leaves it, and each level's offset must suit the
// format's block size, which the caller arranges when it packs them.
func RecordLevelsUpload(cb vk.VkCommandBuffer, img *Image, staging *Buffer, levels []LevelCopy) {
	if len(levels) == 0 {
		return
	}
	n := uint32(len(levels))
	imageBarrierLevels(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT, 0, n,
		vk.VK_IMAGE_LAYOUT_UNDEFINED, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT, 0,
		vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
	regions := make([]vk.VkBufferImageCopy, len(levels))
	for i, lv := range levels {
		regions[i] = vk.VkBufferImageCopy{
			BufferOffset: lv.Offset,
			ImageSubresource: vk.VkImageSubresourceLayers{
				AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, MipLevel: uint32(i), LayerCount: 1,
			},
			ImageExtent: vk.VkExtent3D{Width: lv.Width, Height: lv.Height, Depth: 1},
		}
	}
	vk.VkCmdCopyBufferToImage(cb, staging.Handle, img.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, n, &regions[0])
	imageBarrierLevels(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT, 0, n,
		vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
		vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
}

// NewLevelledTextureImage uploads a whole mip chain outside a frame,
// through a staging buffer of its own that is waited for. Inside a frame
// use NewLevelledImage with RecordLevelsUpload instead, which costs no
// wait.
func (d *Device) NewLevelledTextureImage(extent vk.VkExtent2D, format vk.VkFormat, data []byte, levels []LevelCopy) (*Image, error) {
	img, err := d.NewLevelledImage(extent, format, uint32(len(levels)))
	if err != nil {
		return nil, err
	}
	staging, err := d.NewBuffer(vk.VkDeviceSize(len(data)), vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
		vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
	if err != nil {
		img.Destroy()
		return nil, err
	}
	defer staging.Destroy()
	if err := staging.Write(0, data); err != nil {
		img.Destroy()
		return nil, err
	}
	if err := d.OneShot(func(cb vk.VkCommandBuffer) { RecordLevelsUpload(cb, img, staging, levels) }); err != nil {
		img.Destroy()
		return nil, err
	}
	return img, nil
}
