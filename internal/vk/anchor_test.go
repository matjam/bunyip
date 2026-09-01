package vk

import (
	"testing"
	"unsafe"
)

// TestKnownCSizes anchors the generated layout to sizes taken from a C
// compiler against vulkan_core.h, so a generator error that miscomputes both
// the Go struct and its expected layout still fails.
func TestKnownCSizes(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"VkApplicationInfo", unsafe.Sizeof(VkApplicationInfo{}), 48},
		{"VkInstanceCreateInfo", unsafe.Sizeof(VkInstanceCreateInfo{}), 64},
		{"VkPhysicalDeviceLimits", unsafe.Sizeof(VkPhysicalDeviceLimits{}), 504},
		{"VkPhysicalDeviceProperties", unsafe.Sizeof(VkPhysicalDeviceProperties{}), 824},
		{"VkPhysicalDeviceMemoryProperties", unsafe.Sizeof(VkPhysicalDeviceMemoryProperties{}), 520},
		{"VkPipelineColorBlendStateCreateInfo", unsafe.Sizeof(VkPipelineColorBlendStateCreateInfo{}), 56},
		{"VkClearValue", unsafe.Sizeof(VkClearValue{}), 16},
		{"VkExtent2D", unsafe.Sizeof(VkExtent2D{}), 8},
		{"VkSwapchainCreateInfoKHR", unsafe.Sizeof(VkSwapchainCreateInfoKHR{}), 104},
		{"VkGraphicsPipelineCreateInfo", unsafe.Sizeof(VkGraphicsPipelineCreateInfo{}), 144},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: size %d, C says %d", c.name, c.got, c.want)
		}
	}
}
