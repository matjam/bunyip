package render

import (
	"fmt"
	"slices"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// gpu is a physical device that can draw and present to the surface.
type gpu struct {
	handle      vk.VkPhysicalDevice
	props       vk.VkPhysicalDeviceProperties
	features    vk.VkPhysicalDeviceFeatures
	memProps    vk.VkPhysicalDeviceMemoryProperties
	queueFamily uint32
	extensions  []string
	name        string
}

// pickGPU chooses the first discrete GPU that can render and present to
// surface, falling back to any device that can. surface may be zero for a
// purely offscreen device.
func pickGPU(instance vk.VkInstance, surface vk.VkSurfaceKHR) (*gpu, error) {
	var count uint32
	if err := vk.Check("vkEnumeratePhysicalDevices", vk.VkEnumeratePhysicalDevices(instance, &count, nil)); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, fmt.Errorf("render: no Vulkan devices")
	}
	handles := make([]vk.VkPhysicalDevice, count)
	if err := vk.Check("vkEnumeratePhysicalDevices", vk.VkEnumeratePhysicalDevices(instance, &count, &handles[0])); err != nil {
		return nil, err
	}
	var best *gpu
	for _, h := range handles[:count] {
		g, err := inspectGPU(h, surface)
		if err != nil {
			return nil, err
		}
		if g == nil {
			continue
		}
		if best == nil || g.props.DeviceType == vk.VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU && best.props.DeviceType != vk.VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU {
			best = g
		}
	}
	if best == nil {
		return nil, fmt.Errorf("render: no device supports graphics and presentation")
	}
	return best, nil
}

func inspectGPU(h vk.VkPhysicalDevice, surface vk.VkSurfaceKHR) (*gpu, error) {
	g := &gpu{handle: h}
	vk.VkGetPhysicalDeviceProperties(h, &g.props)
	vk.VkGetPhysicalDeviceMemoryProperties(h, &g.memProps)
	g.name = vk.GoString(g.props.DeviceName[:])
	if g.props.ApiVersion < vk.API_VERSION_1_3 {
		return nil, nil
	}
	var v13 vk.VkPhysicalDeviceVulkan13Features
	v13.SType = vk.VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_VULKAN_1_3_FEATURES
	f2 := vk.VkPhysicalDeviceFeatures2{SType: vk.VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_FEATURES_2, PNext: unsafe.Pointer(&v13)}
	vk.VkGetPhysicalDeviceFeatures2(h, &f2)
	g.features = f2.Features
	if v13.DynamicRendering == 0 || v13.Synchronization2 == 0 {
		return nil, nil
	}
	family, ok, err := findQueueFamily(h, surface)
	if err != nil || !ok {
		return nil, err
	}
	g.queueFamily = family
	if g.extensions, err = deviceExtensions(h); err != nil {
		return nil, err
	}
	// A headless device draws into plain images and never presents, so
	// it does not need the swapchain extension.
	if surface != 0 && !slices.Contains(g.extensions, vk.VK_KHR_SWAPCHAIN_EXTENSION_NAME) {
		return nil, nil
	}
	return g, nil
}

// findQueueFamily returns a family with graphics support that can also
// present to surface (or any graphics family when surface is zero).
func findQueueFamily(h vk.VkPhysicalDevice, surface vk.VkSurfaceKHR) (uint32, bool, error) {
	var count uint32
	vk.VkGetPhysicalDeviceQueueFamilyProperties(h, &count, nil)
	families := make([]vk.VkQueueFamilyProperties, max(count, 1))
	vk.VkGetPhysicalDeviceQueueFamilyProperties(h, &count, &families[0])
	for i, f := range families[:count] {
		if f.QueueFlags&vk.VK_QUEUE_GRAPHICS_BIT == 0 {
			continue
		}
		if surface == 0 {
			return uint32(i), true, nil
		}
		var supported vk.VkBool32
		if err := vk.Check("vkGetPhysicalDeviceSurfaceSupportKHR",
			vk.VkGetPhysicalDeviceSurfaceSupportKHR(h, uint32(i), surface, &supported)); err != nil {
			return 0, false, err
		}
		if supported != 0 {
			return uint32(i), true, nil
		}
	}
	return 0, false, nil
}

func deviceExtensions(h vk.VkPhysicalDevice) ([]string, error) {
	var count uint32
	if err := vk.Check("vkEnumerateDeviceExtensionProperties", vk.VkEnumerateDeviceExtensionProperties(h, nil, &count, nil)); err != nil {
		return nil, err
	}
	props := make([]vk.VkExtensionProperties, max(count, 1))
	if count > 0 {
		if err := vk.Check("vkEnumerateDeviceExtensionProperties", vk.VkEnumerateDeviceExtensionProperties(h, nil, &count, &props[0])); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, count)
	for _, p := range props[:count] {
		names = append(names, vk.GoString(p.ExtensionName[:]))
	}
	return names, nil
}

// memoryType finds a memory type index matching the filter and properties.
func (g *gpu) memoryType(typeBits uint32, props vk.VkMemoryPropertyFlags) (uint32, error) {
	for i := range g.memProps.MemoryTypeCount {
		if typeBits&(1<<i) != 0 && g.memProps.MemoryTypes[i].PropertyFlags&props == props {
			return i, nil
		}
	}
	return 0, fmt.Errorf("render: no memory type for bits 0x%x with properties 0x%x", typeBits, props)
}
