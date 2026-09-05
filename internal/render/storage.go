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
	if err := s.resize(size); err != nil {
		s.Destroy()
		return nil, err
	}
	return s, nil
}

// newPool creates a pool holding one set per frame in flight and
// allocates those sets from it.
func (s *StorageSets) newPool() (vk.VkDescriptorPool, [FramesInFlight]vk.VkDescriptorSet, error) {
	var pool vk.VkDescriptorPool
	var sets [FramesInFlight]vk.VkDescriptorSet
	d := s.dev
	poolSize := vk.VkDescriptorPoolSize{Type: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, DescriptorCount: FramesInFlight}
	poolInfo := vk.VkDescriptorPoolCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO, MaxSets: FramesInFlight, PoolSizeCount: 1, PPoolSizes: &poolSize}
	if err := vk.Check("vkCreateDescriptorPool", vk.VkCreateDescriptorPool(d.Handle, &poolInfo, nil, &pool)); err != nil {
		return 0, sets, err
	}
	for i := range sets {
		alloc := vk.VkDescriptorSetAllocateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO, DescriptorPool: pool, DescriptorSetCount: 1, PSetLayouts: &s.Layout}
		if err := vk.Check("vkAllocateDescriptorSets", vk.VkAllocateDescriptorSets(d.Handle, &alloc, &sets[i])); err != nil {
			vk.VkDestroyDescriptorPool(d.Handle, pool, nil)
			return 0, sets, err
		}
	}
	return pool, sets, nil
}

// resize gives every slot a buffer of size bytes and a fresh descriptor
// set pointing at it, then retires the old pool and buffers. Nothing
// waits for the device: a frame in flight keeps reading the sets and
// buffers it bound until the retire ring frees them.
func (s *StorageSets) resize(size vk.VkDeviceSize) error {
	pool, sets, err := s.newPool()
	if err != nil {
		return err
	}
	var bufs [FramesInFlight]*Buffer
	fail := func(err error) error {
		for _, b := range bufs {
			if b != nil {
				b.Destroy()
			}
		}
		vk.VkDestroyDescriptorPool(s.dev.Handle, pool, nil)
		return err
	}
	for i := range bufs {
		buf, err := s.dev.NewBuffer(size, vk.VK_BUFFER_USAGE_STORAGE_BUFFER_BIT,
			vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
		if err != nil {
			return fail(err)
		}
		bufs[i] = buf
		bufInfo := vk.VkDescriptorBufferInfo{Buffer: buf.Handle, Range: size}
		write := vk.VkWriteDescriptorSet{
			SType: vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, DstSet: sets[i], DescriptorCount: 1,
			DescriptorType: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, PBufferInfo: &bufInfo,
		}
		vk.VkUpdateDescriptorSets(s.dev.Handle, 1, &write, 0, nil)
	}
	oldPool, oldBufs, dev := s.pool, s.Buffers, s.dev
	s.pool, s.Sets, s.Buffers, s.size = pool, sets, bufs, size
	if oldPool != 0 {
		dev.Retire(func() {
			for _, b := range oldBufs {
				if b != nil {
					b.Destroy()
				}
			}
			vk.VkDestroyDescriptorPool(dev.Handle, oldPool, nil)
		})
	}
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
