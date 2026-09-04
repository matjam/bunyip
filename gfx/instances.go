package gfx

import (
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
)

// instanceStream is the per-frame buffer of mesh instances, one copy per
// frame in flight, grown when a frame needs more.
type instanceStream struct {
	items    []meshInstance
	buffers  [render.FramesInFlight]*render.Buffer
	capacity int
	slot     int
}

func (s *instanceStream) reset()              { s.items = s.items[:0] }
func (s *instanceStream) add(in meshInstance) { s.items = append(s.items, in) }

func (s *instanceStream) upload(g *Graphics, slot int) error {
	s.slot = slot
	if len(s.items) > s.capacity {
		newCap := max(s.capacity*2, 1024)
		for newCap < len(s.items) {
			newCap *= 2
		}
		if err := g.growStream(&s.buffers, vk.VkDeviceSize(newCap*meshInstanceSize)); err != nil {
			return err
		}
		s.capacity = newCap
	}
	if len(s.items) == 0 {
		return nil
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(&s.items[0])), len(s.items)*meshInstanceSize)
	return s.buffers[slot].Write(0, data)
}

func (s *instanceStream) destroy() {
	for i := range s.buffers {
		if s.buffers[i] != nil {
			s.buffers[i].Destroy()
			s.buffers[i] = nil
		}
	}
}
