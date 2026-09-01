package render

import (
	"image"

	"github.com/matjam/bunyip/internal/vk"
)

// recordReadback transitions img to a transfer source and copies it into a
// host-visible buffer, which is kept for reuse across captures.
func (r *Renderer) recordReadback(cb vk.VkCommandBuffer, img vk.VkImage) (*Buffer, error) {
	ext := r.Swapchain.Extent
	size := vk.VkDeviceSize(ext.Width) * vk.VkDeviceSize(ext.Height) * 4
	if r.readback == nil || r.readback.Size < size {
		if r.readback != nil {
			r.readback.Destroy()
		}
		var err error
		r.readback, err = r.Device.NewBuffer(size, vk.VK_BUFFER_USAGE_TRANSFER_DST_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			return nil, err
		}
	}
	imageBarrier(cb, img, vk.VK_IMAGE_ASPECT_COLOR_BIT,
		vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL, vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT,
		vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_READ_BIT)
	region := vk.VkBufferImageCopy{
		ImageSubresource: vk.VkImageSubresourceLayers{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, LayerCount: 1},
		ImageExtent:      vk.VkExtent3D{Width: ext.Width, Height: ext.Height, Depth: 1},
	}
	vk.VkCmdCopyImageToBuffer(cb, img, vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, r.readback.Handle, 1, &region)
	// Make the copy visible to the host before the fence signals.
	barrier := vk.VkMemoryBarrier2{
		SType:         vk.VK_STRUCTURE_TYPE_MEMORY_BARRIER_2,
		SrcStageMask:  vk.VK_PIPELINE_STAGE_2_COPY_BIT,
		SrcAccessMask: vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
		DstStageMask:  vk.VK_PIPELINE_STAGE_2_HOST_BIT,
		DstAccessMask: vk.VK_ACCESS_2_HOST_READ_BIT,
	}
	dep := vk.VkDependencyInfo{SType: vk.VK_STRUCTURE_TYPE_DEPENDENCY_INFO, MemoryBarrierCount: 1, PMemoryBarriers: &barrier}
	vk.VkCmdPipelineBarrier2(cb, &dep)
	return r.readback, nil
}

// decodeReadback converts the copied pixels to an RGBA image, swizzling
// BGRA surface formats.
func (r *Renderer) decodeReadback(buf *Buffer) *image.RGBA {
	ext := r.Swapchain.Extent
	src := buf.Bytes()[:int(ext.Width)*int(ext.Height)*4]
	out := image.NewRGBA(image.Rect(0, 0, int(ext.Width), int(ext.Height)))
	copy(out.Pix, src)
	switch r.Swapchain.Format {
	case vk.VK_FORMAT_B8G8R8A8_SRGB, vk.VK_FORMAT_B8G8R8A8_UNORM:
		for i := 0; i+3 < len(out.Pix); i += 4 {
			out.Pix[i], out.Pix[i+2] = out.Pix[i+2], out.Pix[i]
		}
	}
	return out
}
