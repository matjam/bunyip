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
	alloc       allocator
	anisotropy  float32 // max anisotropy the device allows, 1 when unsupported
	arrayIndex  bool    // sampler and sampled-image arrays may be indexed dynamically
	depthClamp  bool    // the device clamps depth instead of clipping, when asked
	waits       uint64  // times the device or its queue was waited on
}

// NewDevice picks a GPU able to present to surface and creates the logical
// device with dynamic rendering and synchronization2 enabled.
func NewDevice(inst *Instance, surface vk.VkSurfaceKHR) (*Device, error) {
	g, err := pickGPU(inst.Handle, surface)
	if err != nil {
		return nil, err
	}
	d := &Device{QueueFamily: g.queueFamily, Name: g.name, log: inst.log, gpu: g, anisotropy: 1}
	d.alloc.dev = d
	// Enabling the swapchain extension without VK_KHR_surface on the
	// instance violates VUID-vkCreateDevice-ppEnabledExtensionNames-01387,
	// and a headless device has nothing to present to anyway.
	var exts []string
	if surface != 0 {
		exts = append(exts, vk.VK_KHR_SWAPCHAIN_EXTENSION_NAME)
	}
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
	if g.features.SamplerAnisotropy != 0 {
		features.Features.SamplerAnisotropy = vk.VK_TRUE
		d.anisotropy = min(g.props.Limits.MaxSamplerAnisotropy, 8)
	}
	// The mesh material set holds one array of samplers that a shader
	// indexes per texture slot, which needs this feature. Every desktop
	// driver and MoltenVK report it; a device without it cannot run the
	// mesh pipelines, and initMeshPass says so.
	if g.features.ShaderSampledImageArrayDynamicIndexing != 0 {
		features.Features.ShaderSampledImageArrayDynamicIndexing = vk.VK_TRUE
		d.arrayIndex = true
	}
	// Depth clamping lets the shadow pipelines keep casters in front of a
	// cascade's near plane. It is optional, so pipelines ask for it and
	// NewPipeline drops the request where the device does not have it.
	if g.features.DepthClamp != 0 {
		features.Features.DepthClamp = vk.VK_TRUE
		d.depthClamp = true
	}
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

// WaitIdle blocks until the device has finished all submitted work. It
// stalls the GPU, so it belongs in setup and teardown rather than in a
// frame; Waits counts every such stall.
func (d *Device) WaitIdle() error {
	d.waits++
	return vk.Check("vkDeviceWaitIdle", vk.VkDeviceWaitIdle(d.Handle))
}

// Waits is how many times the device or its queue has been waited on
// since it was created. A frame that uploads and destroys through the
// staging arena and the retire ring adds nothing to it, so the count
// stands still once a game is running.
func (d *Device) Waits() uint64 { return d.waits }

// ArrayIndexing reports whether sampler and sampled-image arrays may be
// indexed by a dynamically uniform expression, which the mesh material
// set's sampler array needs.
func (d *Device) ArrayIndexing() bool { return d.arrayIndex }

// Destroy releases the device after waiting for it to go idle.
func (d *Device) Destroy() {
	if d.Handle == 0 {
		return
	}
	_ = d.WaitIdle()
	d.alloc.destroy()
	vk.VkDestroyCommandPool(d.Handle, d.pool, nil)
	vk.VkDestroyDevice(d.Handle, nil)
	d.Handle = 0
}

// DepthClamp reports whether the device clamps depth for pipelines that
// ask for it. Without it a pipeline clips at its near and far planes,
// and the caller compensates.
func (d *Device) DepthClamp() bool { return d.depthClamp }

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
	d.waits++
	return vk.Check("vkQueueWaitIdle", vk.VkQueueWaitIdle(d.Queue))
}
