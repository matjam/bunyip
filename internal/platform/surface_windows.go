package platform

import "github.com/matjam/bunyip/internal/vk"

// RequiredInstanceExtensions lists the instance extensions a surface needs.
func RequiredInstanceExtensions() []string {
	return []string{vk.VK_KHR_SURFACE_EXTENSION_NAME, vk.VK_KHR_WIN32_SURFACE_EXTENSION_NAME}
}

// CreateSurface makes a VkSurfaceKHR over the window.
func (w *Window) CreateSurface(instance vk.VkInstance) (vk.VkSurfaceKHR, error) {
	if vk.VkCreateWin32SurfaceKHR == nil {
		return 0, &vk.Error{Command: "vkCreateWin32SurfaceKHR", Result: vk.VK_ERROR_EXTENSION_NOT_PRESENT}
	}
	info := vk.VkWin32SurfaceCreateInfoKHR{
		SType:     vk.VK_STRUCTURE_TYPE_WIN32_SURFACE_CREATE_INFO_KHR,
		Hinstance: w.app.instance,
		Hwnd:      w.hwnd,
	}
	var surface vk.VkSurfaceKHR
	err := vk.Check("vkCreateWin32SurfaceKHR", vk.VkCreateWin32SurfaceKHR(instance, &info, nil, &surface))
	return surface, err
}
