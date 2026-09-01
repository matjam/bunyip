package render

import (
	"fmt"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// Buffer is a VkBuffer bound to allocator memory. Host-visible buffers are
// mapped for their whole life.
type Buffer struct {
	Handle vk.VkBuffer
	Size   vk.VkDeviceSize
	Mapped unsafe.Pointer // nil unless host-visible
	mem    allocation
	dev    *Device
}

// NewBuffer creates and binds a buffer. Host-visible buffers are mapped.
func (d *Device) NewBuffer(size vk.VkDeviceSize, usage vk.VkBufferUsageFlags, props vk.VkMemoryPropertyFlags) (*Buffer, error) {
	b := &Buffer{Size: size, dev: d}
	info := vk.VkBufferCreateInfo{
		SType:       vk.VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,
		Size:        size,
		Usage:       usage,
		SharingMode: vk.VK_SHARING_MODE_EXCLUSIVE,
	}
	if err := vk.Check("vkCreateBuffer", vk.VkCreateBuffer(d.Handle, &info, nil, &b.Handle)); err != nil {
		return nil, err
	}
	var req vk.VkMemoryRequirements
	vk.VkGetBufferMemoryRequirements(d.Handle, b.Handle, &req)
	var err error
	if b.mem, err = d.alloc.allocate(req, props, true); err != nil {
		b.Destroy()
		return nil, err
	}
	if err := vk.Check("vkBindBufferMemory", vk.VkBindBufferMemory(d.Handle, b.Handle, b.mem.Memory, b.mem.Offset)); err != nil {
		b.Destroy()
		return nil, err
	}
	b.Mapped = b.mem.Mapped
	return b, nil
}

// Dev returns the owning device.
func (b *Buffer) Dev() *Device { return b.dev }

// Write copies data into a mapped buffer at offset.
func (b *Buffer) Write(offset int, data []byte) error {
	if b.Mapped == nil {
		return fmt.Errorf("render: buffer is not host-visible")
	}
	if uint64(offset+len(data)) > uint64(b.Size) {
		return fmt.Errorf("render: write of %d bytes at %d exceeds buffer of %d", len(data), offset, b.Size)
	}
	copy(unsafe.Slice((*byte)(unsafe.Add(b.Mapped, offset)), len(data)), data)
	return nil
}

// Bytes views a mapped buffer's contents.
func (b *Buffer) Bytes() []byte {
	if b.Mapped == nil {
		return nil
	}
	return unsafe.Slice((*byte)(b.Mapped), int(b.Size))
}

func (b *Buffer) Destroy() {
	if b.Handle != 0 {
		vk.VkDestroyBuffer(b.dev.Handle, b.Handle, nil)
		b.Handle = 0
	}
	b.dev.alloc.free(b.mem)
	b.mem = allocation{}
	b.Mapped = nil
}

// NewDeviceLocalBuffer uploads data into device-local memory through a
// staging buffer, for vertex and index data that never changes.
func (d *Device) NewDeviceLocalBuffer(data []byte, usage vk.VkBufferUsageFlags) (*Buffer, error) {
	staging, err := d.NewBuffer(vk.VkDeviceSize(len(data)), vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT,
		vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
	if err != nil {
		return nil, err
	}
	defer staging.Destroy()
	if err := staging.Write(0, data); err != nil {
		return nil, err
	}
	buf, err := d.NewBuffer(vk.VkDeviceSize(len(data)), usage|vk.VK_BUFFER_USAGE_TRANSFER_DST_BIT, vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)
	if err != nil {
		return nil, err
	}
	err = d.OneShot(func(cb vk.VkCommandBuffer) {
		region := vk.VkBufferCopy{Size: vk.VkDeviceSize(len(data))}
		vk.VkCmdCopyBuffer(cb, staging.Handle, buf.Handle, 1, &region)
	})
	if err != nil {
		buf.Destroy()
		return nil, err
	}
	return buf, nil
}
