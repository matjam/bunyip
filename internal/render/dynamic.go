package render

import (
	"fmt"

	"github.com/matjam/bunyip/internal/vk"
)

// DynamicUniforms is a growable per-frame arena of uniform data bound
// through one dynamic uniform buffer descriptor: many draws share the
// set and select their block with an offset. Blocks are at most Range
// bytes and start on the device's offset alignment.
type DynamicUniforms struct {
	Layout  vk.VkDescriptorSetLayout
	Sets    [FramesInFlight]vk.VkDescriptorSet
	Range   vk.VkDeviceSize // largest block a shader may declare
	Align   vk.VkDeviceSize
	buffers [FramesInFlight]*Buffer
	size    vk.VkDeviceSize
	pool    vk.VkDescriptorPool
	dev     *Device
}

// NewDynamicUniforms creates the layout (binding 0, visible to stages),
// one set per frame in flight, and buffers of an initial size.
func (d *Device) NewDynamicUniforms(blockRange vk.VkDeviceSize, stages vk.VkShaderStageFlags) (*DynamicUniforms, error) {
	u := &DynamicUniforms{dev: d, Range: blockRange, Align: max(d.Limits().MinUniformBufferOffsetAlignment, 16)}
	binding := vk.VkDescriptorSetLayoutBinding{
		Binding: 0, DescriptorType: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC, DescriptorCount: 1, StageFlags: stages,
	}
	layoutInfo := vk.VkDescriptorSetLayoutCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, BindingCount: 1, PBindings: &binding}
	if err := vk.Check("vkCreateDescriptorSetLayout", vk.VkCreateDescriptorSetLayout(d.Handle, &layoutInfo, nil, &u.Layout)); err != nil {
		return nil, err
	}
	poolSize := vk.VkDescriptorPoolSize{Type: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC, DescriptorCount: FramesInFlight}
	poolInfo := vk.VkDescriptorPoolCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO, MaxSets: FramesInFlight, PoolSizeCount: 1, PPoolSizes: &poolSize}
	if err := vk.Check("vkCreateDescriptorPool", vk.VkCreateDescriptorPool(d.Handle, &poolInfo, nil, &u.pool)); err != nil {
		u.Destroy()
		return nil, err
	}
	for i := range u.Sets {
		alloc := vk.VkDescriptorSetAllocateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO, DescriptorPool: u.pool, DescriptorSetCount: 1, PSetLayouts: &u.Layout}
		if err := vk.Check("vkAllocateDescriptorSets", vk.VkAllocateDescriptorSets(d.Handle, &alloc, &u.Sets[i])); err != nil {
			u.Destroy()
			return nil, err
		}
	}
	if err := u.grow(16 * 1024); err != nil {
		u.Destroy()
		return nil, err
	}
	return u, nil
}

// grow replaces every slot's buffer with one of at least size bytes and
// points the sets at them. The device is idled first, since a frame in
// flight may be reading the old buffers.
func (u *DynamicUniforms) grow(size vk.VkDeviceSize) error {
	if err := u.dev.WaitIdle(); err != nil {
		return err
	}
	newSize := max(u.size*2, 16*1024)
	for newSize < size {
		newSize *= 2
	}
	for i := range u.buffers {
		if u.buffers[i] != nil {
			u.buffers[i].Destroy()
		}
		buf, err := u.dev.NewBuffer(newSize, vk.VK_BUFFER_USAGE_UNIFORM_BUFFER_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			return err
		}
		u.buffers[i] = buf
		bufInfo := vk.VkDescriptorBufferInfo{Buffer: buf.Handle, Range: u.Range}
		write := vk.VkWriteDescriptorSet{
			SType: vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, DstSet: u.Sets[i], DescriptorCount: 1,
			DescriptorType: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER_DYNAMIC, PBufferInfo: &bufInfo,
		}
		vk.VkUpdateDescriptorSets(u.dev.Handle, 1, &write, 0, nil)
	}
	u.size = newSize
	return nil
}

// Write stores a frame's whole arena into the slot's buffer, growing the
// buffers when it does not fit. The data must leave Range bytes of slack
// after its last block, which Arena guarantees.
func (u *DynamicUniforms) Write(slot int, data []byte) error {
	need := vk.VkDeviceSize(len(data)) + u.Range
	if need > u.size {
		if err := u.grow(need); err != nil {
			return err
		}
	}
	if len(data) == 0 {
		return nil
	}
	return u.buffers[slot].Write(0, data)
}

// Arena collects uniform blocks on the CPU for one frame, aligned for
// dynamic offsets.
type Arena struct {
	data  []byte
	align int
}

// NewArena makes an arena for a DynamicUniforms' alignment.
func (u *DynamicUniforms) NewArena() *Arena { return &Arena{align: int(u.Align)} }

// Reset empties the arena for a new frame.
func (a *Arena) Reset() { a.data = a.data[:0] }

// Add copies a block in and returns its offset.
func (a *Arena) Add(block []byte) (uint32, error) {
	if len(block) == 0 {
		return 0, fmt.Errorf("render: empty uniform block")
	}
	off := (len(a.data) + a.align - 1) / a.align * a.align
	if off+len(block) > 1<<30 {
		return 0, fmt.Errorf("render: uniform arena full")
	}
	a.data = append(a.data[:len(a.data):len(a.data)], make([]byte, off-len(a.data))...)
	a.data = append(a.data, block...)
	return uint32(off), nil
}

// Bytes is the frame's data so far.
func (a *Arena) Bytes() []byte { return a.data }

func (u *DynamicUniforms) Destroy() {
	for i := range u.buffers {
		if u.buffers[i] != nil {
			u.buffers[i].Destroy()
			u.buffers[i] = nil
		}
	}
	if u.pool != 0 {
		vk.VkDestroyDescriptorPool(u.dev.Handle, u.pool, nil)
		u.pool = 0
	}
	if u.Layout != 0 {
		vk.VkDestroyDescriptorSetLayout(u.dev.Handle, u.Layout, nil)
		u.Layout = 0
	}
}
