package render

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/matjam/bunyip/internal/vk"
)

// The device's one queue belongs to a goroutine of its own, locked to an
// OS thread. MoltenVK encodes a whole Metal frame inside vkQueueSubmit2,
// which costs the calling thread up to a millisecond a frame, so the
// renderer hands a finished command buffer to that goroutine and goes on
// to the next frame while it is submitted and presented.
//
// The rules that keep this correct:
//
//   - Device.submit is the only way work reaches the queue. It posts a
//     submission, and a present when one is asked for, to the goroutine,
//     which runs them in the order they were posted. vkQueueSubmit2 and
//     vkQueuePresentKHR are called nowhere else; TestQueueCallsStayInSubmit
//     checks the package source for that.
//   - The goroutine holds submitter.mu for each job. Anything else that
//     Vulkan requires to be externally synchronised with the queue or the
//     swapchain (vkQueueWaitIdle, vkDeviceWaitIdle, vkAcquireNextImageKHR,
//     rebuilding the swapchain) drains the goroutine first and runs under
//     the same mutex.
//   - A fence handed to a job is the goroutine's until the job has run:
//     vkQueueSubmit2 requires it to be externally synchronised, so no
//     other thread waits on it, resets it or even reads its status before
//     then. await, drain and hasRun give that order.
//   - Command buffers are recorded on the caller's goroutine. A frame's
//     command buffer is reset only after its fence has signalled, which
//     WaitFrame checks, so the goroutine never reads a buffer being
//     recorded.
//   - An error from a job is kept, and every job after it is skipped. The
//     next call that waits (WaitFrame, EndFrame with capture, OneShot,
//     WaitIdle) returns it, which is how a lost device reaches the loop.

// submitJob is one piece of queue work: a command buffer and what it waits
// on and signals, then an optional present. It is passed by value, so
// posting one allocates nothing.
type submitJob struct {
	seq       uint64
	cb        vk.VkCommandBuffer // zero submits nothing (a job that only orders)
	fence     vk.VkFence         // signalled when the command buffer finishes, or zero
	wait      vk.VkSemaphore     // waited on at the colour output stage, or zero
	signal    vk.VkSemaphore     // signalled when the command buffer finishes, or zero
	swapchain vk.VkSwapchainKHR  // presented after the submit when nonzero
	image     uint32             // the swapchain image to present
}

// submitter owns a device's queue: the goroutine that submits and presents,
// and the bookkeeping the caller uses to wait for it.
type submitter struct {
	queue vk.VkQueue
	jobs  chan submitJob
	// mu is held around every call that touches the queue or a swapchain:
	// by the goroutine for each job, and by the device's owner for the
	// calls it makes itself after draining.
	mu sync.Mutex

	state  sync.Mutex
	ran    sync.Cond // signalled each time a job finishes
	done   uint64    // the last job finished, under state
	err    error     // the first job that failed, under state
	posted uint64    // the last job posted; only the posting goroutine reads it
	closed bool      // stop has run; only the posting goroutine reads it
	exited chan struct{}

	// outOfDate is set when a present reports the swapchain out of date
	// or suboptimal; the renderer takes it at the next BeginFrame.
	outOfDate atomic.Bool

	// The raw entry points, called through purego.SyscallN with the
	// goroutine's own argument slice. The vk package's allocation-free
	// wrappers share one argument slice, which is only safe on the
	// goroutine that records, so the submit goroutine does not use them.
	submitAddr, presentAddr uintptr

	// Only the goroutine touches these. They are what the Vulkan calls
	// take by pointer, kept here so a job allocates nothing.
	args    []uintptr
	cbInfo  vk.VkCommandBufferSubmitInfo
	waitSem vk.VkSemaphoreSubmitInfo
	sigSem  vk.VkSemaphoreSubmitInfo
	info    vk.VkSubmitInfo2
	present vk.VkPresentInfoKHR
	image   uint32
	chain   vk.VkSwapchainKHR
}

// errSubmitterClosed is returned for work posted after the device began
// tearing down.
var errSubmitterClosed = errors.New("render: queue closed")

// newSubmitter starts the goroutine that owns queue. The entry points are
// resolved through the instance, the same dispatch the vk package binds.
func newSubmitter(instance vk.VkInstance, queue vk.VkQueue) *submitter {
	q := &submitter{
		queue:  queue,
		jobs:   make(chan submitJob, 16),
		exited: make(chan struct{}),
		args:   make([]uintptr, 4),
	}
	q.ran.L = &q.state
	if vk.VkGetInstanceProcAddr != nil {
		q.submitAddr = uintptr(vk.VkGetInstanceProcAddr(instance, "vkQueueSubmit2"))
		q.presentAddr = uintptr(vk.VkGetInstanceProcAddr(instance, "vkQueuePresentKHR"))
	}
	go q.run()
	return q
}

// post hands a job to the goroutine and returns its number. It blocks only
// when the goroutine is many jobs behind.
func (q *submitter) post(job submitJob) uint64 {
	if q.closed {
		q.state.Lock()
		if q.err == nil {
			q.err = errSubmitterClosed
		}
		q.state.Unlock()
		return q.posted
	}
	q.posted++
	job.seq = q.posted
	q.jobs <- job
	return job.seq
}

// await blocks until job seq, and every job before it, has run.
func (q *submitter) await(seq uint64) {
	q.state.Lock()
	for q.done < seq {
		q.ran.Wait()
	}
	q.state.Unlock()
}

// hasRun reports whether job seq has run. A fence handed to a job is
// externally synchronised until then, so nothing else may even read its
// status before this reports true.
func (q *submitter) hasRun(seq uint64) bool {
	q.state.Lock()
	defer q.state.Unlock()
	return q.done >= seq
}

// drain blocks until every job posted so far has run, which leaves the
// goroutine idle until the next post.
func (q *submitter) drain() { q.await(q.posted) }

// failed returns the error of the first job that failed, or nil.
func (q *submitter) failed() error {
	q.state.Lock()
	defer q.state.Unlock()
	return q.err
}

// takeOutOfDate reports whether a present since the last call found the
// swapchain out of date or suboptimal.
func (q *submitter) takeOutOfDate() bool { return q.outOfDate.Swap(false) }

// stop runs what is still posted and ends the goroutine.
func (q *submitter) stop() {
	if q.closed {
		return
	}
	q.closed = true
	close(q.jobs)
	<-q.exited
}

// run is the goroutine. It stays on one OS thread for its whole life, so
// the driver sees every submission and present from the same thread.
func (q *submitter) run() {
	runtime.LockOSThread()
	defer close(q.exited)
	for job := range q.jobs {
		q.state.Lock()
		failed := q.err != nil
		q.state.Unlock()
		var err error
		if !failed {
			q.mu.Lock()
			err = q.execute(&job)
			q.mu.Unlock()
		}
		q.state.Lock()
		if err != nil && q.err == nil {
			q.err = err
		}
		q.done = job.seq
		q.ran.Broadcast()
		q.state.Unlock()
	}
}

// execute submits a job's command buffer and presents its image.
func (q *submitter) execute(job *submitJob) error {
	q.info = vk.VkSubmitInfo2{SType: vk.VK_STRUCTURE_TYPE_SUBMIT_INFO_2}
	if job.cb != 0 {
		q.cbInfo = vk.VkCommandBufferSubmitInfo{SType: vk.VK_STRUCTURE_TYPE_COMMAND_BUFFER_SUBMIT_INFO, CommandBuffer: job.cb}
		q.info.CommandBufferInfoCount = 1
		q.info.PCommandBufferInfos = &q.cbInfo
	}
	if job.wait != 0 {
		q.waitSem = vk.VkSemaphoreSubmitInfo{SType: vk.VK_STRUCTURE_TYPE_SEMAPHORE_SUBMIT_INFO, Semaphore: job.wait, StageMask: vk.VK_PIPELINE_STAGE_2_COLOR_ATTACHMENT_OUTPUT_BIT}
		q.info.WaitSemaphoreInfoCount = 1
		q.info.PWaitSemaphoreInfos = &q.waitSem
	}
	if job.signal != 0 {
		q.sigSem = vk.VkSemaphoreSubmitInfo{SType: vk.VK_STRUCTURE_TYPE_SEMAPHORE_SUBMIT_INFO, Semaphore: job.signal, StageMask: vk.VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT}
		q.info.SignalSemaphoreInfoCount = 1
		q.info.PSignalSemaphoreInfos = &q.sigSem
	}
	if job.cb != 0 || job.fence != 0 || job.wait != 0 || job.signal != 0 {
		if err := vk.Check("vkQueueSubmit2", q.queueSubmit2(job.fence)); err != nil {
			return err
		}
	}
	if job.swapchain == 0 {
		return nil
	}
	q.chain, q.image = job.swapchain, job.image
	q.present = vk.VkPresentInfoKHR{
		SType:          vk.VK_STRUCTURE_TYPE_PRESENT_INFO_KHR,
		SwapchainCount: 1,
		PSwapchains:    &q.chain,
		PImageIndices:  &q.image,
	}
	if job.signal != 0 {
		q.present.WaitSemaphoreCount = 1
		q.present.PWaitSemaphores = &q.sigSem.Semaphore
	}
	switch res := q.queuePresent(); res {
	case vk.VK_ERROR_OUT_OF_DATE_KHR, vk.VK_SUBOPTIMAL_KHR:
		q.outOfDate.Store(true)
	default:
		if err := vk.Check("vkQueuePresentKHR", res); err != nil {
			return err
		}
	}
	return nil
}

// queueSubmit2 submits q.info.
func (q *submitter) queueSubmit2(fence vk.VkFence) vk.VkResult {
	if q.submitAddr == 0 {
		return vk.VkQueueSubmit2(q.queue, 1, &q.info, fence)
	}
	a := q.args[:4]
	a[0], a[1], a[2], a[3] = uintptr(q.queue), 1, uintptr(unsafe.Pointer(&q.info)), uintptr(fence)
	r1, _, _ := purego.SyscallN(q.submitAddr, a...)
	return vk.VkResult(int32(uint32(r1)))
}

// queuePresent presents q.present.
func (q *submitter) queuePresent() vk.VkResult {
	if q.presentAddr == 0 {
		return vk.VkQueuePresentKHR(q.queue, &q.present)
	}
	a := q.args[:2]
	a[0], a[1] = uintptr(q.queue), uintptr(unsafe.Pointer(&q.present))
	r1, _, _ := purego.SyscallN(q.presentAddr, a...)
	return vk.VkResult(int32(uint32(r1)))
}

// submit is the one way work reaches the device's queue. It posts the job
// to the submit goroutine and returns its number without waiting; the
// caller awaits the number before waiting on the job's fence or reading
// what it wrote. Jobs run in the order they were posted.
func (d *Device) submit(job submitJob) uint64 {
	d.submitted()
	return d.q.post(job)
}

// fenceSignalled reports, without waiting, whether the fence that job seq
// submitted has signalled. Until the job has run the fence belongs to the
// submit goroutine, and it reports false without touching it.
func (d *Device) fenceSignalled(seq uint64, fence vk.VkFence) bool {
	return d.q.hasRun(seq) && vk.VkGetFenceStatus(d.Handle, fence) == vk.VK_SUCCESS
}

// waitFence waits for a fence whose submission was posted through submit:
// it drains the submit goroutine first, so the fence is on the queue, and
// returns the goroutine's error instead of waiting when a submission
// failed, since the fence would then never signal.
func (d *Device) waitFence(fence vk.VkFence) error {
	d.q.drain()
	if err := d.q.failed(); err != nil {
		return err
	}
	return vk.Check("vkWaitForFences", vk.WaitForFences(d.Handle, 1, &fence, vk.VK_TRUE, ^uint64(0)))
}
