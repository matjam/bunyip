package render

import "github.com/matjam/bunyip/internal/vk"

// StorageSets is a descriptor set layout with one storage buffer at
// binding 0, with one host-visible buffer and set per frame in flight that
// grow on demand: joint matrices and other per-frame arrays live here.
type StorageSets struct {
	Layout  vk.VkDescriptorSetLayout
	Sets    [FramesInFlight]vk.VkDescriptorSet
	Buffers [FramesInFlight]*Buffer
	pool    vk.VkDescriptorPool
	dev     *Device
	size    vk.VkDeviceSize
	stages  vk.VkShaderStageFlags
}

// NewStorageSets creates the layout, pool, buffers and sets for size bytes.
func (d *Device) NewStorageSets(size vk.VkDeviceSize, stages vk.VkShaderStageFlags) (*StorageSets, error) {
	s := &StorageSets{dev: d, stages: stages}
	binding := vk.VkDescriptorSetLayoutBinding{Binding: 0, DescriptorType: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, DescriptorCount: 1, StageFlags: stages}
	layoutInfo := vk.VkDescriptorSetLayoutCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, BindingCount: 1, PBindings: &binding}
	if err := vk.Check("vkCreateDescriptorSetLayout", vk.VkCreateDescriptorSetLayout(d.Handle, &layoutInfo, nil, &s.Layout)); err != nil {
		return nil, err
	}
	poolSize := vk.VkDescriptorPoolSize{Type: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, DescriptorCount: FramesInFlight}
	poolInfo := vk.VkDescriptorPoolCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO, MaxSets: FramesInFlight, PoolSizeCount: 1, PPoolSizes: &poolSize}
	if err := vk.Check("vkCreateDescriptorPool", vk.VkCreateDescriptorPool(d.Handle, &poolInfo, nil, &s.pool)); err != nil {
		s.Destroy()
		return nil, err
	}
	for i := range s.Sets {
		alloc := vk.VkDescriptorSetAllocateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO, DescriptorPool: s.pool, DescriptorSetCount: 1, PSetLayouts: &s.Layout}
		if err := vk.Check("vkAllocateDescriptorSets", vk.VkAllocateDescriptorSets(d.Handle, &alloc, &s.Sets[i])); err != nil {
			s.Destroy()
			return nil, err
		}
	}
	if err := s.resize(size); err != nil {
		s.Destroy()
		return nil, err
	}
	return s, nil
}

// resize replaces every slot's buffer and rewrites the sets.
func (s *StorageSets) resize(size vk.VkDeviceSize) error {
	if err := s.dev.WaitIdle(); err != nil {
		return err
	}
	for i := range s.Buffers {
		if s.Buffers[i] != nil {
			s.Buffers[i].Destroy()
		}
		buf, err := s.dev.NewBuffer(size, vk.VK_BUFFER_USAGE_STORAGE_BUFFER_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			return err
		}
		s.Buffers[i] = buf
		bufInfo := vk.VkDescriptorBufferInfo{Buffer: buf.Handle, Range: size}
		write := vk.VkWriteDescriptorSet{
			SType: vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, DstSet: s.Sets[i], DescriptorCount: 1,
			DescriptorType: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, PBufferInfo: &bufInfo,
		}
		vk.VkUpdateDescriptorSets(s.dev.Handle, 1, &write, 0, nil)
	}
	s.size = size
	return nil
}

// Write stores data into the slot's buffer, growing every slot when needed.
func (s *StorageSets) Write(slot int, data []byte) error {
	if vk.VkDeviceSize(len(data)) > s.size {
		size := s.size * 2
		for size < vk.VkDeviceSize(len(data)) {
			size *= 2
		}
		if err := s.resize(size); err != nil {
			return err
		}
	}
	if len(data) == 0 {
		return nil
	}
	return s.Buffers[slot].Write(0, data)
}

func (s *StorageSets) Destroy() {
	for i := range s.Buffers {
		if s.Buffers[i] != nil {
			s.Buffers[i].Destroy()
			s.Buffers[i] = nil
		}
	}
	if s.pool != 0 {
		vk.VkDestroyDescriptorPool(s.dev.Handle, s.pool, nil)
		s.pool = 0
	}
	if s.Layout != 0 {
		vk.VkDestroyDescriptorSetLayout(s.dev.Handle, s.Layout, nil)
		s.Layout = 0
	}
}
