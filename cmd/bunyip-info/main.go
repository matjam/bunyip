// Command bunyip-info reports the graphics stack Bunyip would run on: which
// Vulkan library loaded, the instance version, and every physical device with
// its driver and queue families. It opens no window, so it doubles as the
// smoke test for the generated binding.
package main

import (
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bunyip-info:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := vk.Load(); err != nil {
		return err
	}
	var version uint32
	if vk.VkEnumerateInstanceVersion != nil {
		if err := vk.Check("vkEnumerateInstanceVersion", vk.VkEnumerateInstanceVersion(&version)); err != nil {
			return err
		}
	} else {
		version = vk.API_VERSION_1_0
	}
	fmt.Printf("loader: Vulkan %s (binding header %d, %s/%s)\n", vk.FormatVersion(version), vk.HeaderVersion, runtime.GOOS, runtime.GOARCH)

	exts, err := instanceExtensions()
	if err != nil {
		return err
	}
	fmt.Printf("instance extensions: %d\n", len(exts))
	for _, e := range exts {
		fmt.Printf("  %s (rev %d)\n", vk.GoString(e.ExtensionName[:]), e.SpecVersion)
	}

	instance, err := createInstance(exts)
	if err != nil {
		return err
	}
	if err := vk.LoadInstance(instance); err != nil {
		return err
	}
	// Bound only by LoadInstance, so the function value is read after it.
	defer func() { vk.VkDestroyInstance(instance, nil) }()
	return listDevices(instance)
}

func instanceExtensions() ([]vk.VkExtensionProperties, error) {
	var count uint32
	if err := vk.Check("vkEnumerateInstanceExtensionProperties", vk.VkEnumerateInstanceExtensionProperties(nil, &count, nil)); err != nil {
		return nil, err
	}
	exts := make([]vk.VkExtensionProperties, count)
	if count == 0 {
		return exts, nil
	}
	if err := vk.Check("vkEnumerateInstanceExtensionProperties", vk.VkEnumerateInstanceExtensionProperties(nil, &count, &exts[0])); err != nil {
		return nil, err
	}
	return exts[:count], nil
}

// createInstance asks for portability enumeration when the loader offers it,
// which is what makes MoltenVK visible as a device on macOS.
func createInstance(available []vk.VkExtensionProperties) (vk.VkInstance, error) {
	var keep [][]byte
	cstr := func(s string) *byte {
		p, b := vk.CString(s)
		keep = append(keep, b)
		return p
	}
	var enabled []*byte
	var flags vk.VkInstanceCreateFlags
	for _, e := range available {
		if vk.GoString(e.ExtensionName[:]) == vk.VK_KHR_PORTABILITY_ENUMERATION_EXTENSION_NAME {
			enabled = append(enabled, cstr(vk.VK_KHR_PORTABILITY_ENUMERATION_EXTENSION_NAME))
			flags |= vk.VK_INSTANCE_CREATE_ENUMERATE_PORTABILITY_BIT_KHR
		}
	}
	app := vk.VkApplicationInfo{
		SType:            vk.VK_STRUCTURE_TYPE_APPLICATION_INFO,
		PApplicationName: cstr("bunyip-info"),
		PEngineName:      cstr("bunyip"),
		ApiVersion:       vk.API_VERSION_1_3,
	}
	info := vk.VkInstanceCreateInfo{
		SType:                   vk.VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO,
		Flags:                   flags,
		PApplicationInfo:        &app,
		EnabledExtensionCount:   uint32(len(enabled)),
		PpEnabledExtensionNames: firstOrNil(enabled),
	}
	var instance vk.VkInstance
	err := vk.Check("vkCreateInstance", vk.VkCreateInstance(&info, nil, &instance))
	runtime.KeepAlive(keep)
	runtime.KeepAlive(enabled)
	return instance, err
}

func firstOrNil[T any](s []T) *T {
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}

func listDevices(instance vk.VkInstance) error {
	var count uint32
	if err := vk.Check("vkEnumeratePhysicalDevices", vk.VkEnumeratePhysicalDevices(instance, &count, nil)); err != nil {
		return err
	}
	devices := make([]vk.VkPhysicalDevice, count)
	if count > 0 {
		if err := vk.Check("vkEnumeratePhysicalDevices", vk.VkEnumeratePhysicalDevices(instance, &count, &devices[0])); err != nil {
			return err
		}
	}
	fmt.Printf("physical devices: %d\n", count)
	for i, dev := range devices[:count] {
		var props vk.VkPhysicalDeviceProperties
		vk.VkGetPhysicalDeviceProperties(dev, &props)
		fmt.Printf("  [%d] %s: type %d, api %s, driver 0x%x, vendor 0x%04x device 0x%04x\n",
			i, vk.GoString(props.DeviceName[:]), props.DeviceType, vk.FormatVersion(props.ApiVersion),
			props.DriverVersion, props.VendorID, props.DeviceID)
		var driver vk.VkPhysicalDeviceDriverProperties
		driver.SType = vk.VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_DRIVER_PROPERTIES
		props2 := vk.VkPhysicalDeviceProperties2{SType: vk.VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2, PNext: unsafe.Pointer(&driver)}
		vk.VkGetPhysicalDeviceProperties2(dev, &props2)
		fmt.Printf("      driver: %s %s (id %d)\n", vk.GoString(driver.DriverName[:]), vk.GoString(driver.DriverInfo[:]), driver.DriverID)
		fmt.Printf("      limits: maxImageDimension2D %d, maxPushConstantsSize %d, timestampPeriod %g\n",
			props.Limits.MaxImageDimension2D, props.Limits.MaxPushConstantsSize, props.Limits.TimestampPeriod)
		var mem vk.VkPhysicalDeviceMemoryProperties
		vk.VkGetPhysicalDeviceMemoryProperties(dev, &mem)
		for h := range mem.MemoryHeapCount {
			fmt.Printf("      heap %d: %d MiB flags 0x%x\n", h, mem.MemoryHeaps[h].Size>>20, mem.MemoryHeaps[h].Flags)
		}
		var qcount uint32
		vk.VkGetPhysicalDeviceQueueFamilyProperties(dev, &qcount, nil)
		queues := make([]vk.VkQueueFamilyProperties, qcount)
		if qcount > 0 {
			vk.VkGetPhysicalDeviceQueueFamilyProperties(dev, &qcount, &queues[0])
		}
		for q, qf := range queues[:qcount] {
			fmt.Printf("      queue family %d: %d queues, flags 0x%x\n", q, qf.QueueCount, qf.QueueFlags)
		}
	}
	return nil
}
