package platform

import (
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// RequiredInstanceExtensions lists the instance extensions a surface needs.
func RequiredInstanceExtensions() []string {
	return []string{vk.VK_KHR_SURFACE_EXTENSION_NAME, vk.VK_EXT_METAL_SURFACE_EXTENSION_NAME}
}

// CreateSurface makes a VkSurfaceKHR over the window's Metal layer.
func (w *Window) CreateSurface(instance vk.VkInstance) (vk.VkSurfaceKHR, error) {
	if vk.VkCreateMetalSurfaceEXT == nil {
		return 0, &vk.Error{Command: "vkCreateMetalSurfaceEXT", Result: vk.VK_ERROR_EXTENSION_NOT_PRESENT}
	}
	info := vk.VkMetalSurfaceCreateInfoEXT{
		SType:  vk.VK_STRUCTURE_TYPE_METAL_SURFACE_CREATE_INFO_EXT,
		PLayer: *(*unsafe.Pointer)(unsafe.Pointer(&w.layer)), // an Objective-C object, not Go memory
	}
	var surface vk.VkSurfaceKHR
	err := vk.Check("vkCreateMetalSurfaceEXT", vk.VkCreateMetalSurfaceEXT(instance, &info, nil, &surface))
	return surface, err
}
