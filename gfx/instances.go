package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// instanceStream is the per-frame buffer of mesh instances, one copy per
// frame in flight, grown when a frame needs more. prepareDraws writes the
// records straight into the mapped buffer of the frame's slot, so an
// instance is built once, where the device reads it.
type instanceStream struct {
	buffers  [render.FramesInFlight]*render.Buffer
	capacity int
	slot     int
}

// reserve makes the slot's buffer hold at least n records and returns
// them, mapped. The slot's previous contents are whatever an earlier
// frame left, so the caller writes every record the device will read.
// The slot's buffer is not in use by the device: its frame's fence has
// been waited on, or the queue records on a one-shot buffer that waits.
func (s *instanceStream) reserve(g *Graphics, slot, n int) ([]meshInstance, error) {
	s.slot = slot
	if n > s.capacity {
		newCap := max(s.capacity*2, 1024)
		for newCap < n {
			newCap *= 2
		}
		if err := g.growStream(&s.buffers, vk.VkDeviceSize(newCap*meshInstanceSize)); err != nil {
			return nil, err
		}
		s.capacity = newCap
	}
	if n == 0 {
		return nil, nil
	}
	return unsafe.Slice((*meshInstance)(s.buffers[slot].Mapped), n), nil
}

func (s *instanceStream) destroy() {
	for i := range s.buffers {
		if s.buffers[i] != nil {
			s.buffers[i].Destroy()
			s.buffers[i] = nil
		}
	}
}
