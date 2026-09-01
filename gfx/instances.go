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

func (s *instanceStream) upload(dev *render.Device, slot int) error {
	s.slot = slot
	if len(s.items) > s.capacity {
		if err := dev.WaitIdle(); err != nil {
			return err
		}
		newCap := max(s.capacity*2, 1024)
		for newCap < len(s.items) {
			newCap *= 2
		}
		for i := range s.buffers {
			if s.buffers[i] != nil {
				s.buffers[i].Destroy()
			}
			buf, err := dev.NewBuffer(vk.VkDeviceSize(newCap*meshInstanceSize), vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT,
				vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
			if err != nil {
				return err
			}
			s.buffers[i] = buf
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
