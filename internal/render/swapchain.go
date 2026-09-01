package render

import (
	"fmt"

	"github.com/matjam/bunyip/internal/vk"
)

// Swapchain owns the presentable images for one surface. Recreate handles
// resizes; the caller decides when by watching the platform's resize events
// and the results of Acquire and Present.
type Swapchain struct {
	Handle  vk.VkSwapchainKHR
	Format  vk.VkFormat
	Extent  vk.VkExtent2D
	Images  []vk.VkImage
	Views   []vk.VkImageView
	dev     *Device
	surface vk.VkSurfaceKHR
	vsync   bool
	// One render-finished semaphore per image: the semaphore a present
	// waits on must not be reused until that image comes back.
	renderDone []vk.VkSemaphore
}

// NewSwapchain creates a swapchain sized to the surface's current extent, or
// to fallback when the surface leaves the size to the application.
func (d *Device) NewSwapchain(surface vk.VkSurfaceKHR, fallback vk.VkExtent2D, vsync bool) (*Swapchain, error) {
	s := &Swapchain{dev: d, surface: surface, vsync: vsync}
	if err := s.create(fallback, 0); err != nil {
		return nil, err
	}
	return s, nil
}

// Recreate rebuilds the swapchain after a resize, reusing the old one.
func (s *Swapchain) Recreate(fallback vk.VkExtent2D) error {
	if err := s.dev.WaitIdle(); err != nil {
		return err
	}
	old := s.Handle
	s.destroyViews()
	err := s.create(fallback, old)
	if old != 0 {
		vk.VkDestroySwapchainKHR(s.dev.Handle, old, nil)
	}
	return err
}

func (s *Swapchain) create(fallback vk.VkExtent2D, old vk.VkSwapchainKHR) error {
	d := s.dev
	var caps vk.VkSurfaceCapabilitiesKHR
	if err := vk.Check("vkGetPhysicalDeviceSurfaceCapabilitiesKHR",
		vk.VkGetPhysicalDeviceSurfaceCapabilitiesKHR(d.Physical(), s.surface, &caps)); err != nil {
		return err
	}
	format, colorSpace, err := s.chooseFormat()
	if err != nil {
		return err
	}
	extent := caps.CurrentExtent
	if extent.Width == 0xFFFFFFFF { // the surface lets us choose
		extent = fallback
	}
	extent.Width = min(max(extent.Width, caps.MinImageExtent.Width), caps.MaxImageExtent.Width)
	extent.Height = min(max(extent.Height, caps.MinImageExtent.Height), caps.MaxImageExtent.Height)
	if extent.Width == 0 || extent.Height == 0 {
		return fmt.Errorf("render: surface has zero extent")
	}
	imageCount := caps.MinImageCount + 1
	if caps.MaxImageCount > 0 {
		imageCount = min(imageCount, caps.MaxImageCount)
	}
	presentMode := vk.VK_PRESENT_MODE_FIFO_KHR // always available
	if !s.vsync {
		presentMode = s.chooseImmediate()
	}
	info := vk.VkSwapchainCreateInfoKHR{
		SType:            vk.VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR,
		Surface:          s.surface,
		MinImageCount:    imageCount,
		ImageFormat:      format,
		ImageColorSpace:  colorSpace,
		ImageExtent:      extent,
		ImageArrayLayers: 1,
		ImageUsage:       vk.VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | vk.VK_IMAGE_USAGE_TRANSFER_SRC_BIT,
		ImageSharingMode: vk.VK_SHARING_MODE_EXCLUSIVE,
		PreTransform:     caps.CurrentTransform,
		CompositeAlpha:   vk.VK_COMPOSITE_ALPHA_OPAQUE_BIT_KHR,
		PresentMode:      presentMode,
		Clipped:          vk.VK_TRUE,
		OldSwapchain:     old,
	}
	if err := vk.Check("vkCreateSwapchainKHR", vk.VkCreateSwapchainKHR(d.Handle, &info, nil, &s.Handle)); err != nil {
		return err
	}
	s.Format, s.Extent = format, extent
	var count uint32
	if err := vk.Check("vkGetSwapchainImagesKHR", vk.VkGetSwapchainImagesKHR(d.Handle, s.Handle, &count, nil)); err != nil {
		return err
	}
	s.Images = make([]vk.VkImage, count)
	if err := vk.Check("vkGetSwapchainImagesKHR", vk.VkGetSwapchainImagesKHR(d.Handle, s.Handle, &count, &s.Images[0])); err != nil {
		return err
	}
	s.Views = make([]vk.VkImageView, count)
	s.renderDone = make([]vk.VkSemaphore, count)
	for i, img := range s.Images {
		if s.Views[i], err = d.newImageView(img, format, vk.VK_IMAGE_ASPECT_COLOR_BIT); err != nil {
			return err
		}
		if s.renderDone[i], err = d.newSemaphore(); err != nil {
			return err
		}
	}
	d.log.Debug("render: swapchain created", "extent", fmt.Sprintf("%dx%d", extent.Width, extent.Height),
		"format", format, "images", count, "presentMode", presentMode)
	return nil
}

// chooseFormat prefers an 8-bit BGRA/RGBA sRGB surface so that shaders work
// in linear light and the swapchain does the encoding.
func (s *Swapchain) chooseFormat() (vk.VkFormat, vk.VkColorSpaceKHR, error) {
	var count uint32
	if err := vk.Check("vkGetPhysicalDeviceSurfaceFormatsKHR",
		vk.VkGetPhysicalDeviceSurfaceFormatsKHR(s.dev.Physical(), s.surface, &count, nil)); err != nil {
		return 0, 0, err
	}
	formats := make([]vk.VkSurfaceFormatKHR, max(count, 1))
	if err := vk.Check("vkGetPhysicalDeviceSurfaceFormatsKHR",
		vk.VkGetPhysicalDeviceSurfaceFormatsKHR(s.dev.Physical(), s.surface, &count, &formats[0])); err != nil {
		return 0, 0, err
	}
	for _, want := range []vk.VkFormat{vk.VK_FORMAT_B8G8R8A8_SRGB, vk.VK_FORMAT_R8G8B8A8_SRGB} {
		for _, f := range formats[:count] {
			if f.Format == want && f.ColorSpace == vk.VK_COLOR_SPACE_SRGB_NONLINEAR_KHR {
				return f.Format, f.ColorSpace, nil
			}
		}
	}
	if count == 0 {
		return 0, 0, fmt.Errorf("render: surface offers no formats")
	}
	return formats[0].Format, formats[0].ColorSpace, nil
}

func (s *Swapchain) chooseImmediate() vk.VkPresentModeKHR {
	var count uint32
	vk.VkGetPhysicalDeviceSurfacePresentModesKHR(s.dev.Physical(), s.surface, &count, nil)
	modes := make([]vk.VkPresentModeKHR, max(count, 1))
	vk.VkGetPhysicalDeviceSurfacePresentModesKHR(s.dev.Physical(), s.surface, &count, &modes[0])
	for _, want := range []vk.VkPresentModeKHR{vk.VK_PRESENT_MODE_MAILBOX_KHR, vk.VK_PRESENT_MODE_IMMEDIATE_KHR} {
		for _, m := range modes[:count] {
			if m == want {
				return m
			}
		}
	}
	return vk.VK_PRESENT_MODE_FIFO_KHR
}

func (s *Swapchain) destroyViews() {
	for _, v := range s.Views {
		vk.VkDestroyImageView(s.dev.Handle, v, nil)
	}
	for _, sem := range s.renderDone {
		vk.VkDestroySemaphore(s.dev.Handle, sem, nil)
	}
	s.Views, s.renderDone = nil, nil
}

// Destroy releases the swapchain but not the surface, which the platform owns.
func (s *Swapchain) Destroy() {
	_ = s.dev.WaitIdle()
	s.destroyViews()
	if s.Handle != 0 {
		vk.VkDestroySwapchainKHR(s.dev.Handle, s.Handle, nil)
		s.Handle = 0
	}
}
