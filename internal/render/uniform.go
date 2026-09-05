package render

import "github.com/matjam/bunyip/internal/vk"

// UniformSets is a descriptor set layout with one uniform buffer at
// binding 0, plus one buffer and set per frame in flight so that a frame's
// data can be written while the previous frame still reads its own. With
// NewUniformStorageSets the same set carries a storage buffer at binding
// 1, for a per-frame array too large for a uniform block, and with
// NewFrameSets more of them at bindings 2 and up, each fixed at its size.
type UniformSets struct {
	Layout  vk.VkDescriptorSetLayout
	Sets    [FramesInFlight]vk.VkDescriptorSet
	Buffers [FramesInFlight]*Buffer
	storage [FramesInFlight]*Buffer
	stored  vk.VkDeviceSize // the storage buffers' size, 0 when there are none
	// fixed holds the buffers of bindings 2 and up, one per frame in
	// flight for each, at the sizes they were made with.
	fixed [][FramesInFlight]*Buffer
	pool  vk.VkDescriptorPool
	dev   *Device
}

// NewUniformSets creates the layout, pool, buffers and sets for size bytes
// of uniform data visible to stages.
func (d *Device) NewUniformSets(size vk.VkDeviceSize, stages vk.VkShaderStageFlags) (*UniformSets, error) {
	return d.newUniformSets(size, 0, nil, stages)
}

// NewUniformStorageSets is NewUniformSets with a storage buffer at binding
// 1 of the same set, holding storageSize bytes to start with and grown by
// WriteStorage. Use it for a per-frame array a uniform block cannot hold,
// such as an irradiance grid.
func (d *Device) NewUniformStorageSets(size, storageSize vk.VkDeviceSize, stages vk.VkShaderStageFlags) (*UniformSets, error) {
	return d.newUniformSets(size, max(storageSize, 16), nil, stages)
}

// NewFrameSets is NewUniformStorageSets with a storage buffer at each of
// bindings 2 and up, one for every size in fixed. Those buffers keep the
// size they are made with, so WriteFixed never grows one and a frame
// writes them without waiting for the device. Use them for a per-frame
// array whose size is a constant, such as a cluster grid's tables.
func (d *Device) NewFrameSets(size, storageSize vk.VkDeviceSize, fixed []vk.VkDeviceSize, stages vk.VkShaderStageFlags) (*UniformSets, error) {
	return d.newUniformSets(size, max(storageSize, 16), fixed, stages)
}

func (d *Device) newUniformSets(size, storageSize vk.VkDeviceSize, fixed []vk.VkDeviceSize, stages vk.VkShaderStageFlags) (*UniformSets, error) {
	u := &UniformSets{dev: d}
	bindings := []vk.VkDescriptorSetLayoutBinding{{
		Binding: 0, DescriptorType: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER, DescriptorCount: 1, StageFlags: stages,
	}}
	poolSizes := []vk.VkDescriptorPoolSize{{Type: vk.VK_DESCRIPTOR_TYPE_UNIFORM_BUFFER, DescriptorCount: FramesInFlight}}
	if storageSize > 0 {
		bindings = append(bindings, vk.VkDescriptorSetLayoutBinding{
			Binding: 1, DescriptorType: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, DescriptorCount: 1, StageFlags: stages,
		})
	}
	for i := range fixed {
		bindings = append(bindings, vk.VkDescriptorSetLayoutBinding{
			Binding: uint32(i + 2), DescriptorType: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, DescriptorCount: 1, StageFlags: stages,
		})
	}
	if n := len(bindings) - 1; n > 0 {
		poolSizes = append(poolSizes, vk.VkDescriptorPoolSize{Type: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, DescriptorCount: FramesInFlight * uint32(n)})
	}
	layoutInfo := vk.VkDescriptorSetLayoutCreateInfo{
		SType:        vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO,
		BindingCount: uint32(len(bindings)), PBindings: &bindings[0],
	}
	if err := vk.Check("vkCreateDescriptorSetLayout", vk.VkCreateDescriptorSetLayout(d.Handle, &layoutInfo, nil, &u.Layout)); err != nil {
		return nil, err
	}
	poolInfo := vk.VkDescriptorPoolCreateInfo{
		SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO, MaxSets: FramesInFlight,
		PoolSizeCount: uint32(len(poolSizes)), PPoolSizes: &poolSizes[0],
	}
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
	if storageSize > 0 {
		if err := u.resizeStorage(storageSize); err != nil {
			u.Destroy()
			return nil, err
		}
	}
	u.fixed = make([][FramesInFlight]*Buffer, len(fixed))
	for k, size := range fixed {
		for i := range FramesInFlight {
			buf, err := d.NewBuffer(size, vk.VK_BUFFER_USAGE_STORAGE_BUFFER_BIT,
				vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
			if err != nil {
				u.Destroy()
				return nil, err
			}
			u.fixed[k][i] = buf
			bufInfo := vk.VkDescriptorBufferInfo{Buffer: buf.Handle, Range: size}
			write := vk.VkWriteDescriptorSet{
				SType: vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, DstSet: u.Sets[i], DstBinding: uint32(k + 2), DescriptorCount: 1,
				DescriptorType: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, PBufferInfo: &bufInfo,
			}
			vk.VkUpdateDescriptorSets(d.Handle, 1, &write, 0, nil)
		}
	}
	return u, nil
}

// resizeStorage replaces every slot's storage buffer and rewrites binding 1.
func (u *UniformSets) resizeStorage(size vk.VkDeviceSize) error {
	if err := u.dev.WaitIdle(); err != nil {
		return err
	}
	for i := range u.storage {
		if u.storage[i] != nil {
			u.storage[i].Destroy()
		}
		buf, err := u.dev.NewBuffer(size, vk.VK_BUFFER_USAGE_STORAGE_BUFFER_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			return err
		}
		u.storage[i] = buf
		bufInfo := vk.VkDescriptorBufferInfo{Buffer: buf.Handle, Range: size}
		write := vk.VkWriteDescriptorSet{
			SType: vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, DstSet: u.Sets[i], DstBinding: 1, DescriptorCount: 1,
			DescriptorType: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, PBufferInfo: &bufInfo,
		}
		vk.VkUpdateDescriptorSets(u.dev.Handle, 1, &write, 0, nil)
	}
	u.stored = size
	return nil
}

// Write stores this frame's uniform data into the slot's buffer.
func (u *UniformSets) Write(slot int, data []byte) error {
	return u.Buffers[slot].Write(0, data)
}

// WriteStorage stores data into the slot's storage buffer at binding 1,
// growing every slot's buffer when it no longer fits. Growing waits for
// the device, so it happens when a grid changes size and not per frame.
func (u *UniformSets) WriteStorage(slot int, data []byte) error {
	if u.stored == 0 {
		return nil
	}
	if vk.VkDeviceSize(len(data)) > u.stored {
		size := u.stored * 2
		for size < vk.VkDeviceSize(len(data)) {
			size *= 2
		}
		if err := u.resizeStorage(size); err != nil {
			return err
		}
	}
	if len(data) == 0 {
		return nil
	}
	return u.storage[slot].Write(0, data)
}

// WriteFixed stores data at the start of the slot's buffer for one of the
// fixed storage bindings, counting from zero at binding 2. Data longer
// than the buffer is an error, since those buffers never grow.
func (u *UniformSets) WriteFixed(slot, binding int, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return u.fixed[binding][slot].Write(0, data)
}

func (u *UniformSets) Destroy() {
	for i := range u.Buffers {
		if u.Buffers[i] != nil {
			u.Buffers[i].Destroy()
			u.Buffers[i] = nil
		}
		if u.storage[i] != nil {
			u.storage[i].Destroy()
			u.storage[i] = nil
		}
	}
	for _, bufs := range u.fixed {
		for _, b := range bufs {
			if b != nil {
				b.Destroy()
			}
		}
	}
	u.fixed = nil
	if u.pool != 0 {
		vk.VkDestroyDescriptorPool(u.dev.Handle, u.pool, nil)
		u.pool = 0
	}
	if u.Layout != 0 {
		vk.VkDestroyDescriptorSetLayout(u.dev.Handle, u.Layout, nil)
		u.Layout = 0
	}
}
