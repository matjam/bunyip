package render

import (
	"fmt"

	"github.com/matjam/bunyip/internal/vk"
)

// NewCubemapImage uploads a cube map with mip levels: faces[level][face]
// holds a square of side size>>level texels in format's layout, faces in
// Vulkan order (+X, -X, +Y, -Y, +Z, -Z). The image ends in shader-read-only
// layout with a cube view over every level.
func (d *Device) NewCubemapImage(size uint32, format vk.VkFormat, texelBytes int, faces [][6][]byte) (*Image, error) {
	mips := uint32(len(faces))
	if mips == 0 || size == 0 {
		return nil, fmt.Errorf("render: cubemap needs at least one level")
	}
	img := &Image{Format: format, Extent: vk.VkExtent2D{Width: size, Height: size}, Mips: mips, dev: d}
	info := vk.VkImageCreateInfo{
		SType:         vk.VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,
		Flags:         vk.VK_IMAGE_CREATE_CUBE_COMPATIBLE_BIT,
		ImageType:     vk.VK_IMAGE_TYPE_2D,
		Format:        format,
		Extent:        vk.VkExtent3D{Width: size, Height: size, Depth: 1},
		MipLevels:     mips,
		ArrayLayers:   6,
		Samples:       vk.VK_SAMPLE_COUNT_1_BIT,
		Tiling:        vk.VK_IMAGE_TILING_OPTIMAL,
		Usage:         vk.VK_IMAGE_USAGE_SAMPLED_BIT | vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT,
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
	viewInfo := vk.VkImageViewCreateInfo{
		SType:    vk.VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO,
		Image:    img.Handle,
		ViewType: vk.VK_IMAGE_VIEW_TYPE_CUBE,
		Format:   format,
		SubresourceRange: vk.VkImageSubresourceRange{
			AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, LevelCount: mips, LayerCount: 6,
		},
	}
	if err := vk.Check("vkCreateImageView", vk.VkCreateImageView(d.Handle, &viewInfo, nil, &img.View)); err != nil {
		img.Destroy()
		return nil, err
	}
	// One staging buffer holding every level and face in order.
	total := 0
	for level := range faces {
		side := int(max(size>>level, 1))
		total += 6 * side * side * texelBytes
	}
	staging, err := d.NewBuffer(vk.VkDeviceSize(total), vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
		vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
	if err != nil {
		img.Destroy()
		return nil, err
	}
	defer staging.Destroy()
	var regions []vk.VkBufferImageCopy
	offset := 0
	for level := range faces {
		side := int(max(size>>level, 1))
		for face := range 6 {
			pix := faces[level][face]
			if len(pix) != side*side*texelBytes {
				img.Destroy()
				return nil, fmt.Errorf("render: cubemap level %d face %d has %d bytes, want %d", level, face, len(pix), side*side*texelBytes)
			}
			if err := staging.Write(offset, pix); err != nil {
				img.Destroy()
				return nil, err
			}
			regions = append(regions, vk.VkBufferImageCopy{
				BufferOffset:     vk.VkDeviceSize(offset),
				ImageSubresource: vk.VkImageSubresourceLayers{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, MipLevel: uint32(level), BaseArrayLayer: uint32(face), LayerCount: 1},
				ImageExtent:      vk.VkExtent3D{Width: uint32(side), Height: uint32(side), Depth: 1},
			})
			offset += len(pix)
		}
	}
	err = d.OneShot(func(cb vk.VkCommandBuffer) {
		cubeBarrier(cb, img.Handle, mips, vk.VK_IMAGE_LAYOUT_UNDEFINED, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT, 0, vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
		vk.VkCmdCopyBufferToImage(cb, staging.Handle, img.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, uint32(len(regions)), &regions[0])
		cubeBarrier(cb, img.Handle, mips, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT|vk.VK_PIPELINE_STAGE_2_VERTEX_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
	})
	if err != nil {
		img.Destroy()
		return nil, err
	}
	return img, nil
}

// cubeBarrier transitions every level and face of a cube map.
func cubeBarrier(cb vk.VkCommandBuffer, img vk.VkImage, mips uint32, oldLayout, newLayout vk.VkImageLayout,
	srcStage vk.VkPipelineStageFlags2, srcAccess vk.VkAccessFlags2, dstStage vk.VkPipelineStageFlags2, dstAccess vk.VkAccessFlags2) {
	barrier := vk.VkImageMemoryBarrier2{
		SType:         vk.VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER_2,
		SrcStageMask:  srcStage,
		SrcAccessMask: srcAccess,
		DstStageMask:  dstStage,
		DstAccessMask: dstAccess,
		OldLayout:     oldLayout,
		NewLayout:     newLayout,
		Image:         img,
		SubresourceRange: vk.VkImageSubresourceRange{
			AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, LevelCount: mips, LayerCount: 6,
		},
	}
	dep := vk.VkDependencyInfo{SType: vk.VK_STRUCTURE_TYPE_DEPENDENCY_INFO, ImageMemoryBarrierCount: 1, PImageMemoryBarriers: &barrier}
	vk.VkCmdPipelineBarrier2(cb, &dep)
}
