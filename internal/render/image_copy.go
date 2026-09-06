package render

import (
	"github.com/matjam/bunyip/internal/vk"
	"image"
)

// RecordImageCopy copies a validated level-zero region between identical-format
// single-sample images in shader-read-only layout. It rebuilds destination mips
// and restores that layout. Same-image regions must not overlap.
func RecordImageCopy(cb vk.VkCommandBuffer, src, dst *Image, rect image.Rectangle, point image.Point) {
	const aspect = vk.VK_IMAGE_ASPECT_COLOR_BIT
	const priorStage = vk.VK_PIPELINE_STAGE_2_VERTEX_SHADER_BIT | vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT | vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT
	const priorAccess = vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT | vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT
	srcLayout, dstLayout := vk.VkImageLayout(vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL), vk.VkImageLayout(vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL)
	if src == dst {
		// A subresource has one layout, even when the copied rectangles differ.
		srcLayout, dstLayout = vk.VK_IMAGE_LAYOUT_GENERAL, vk.VK_IMAGE_LAYOUT_GENERAL
		imageBarrierLevels(cb, dst.Handle, aspect, 0, dst.Mips,
			vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL, vk.VK_IMAGE_LAYOUT_GENERAL,
			priorStage, priorAccess, vk.VK_PIPELINE_STAGE_2_COPY_BIT,
			vk.VK_ACCESS_2_TRANSFER_READ_BIT|vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
	} else {
		imageBarrierLevels(cb, src.Handle, aspect, 0, 1,
			vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL, srcLayout,
			priorStage, priorAccess, vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_READ_BIT)
		imageBarrierLevels(cb, dst.Handle, aspect, 0, dst.Mips,
			vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL, dstLayout,
			priorStage, priorAccess, vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
	}
	region := vk.VkImageCopy{
		SrcSubresource: vk.VkImageSubresourceLayers{AspectMask: aspect, LayerCount: 1},
		SrcOffset:      vk.VkOffset3D{X: int32(rect.Min.X), Y: int32(rect.Min.Y)},
		DstSubresource: vk.VkImageSubresourceLayers{AspectMask: aspect, LayerCount: 1},
		DstOffset:      vk.VkOffset3D{X: int32(point.X), Y: int32(point.Y)},
		Extent:         vk.VkExtent3D{Width: uint32(rect.Dx()), Height: uint32(rect.Dy()), Depth: 1},
	}
	vk.VkCmdCopyImage(cb, src.Handle, srcLayout, dst.Handle, dstLayout, 1, &region)
	if src == dst {
		imageBarrierLevels(cb, dst.Handle, aspect, 0, dst.Mips,
			vk.VK_IMAGE_LAYOUT_GENERAL, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_READ_BIT|vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
			vk.VK_PIPELINE_STAGE_2_BLIT_BIT|vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
	} else {
		imageBarrierLevels(cb, src.Handle, aspect, 0, 1, srcLayout, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_READ_BIT,
			vk.VK_PIPELINE_STAGE_2_VERTEX_SHADER_BIT|vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
	}
	generateMips(cb, dst)
}
