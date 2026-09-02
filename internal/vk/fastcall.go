package vk

import (
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

// The commands a frame records once per draw run go through purego's
// reflect-based wrappers, which allocate an argument frame on every call.
// This file binds the raw entry points of those commands beside the
// generated function variables and calls them through purego.SyscallN,
// which allocates nothing once its arguments come from memory that already
// lives on the heap.
//
// Pointer arguments must refer to memory that outlives the call: a field of
// a long-lived struct, an element of a slice, or a package variable. The
// commands here copy what a pointer refers to as they record, so filling one
// buffer in place before each call is enough.
var (
	pfnAcquireNextImageKHR   uintptr
	pfnBeginCommandBuffer    uintptr
	pfnCmdBeginRendering     uintptr
	pfnCmdEndRendering       uintptr
	pfnEndCommandBuffer      uintptr
	pfnQueuePresentKHR       uintptr
	pfnQueueSubmit2          uintptr
	pfnResetFences           uintptr
	pfnWaitForFences         uintptr
	pfnCmdBindDescriptorSets uintptr
	pfnCmdBindIndexBuffer    uintptr
	pfnCmdBindPipeline       uintptr
	pfnCmdBindVertexBuffers  uintptr
	pfnCmdDraw               uintptr
	pfnCmdDrawIndexed        uintptr
	pfnCmdPipelineBarrier2   uintptr
	pfnCmdPushConstants      uintptr
	pfnCmdSetScissor         uintptr
	pfnCmdSetViewport        uintptr
)

// fastAddrs maps a generated command variable to the raw address slot that
// bind fills in beside it. A command the driver does not expose leaves its
// slot zero and the wrapper falls back to the function variable.
var fastAddrs = map[any]*uintptr{
	&VkAcquireNextImageKHR:   &pfnAcquireNextImageKHR,
	&VkBeginCommandBuffer:    &pfnBeginCommandBuffer,
	&VkCmdBeginRendering:     &pfnCmdBeginRendering,
	&VkCmdEndRendering:       &pfnCmdEndRendering,
	&VkEndCommandBuffer:      &pfnEndCommandBuffer,
	&VkQueuePresentKHR:       &pfnQueuePresentKHR,
	&VkQueueSubmit2:          &pfnQueueSubmit2,
	&VkResetFences:           &pfnResetFences,
	&VkWaitForFences:         &pfnWaitForFences,
	&VkCmdBindDescriptorSets: &pfnCmdBindDescriptorSets,
	&VkCmdBindIndexBuffer:    &pfnCmdBindIndexBuffer,
	&VkCmdBindPipeline:       &pfnCmdBindPipeline,
	&VkCmdBindVertexBuffers:  &pfnCmdBindVertexBuffers,
	&VkCmdDraw:               &pfnCmdDraw,
	&VkCmdDrawIndexed:        &pfnCmdDrawIndexed,
	&VkCmdPipelineBarrier2:   &pfnCmdPipelineBarrier2,
	&VkCmdPushConstants:      &pfnCmdPushConstants,
	&VkCmdSetScissor:         &pfnCmdSetScissor,
	&VkCmdSetViewport:        &pfnCmdSetViewport,
}

// callArgs is the argument slice for the calls below. Spreading a slice
// that is already on the heap is what keeps the variadic call from
// allocating an argument array per call, so the slice is made once and
// refilled. Command buffers are recorded from the goroutine that owns the
// device, one call at a time.
var callArgs = make([]uintptr, 8)

// WaitForFences waits on fences. It is VkWaitForFences without the
// allocating call wrapper. pFences must point into memory that outlives
// the call.
func WaitForFences(device VkDevice, count uint32, pFences *VkFence, waitAll VkBool32, timeout uint64) VkResult {
	if pfnWaitForFences == 0 {
		return VkWaitForFences(device, count, pFences, waitAll, timeout)
	}
	a := callArgs[:5]
	a[0], a[1], a[2] = uintptr(device), uintptr(count), uintptr(unsafe.Pointer(pFences))
	a[3], a[4] = uintptr(waitAll), uintptr(timeout)
	r1, _, _ := purego.SyscallN(pfnWaitForFences, a...)
	runtime.KeepAlive(pFences)
	return VkResult(int32(uint32(r1)))
}

// ResetFences resets fences. It is VkResetFences without the allocating
// call wrapper. pFences must point into memory that outlives the call.
func ResetFences(device VkDevice, count uint32, pFences *VkFence) VkResult {
	if pfnResetFences == 0 {
		return VkResetFences(device, count, pFences)
	}
	a := callArgs[:3]
	a[0], a[1], a[2] = uintptr(device), uintptr(count), uintptr(unsafe.Pointer(pFences))
	r1, _, _ := purego.SyscallN(pfnResetFences, a...)
	runtime.KeepAlive(pFences)
	return VkResult(int32(uint32(r1)))
}

// AcquireNextImageKHR acquires a swapchain image. It is
// VkAcquireNextImageKHR without the allocating call wrapper. pImageIndex
// must point into memory that outlives the call.
func AcquireNextImageKHR(device VkDevice, swapchain VkSwapchainKHR, timeout uint64, semaphore VkSemaphore, fence VkFence, pImageIndex *uint32) VkResult {
	if pfnAcquireNextImageKHR == 0 {
		return VkAcquireNextImageKHR(device, swapchain, timeout, semaphore, fence, pImageIndex)
	}
	a := callArgs[:6]
	a[0], a[1], a[2] = uintptr(device), uintptr(swapchain), uintptr(timeout)
	a[3], a[4], a[5] = uintptr(semaphore), uintptr(fence), uintptr(unsafe.Pointer(pImageIndex))
	r1, _, _ := purego.SyscallN(pfnAcquireNextImageKHR, a...)
	runtime.KeepAlive(pImageIndex)
	return VkResult(int32(uint32(r1)))
}

// BeginCommandBuffer starts recording. It is VkBeginCommandBuffer without
// the allocating call wrapper. info must point into memory that outlives
// the call.
func BeginCommandBuffer(cb VkCommandBuffer, info *VkCommandBufferBeginInfo) VkResult {
	if pfnBeginCommandBuffer == 0 {
		return VkBeginCommandBuffer(cb, info)
	}
	a := callArgs[:2]
	a[0], a[1] = uintptr(cb), uintptr(unsafe.Pointer(info))
	r1, _, _ := purego.SyscallN(pfnBeginCommandBuffer, a...)
	runtime.KeepAlive(info)
	return VkResult(int32(uint32(r1)))
}

// EndCommandBuffer finishes recording. It is VkEndCommandBuffer without
// the allocating call wrapper.
func EndCommandBuffer(cb VkCommandBuffer) VkResult {
	if pfnEndCommandBuffer == 0 {
		return VkEndCommandBuffer(cb)
	}
	a := callArgs[:1]
	a[0] = uintptr(cb)
	r1, _, _ := purego.SyscallN(pfnEndCommandBuffer, a...)
	return VkResult(int32(uint32(r1)))
}

// CmdBeginRendering starts a dynamic rendering pass. It is
// VkCmdBeginRendering without the allocating call wrapper. info, and
// everything it points to, must outlive the call.
func CmdBeginRendering(cb VkCommandBuffer, info *VkRenderingInfo) {
	if pfnCmdBeginRendering == 0 {
		VkCmdBeginRendering(cb, info)
		return
	}
	a := callArgs[:2]
	a[0], a[1] = uintptr(cb), uintptr(unsafe.Pointer(info))
	purego.SyscallN(pfnCmdBeginRendering, a...)
	runtime.KeepAlive(info)
}

// CmdEndRendering ends a dynamic rendering pass. It is VkCmdEndRendering
// without the allocating call wrapper.
func CmdEndRendering(cb VkCommandBuffer) {
	if pfnCmdEndRendering == 0 {
		VkCmdEndRendering(cb)
		return
	}
	a := callArgs[:1]
	a[0] = uintptr(cb)
	purego.SyscallN(pfnCmdEndRendering, a...)
}

// QueueSubmit2 submits work. It is VkQueueSubmit2 without the allocating
// call wrapper. pSubmits, and everything it points to, must outlive the
// call.
func QueueSubmit2(queue VkQueue, count uint32, pSubmits *VkSubmitInfo2, fence VkFence) VkResult {
	if pfnQueueSubmit2 == 0 {
		return VkQueueSubmit2(queue, count, pSubmits, fence)
	}
	a := callArgs[:4]
	a[0], a[1] = uintptr(queue), uintptr(count)
	a[2], a[3] = uintptr(unsafe.Pointer(pSubmits)), uintptr(fence)
	r1, _, _ := purego.SyscallN(pfnQueueSubmit2, a...)
	runtime.KeepAlive(pSubmits)
	return VkResult(int32(uint32(r1)))
}

// QueuePresentKHR presents a swapchain image. It is VkQueuePresentKHR
// without the allocating call wrapper. info, and everything it points to,
// must outlive the call.
func QueuePresentKHR(queue VkQueue, info *VkPresentInfoKHR) VkResult {
	if pfnQueuePresentKHR == 0 {
		return VkQueuePresentKHR(queue, info)
	}
	a := callArgs[:2]
	a[0], a[1] = uintptr(queue), uintptr(unsafe.Pointer(info))
	r1, _, _ := purego.SyscallN(pfnQueuePresentKHR, a...)
	runtime.KeepAlive(info)
	return VkResult(int32(uint32(r1)))
}

// CmdDraw records a non-indexed draw. It is VkCmdDraw without the
// allocating call wrapper.
func CmdDraw(cb VkCommandBuffer, vertexCount, instanceCount, firstVertex, firstInstance uint32) {
	if pfnCmdDraw == 0 {
		VkCmdDraw(cb, vertexCount, instanceCount, firstVertex, firstInstance)
		return
	}
	a := callArgs[:5]
	a[0], a[1], a[2] = uintptr(cb), uintptr(vertexCount), uintptr(instanceCount)
	a[3], a[4] = uintptr(firstVertex), uintptr(firstInstance)
	purego.SyscallN(pfnCmdDraw, a...)
}

// CmdDrawIndexed records an indexed draw. It is VkCmdDrawIndexed without
// the allocating call wrapper.
func CmdDrawIndexed(cb VkCommandBuffer, indexCount, instanceCount, firstIndex uint32, vertexOffset int32, firstInstance uint32) {
	if pfnCmdDrawIndexed == 0 {
		VkCmdDrawIndexed(cb, indexCount, instanceCount, firstIndex, vertexOffset, firstInstance)
		return
	}
	a := callArgs[:6]
	a[0], a[1], a[2] = uintptr(cb), uintptr(indexCount), uintptr(instanceCount)
	a[3], a[4], a[5] = uintptr(firstIndex), uintptr(uint32(vertexOffset)), uintptr(firstInstance)
	purego.SyscallN(pfnCmdDrawIndexed, a...)
}

// CmdBindPipeline binds a pipeline. It is VkCmdBindPipeline without the
// allocating call wrapper.
func CmdBindPipeline(cb VkCommandBuffer, bindPoint VkPipelineBindPoint, pipeline VkPipeline) {
	if pfnCmdBindPipeline == 0 {
		VkCmdBindPipeline(cb, bindPoint, pipeline)
		return
	}
	a := callArgs[:3]
	a[0], a[1], a[2] = uintptr(cb), uintptr(bindPoint), uintptr(pipeline)
	purego.SyscallN(pfnCmdBindPipeline, a...)
}

// CmdBindDescriptorSets binds descriptor sets. It is
// VkCmdBindDescriptorSets without the allocating call wrapper. pSets and
// pDynamic must point into memory that outlives the call.
func CmdBindDescriptorSets(cb VkCommandBuffer, bindPoint VkPipelineBindPoint, layout VkPipelineLayout,
	firstSet, setCount uint32, pSets *VkDescriptorSet, dynamicCount uint32, pDynamic *uint32) {
	if pfnCmdBindDescriptorSets == 0 {
		VkCmdBindDescriptorSets(cb, bindPoint, layout, firstSet, setCount, pSets, dynamicCount, pDynamic)
		return
	}
	a := callArgs[:8]
	a[0], a[1], a[2] = uintptr(cb), uintptr(bindPoint), uintptr(layout)
	a[3], a[4], a[5] = uintptr(firstSet), uintptr(setCount), uintptr(unsafe.Pointer(pSets))
	a[6], a[7] = uintptr(dynamicCount), uintptr(unsafe.Pointer(pDynamic))
	purego.SyscallN(pfnCmdBindDescriptorSets, a...)
	runtime.KeepAlive(pSets)
	runtime.KeepAlive(pDynamic)
}

// CmdPushConstants writes push constants. It is VkCmdPushConstants without
// the allocating call wrapper. values must point into memory that outlives
// the call.
func CmdPushConstants(cb VkCommandBuffer, layout VkPipelineLayout, stages VkShaderStageFlags, offset, size uint32, values unsafe.Pointer) {
	if pfnCmdPushConstants == 0 {
		VkCmdPushConstants(cb, layout, stages, offset, size, values)
		return
	}
	a := callArgs[:6]
	a[0], a[1], a[2] = uintptr(cb), uintptr(layout), uintptr(stages)
	a[3], a[4], a[5] = uintptr(offset), uintptr(size), uintptr(values)
	purego.SyscallN(pfnCmdPushConstants, a...)
	runtime.KeepAlive(values)
}

// CmdBindVertexBuffers binds vertex buffers. It is VkCmdBindVertexBuffers
// without the allocating call wrapper. pBuffers and pOffsets must point
// into memory that outlives the call.
func CmdBindVertexBuffers(cb VkCommandBuffer, firstBinding, bindingCount uint32, pBuffers *VkBuffer, pOffsets *VkDeviceSize) {
	if pfnCmdBindVertexBuffers == 0 {
		VkCmdBindVertexBuffers(cb, firstBinding, bindingCount, pBuffers, pOffsets)
		return
	}
	a := callArgs[:5]
	a[0], a[1], a[2] = uintptr(cb), uintptr(firstBinding), uintptr(bindingCount)
	a[3], a[4] = uintptr(unsafe.Pointer(pBuffers)), uintptr(unsafe.Pointer(pOffsets))
	purego.SyscallN(pfnCmdBindVertexBuffers, a...)
	runtime.KeepAlive(pBuffers)
	runtime.KeepAlive(pOffsets)
}

// CmdBindIndexBuffer binds an index buffer. It is VkCmdBindIndexBuffer
// without the allocating call wrapper.
func CmdBindIndexBuffer(cb VkCommandBuffer, buffer VkBuffer, offset VkDeviceSize, indexType VkIndexType) {
	if pfnCmdBindIndexBuffer == 0 {
		VkCmdBindIndexBuffer(cb, buffer, offset, indexType)
		return
	}
	a := callArgs[:4]
	a[0], a[1] = uintptr(cb), uintptr(buffer)
	a[2], a[3] = uintptr(offset), uintptr(indexType)
	purego.SyscallN(pfnCmdBindIndexBuffer, a...)
}

// CmdPipelineBarrier2 records a dependency. It is VkCmdPipelineBarrier2
// without the allocating call wrapper. dep, and everything it points to,
// must live in memory that outlives the call.
func CmdPipelineBarrier2(cb VkCommandBuffer, dep *VkDependencyInfo) {
	if pfnCmdPipelineBarrier2 == 0 {
		VkCmdPipelineBarrier2(cb, dep)
		return
	}
	a := callArgs[:2]
	a[0], a[1] = uintptr(cb), uintptr(unsafe.Pointer(dep))
	purego.SyscallN(pfnCmdPipelineBarrier2, a...)
	runtime.KeepAlive(dep)
}

// CmdSetViewport sets viewports. It is VkCmdSetViewport without the
// allocating call wrapper. pViewports must point into memory that outlives
// the call.
func CmdSetViewport(cb VkCommandBuffer, first, count uint32, pViewports *VkViewport) {
	if pfnCmdSetViewport == 0 {
		VkCmdSetViewport(cb, first, count, pViewports)
		return
	}
	a := callArgs[:4]
	a[0], a[1] = uintptr(cb), uintptr(first)
	a[2], a[3] = uintptr(count), uintptr(unsafe.Pointer(pViewports))
	purego.SyscallN(pfnCmdSetViewport, a...)
	runtime.KeepAlive(pViewports)
}

// CmdSetScissor sets scissor rectangles. It is VkCmdSetScissor without the
// allocating call wrapper. pScissors must point into memory that outlives
// the call.
func CmdSetScissor(cb VkCommandBuffer, first, count uint32, pScissors *VkRect2D) {
	if pfnCmdSetScissor == 0 {
		VkCmdSetScissor(cb, first, count, pScissors)
		return
	}
	a := callArgs[:4]
	a[0], a[1] = uintptr(cb), uintptr(first)
	a[2], a[3] = uintptr(count), uintptr(unsafe.Pointer(pScissors))
	purego.SyscallN(pfnCmdSetScissor, a...)
	runtime.KeepAlive(pScissors)
}
