package render

import (
	"fmt"

	"github.com/matjam/bunyip/internal/vk"
)

// chooseDepthFormat picks the first depth format the GPU can render to,
// preferring one with a stencil aspect (outlines and x-ray need it).
// D32_SFLOAT_S8 is universal on Apple GPUs, which lack D24_UNORM_S8.
func (d *Device) chooseDepthFormat() (vk.VkFormat, error) {
	for _, f := range []vk.VkFormat{vk.VK_FORMAT_D32_SFLOAT_S8_UINT, vk.VK_FORMAT_D24_UNORM_S8_UINT, vk.VK_FORMAT_D32_SFLOAT} {
		var props vk.VkFormatProperties
		vk.VkGetPhysicalDeviceFormatProperties(d.Physical(), f, &props)
		if props.OptimalTilingFeatures&vk.VK_FORMAT_FEATURE_DEPTH_STENCIL_ATTACHMENT_BIT != 0 {
			return f, nil
		}
	}
	return 0, fmt.Errorf("render: no supported depth format")
}

// ShadowFormat picks the format for a depth image that is only ever
// rendered to and sampled, never stencilled: a shadow atlas. It prefers
// a format without a stencil aspect, which costs half the memory of
// D32_SFLOAT_S8 on the desktop GPUs that pad that format to eight bytes
// a texel, and falls back to the renderer's own depth format. Both
// candidates must also filter, since the shadow sampler compares with
// linear filtering.
func (d *Device) ShadowFormat(fallback vk.VkFormat) vk.VkFormat {
	const want = vk.VK_FORMAT_FEATURE_DEPTH_STENCIL_ATTACHMENT_BIT | vk.VK_FORMAT_FEATURE_SAMPLED_IMAGE_FILTER_LINEAR_BIT
	for _, f := range []vk.VkFormat{vk.VK_FORMAT_D32_SFLOAT, vk.VK_FORMAT_X8_D24_UNORM_PACK32, vk.VK_FORMAT_D16_UNORM} {
		var props vk.VkFormatProperties
		vk.VkGetPhysicalDeviceFormatProperties(d.Physical(), f, &props)
		if props.OptimalTilingFeatures&want == want {
			return f
		}
	}
	return fallback
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
