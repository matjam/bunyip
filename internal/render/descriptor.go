package render

import "github.com/matjam/bunyip/internal/vk"

// DescriptorSets hands out sets of one combined image sampler each, which is
// the only layout the 2D path needs. A second allocator for per-frame
// uniform buffers sits beside it for the 3D path.
type DescriptorSets struct {
	Layout vk.VkDescriptorSetLayout
	pool   vk.VkDescriptorPool
	dev    *Device
}

// NewTextureDescriptors creates the sampler layout and a pool for capacity sets.
func (d *Device) NewTextureDescriptors(capacity uint32) (*DescriptorSets, error) {
	ds := &DescriptorSets{dev: d}
	binding := vk.VkDescriptorSetLayoutBinding{
		Binding:         0,
		DescriptorType:  vk.VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,
		DescriptorCount: 1,
		StageFlags:      vk.VK_SHADER_STAGE_FRAGMENT_BIT,
	}
	layoutInfo := vk.VkDescriptorSetLayoutCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, BindingCount: 1, PBindings: &binding}
	if err := vk.Check("vkCreateDescriptorSetLayout", vk.VkCreateDescriptorSetLayout(d.Handle, &layoutInfo, nil, &ds.Layout)); err != nil {
		return nil, err
	}
	size := vk.VkDescriptorPoolSize{Type: vk.VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, DescriptorCount: capacity}
	poolInfo := vk.VkDescriptorPoolCreateInfo{
		SType:         vk.VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,
		Flags:         vk.VK_DESCRIPTOR_POOL_CREATE_FREE_DESCRIPTOR_SET_BIT,
		MaxSets:       capacity,
		PoolSizeCount: 1,
		PPoolSizes:    &size,
	}
	if err := vk.Check("vkCreateDescriptorPool", vk.VkCreateDescriptorPool(d.Handle, &poolInfo, nil, &ds.pool)); err != nil {
		vk.VkDestroyDescriptorSetLayout(d.Handle, ds.Layout, nil)
		return nil, err
	}
	return ds, nil
}

// Allocate makes a set pointing at view through sampler.
func (ds *DescriptorSets) Allocate(view vk.VkImageView, sampler vk.VkSampler) (vk.VkDescriptorSet, error) {
	info := vk.VkDescriptorSetAllocateInfo{
		SType:              vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,
		DescriptorPool:     ds.pool,
		DescriptorSetCount: 1,
		PSetLayouts:        &ds.Layout,
	}
	var set vk.VkDescriptorSet
	if err := vk.Check("vkAllocateDescriptorSets", vk.VkAllocateDescriptorSets(ds.dev.Handle, &info, &set)); err != nil {
		return 0, err
	}
	imageInfo := vk.VkDescriptorImageInfo{Sampler: sampler, ImageView: view, ImageLayout: vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL}
	write := vk.VkWriteDescriptorSet{
		SType:           vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,
		DstSet:          set,
		DstBinding:      0,
		DescriptorCount: 1,
		DescriptorType:  vk.VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,
		PImageInfo:      &imageInfo,
	}
	vk.VkUpdateDescriptorSets(ds.dev.Handle, 1, &write, 0, nil)
	return set, nil
}

// Free returns a set to the pool.
func (ds *DescriptorSets) Free(set vk.VkDescriptorSet) {
	vk.VkFreeDescriptorSets(ds.dev.Handle, ds.pool, 1, &set)
}

func (ds *DescriptorSets) Destroy() {
	vk.VkDestroyDescriptorPool(ds.dev.Handle, ds.pool, nil)
	vk.VkDestroyDescriptorSetLayout(ds.dev.Handle, ds.Layout, nil)
}
