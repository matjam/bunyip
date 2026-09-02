package render

import (
	"errors"
	"fmt"
	"image"

	"github.com/matjam/bunyip/internal/vk"
)

// ErrDeviceLost reports that the GPU was reset; the renderer and every
// resource created from it must be rebuilt.
var ErrDeviceLost = errors.New("render: device lost")

// FramesInFlight is how many frames the CPU may record ahead of the GPU.
// Per-frame resources such as instance buffers need one copy each.
const FramesInFlight = 2

// Renderer ties an instance, device and swapchain together with a ring of
// in-flight frames. A frame is BeginFrame, record, EndFrame.
type Renderer struct {
	Instance  *Instance
	Device    *Device
	Swapchain *Swapchain
	surface   vk.VkSurfaceKHR
	frames    [FramesInFlight]frame
	current   int
	extent    vk.VkExtent2D // requested size, used when the surface defers to us
	resize    bool
	inFrame   bool
	inPass    bool
	onResize  func(vk.VkExtent2D) error
	readback  *Buffer
	depth     *Image
	// DepthFormat is the format of the depth attachment every frame carries;
	// pipelines must declare it.
	DepthFormat vk.VkFormat
}

type frame struct {
	cb             vk.VkCommandBuffer
	fence          vk.VkFence
	imageAvailable vk.VkSemaphore
}

// Frame is what a caller records into between BeginFrame and EndFrame.
type Frame struct {
	CB         vk.VkCommandBuffer
	ImageIndex uint32
	Slot       int // which of the FramesInFlight per-frame resources to use
	Extent     vk.VkExtent2D
}

// SurfaceFunc creates the presentation surface once the instance exists.
type SurfaceFunc func(instance vk.VkInstance) (vk.VkSurfaceKHR, error)

// NewRenderer brings the whole stack up: instance, surface, device, swapchain
// and frame ring. extent is used only when the surface has no fixed size.
func NewRenderer(cfg Config, surfaceExts []string, makeSurface SurfaceFunc, extent vk.VkExtent2D, vsync bool) (*Renderer, error) {
	inst, err := NewInstance(cfg, surfaceExts)
	if err != nil {
		return nil, err
	}
	r := &Renderer{Instance: inst, extent: extent}
	if r.surface, err = makeSurface(inst.Handle); err != nil {
		inst.Destroy()
		return nil, err
	}
	if r.Device, err = NewDevice(inst, r.surface); err != nil {
		r.Destroy()
		return nil, err
	}
	if r.Swapchain, err = r.Device.NewSwapchain(r.surface, extent, vsync); err != nil {
		r.Destroy()
		return nil, err
	}
	if err := r.createDepth(); err != nil {
		r.Destroy()
		return nil, err
	}
	if err := r.initFrames(); err != nil {
		r.Destroy()
		return nil, err
	}
	return r, nil
}

func (r *Renderer) initFrames() error {
	cbs, err := r.Device.allocateCommandBuffers(FramesInFlight)
	if err != nil {
		return err
	}
	for i := range r.frames {
		f := &r.frames[i]
		f.cb = cbs[i]
		if f.fence, err = r.Device.newFence(true); err != nil {
			return err
		}
		if f.imageAvailable, err = r.Device.newSemaphore(); err != nil {
			return err
		}
	}
	return nil
}

// Resize records a new framebuffer size; the swapchain is rebuilt on the
// next BeginFrame.
func (r *Renderer) Resize(width, height int) {
	r.extent = vk.VkExtent2D{Width: uint32(width), Height: uint32(height)}
	r.resize = true
}

// BeginFrame waits for the frame slot, acquires a swapchain image and
// starts the command buffer. It returns ok=false, with no error, when the
// swapchain had to be rebuilt and the caller should try again next loop.
// The caller then records any offscreen passes and calls BeginSwapchainPass.
func (r *Renderer) BeginFrame() (*Frame, bool, error) {
	if r.inFrame {
		return nil, false, fmt.Errorf("render: BeginFrame called twice")
	}
	if r.resize {
		r.resize = false
		if err := r.Swapchain.Recreate(r.extent); err != nil {
			return nil, false, err
		}
		if err := r.createDepth(); err != nil {
			return nil, false, err
		}
		if r.onResize != nil {
			if err := r.onResize(r.Swapchain.Extent); err != nil {
				return nil, false, err
			}
		}
	}
	d := r.Device
	f := &r.frames[r.current]
	if err := vk.Check("vkWaitForFences", vk.VkWaitForFences(d.Handle, 1, &f.fence, vk.VK_TRUE, ^uint64(0))); err != nil {
		return nil, false, deviceLostOr(err)
	}
	var index uint32
	res := vk.VkAcquireNextImageKHR(d.Handle, r.Swapchain.Handle, ^uint64(0), f.imageAvailable, 0, &index)
	if res == vk.VK_ERROR_OUT_OF_DATE_KHR {
		r.resize = true
		return nil, false, nil
	}
	if res != vk.VK_SUBOPTIMAL_KHR {
		if err := vk.Check("vkAcquireNextImageKHR", res); err != nil {
			return nil, false, err
		}
	}
	if err := vk.Check("vkResetFences", vk.VkResetFences(d.Handle, 1, &f.fence)); err != nil {
		return nil, false, err
	}
	begin := vk.VkCommandBufferBeginInfo{SType: vk.VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO, Flags: vk.VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT}
	if err := vk.Check("vkBeginCommandBuffer", vk.VkBeginCommandBuffer(f.cb, &begin)); err != nil {
		return nil, false, err
	}
	r.inFrame = true
	r.inPass = false
	return &Frame{CB: f.cb, ImageIndex: index, Slot: r.current, Extent: r.Swapchain.Extent}, true, nil
}

// OnResize registers a callback run after the swapchain is rebuilt, for
// offscreen targets that must match its size.
func (r *Renderer) OnResize(fn func(extent vk.VkExtent2D) error) { r.onResize = fn }

// BeginSwapchainPass opens the final pass into the acquired swapchain image,
// with the frame's depth attachment, cleared to clear.
func (r *Renderer) BeginSwapchainPass(fr *Frame, clear [4]float32) {
	f := &r.frames[r.current]
	img := r.Swapchain.Images[fr.ImageIndex]
	imageBarrier(f.cb, img, vk.VK_IMAGE_ASPECT_COLOR_BIT,
		vk.VK_IMAGE_LAYOUT_UNDEFINED, vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
		vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, 0,
		vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT, vk.VK_ACCESS_2_COLOR_ATTACHMENT_WRITE_BIT)
	imageBarrier(f.cb, r.depth.Handle, depthAspect(r.DepthFormat),
		vk.VK_IMAGE_LAYOUT_UNDEFINED, depthLayout(r.DepthFormat),
		vk.VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT|vk.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT, 0,
		vk.VK_PIPELINE_STAGE_2_EARLY_FRAGMENT_TESTS_BIT|vk.VK_PIPELINE_STAGE_2_LATE_FRAGMENT_TESTS_BIT,
		vk.VK_ACCESS_2_DEPTH_STENCIL_ATTACHMENT_WRITE_BIT)
	var clearValue vk.VkClearValue
	*clearValue.Color().Float32() = clear
	var depthClear vk.VkClearValue
	*depthClear.DepthStencil() = vk.VkClearDepthStencilValue{Depth: 1}
	depthAttachment := vk.VkRenderingAttachmentInfo{
		SType:       vk.VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO,
		ImageView:   r.depth.AttachView,
		ImageLayout: depthLayout(r.DepthFormat),
		LoadOp:      vk.VK_ATTACHMENT_LOAD_OP_CLEAR,
		StoreOp:     vk.VK_ATTACHMENT_STORE_OP_DONT_CARE,
		ClearValue:  depthClear,
	}
	stencilAttachment := depthAttachment
	color := vk.VkRenderingAttachmentInfo{
		SType:       vk.VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO,
		ImageView:   r.Swapchain.Views[fr.ImageIndex],
		ImageLayout: vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
		LoadOp:      vk.VK_ATTACHMENT_LOAD_OP_CLEAR,
		StoreOp:     vk.VK_ATTACHMENT_STORE_OP_STORE,
		ClearValue:  clearValue,
	}
	rendering := vk.VkRenderingInfo{
		SType:                vk.VK_STRUCTURE_TYPE_RENDERING_INFO,
		RenderArea:           vk.VkRect2D{Extent: r.Swapchain.Extent},
		LayerCount:           1,
		ColorAttachmentCount: 1,
		PColorAttachments:    &color,
		PDepthAttachment:     &depthAttachment,
	}
	if HasStencil(r.DepthFormat) {
		rendering.PStencilAttachment = &stencilAttachment
	}
	vk.VkCmdBeginRendering(f.cb, &rendering)
	SetViewport(f.cb, r.Swapchain.Extent)
	r.inPass = true
}

// EndFrame closes the render pass, submits and presents. With capture set it
// also copies the finished image to host memory and returns it, which costs
// a full GPU sync and is meant for screenshots and tests.
func (r *Renderer) EndFrame(fr *Frame, capture bool) (*image.RGBA, error) {
	if !r.inFrame {
		return nil, fmt.Errorf("render: EndFrame without BeginFrame")
	}
	r.inFrame = false
	d := r.Device
	f := &r.frames[r.current]
	img := r.Swapchain.Images[fr.ImageIndex]
	if !r.inPass {
		r.BeginSwapchainPass(fr, [4]float32{0, 0, 0, 1})
	}
	r.inPass = false
	vk.VkCmdEndRendering(f.cb)
	var readback *Buffer
	if capture {
		var err error
		if readback, err = r.recordReadback(f.cb, img); err != nil {
			return nil, err
		}
	}
	srcLayout := vk.VkImageLayout(vk.VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL)
	if capture {
		srcLayout = vk.VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL
	}
	imageBarrier(f.cb, img, vk.VK_IMAGE_ASPECT_COLOR_BIT,
		srcLayout, vk.VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
		vk.VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT, vk.VK_ACCESS_2_MEMORY_WRITE_BIT,
		vk.VK_PIPELINE_STAGE_2_BOTTOM_OF_PIPE_BIT, 0)
	if err := vk.Check("vkEndCommandBuffer", vk.VkEndCommandBuffer(f.cb)); err != nil {
		return nil, err
	}
	wait := vk.VkSemaphoreSubmitInfo{SType: vk.VK_STRUCTURE_TYPE_SEMAPHORE_SUBMIT_INFO, Semaphore: f.imageAvailable, StageMask: vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT}
	signal := vk.VkSemaphoreSubmitInfo{SType: vk.VK_STRUCTURE_TYPE_SEMAPHORE_SUBMIT_INFO, Semaphore: r.Swapchain.renderDone[fr.ImageIndex], StageMask: vk.VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT}
	cbInfo := vk.VkCommandBufferSubmitInfo{SType: vk.VK_STRUCTURE_TYPE_COMMAND_BUFFER_SUBMIT_INFO, CommandBuffer: f.cb}
	submit := vk.VkSubmitInfo2{
		SType:                    vk.VK_STRUCTURE_TYPE_SUBMIT_INFO_2,
		WaitSemaphoreInfoCount:   1,
		PWaitSemaphoreInfos:      &wait,
		CommandBufferInfoCount:   1,
		PCommandBufferInfos:      &cbInfo,
		SignalSemaphoreInfoCount: 1,
		PSignalSemaphoreInfos:    &signal,
	}
	if err := vk.Check("vkQueueSubmit2", vk.VkQueueSubmit2(d.Queue, 1, &submit, f.fence)); err != nil {
		return nil, deviceLostOr(err)
	}
	present := vk.VkPresentInfoKHR{
		SType:              vk.VK_STRUCTURE_TYPE_PRESENT_INFO_KHR,
		WaitSemaphoreCount: 1,
		PWaitSemaphores:    &r.Swapchain.renderDone[fr.ImageIndex],
		SwapchainCount:     1,
		PSwapchains:        &r.Swapchain.Handle,
		PImageIndices:      &fr.ImageIndex,
	}
	res := vk.VkQueuePresentKHR(d.Queue, &present)
	switch res {
	case vk.VK_ERROR_OUT_OF_DATE_KHR, vk.VK_SUBOPTIMAL_KHR:
		r.resize = true
	default:
		if err := vk.Check("vkQueuePresentKHR", res); err != nil {
			return nil, deviceLostOr(err)
		}
	}
	r.current = (r.current + 1) % FramesInFlight
	if !capture {
		return nil, nil
	}
	if err := vk.Check("vkWaitForFences", vk.VkWaitForFences(d.Handle, 1, &f.fence, vk.VK_TRUE, ^uint64(0))); err != nil {
		return nil, err
	}
	return r.decodeReadback(readback), nil
}

// Destroy tears everything down in reverse order.
func (r *Renderer) Destroy() {
	if r.Device != nil {
		_ = r.Device.WaitIdle()
		if r.readback != nil {
			r.readback.Destroy()
		}
		if r.depth != nil {
			r.depth.Destroy()
		}
		for _, f := range r.frames {
			if f.fence != 0 {
				vk.VkDestroyFence(r.Device.Handle, f.fence, nil)
			}
			if f.imageAvailable != 0 {
				vk.VkDestroySemaphore(r.Device.Handle, f.imageAvailable, nil)
			}
		}
		if r.Swapchain != nil {
			r.Swapchain.Destroy()
		}
		r.Device.Destroy()
	}
	if r.surface != 0 {
		vk.VkDestroySurfaceKHR(r.Instance.Handle, r.surface, nil)
	}
	r.Instance.Destroy()
}

// deviceLostOr maps a VK_ERROR_DEVICE_LOST result onto ErrDeviceLost,
// keeping the original error as the cause.
func deviceLostOr(err error) error {
	var ve *vk.Error
	if errors.As(err, &ve) && ve.Result == vk.VK_ERROR_DEVICE_LOST {
		return fmt.Errorf("%w: %w", ErrDeviceLost, err)
	}
	return err
}
