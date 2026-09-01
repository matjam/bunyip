package render

import (
	"log/slog"
	"runtime"
	"slices"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// Device is a logical device with one graphics+present queue and a command
// pool for the frame ring. Everything drawn goes through it.
type Device struct {
	Handle      vk.VkDevice
	Queue       vk.VkQueue
	QueueFamily uint32
	Name        string
	log         *slog.Logger
	gpu         *gpu
	pool        vk.VkCommandPool
	portability bool
}

// NewDevice picks a GPU able to present to surface and creates the logical
// device with dynamic rendering and synchronization2 enabled.
func NewDevice(inst *Instance, surface vk.VkSurfaceKHR) (*Device, error) {
	g, err := pickGPU(inst.Handle, surface)
	if err != nil {
		return nil, err
	}
	d := &Device{QueueFamily: g.queueFamily, Name: g.name, log: inst.log, gpu: g}
	exts := []string{vk.VK_KHR_SWAPCHAIN_EXTENSION_NAME}
	if slices.Contains(g.extensions, vk.VK_KHR_PORTABILITY_SUBSET_EXTENSION_NAME) {
		exts = append(exts, vk.VK_KHR_PORTABILITY_SUBSET_EXTENSION_NAME) // required when advertised
		d.portability = true
	}
	extNames := newCStrings(exts)
	priority := float32(1)
	queueInfo := vk.VkDeviceQueueCreateInfo{
		SType:            vk.VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,
		QueueFamilyIndex: g.queueFamily,
		QueueCount:       1,
		PQueuePriorities: &priority,
	}
	v13 := vk.VkPhysicalDeviceVulkan13Features{
		SType:            vk.VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_VULKAN_1_3_FEATURES,
		DynamicRendering: vk.VK_TRUE,
		Synchronization2: vk.VK_TRUE,
	}
	features := vk.VkPhysicalDeviceFeatures2{SType: vk.VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_FEATURES_2, PNext: unsafe.Pointer(&v13)}
	info := vk.VkDeviceCreateInfo{
		SType:                   vk.VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,
		PNext:                   unsafe.Pointer(&features),
		QueueCreateInfoCount:    1,
		PQueueCreateInfos:       &queueInfo,
		EnabledExtensionCount:   extNames.count(),
		PpEnabledExtensionNames: extNames.first(),
	}
	if err := vk.Check("vkCreateDevice", vk.VkCreateDevice(g.handle, &info, nil, &d.Handle)); err != nil {
		return nil, err
	}
	runtime.KeepAlive(extNames)
	if err := vk.LoadDevice(d.Handle); err != nil {
		return nil, err
	}
	vk.VkGetDeviceQueue(d.Handle, g.queueFamily, 0, &d.Queue)
	poolInfo := vk.VkCommandPoolCreateInfo{
		SType:            vk.VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO,
		Flags:            vk.VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT,
		QueueFamilyIndex: g.queueFamily,
	}
	if err := vk.Check("vkCreateCommandPool", vk.VkCreateCommandPool(d.Handle, &poolInfo, nil, &d.pool)); err != nil {
		return nil, err
	}
	d.log.Info("render: device created", "gpu", g.name, "api", vk.FormatVersion(g.props.ApiVersion),
		"queueFamily", g.queueFamily, "portabilitySubset", d.portability)
	return d, nil
}

// WaitIdle blocks until the device has finished all submitted work.
func (d *Device) WaitIdle() error {
	return vk.Check("vkDeviceWaitIdle", vk.VkDeviceWaitIdle(d.Handle))
}

// Destroy releases the device after waiting for it to go idle.
func (d *Device) Destroy() {
	if d.Handle == 0 {
		return
	}
	_ = d.WaitIdle()
	vk.VkDestroyCommandPool(d.Handle, d.pool, nil)
	vk.VkDestroyDevice(d.Handle, nil)
	d.Handle = 0
}

// Limits exposes the physical device limits.
func (d *Device) Limits() *vk.VkPhysicalDeviceLimits { return &d.gpu.props.Limits }

// Physical exposes the physical device handle for surface queries.
func (d *Device) Physical() vk.VkPhysicalDevice { return d.gpu.handle }

// allocateCommandBuffers takes primary command buffers from the pool.
func (d *Device) allocateCommandBuffers(n uint32) ([]vk.VkCommandBuffer, error) {
	info := vk.VkCommandBufferAllocateInfo{
		SType:              vk.VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO,
		CommandPool:        d.pool,
		Level:              vk.VK_COMMAND_BUFFER_LEVEL_PRIMARY,
		CommandBufferCount: n,
	}
	bufs := make([]vk.VkCommandBuffer, n)
	err := vk.Check("vkAllocateCommandBuffers", vk.VkAllocateCommandBuffers(d.Handle, &info, &bufs[0]))
	return bufs, err
}

// OneShot records commands into a fresh buffer, submits them and waits.
// It is for setup-time uploads and readback, not per-frame work.
func (d *Device) OneShot(record func(cb vk.VkCommandBuffer)) error {
	bufs, err := d.allocateCommandBuffers(1)
	if err != nil {
		return err
	}
	cb := bufs[0]
	defer vk.VkFreeCommandBuffers(d.Handle, d.pool, 1, &cb)
	begin := vk.VkCommandBufferBeginInfo{SType: vk.VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO, Flags: vk.VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT}
	if err := vk.Check("vkBeginCommandBuffer", vk.VkBeginCommandBuffer(cb, &begin)); err != nil {
		return err
	}
	record(cb)
	if err := vk.Check("vkEndCommandBuffer", vk.VkEndCommandBuffer(cb)); err != nil {
		return err
	}
	cbInfo := vk.VkCommandBufferSubmitInfo{SType: vk.VK_STRUCTURE_TYPE_COMMAND_BUFFER_SUBMIT_INFO, CommandBuffer: cb}
	submit := vk.VkSubmitInfo2{SType: vk.VK_STRUCTURE_TYPE_SUBMIT_INFO_2, CommandBufferInfoCount: 1, PCommandBufferInfos: &cbInfo}
	if err := vk.Check("vkQueueSubmit2", vk.VkQueueSubmit2(d.Queue, 1, &submit, 0)); err != nil {
		return err
	}
	return vk.Check("vkQueueWaitIdle", vk.VkQueueWaitIdle(d.Queue))
}
