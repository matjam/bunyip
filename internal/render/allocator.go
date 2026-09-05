package render

import (
	"fmt"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// The allocator carves device memory into large blocks per memory type and
// hands out aligned spans from a free list, so thousands of buffers and
// images share a handful of driver allocations. Large resources and
// MoltenVK index buffers get their own allocations. Host-visible blocks
// stay mapped for their whole life.

const (
	blockSize     = 64 << 20 // bytes per shared block
	dedicatedSize = 16 << 20 // resources at or above this get their own memory
)

// allocation is a span of device memory a resource is bound to.
type allocation struct {
	Memory vk.VkDeviceMemory
	Offset vk.VkDeviceSize
	Size   vk.VkDeviceSize
	Mapped unsafe.Pointer // nil unless host-visible
	block  *memBlock      // nil for dedicated allocations
}

type span struct {
	offset, size vk.VkDeviceSize
}

type memBlock struct {
	memory    vk.VkDeviceMemory
	size      vk.VkDeviceSize
	typeIndex uint32
	linear    bool // buffers; images (optimal tiling) get separate blocks
	mapped    unsafe.Pointer
	free      []span // sorted by offset, non-adjacent
	used      int    // live allocations
}

type allocator struct {
	dev    *Device
	blocks []*memBlock
	stats  AllocStats
}

// AllocStats reports allocator activity for diagnostics.
type AllocStats struct {
	Blocks, Dedicated, Live int
	Reserved, Used          vk.VkDeviceSize
}

// Stats returns the current allocator statistics.
func (d *Device) Stats() AllocStats { return d.alloc.stats }

// allocateBuffer keeps MoltenVK index buffers at offset zero. MoltenVK
// 1.4.2 on the Apple Paravirtual device can drop indexed draws with pooled
// index buffers even though GPU readback confirms their bytes are intact.
// Index-only separate allocations fix both dense meshes and mesh updates;
// vertex and other buffers still pool normally. Each index buffer uses one
// device allocation of exactly the required size, not a whole shared block.
func (a *allocator) allocateBuffer(req vk.VkMemoryRequirements, props vk.VkMemoryPropertyFlags, usage vk.VkBufferUsageFlags) (allocation, error) {
	if !a.dev.portability || a.dev.gpu.driverID != vk.VK_DRIVER_ID_MOLTENVK || usage&vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT == 0 {
		return a.allocate(req, props, true)
	}
	typeIndex, err := a.dev.gpu.memoryType(req.MemoryTypeBits, props)
	if err != nil {
		return allocation{}, err
	}
	return a.dedicated(req.Size, typeIndex, props&vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT != 0)
}

// allocate returns memory for a resource with the given requirements.
func (a *allocator) allocate(req vk.VkMemoryRequirements, props vk.VkMemoryPropertyFlags, linear bool) (allocation, error) {
	typeIndex, err := a.dev.gpu.memoryType(req.MemoryTypeBits, props)
	if err != nil {
		return allocation{}, err
	}
	hostVisible := props&vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT != 0
	if req.Size >= dedicatedSize {
		return a.dedicated(req.Size, typeIndex, hostVisible)
	}
	for _, b := range a.blocks {
		if b.typeIndex != typeIndex || b.linear != linear {
			continue
		}
		if al, ok := b.take(req.Size, req.Alignment); ok {
			a.stats.Live++
			a.stats.Used += al.Size
			return al, nil
		}
	}
	b, err := a.newBlock(max(blockSize, req.Size), typeIndex, linear, hostVisible)
	if err != nil {
		return allocation{}, err
	}
	al, ok := b.take(req.Size, req.Alignment)
	if !ok {
		return allocation{}, fmt.Errorf("render: fresh block cannot fit %d bytes", req.Size)
	}
	a.stats.Live++
	a.stats.Used += al.Size
	return al, nil
}

func (a *allocator) dedicated(size vk.VkDeviceSize, typeIndex uint32, hostVisible bool) (allocation, error) {
	mem, mapped, err := a.dev.allocateRaw(size, typeIndex, hostVisible)
	if err != nil {
		return allocation{}, err
	}
	a.stats.Dedicated++
	a.stats.Live++
	a.stats.Reserved += size
	a.stats.Used += size
	return allocation{Memory: mem, Size: size, Mapped: mapped}, nil
}

func (a *allocator) newBlock(size vk.VkDeviceSize, typeIndex uint32, linear, hostVisible bool) (*memBlock, error) {
	mem, mapped, err := a.dev.allocateRaw(size, typeIndex, hostVisible)
	if err != nil {
		return nil, err
	}
	b := &memBlock{memory: mem, size: size, typeIndex: typeIndex, linear: linear, mapped: mapped, free: []span{{0, size}}}
	a.blocks = append(a.blocks, b)
	a.stats.Blocks++
	a.stats.Reserved += size
	return b, nil
}

// take carves an aligned span out of the first free span that fits.
func (b *memBlock) take(size, align vk.VkDeviceSize) (allocation, bool) {
	if align == 0 {
		align = 1
	}
	for i, s := range b.free {
		start := (s.offset + align - 1) / align * align
		end := start + size
		if end > s.offset+s.size {
			continue
		}
		// Split the free span around [start, end).
		var rest []span
		if start > s.offset {
			rest = append(rest, span{s.offset, start - s.offset})
		}
		if end < s.offset+s.size {
			rest = append(rest, span{end, s.offset + s.size - end})
		}
		b.free = append(b.free[:i], append(rest, b.free[i+1:]...)...)
		b.used++
		al := allocation{Memory: b.memory, Offset: start, Size: size, block: b}
		if b.mapped != nil {
			al.Mapped = unsafe.Add(b.mapped, start)
		}
		return al, true
	}
	return allocation{}, false
}

// free returns a span to its block, merging with neighbours, or releases a
// dedicated allocation.
func (a *allocator) free(al allocation) {
	if al.Memory == 0 {
		return
	}
	a.stats.Live--
	a.stats.Used -= al.Size
	if al.block == nil {
		if al.Mapped != nil {
			vk.VkUnmapMemory(a.dev.Handle, al.Memory)
		}
		vk.VkFreeMemory(a.dev.Handle, al.Memory, nil)
		a.stats.Dedicated--
		a.stats.Reserved -= al.Size
		return
	}
	b := al.block
	b.used--
	s := span{al.Offset, al.Size}
	i := 0
	for i < len(b.free) && b.free[i].offset < s.offset {
		i++
	}
	b.free = append(b.free[:i], append([]span{s}, b.free[i:]...)...)
	if i+1 < len(b.free) && s.offset+s.size == b.free[i+1].offset {
		b.free[i].size += b.free[i+1].size
		b.free = append(b.free[:i+1], b.free[i+2:]...)
	}
	if i > 0 && b.free[i-1].offset+b.free[i-1].size == b.free[i].offset {
		b.free[i-1].size += b.free[i].size
		b.free = append(b.free[:i], b.free[i+1:]...)
	}
}

// destroy releases every block; resources must be gone already.
func (a *allocator) destroy() {
	for _, b := range a.blocks {
		if b.mapped != nil {
			vk.VkUnmapMemory(a.dev.Handle, b.memory)
		}
		vk.VkFreeMemory(a.dev.Handle, b.memory, nil)
	}
	a.blocks = nil
}

// allocateRaw asks the driver for one allocation, mapping it when host-visible.
func (d *Device) allocateRaw(size vk.VkDeviceSize, typeIndex uint32, hostVisible bool) (vk.VkDeviceMemory, unsafe.Pointer, error) {
	if limit := d.gpu.props.Limits.MaxMemoryAllocationCount; limit != 0 && uint64(d.alloc.stats.Blocks)+uint64(d.alloc.stats.Dedicated) >= uint64(limit) {
		return 0, nil, fmt.Errorf("render: device memory allocation limit reached (%d); release resources before allocating more", limit)
	}
	info := vk.VkMemoryAllocateInfo{
		SType:           vk.VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
		AllocationSize:  size,
		MemoryTypeIndex: typeIndex,
	}
	var mem vk.VkDeviceMemory
	if err := vk.Check("vkAllocateMemory", vk.VkAllocateMemory(d.Handle, &info, nil, &mem)); err != nil {
		return 0, nil, err
	}
	var mapped unsafe.Pointer
	if hostVisible {
		if err := vk.Check("vkMapMemory", vk.VkMapMemory(d.Handle, mem, 0, vk.VK_WHOLE_SIZE, 0, &mapped)); err != nil {
			vk.VkFreeMemory(d.Handle, mem, nil)
			return 0, nil, err
		}
	}
	return mem, mapped, nil
}
