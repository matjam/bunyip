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
	depthClamp  bool    // the device clamps depth instead of clipping, when asked
	// independentBlend is whether colour attachments may blend
	// differently from one another, which one pipeline needs and the rest
	// do not.
	independentBlend bool
	waits            uint64 // times the device or its queue was waited on
	frameNo          uint64 // frames begun, for the retire ring
	retired          []deferred
}

// NewDevice picks a GPU able to present to surface and creates the logical
// device with dynamic rendering and synchronization2 enabled.
func NewDevice(inst *Instance, surface vk.VkSurfaceKHR) (*Device, error) {
	g, err := pickGPU(inst.Handle, surface)
	if err != nil {
		return nil, err
	}
	d := &Device{QueueFamily: g.queueFamily, Name: g.name, log: inst.log, gpu: g, anisotropy: 1}
	complete := false
	defer func() {
		if !complete && d.Handle != 0 {
			d.Destroy()
		}
	}()
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
	// Depth clamping lets the shadow pipelines keep casters in front of a
	// cascade's near plane. It is optional, so pipelines ask for it and
	// NewPipeline drops the request where the device does not have it.
	if g.features.DepthClamp != 0 {
		features.Features.DepthClamp = vk.VK_TRUE
		d.depthClamp = true
	}
	// Blending each colour attachment differently is what the
	// order-independent transparency pass needs: its colour attachment
	// adds and its revealage attachment multiplies. Without it that pass
	// cannot be built and the caller keeps sorting.
	if g.features.IndependentBlend != 0 {
		features.Features.IndependentBlend = vk.VK_TRUE
		d.independentBlend = true
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
	if err := vk.LoadDevice(inst.Handle); err != nil {
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
	complete = true
	return d, nil
}

// WaitIdle blocks until the device has finished all submitted work. It
// stalls the GPU, so it belongs in setup and teardown rather than in a
// frame; Waits counts every such stall. Prefer Retire for an object a
// recorded frame may still reference.
func (d *Device) WaitIdle() error {
	d.waits++
	return vk.Check("vkDeviceWaitIdle", vk.VkDeviceWaitIdle(d.Handle))
}

// Waits is how many times the device or its queue has been waited on
// since it was created. A frame that uploads and destroys through the
// staging arena and the retire ring adds nothing to it, so the count
// stands still once a game is running.
func (d *Device) Waits() uint64 { return d.waits }

// Destroy releases the device after waiting for it to go idle.
func (d *Device) Destroy() {
	if d.Handle == 0 {
		return
	}
	_ = d.WaitIdle()
	d.flushRetired()
	d.alloc.destroy()
	vk.VkDestroyCommandPool(d.Handle, d.pool, nil)
	vk.VkDestroyDevice(d.Handle, nil)
	d.Handle = 0
}

// DepthClamp reports whether the device clamps depth for pipelines that
// ask for it. Without it a pipeline clips at its near and far planes,
// and the caller compensates.
func (d *Device) DepthClamp() bool { return d.depthClamp }

// IndependentBlend reports whether a pipeline may give each colour
// attachment its own blend equation. A pipeline whose ExtraColor differs
// from its first attachment needs it, and the caller falls back to
// something else where it is missing.
func (d *Device) IndependentBlend() bool { return d.independentBlend }

// Limits exposes the physical device limits.
func (d *Device) Limits() *vk.VkPhysicalDeviceLimits { return &d.gpu.props.Limits }

// MaxSamples is the highest sample count the device supports for colour
// and depth attachments together, one of 1, 2, 4, 8, 16, 32 or 64. It is
// never below 1: every device supports single-sample rendering.
func (d *Device) MaxSamples() int {
	limits := d.Limits()
	counts := limits.FramebufferColorSampleCounts & limits.FramebufferDepthSampleCounts & limits.FramebufferStencilSampleCounts
	best := 1
	for n := 2; n <= 64; n *= 2 {
		if counts&vk.VkSampleCountFlags(n) != 0 {
			best = n
		}
	}
	return best
}

// SampleCount turns a requested sample count into the flag bit for it,
// rounded down to a power of two the device supports. Zero and one both
// mean no multisampling.
func (d *Device) SampleCount(n int) vk.VkSampleCountFlagBits {
	n = min(n, d.MaxSamples())
	count := 1
	for c := 2; c <= n; c *= 2 {
		count = c
	}
	return vk.VkSampleCountFlagBits(count)
}

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
