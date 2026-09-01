package render

import (
	"fmt"

	"github.com/matjam/bunyip/internal/vk"
)

// chooseDepthFormat picks the first depth format the GPU can render to.
// D32_SFLOAT is universal on Apple GPUs, which lack D24_UNORM_S8.
func (d *Device) chooseDepthFormat() (vk.VkFormat, error) {
	for _, f := range []vk.VkFormat{vk.VK_FORMAT_D32_SFLOAT, vk.VK_FORMAT_D32_SFLOAT_S8_UINT, vk.VK_FORMAT_D24_UNORM_S8_UINT} {
		var props vk.VkFormatProperties
		vk.VkGetPhysicalDeviceFormatProperties(d.Physical(), f, &props)
		if props.OptimalTilingFeatures&vk.VK_FORMAT_FEATURE_DEPTH_STENCIL_ATTACHMENT_BIT != 0 {
			return f, nil
		}
	}
	return 0, fmt.Errorf("render: no supported depth format")
}

// createDepth (re)builds the depth image to match the swapchain.
func (r *Renderer) createDepth() error {
	if r.depth != nil {
		r.depth.Destroy()
		r.depth = nil
	}
	if r.DepthFormat == 0 {
		f, err := r.Device.chooseDepthFormat()
		if err != nil {
			return err
		}
		r.DepthFormat = f
	}
	img, err := r.Device.NewImage(r.Swapchain.Extent, r.DepthFormat,
		vk.VK_IMAGE_USAGE_DEPTH_STENCIL_ATTACHMENT_BIT, vk.VK_IMAGE_ASPECT_DEPTH_BIT)
	if err != nil {
		return err
	}
	r.depth = img
	return nil
}
