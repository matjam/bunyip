package platform

import (
	"github.com/matjam/bunyip/internal/vk"
)

// RequiredInstanceExtensions lists the instance extensions a surface needs.
// The backend NewApp chose decides which one: VK_KHR_wayland_surface under
// Wayland and VK_KHR_xcb_surface under X11.
func RequiredInstanceExtensions() []string {
	if backendName == "wayland" {
		return []string{vk.VK_KHR_SURFACE_EXTENSION_NAME, vk.VK_KHR_WAYLAND_SURFACE_EXTENSION_NAME}
	}
	return []string{vk.VK_KHR_SURFACE_EXTENSION_NAME, vk.VK_KHR_XCB_SURFACE_EXTENSION_NAME}
}

// CreateSurface makes a VkSurfaceKHR over the window. Under Wayland the
// swapchain's size comes from the last xdg_toplevel.configure, because the
// compositor does not resize a surface: the client picks the buffer size and
// the configure says what it should be.
func (w *Window) CreateSurface(instance vk.VkInstance) (vk.VkSurfaceKHR, error) {
	var surface vk.VkSurfaceKHR
	if w.wl != nil {
		if vk.VkCreateWaylandSurfaceKHR == nil {
			return 0, &vk.Error{Command: "vkCreateWaylandSurfaceKHR", Result: vk.VK_ERROR_EXTENSION_NOT_PRESENT}
		}
		info := vk.VkWaylandSurfaceCreateInfoKHR{
			SType:   vk.VK_STRUCTURE_TYPE_WAYLAND_SURFACE_CREATE_INFO_KHR,
			Display: w.wl.app.display,
			Surface: w.wl.surface,
		}
		err := vk.Check("vkCreateWaylandSurfaceKHR", vk.VkCreateWaylandSurfaceKHR(instance, &info, nil, &surface))
		return surface, err
	}
	if vk.VkCreateXcbSurfaceKHR == nil {
		return 0, &vk.Error{Command: "vkCreateXcbSurfaceKHR", Result: vk.VK_ERROR_EXTENSION_NOT_PRESENT}
	}
	info := vk.VkXcbSurfaceCreateInfoKHR{
		SType:      vk.VK_STRUCTURE_TYPE_XCB_SURFACE_CREATE_INFO_KHR,
		Connection: w.app.conn,
		Window:     w.id,
	}
	err := vk.Check("vkCreateXcbSurfaceKHR", vk.VkCreateXcbSurfaceKHR(instance, &info, nil, &surface))
	return surface, err
}
