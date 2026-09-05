package render

import "github.com/matjam/bunyip/internal/vk"

func (d *Device) newImageView(img vk.VkImage, format vk.VkFormat, aspect vk.VkImageAspectFlags) (vk.VkImageView, error) {
	return d.newImageViewMips(img, format, aspect, 1)
}

func (d *Device) newImageViewMips(img vk.VkImage, format vk.VkFormat, aspect vk.VkImageAspectFlags, mips uint32) (vk.VkImageView, error) {
	info := vk.VkImageViewCreateInfo{
		SType:    vk.VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
		Image:    img,
		ViewType: vk.VK_IMAGE_VIEW_TYPE_2D,
		Format:   format,
		SubresourceRange: vk.VkImageSubresourceRange{
			AspectMask: aspect, LevelCount: mips, LayerCount: 1,
		},
	}
	var view vk.VkImageView
	err := vk.Check("vkCreateImageView", vk.VkCreateImageView(d.Handle, &info, nil, &view))
	return view, err
}

func (d *Device) newSemaphore() (vk.VkSemaphore, error) {
	info := vk.VkSemaphoreCreateInfo{SType: vk.VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO}
	var sem vk.VkSemaphore
	err := vk.Check("vkCreateSemaphore", vk.VkCreateSemaphore(d.Handle, &info, nil, &sem))
	return sem, err
}

func (d *Device) newFence(signaled bool) (vk.VkFence, error) {
	info := vk.VkFenceCreateInfo{SType: vk.VK_STRUCTURE_TYPE_FENCE_CREATE_INFO}
	if signaled {
		info.Flags = vk.VK_FENCE_CREATE_SIGNALED_BIT
	}
	var fence vk.VkFence
	err := vk.Check("vkCreateFence", vk.VkCreateFence(d.Handle, &info, nil, &fence))
	return fence, err
}

// imageBarrier records a layout transition of the first mip level with
// synchronization2 stage and access masks.
func imageBarrier(cb vk.VkCommandBuffer, img vk.VkImage, aspect vk.VkImageAspectFlags,
	oldLayout, newLayout vk.VkImageLayout,
	srcStage vk.VkPipelineStageFlags2, srcAccess vk.VkAccessFlags2,
	dstStage vk.VkPipelineStageFlags2, dstAccess vk.VkAccessFlags2) {
	imageBarrierLevels(cb, img, aspect, 0, 1, oldLayout, newLayout, srcStage, srcAccess, dstStage, dstAccess)
}

// imageBarrierLevels is imageBarrier over a range of mip levels.
func imageBarrierLevels(cb vk.VkCommandBuffer, img vk.VkImage, aspect vk.VkImageAspectFlags, baseLevel, levels uint32,
	oldLayout, newLayout vk.VkImageLayout,
	srcStage vk.VkPipelineStageFlags2, srcAccess vk.VkAccessFlags2,
	dstStage vk.VkPipelineStageFlags2, dstAccess vk.VkAccessFlags2) {
	barrierScratch = vk.VkImageMemoryBarrier2{
		SType:               vk.VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER_2,
		SrcStageMask:        srcStage,
		SrcAccessMask:       srcAccess,
		DstStageMask:        dstStage,
		DstAccessMask:       dstAccess,
		OldLayout:           oldLayout,
		NewLayout:           newLayout,
		SrcQueueFamilyIndex: vk.VK_QUEUE_FAMILY_IGNORED,
		DstQueueFamilyIndex: vk.VK_QUEUE_FAMILY_IGNORED,
		Image:               img,
		SubresourceRange:    vk.VkImageSubresourceRange{AspectMask: aspect, BaseMipLevel: baseLevel, LevelCount: levels, LayerCount: 1},
	}
	depScratch = vk.VkDependencyInfo{
		SType:                   vk.VK_STRUCTURE_TYPE_DEPENDENCY_INFO,
		ImageMemoryBarrierCount: 1,
		PImageMemoryBarriers:    &barrierScratch,
	}
	vk.CmdPipelineBarrier2(cb, &depScratch)
}

// bufferBarrier orders access to the whole of a buffer, for a transfer
// that a later command in the same command buffer reads.
func bufferBarrier(cb vk.VkCommandBuffer, buf *Buffer,
	srcStage vk.VkPipelineStageFlags2, srcAccess vk.VkAccessFlags2,
	dstStage vk.VkPipelineStageFlags2, dstAccess vk.VkAccessFlags2) {
	bufBarrierScratch = vk.VkBufferMemoryBarrier2{
		SType:               vk.VK_STRUCTURE_TYPE_BUFFER_MEMORY_BARRIER_2,
		SrcStageMask:        srcStage,
		SrcAccessMask:       srcAccess,
		DstStageMask:        dstStage,
		DstAccessMask:       dstAccess,
		SrcQueueFamilyIndex: vk.VK_QUEUE_FAMILY_IGNORED,
		DstQueueFamilyIndex: vk.VK_QUEUE_FAMILY_IGNORED,
		Buffer:              buf.Handle,
		Size:                vk.VK_WHOLE_SIZE,
	}
	depScratch = vk.VkDependencyInfo{
		SType:                    vk.VK_STRUCTURE_TYPE_DEPENDENCY_INFO,
		BufferMemoryBarrierCount: 1,
		PBufferMemoryBarriers:    &bufBarrierScratch,
	}
	vk.CmdPipelineBarrier2(cb, &depScratch)
}

// A barrier is recorded by pointer, and a pointer to a local would be
// forced onto the heap once per barrier. These live for the process
// and are filled in place; commands are recorded from the goroutine that
// owns the device, so there is one recorder at a time.
var (
	barrierScratch    vk.VkImageMemoryBarrier2
	bufBarrierScratch vk.VkBufferMemoryBarrier2
	depScratch        vk.VkDependencyInfo
)
