package render

import "github.com/matjam/bunyip/internal/vk"

// stagingBlock is the size a slot's arena starts at, and the step it
// grows by.
const stagingBlock = 1 << 20 // 1 MiB

// stagingMax is the largest arena a slot keeps. A request above it gets
// a buffer of its own, retired with the slot, so one huge upload does
// not pin that much host memory for the rest of the run.
const stagingMax = 8 << 20 // 8 MiB

// Staging is the upload arena of the frame ring: one host-visible bump
// allocator per frame slot, for copies recorded into that frame's
// command buffer. Take space with Alloc, record a copy from the buffer
// and offset it returns, and call Begin(slot) once the slot's fence has
// been waited on, which frees what the last pass through the ring used.
type Staging struct {
	dev   *Device
	slots [FramesInFlight]stagingSlot
	align vk.VkDeviceSize
}

type stagingSlot struct {
	buf  *Buffer
	used vk.VkDeviceSize
	old  []*Buffer // grown out of or too large for the arena, still in flight
}

// NewStaging makes the upload arena for a device's frame ring.
func (d *Device) NewStaging() *Staging {
	// Buffer-to-image copies want the device's preferred offset
	// alignment, and never less than one RGBA texel.
	align := max(d.Limits().OptimalBufferCopyOffsetAlignment, 16)
	return &Staging{dev: d, align: align}
}

// Begin frees what the slot's last frame staged and starts it empty.
// The caller must have waited on that slot's fence first.
func (s *Staging) Begin(slot int) {
	sl := &s.slots[slot]
	for _, b := range sl.old {
		b.Destroy()
	}
	sl.old = sl.old[:0]
	sl.used = 0
}

// Alloc copies data into the slot's arena and returns the buffer and the
// offset within it to copy from. The space is valid until the next
// Begin of that slot, which is why the copy must be recorded into that
// slot's frame.
func (s *Staging) Alloc(slot int, data []byte) (*Buffer, vk.VkDeviceSize, error) {
	sl := &s.slots[slot]
	size := vk.VkDeviceSize(len(data))
	if size > stagingMax {
		// Too big for the arena: one buffer for this upload alone.
		buf, err := s.dev.NewBuffer(size, vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			return nil, 0, err
		}
		if err := buf.Write(0, data); err != nil {
			buf.Destroy()
			return nil, 0, err
		}
		sl.old = append(sl.old, buf)
		return buf, 0, nil
	}
	offset := (sl.used + s.align - 1) / s.align * s.align
	if sl.buf == nil || offset+size > sl.buf.Size {
		// Grow: a larger buffer, keeping the one in flight until the
		// slot comes round again. The fresh buffer starts empty, so it
		// only has to hold this upload. The arena never shrinks.
		want := max(sl.bufSize()*2, stagingBlock)
		for want < size {
			want *= 2
		}
		buf, err := s.dev.NewBuffer(min(want, stagingMax), vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			return nil, 0, err
		}
		if sl.buf != nil {
			sl.old = append(sl.old, sl.buf)
		}
		sl.buf, offset = buf, 0
	}
	if err := sl.buf.Write(int(offset), data); err != nil {
		return nil, 0, err
	}
	sl.used = offset + size
	return sl.buf, offset, nil
}

// bufSize is the slot's current arena size, zero before its first use.
func (sl *stagingSlot) bufSize() vk.VkDeviceSize {
	if sl.buf == nil {
		return 0
	}
	return sl.buf.Size
}

// Destroy frees every arena. The device must be idle.
func (s *Staging) Destroy() {
	for i := range s.slots {
		sl := &s.slots[i]
		for _, b := range sl.old {
			b.Destroy()
		}
		if sl.buf != nil {
			sl.buf.Destroy()
		}
		*sl = stagingSlot{}
	}
}
