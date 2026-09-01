package render

import "github.com/matjam/bunyip/internal/vk"

// DescriptorSets hands out sets of combined image samplers with a fixed
// number of bindings, all visible to the fragment stage.
type DescriptorSets struct {
	Layout   vk.VkDescriptorSetLayout
	Bindings int
	pool     vk.VkDescriptorPool
	dev      *Device
}

// NewTextureDescriptors creates a one-binding sampler layout, the 2D path's.
func (d *Device) NewTextureDescriptors(capacity uint32) (*DescriptorSets, error) {
	return d.NewSamplerDescriptors(1, capacity)
}

// NewSamplerDescriptors creates a layout with bindings combined image
// samplers and a pool for capacity sets.
func (d *Device) NewSamplerDescriptors(bindings int, capacity uint32) (*DescriptorSets, error) {
	return d.newSamplerDescriptors(bindings, capacity, 0)
}

// NewImmutableSamplerDescriptors is NewSamplerDescriptors with the sampler
// baked into the layout, which MoltenVK requires for comparison samplers.
func (d *Device) NewImmutableSamplerDescriptors(bindings int, capacity uint32, sampler vk.VkSampler) (*DescriptorSets, error) {
	return d.newSamplerDescriptors(bindings, capacity, sampler)
}

func (d *Device) newSamplerDescriptors(bindings int, capacity uint32, immutable vk.VkSampler) (*DescriptorSets, error) {
	ds := &DescriptorSets{dev: d, Bindings: bindings}
	layoutBindings := make([]vk.VkDescriptorSetLayoutBinding, bindings)
	for i := range layoutBindings {
		layoutBindings[i] = vk.VkDescriptorSetLayoutBinding{
			Binding:         uint32(i),
			DescriptorType:  vk.VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,
			DescriptorCount: 1,
			StageFlags:      vk.VK_SHADER_STAGE_FRAGMENT_BIT,
		}
		if immutable != 0 {
			layoutBindings[i].PImmutableSamplers = &immutable
		}
	}
	layoutInfo := vk.VkDescriptorSetLayoutCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, BindingCount: uint32(bindings), PBindings: &layoutBindings[0]}
	if err := vk.Check("vkCreateDescriptorSetLayout", vk.VkCreateDescriptorSetLayout(d.Handle, &layoutInfo, nil, &ds.Layout)); err != nil {
		return nil, err
	}
	size := vk.VkDescriptorPoolSize{Type: vk.VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, DescriptorCount: capacity * uint32(bindings)}
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

// SamplerBinding pairs an image view with the sampler to read it through.
type SamplerBinding struct {
	View    vk.VkImageView
	Sampler vk.VkSampler
	Layout  vk.VkImageLayout // zero means shader-read-only
}

// Allocate makes a set pointing at view through sampler (one binding).
func (ds *DescriptorSets) Allocate(view vk.VkImageView, sampler vk.VkSampler) (vk.VkDescriptorSet, error) {
	return ds.AllocateMany([]SamplerBinding{{View: view, Sampler: sampler}})
}

// AllocateMany makes a set with one entry per binding.
func (ds *DescriptorSets) AllocateMany(bindings []SamplerBinding) (vk.VkDescriptorSet, error) {
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
	ds.Update(set, bindings)
	return set, nil
}

// Update rewrites a set's bindings.
func (ds *DescriptorSets) Update(set vk.VkDescriptorSet, bindings []SamplerBinding) {
	infos := make([]vk.VkDescriptorImageInfo, len(bindings))
	writes := make([]vk.VkWriteDescriptorSet, len(bindings))
	for i, b := range bindings {
		layout := b.Layout
		if layout == 0 {
			layout = vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL
		}
		infos[i] = vk.VkDescriptorImageInfo{Sampler: b.Sampler, ImageView: b.View, ImageLayout: layout}
		writes[i] = vk.VkWriteDescriptorSet{
			SType:           vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,
			DstSet:          set,
			DstBinding:      uint32(i),
			DescriptorCount: 1,
			DescriptorType:  vk.VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER,
			PImageInfo:      &infos[i],
		}
	}
	vk.VkUpdateDescriptorSets(ds.dev.Handle, uint32(len(writes)), &writes[0], 0, nil)
}

// Free returns a set to the pool.
func (ds *DescriptorSets) Free(set vk.VkDescriptorSet) {
	vk.VkFreeDescriptorSets(ds.dev.Handle, ds.pool, 1, &set)
}

func (ds *DescriptorSets) Destroy() {
	vk.VkDestroyDescriptorPool(ds.dev.Handle, ds.pool, nil)
	vk.VkDestroyDescriptorSetLayout(ds.dev.Handle, ds.Layout, nil)
}
