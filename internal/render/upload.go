package render

import (
	"errors"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// Uploads made outside a frame (NewTextureImage, NewDeviceLocalBuffer,
// NewCubemapImage, NewLevelledTextureImage, WriteImage) do not wait for
// the GPU. Each one copies its bytes into the staging of the device's
// open upload batch and records its copy into the batch's command
// buffer. FlushUploads submits the batch with a fence of its own, and
// the staging is freed once that fence has signalled. The renderer
// flushes before it submits a frame, and WaitIdle and OneShot flush
// before they wait, so a resource is on the GPU before anything that
// reads it runs: the device has one queue, the batch is submitted ahead
// of the work that uses it, and the batch ends in a barrier that makes
// its transfers visible to every later command on the queue.
const (
	// uploadBlock is the size of one staging block a batch bumps
	// through. An upload larger than half a block gets a buffer of its
	// own.
	uploadBlock = 4 << 20 // 4 MiB
	// uploadBatchBytes and uploadBatchCount bound one batch: the next
	// upload past either submits it first, so a long load keeps the GPU
	// busy while the processor goes on.
	uploadBatchBytes = 64 << 20 // 64 MiB
	uploadBatchCount = 1024
	// uploadInFlight is how much staging submitted batches may hold
	// before a flush waits for the oldest one, which bounds the host
	// memory a very large load pins.
	uploadInFlight = 256 << 20 // 256 MiB
)

// uploader is a device's batch of uploads recorded outside a frame.
type uploader struct {
	cb     vk.VkCommandBuffer // the open batch's command buffer, zero when none is open
	serial uint64             // the open batch's number, counting from one
	blocks []*Buffer          // staging the open batch copies from
	cur    *Buffer            // the block being filled, nil before the first
	used   vk.VkDeviceSize    // bytes used in cur
	bytes  vk.VkDeviceSize    // bytes staged in the open batch
	count  int                // uploads recorded in the open batch

	inflight      []uploadBatch // submitted, fences not yet seen signalled
	inflightBytes vk.VkDeviceSize
	fences        []vk.VkFence // unsignalled fences free for the next batch
	align         vk.VkDeviceSize
	region        vk.VkBufferCopy // scratch for a buffer copy, so recording one allocates nothing
}

// uploadBatch is one submitted batch and what it holds until its fence
// signals.
type uploadBatch struct {
	serial uint64
	fence  vk.VkFence
	cb     vk.VkCommandBuffer
	blocks []*Buffer
	bytes  vk.VkDeviceSize
}

// NoteUpload records that the open upload batch writes the image, so
// that Destroy first makes sure the batch has run rather than freeing an
// image a pending copy still targets. The upload helpers in this package
// call it; a caller recording into UploadCommands itself calls it too.
func (i *Image) NoteUpload() { i.upload = i.dev.up.serial }

// NoteUpload is Image.NoteUpload for a buffer an upload batch writes.
func (b *Buffer) NoteUpload() { b.upload = b.dev.up.serial }

// settleUpload makes sure the batch numbered serial has finished before
// something it writes is destroyed: an open batch is submitted, and a
// submitted one still running is waited for. A batch already seen to
// finish costs nothing, which is the usual case, because WaitIdle and
// the frame fences have long covered it.
func (d *Device) settleUpload(serial uint64) {
	u := &d.up
	if serial == 0 {
		return
	}
	if u.cb != 0 && serial == u.serial {
		_ = d.FlushUploads()
	}
	for _, b := range u.inflight {
		if b.serial != serial {
			continue
		}
		if vk.VkGetFenceStatus(d.Handle, b.fence) != vk.VK_SUCCESS {
			d.waits++
			_ = vk.WaitForFences(d.Handle, 1, &b.fence, vk.VK_TRUE, ^uint64(0))
		}
		d.reclaimUploads()
		return
	}
}

// StageUpload reserves size bytes of host-visible staging in the open
// upload batch and returns the buffer, the offset within it and the
// mapped bytes to fill. Record the copy from that buffer and offset into
// the command buffer UploadCommands returns, before the next call that
// can flush: another StageUpload, FlushUploads, WaitIdle or OneShot. The
// offset is a multiple of 16 and of the device's preferred copy
// alignment.
func (d *Device) StageUpload(size vk.VkDeviceSize) (*Buffer, vk.VkDeviceSize, []byte, error) {
	u := &d.up
	if u.cb != 0 && u.count > 0 && (u.bytes+size > uploadBatchBytes || u.count >= uploadBatchCount) {
		if err := d.FlushUploads(); err != nil {
			return nil, 0, nil, err
		}
	}
	if err := d.beginUploads(); err != nil {
		return nil, 0, nil, err
	}
	u.bytes += size
	if size > uploadBlock/2 {
		buf, err := d.NewBuffer(max(size, 4), vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			return nil, 0, nil, err
		}
		u.blocks = append(u.blocks, buf)
		return buf, 0, unsafe.Slice((*byte)(buf.Mapped), int(size)), nil
	}
	if u.align == 0 {
		u.align = max(d.Limits().OptimalBufferCopyOffsetAlignment, 16)
	}
	offset := (u.used + u.align - 1) / u.align * u.align
	if u.cur == nil || offset+size > u.cur.Size {
		block, err := d.NewBuffer(uploadBlock, vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			return nil, 0, nil, err
		}
		u.blocks = append(u.blocks, block)
		u.cur, offset = block, 0
	}
	u.used = offset + size
	return u.cur, offset, unsafe.Slice((*byte)(unsafe.Add(u.cur.Mapped, offset)), int(size)), nil
}

// UploadCommands returns the open upload batch's command buffer, opening
// a batch if there is none, and counts one upload against the batch's
// limit. Record into it at once: the next StageUpload, FlushUploads,
// WaitIdle or OneShot may submit it.
func (d *Device) UploadCommands() (vk.VkCommandBuffer, error) {
	if err := d.beginUploads(); err != nil {
		return 0, err
	}
	d.up.count++
	return d.up.cb, nil
}

// beginUploads opens a batch when none is open.
func (d *Device) beginUploads() error {
	u := &d.up
	if u.cb != 0 {
		return nil
	}
	d.reclaimUploads()
	bufs, err := d.allocateCommandBuffers(1)
	if err != nil {
		return err
	}
	begin := vk.VkCommandBufferBeginInfo{SType: vk.VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO, Flags: vk.VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT}
	if err := vk.Check("vkBeginCommandBuffer", vk.VkBeginCommandBuffer(bufs[0], &begin)); err != nil {
		vk.VkFreeCommandBuffers(d.Handle, d.pool, 1, &bufs[0])
		return err
	}
	u.cb = bufs[0]
	u.serial++
	return nil
}

// FlushUploads submits the open upload batch, if there is one, without
// waiting for it. Work submitted to the queue afterwards sees what the
// batch wrote. The renderer calls it before submitting a frame, and
// WaitIdle and OneShot call it before they wait, so a caller only needs
// it to start the transfers early. It also frees the staging of batches
// that have finished.
func (d *Device) FlushUploads() error {
	u := &d.up
	if u.cb == 0 {
		d.reclaimUploads()
		return nil
	}
	cb := u.cb
	batch := uploadBatch{serial: u.serial, cb: cb, blocks: u.blocks, bytes: u.bytes}
	u.cb, u.blocks, u.cur, u.used, u.bytes, u.count = 0, nil, nil, 0, 0, 0
	// One barrier at the end makes every transfer in the batch visible to
	// whatever runs after it on the queue: vertex and index reads, storage
	// reads, sampling, and further transfers.
	mb := vk.VkMemoryBarrier2{
		SType:         vk.VK_STRUCTURE_TYPE_MEMORY_BARRIER_2,
		SrcStageMask:  vk.VK_PIPELINE_STAGE_2_ALL_TRANSFER_BIT,
		SrcAccessMask: vk.VK_ACCESS_2_TRANSFER_WRITE_BIT,
		DstStageMask:  vk.VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT,
		DstAccessMask: vk.VK_ACCESS_2_MEMORY_READ_BIT | vk.VK_ACCESS_2_MEMORY_WRITE_BIT,
	}
	dep := vk.VkDependencyInfo{SType: vk.VK_STRUCTURE_TYPE_DEPENDENCY_INFO, MemoryBarrierCount: 1, PMemoryBarriers: &mb}
	vk.CmdPipelineBarrier2(cb, &dep)
	fail := func(err error) error {
		vk.VkFreeCommandBuffers(d.Handle, d.pool, 1, &cb)
		for _, b := range batch.blocks {
			b.Destroy()
		}
		return err
	}
	if err := vk.Check("vkEndCommandBuffer", vk.VkEndCommandBuffer(cb)); err != nil {
		return fail(err)
	}
	if n := len(u.fences); n > 0 {
		batch.fence, u.fences = u.fences[n-1], u.fences[:n-1]
	} else {
		var err error
		if batch.fence, err = d.newFence(false); err != nil {
			return fail(err)
		}
	}
	cbInfo := vk.VkCommandBufferSubmitInfo{SType: vk.VK_STRUCTURE_TYPE_COMMAND_BUFFER_SUBMIT_INFO, CommandBuffer: cb}
	submit := vk.VkSubmitInfo2{SType: vk.VK_STRUCTURE_TYPE_SUBMIT_INFO_2, CommandBufferInfoCount: 1, PCommandBufferInfos: &cbInfo}
	if err := vk.Check("vkQueueSubmit2", vk.QueueSubmit2(d.Queue, 1, &submit, batch.fence)); err != nil {
		vk.VkDestroyFence(d.Handle, batch.fence, nil)
		return fail(err)
	}
	u.inflight = append(u.inflight, batch)
	u.inflightBytes += batch.bytes
	d.reclaimUploads()
	// A load far larger than memory should not pin all of it as staging:
	// past the limit, wait for the oldest batches to finish.
	for u.inflightBytes > uploadInFlight && len(u.inflight) > 0 {
		d.waits++
		if err := vk.Check("vkWaitForFences", vk.WaitForFences(d.Handle, 1, &u.inflight[0].fence, vk.VK_TRUE, ^uint64(0))); err != nil {
			return err
		}
		d.reclaimUploads()
	}
	return nil
}

// settleUploads submits the open batch and waits for every submitted one,
// then frees their staging.
func (d *Device) settleUploads() {
	_ = d.FlushUploads()
	for _, b := range d.up.inflight {
		if vk.VkGetFenceStatus(d.Handle, b.fence) != vk.VK_SUCCESS {
			d.waits++
			_ = vk.WaitForFences(d.Handle, 1, &b.fence, vk.VK_TRUE, ^uint64(0))
		}
	}
	d.reclaimUploads()
}

// reclaimUploads frees the command buffers and staging of submitted
// batches whose fences have signalled, keeping the fences for reuse.
// Nothing else is kept, so a device with no upload in flight holds no
// staging at all.
func (d *Device) reclaimUploads() {
	u := &d.up
	if len(u.inflight) == 0 {
		return
	}
	keep := u.inflight[:0]
	for _, b := range u.inflight {
		if vk.VkGetFenceStatus(d.Handle, b.fence) != vk.VK_SUCCESS {
			keep = append(keep, b)
			continue
		}
		vk.VkFreeCommandBuffers(d.Handle, d.pool, 1, &b.cb)
		for _, buf := range b.blocks {
			buf.Destroy()
		}
		if vk.ResetFences(d.Handle, 1, &b.fence) == vk.VK_SUCCESS {
			u.fences = append(u.fences, b.fence)
		} else {
			vk.VkDestroyFence(d.Handle, b.fence, nil)
		}
		u.inflightBytes -= b.bytes
	}
	clear(u.inflight[len(keep):])
	u.inflight = keep
}

// destroyUploads frees everything the uploader holds. The device must be
// idle, which WaitIdle has made it, so every batch has finished.
func (d *Device) destroyUploads() {
	u := &d.up
	if u.cb != 0 {
		// Never submitted: WaitIdle flushes, so this is only reached when
		// that flush failed.
		vk.VkFreeCommandBuffers(d.Handle, d.pool, 1, &u.cb)
		for _, b := range u.blocks {
			b.Destroy()
		}
	}
	for _, b := range u.inflight {
		vk.VkFreeCommandBuffers(d.Handle, d.pool, 1, &b.cb)
		for _, buf := range b.blocks {
			buf.Destroy()
		}
		vk.VkDestroyFence(d.Handle, b.fence, nil)
	}
	for _, f := range u.fences {
		vk.VkDestroyFence(d.Handle, f, nil)
	}
	d.up = uploader{serial: u.serial}
}

// errNoUpload is returned for an upload of nothing.
var errNoUpload = errors.New("render: an upload needs at least one byte")
