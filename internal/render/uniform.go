package render

import "github.com/matjam/bunyip/internal/vk"

// UniformSets is a descriptor set layout with one uniform buffer at
// binding 0, plus one buffer and set per frame in flight so that a frame's
// data can be written while the previous frame still reads its own.
type UniformSets struct {
	Layout  vk.VkDescriptorSetLayout
	Sets    [FramesInFlight]vk.VkDescriptorSet
	Buffers [FramesInFlight]*Buffer
	pool    vk.VkDescriptorPool
	dev     *Device
}

// NewUniformSets creates the layout, pool, buffers and sets for size bytes
// of uniform data visible to stages.
func (d *Device) NewUniformSets(size vk.VkDeviceSize, stages vk.VkShaderStageFlags) (*UniformSets, error) {
	u := &UniformSets{dev: d}
	binding := vk.VkDescriptorSetLayoutBinding{
		Binding: 0, DescriptorType: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER, DescriptorCount: 1, StageFlags: stages,
	}
	layoutInfo := vk.VkDescriptorSetLayoutCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, BindingCount: 1, PBindings: &binding}
	if err := vk.Check("vkCreateDescriptorSetLayout", vk.VkCreateDescriptorSetLayout(d.Handle, &layoutInfo, nil, &u.Layout)); err != nil {
		return nil, err
	}
	poolSize := vk.VkDescriptorPoolSize{Type: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER, DescriptorCount: FramesInFlight}
	poolInfo := vk.VkDescriptorPoolCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO, MaxSets: FramesInFlight, PoolSizeCount: 1, PPoolSizes: &poolSize}
	if err := vk.Check("vkCreateDescriptorPool", vk.VkCreateDescriptorPool(d.Handle, &poolInfo, nil, &u.pool)); err != nil {
		u.Destroy()
		return nil, err
	}
	for i := range u.Sets {
		buf, err := d.NewBuffer(size, vk.VK_BUFFER_USAGE_UNIFORM_BUFFER_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			u.Destroy()
			return nil, err
		}
		u.Buffers[i] = buf
		alloc := vk.VkDescriptorSetAllocateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO, DescriptorPool: u.pool, DescriptorSetCount: 1, PSetLayouts: &u.Layout}
		if err := vk.Check("vkAllocateDescriptorSets", vk.VkAllocateDescriptorSets(d.Handle, &alloc, &u.Sets[i])); err != nil {
			u.Destroy()
			return nil, err
		}
		bufInfo := vk.VkDescriptorBufferInfo{Buffer: buf.Handle, Range: size}
		write := vk.VkWriteDescriptorSet{
			SType: vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, DstSet: u.Sets[i], DescriptorCount: 1,
			DescriptorType: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER, PBufferInfo: &bufInfo,
		}
		vk.VkUpdateDescriptorSets(d.Handle, 1, &write, 0, nil)
	}
	return u, nil
}

// Write stores this frame's uniform data into the slot's buffer.
func (u *UniformSets) Write(slot int, data []byte) error {
	return u.Buffers[slot].Write(0, data)
}

func (u *UniformSets) Destroy() {
	for i := range u.Buffers {
		if u.Buffers[i] != nil {
			u.Buffers[i].Destroy()
			u.Buffers[i] = nil
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
