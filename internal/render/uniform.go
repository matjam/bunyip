package render

import "github.com/matjam/bunyip/internal/vk"

// UniformSets is a descriptor set layout with one uniform buffer at
// binding 0 and any number of storage buffers after it, plus one buffer
// and set per frame in flight so that a frame's data can be written
// while the previous frame still reads its own. The storage buffers hold
// the per-frame arrays that outgrow a uniform block, such as a scene's
// lights, and never change size, so a frame writes them without waiting
// for the device.
type UniformSets struct {
	Layout  vk.VkDescriptorSetLayout
	Sets    [FramesInFlight]vk.VkDescriptorSet
	Buffers [FramesInFlight]*Buffer
	// Storage holds one buffer per frame in flight for each storage
	// binding, in binding order from binding 1.
	Storage [][FramesInFlight]*Buffer
	pool    vk.VkDescriptorPool
	dev     *Device
}

// NewUniformSets creates the layout, pool, buffers and sets for size bytes
// of uniform data visible to stages.
func (d *Device) NewUniformSets(size vk.VkDeviceSize, stages vk.VkShaderStageFlags) (*UniformSets, error) {
	return d.NewFrameSets(size, nil, stages)
}

// NewFrameSets is NewUniformSets with a storage buffer at binding 1 and
// up, one for each size in storage. Every buffer is host visible and
// fixed at the size given.
func (d *Device) NewFrameSets(size vk.VkDeviceSize, storage []vk.VkDeviceSize, stages vk.VkShaderStageFlags) (*UniformSets, error) {
	u := &UniformSets{dev: d}
	bindings := []vk.VkDescriptorSetLayoutBinding{{
		Binding: 0, DescriptorType: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER, DescriptorCount: 1, StageFlags: stages,
	}}
	for i := range storage {
		bindings = append(bindings, vk.VkDescriptorSetLayoutBinding{
			Binding: uint32(i + 1), DescriptorType: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, DescriptorCount: 1, StageFlags: stages,
		})
	}
	layoutInfo := vk.VkDescriptorSetLayoutCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, BindingCount: uint32(len(bindings)), PBindings: &bindings[0]}
	if err := vk.Check("vkCreateDescriptorSetLayout", vk.VkCreateDescriptorSetLayout(d.Handle, &layoutInfo, nil, &u.Layout)); err != nil {
		return nil, err
	}
	poolSizes := []vk.VkDescriptorPoolSize{{Type: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER, DescriptorCount: FramesInFlight}}
	if len(storage) > 0 {
		poolSizes = append(poolSizes, vk.VkDescriptorPoolSize{Type: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, DescriptorCount: FramesInFlight * uint32(len(storage))})
	}
	poolInfo := vk.VkDescriptorPoolCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO, MaxSets: FramesInFlight, PoolSizeCount: uint32(len(poolSizes)), PPoolSizes: &poolSizes[0]}
	if err := vk.Check("vkCreateDescriptorPool", vk.VkCreateDescriptorPool(d.Handle, &poolInfo, nil, &u.pool)); err != nil {
		u.Destroy()
		return nil, err
	}
	u.Storage = make([][FramesInFlight]*Buffer, len(storage))
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
		// The infos and writes are addressed by pointer, so they live in
		// slices that outlast the call to vkUpdateDescriptorSets.
		infos := []vk.VkDescriptorBufferInfo{{Buffer: buf.Handle, Range: size}}
		writes := []vk.VkWriteDescriptorSet{{
			SType: vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, DstSet: u.Sets[i], DescriptorCount: 1,
			DescriptorType: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER,
		}}
		for k, s := range storage {
			sbuf, err := d.NewBuffer(s, vk.VK_BUFFER_USAGE_STORAGE_BUFFER_BIT,
				vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
			if err != nil {
				u.Destroy()
				return nil, err
			}
			u.Storage[k][i] = sbuf
			infos = append(infos, vk.VkDescriptorBufferInfo{Buffer: sbuf.Handle, Range: s})
			writes = append(writes, vk.VkWriteDescriptorSet{
				SType: vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, DstSet: u.Sets[i], DstBinding: uint32(k + 1), DescriptorCount: 1,
				DescriptorType: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER,
			})
		}
		for k := range writes {
			writes[k].PBufferInfo = &infos[k]
		}
		vk.VkUpdateDescriptorSets(d.Handle, uint32(len(writes)), &writes[0], 0, nil)
	}
	return u, nil
}

// Write stores this frame's uniform data into the slot's buffer.
func (u *UniformSets) Write(slot int, data []byte) error {
	return u.Buffers[slot].Write(0, data)
}

// WriteStorage stores data at the start of the slot's buffer for one
// storage binding, counting from zero at binding 1. Data longer than the
// buffer is an error, since the buffers never grow.
func (u *UniformSets) WriteStorage(slot, binding int, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return u.Storage[binding][slot].Write(0, data)
}

func (u *UniformSets) Destroy() {
	for i := range u.Buffers {
		if u.Buffers[i] != nil {
			u.Buffers[i].Destroy()
			u.Buffers[i] = nil
		}
	}
	for _, bufs := range u.Storage {
		for _, b := range bufs {
			if b != nil {
				b.Destroy()
			}
		}
	}
	u.Storage = nil
	if u.pool != 0 {
		vk.VkDestroyDescriptorPool(u.dev.Handle, u.pool, nil)
		u.pool = 0
	}
	if u.Layout != 0 {
		vk.VkDestroyDescriptorSetLayout(u.dev.Handle, u.Layout, nil)
		u.Layout = 0
	}
}
