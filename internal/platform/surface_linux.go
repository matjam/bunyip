package platform

import (
	"github.com/matjam/bunyip/internal/vk"
)

// RequiredInstanceExtensions lists the instance extensions a surface needs.
func RequiredInstanceExtensions() []string {
	return []string{vk.VK_KHR_SURFACE_EXTENSION_NAME, vk.VK_KHR_XCB_SURFACE_EXTENSION_NAME}
}

// CreateSurface makes a VkSurfaceKHR over the X window.
func (w *Window) CreateSurface(instance vk.VkInstance) (vk.VkSurfaceKHR, error) {
	if vk.VkCreateXcbSurfaceKHR == nil {
		return 0, &vk.Error{Command: "vkCreateXcbSurfaceKHR", Result: vk.VK_ERROR_EXTENSION_NOT_PRESENT}
	}
	info := vk.VkXcbSurfaceCreateInfoKHR{
		SType:      vk.VK_STRUCTURE_TYPE_XCB_SURFACE_CREATE_INFO_KHR,
		Connection: w.app.conn,
		Window:     w.id,
	}
	var surface vk.VkSurfaceKHR
	err := vk.Check("vkCreateXcbSurfaceKHR", vk.VkCreateXcbSurfaceKHR(instance, &info, nil, &surface))
	return surface, err
}
