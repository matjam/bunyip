package render

import (
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// Image is a VkImage with its memory and a view over the whole thing.
type Image struct {
	Handle vk.VkImage
	Memory vk.VkDeviceMemory
	View   vk.VkImageView
	Format vk.VkFormat
	Extent vk.VkExtent2D
	dev    *Device
}

// NewImage creates a 2D image, binds memory and makes a view.
func (d *Device) NewImage(extent vk.VkExtent2D, format vk.VkFormat, usage vk.VkImageUsageFlags, aspect vk.VkImageAspectFlags) (*Image, error) {
	img := &Image{Format: format, Extent: extent, dev: d}
	info := vk.VkImageCreateInfo{
		SType:         vk.VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO,
		ImageType:     vk.VK_IMAGE_TYPE_2D,
		Format:        format,
		Extent:        vk.VkExtent3D{Width: extent.Width, Height: extent.Height, Depth: 1},
		MipLevels:     1,
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
	if img.Memory, err = d.allocate(req, vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT); err != nil {
		img.Destroy()
		return nil, err
	}
	if err := vk.Check("vkBindImageMemory", vk.VkBindImageMemory(d.Handle, img.Handle, img.Memory, 0)); err != nil {
		img.Destroy()
		return nil, err
	}
	if img.View, err = d.newImageView(img.Handle, format, aspect); err != nil {
		img.Destroy()
		return nil, err
	}
	return img, nil
}

// NewTextureImage uploads RGBA pixels (row-major, 4 bytes per pixel) into a
// sampled image and leaves it in shader-read-only layout.
func (d *Device) NewTextureImage(extent vk.VkExtent2D, format vk.VkFormat, pixels []byte) (*Image, error) {
	img, err := d.NewImage(extent, format, vk.VK_IMAGE_USAGE_SAMPLED_BIT|vk.VK_IMAGE_USAGE_TRANSFER_DST_BIT, vk.VK_IMAGE_ASPECT_COLOR_BIT)
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
	err = d.OneShot(func(cb vk.VkCommandBuffer) {
		imageBarrier(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
			vk.VK_IMAGE_LAYOUT_UNDEFINED, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_TOP_OF_PIPE_BIT, 0,
			vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT)
		region := vk.VkBufferImageCopy{
			ImageSubresource: vk.VkImageSubresourceLayers{AspectMask: vk.VK_IMAGE_ASPECT_COLOR_BIT, LayerCount: 1},
			ImageExtent:      vk.VkExtent3D{Width: extent.Width, Height: extent.Height, Depth: 1},
		}
		vk.VkCmdCopyBufferToImage(cb, staging.Handle, img.Handle, vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &region)
		imageBarrier(cb, img.Handle, vk.VK_IMAGE_ASPECT_COLOR_BIT,
			vk.VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
			vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
			vk.VK_PIPELINE_STAGE_2_FRAGMENT_SHADER_BIT, vk.VK_ACCESS_2_SHADER_SAMPLED_READ_BIT)
	})
	if err != nil {
		img.Destroy()
		return nil, err
	}
	return img, nil
}

func (i *Image) Destroy() {
	if i.View != 0 {
		vk.VkDestroyImageView(i.dev.Handle, i.View, nil)
		i.View = 0
	}
	if i.Handle != 0 {
		vk.VkDestroyImage(i.dev.Handle, i.Handle, nil)
		i.Handle = 0
	}
	if i.Memory != 0 {
		vk.VkFreeMemory(i.dev.Handle, i.Memory, nil)
		i.Memory = 0
	}
}

// NewSampler makes a clamp-to-edge sampler, nearest or linear.
func (d *Device) NewSampler(linear bool) (vk.VkSampler, error) {
	filter := vk.VkFilter(vk.VK_FILTER_NEAREST)
	if linear {
		filter = vk.VK_FILTER_LINEAR
	}
	info := vk.VkSamplerCreateInfo{
		SType:        vk.VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO,
		MagFilter:    filter,
		MinFilter:    filter,
		MipmapMode:   vk.VK_SAMPLER_MIPMAP_MODE_NEAREST,
		AddressModeU: vk.VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
		AddressModeV: vk.VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
		AddressModeW: vk.VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE,
		MaxLod:       1,
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

// bytesOf views any fixed-size value as bytes, for uploads.
func bytesOf[T any](v *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(v)), unsafe.Sizeof(*v))
}
