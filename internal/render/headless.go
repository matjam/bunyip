package render

import "github.com/matjam/bunyip/internal/vk"

// HeadlessSurfaceExtensions lists what a headless surface needs on the instance.
func HeadlessSurfaceExtensions() []string {
	return []string{vk.VK_KHR_SURFACE_EXTENSION_NAME, vk.VK_EXT_HEADLESS_SURFACE_EXTENSION_NAME}
}

// NewHeadlessSurface creates a surface with no window behind it, for tests
// and offscreen rendering. The swapchain extent then comes from the caller.
func NewHeadlessSurface(instance vk.VkInstance) (vk.VkSurfaceKHR, error) {
	if vk.VkCreateHeadlessSurfaceEXT == nil {
		return 0, &vk.Error{Command: "vkCreateHeadlessSurfaceEXT", Result: vk.VK_ERROR_EXTENSION_NOT_PRESENT}
	}
	info := vk.VkHeadlessSurfaceCreateInfoEXT{SType: vk.VK_STRUCTURE_TYPE_HEADLESS_SURFACE_CREATE_INFO_EXT}
	var surface vk.VkSurfaceKHR
	err := vk.Check("vkCreateHeadlessSurfaceEXT", vk.VkCreateHeadlessSurfaceEXT(instance, &info, nil, &surface))
	return surface, err
}
