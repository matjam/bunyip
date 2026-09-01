package render

import "github.com/matjam/bunyip/internal/vk"

func (d *Device) newImageView(img vk.VkImage, format vk.VkFormat, aspect vk.VkImageAspectFlags) (vk.VkImageView, error) {
	info := vk.VkImageViewCreateInfo{
		SType:    vk.VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
		Image:    img,
		ViewType: vk.VK_IMAGE_VIEW_TYPE_2D,
		Format:   format,
		SubresourceRange: vk.VkImageSubresourceRange{
			AspectMask: aspect, LevelCount: 1, LayerCount: 1,
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

// imageBarrier records a full-subresource layout transition with
// synchronization2 stage and access masks.
func imageBarrier(cb vk.VkCommandBuffer, img vk.VkImage, aspect vk.VkImageAspectFlags,
	oldLayout, newLayout vk.VkImageLayout,
	srcStage vk.VkPipelineStageFlags2, srcAccess vk.VkAccessFlags2,
	dstStage vk.VkPipelineStageFlags2, dstAccess vk.VkAccessFlags2) {
	barrier := vk.VkImageMemoryBarrier2{
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
		SubresourceRange:    vk.VkImageSubresourceRange{AspectMask: aspect, LevelCount: 1, LayerCount: 1},
	}
	dep := vk.VkDependencyInfo{
		SType:                   vk.VK_STRUCTURE_TYPE_DEPENDENCY_INFO,
		ImageMemoryBarrierCount: 1,
		PImageMemoryBarriers:    &barrier,
	}
	vk.VkCmdPipelineBarrier2(cb, &dep)
}
