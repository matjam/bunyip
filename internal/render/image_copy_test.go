package render

import (
	"image"
	"testing"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

func TestImageTransferBarriersCoverShaderConsumers(t *testing.T) {
	oldBarrier, oldCopy, oldUpload, oldBlit := vk.VkCmdPipelineBarrier2, vk.VkCmdCopyImage, vk.VkCmdCopyBufferToImage, vk.VkCmdBlitImage
	t.Cleanup(func() {
		vk.VkCmdPipelineBarrier2, vk.VkCmdCopyImage, vk.VkCmdCopyBufferToImage, vk.VkCmdBlitImage = oldBarrier, oldCopy, oldUpload, oldBlit
	})
	var barriers []vk.VkImageMemoryBarrier2
	vk.VkCmdPipelineBarrier2 = func(_ vk.VkCommandBuffer, dep *vk.VkDependencyInfo) {
		barriers = append(barriers, unsafe.Slice(dep.PImageMemoryBarriers, int(dep.ImageMemoryBarrierCount))...)
	}
	vk.VkCmdCopyBufferToImage = func(vk.VkCommandBuffer, vk.VkBuffer, vk.VkImage, vk.VkImageLayout, uint32, *vk.VkBufferImageCopy) {}
	vk.VkCmdBlitImage = func(vk.VkCommandBuffer, vk.VkImage, vk.VkImageLayout, vk.VkImage, vk.VkImageLayout, uint32, *vk.VkImageBlit, vk.VkFilter) {
	}
	selfCopy := false
	vk.VkCmdCopyImage = func(_ vk.VkCommandBuffer, src vk.VkImage, sl vk.VkImageLayout, dst vk.VkImage, dl vk.VkImageLayout, _ uint32, _ *vk.VkImageCopy) {
		if src == dst {
			selfCopy = true
			if sl != vk.VK_IMAGE_LAYOUT_GENERAL || dl != vk.VK_IMAGE_LAYOUT_GENERAL {
				t.Fatal("same subresource uses incompatible layouts", sl, dl)
			}
		}
	}
	src := &Image{Handle: 1, Extent: vk.VkExtent2D{Width: 4, Height: 4}, Mips: 3}
	dst := &Image{Handle: 2, Extent: src.Extent, Mips: 3}
	RecordImageUpload(0, src, &Buffer{}, 0)
	RecordImageCopy(0, src, dst, image.Rect(0, 0, 4, 4), image.Point{})
	RecordImageWrite(0, dst, 0, 0, 1, 1, &Buffer{}, 0)
	RecordImageCopy(0, dst, dst, image.Rect(0, 0, 1, 1), image.Pt(2, 2))
	const consumers = vk.VK_PIPELINE_STAGE_2_VERTEX_SHADER_BIT | vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT
	reads, restores := 0, 0
	for _, b := range barriers {
		if b.OldLayout == vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL {
			reads++
			if b.SrcStageMask&consumers != consumers {
				t.Fatal("transfer ignores prior shader reads", b.SrcStageMask)
			}
		}
		if b.NewLayout == vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL {
			restores++
			if b.DstStageMask&consumers != consumers {
				t.Fatal("transfer invisible to shader consumer", b.DstStageMask)
			}
		}
	}
	if !selfCopy || reads == 0 || restores < 12 {
		t.Fatal(selfCopy, reads, restores)
	}
}
