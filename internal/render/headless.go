package render

import "github.com/matjam/bunyip/internal/vk"

// HeadlessSurfaceExtensions lists what a headless renderer needs on the
// instance: nothing. Headless rendering draws into ordinary images and
// never creates a surface, so no window system and no surface extension
// is involved.
func HeadlessSurfaceExtensions() []string { return nil }

// NewHeadlessSurface is the SurfaceFunc for a headless renderer. It
// returns a zero surface; the renderer then picks any graphics-capable
// device and builds a headless swapchain of plain images, for tests and
// offscreen rendering on machines whose driver has no windowed path.
func NewHeadlessSurface(instance vk.VkInstance) (vk.VkSurfaceKHR, error) {
	return 0, nil
}
