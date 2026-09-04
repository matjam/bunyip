package render

import (
	"errors"

	"github.com/matjam/bunyip/internal/vk"
)

// DescriptorSets hands out sets of combined image samplers with a fixed
// number of bindings, all visible to the fragment stage. Sets come from
// a chain of pools: when the current pool is full another of the same
// capacity is added, so the number of live textures, materials and
// render textures is bounded by memory rather than by a starting guess.
type DescriptorSets struct {
	Layout   vk.VkDescriptorSetLayout
	Bindings int
	pools    []vk.VkDescriptorPool
	capacity uint32
	owner    map[vk.VkDescriptorSet]vk.VkDescriptorPool // which pool each live set came from
	dev      *Device
}

// NewTextureDescriptors creates a one-binding sampler layout, the 2D path's.
func (d *Device) NewTextureDescriptors(capacity uint32) (*DescriptorSets, error) {
	return d.NewSamplerDescriptors(1, capacity)
}

// NewSamplerDescriptors creates a layout with bindings combined image
// samplers and a first pool for capacity sets; more pools follow as
// needed.
func (d *Device) NewSamplerDescriptors(bindings int, capacity uint32) (*DescriptorSets, error) {
	return d.newSamplerDescriptors(bindings, capacity, 0)
}

// NewImmutableSamplerDescriptors is NewSamplerDescriptors with the sampler
// baked into the layout, which MoltenVK requires for comparison samplers.
func (d *Device) NewImmutableSamplerDescriptors(bindings int, capacity uint32, sampler vk.VkSampler) (*DescriptorSets, error) {
	return d.newSamplerDescriptors(bindings, capacity, sampler)
}

func (d *Device) newSamplerDescriptors(bindings int, capacity uint32, immutable vk.VkSampler) (*DescriptorSets, error) {
	ds := &DescriptorSets{dev: d, Bindings: bindings, capacity: max(capacity, 1), owner: map[vk.VkDescriptorSet]vk.VkDescriptorPool{}}
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
	if err := ds.addPool(); err != nil {
		vk.VkDestroyDescriptorSetLayout(d.Handle, ds.Layout, nil)
		return nil, err
	}
	return ds, nil
}

// addPool appends a pool of the standard capacity.
func (ds *DescriptorSets) addPool() error {
	size := vk.VkDescriptorPoolSize{Type: vk.VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, DescriptorCount: ds.capacity * uint32(ds.Bindings)}
	poolInfo := vk.VkDescriptorPoolCreateInfo{
		SType:         vk.VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,
		Flags:         vk.VK_DESCRIPTOR_POOL_CREATE_FREE_DESCRIPTOR_SET_BIT,
		MaxSets:       ds.capacity,
		PoolSizeCount: 1,
		PPoolSizes:    &size,
	}
	var pool vk.VkDescriptorPool
	if err := vk.Check("vkCreateDescriptorPool", vk.VkCreateDescriptorPool(ds.dev.Handle, &poolInfo, nil, &pool)); err != nil {
		return err
	}
	ds.pools = append(ds.pools, pool)
	return nil
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

// AllocateMany makes a set with one entry per binding, adding a pool
// when the current one is full.
func (ds *DescriptorSets) AllocateMany(bindings []SamplerBinding) (vk.VkDescriptorSet, error) {
	set, err := ds.allocate(ds.pools[len(ds.pools)-1])
	if poolFull(err) {
		if err = ds.addPool(); err == nil {
			set, err = ds.allocate(ds.pools[len(ds.pools)-1])
		}
	}
	if err != nil {
		return 0, err
	}
	ds.Update(set, bindings)
	return set, nil
}

func (ds *DescriptorSets) allocate(pool vk.VkDescriptorPool) (vk.VkDescriptorSet, error) {
	info := vk.VkDescriptorSetAllocateInfo{
		SType:              vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO,
		DescriptorPool:     pool,
		DescriptorSetCount: 1,
		PSetLayouts:        &ds.Layout,
	}
	var set vk.VkDescriptorSet
	if err := vk.Check("vkAllocateDescriptorSets", vk.VkAllocateDescriptorSets(ds.dev.Handle, &info, &set)); err != nil {
		return 0, err
	}
	ds.owner[set] = pool
	return set, nil
}

// poolFull reports the two results that mean the pool, not the device,
// ran out.
func poolFull(err error) bool {
	var ve *vk.Error
	return errors.As(err, &ve) && (ve.Result == vk.VK_ERROR_OUT_OF_POOL_MEMORY || ve.Result == vk.VK_ERROR_FRAGMENTED_POOL)
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

// Free returns a set to the pool it came from.
func (ds *DescriptorSets) Free(set vk.VkDescriptorSet) {
	pool, ok := ds.owner[set]
	if !ok {
		return
	}
	delete(ds.owner, set)
	vk.VkFreeDescriptorSets(ds.dev.Handle, pool, 1, &set)
}

func (ds *DescriptorSets) Destroy() {
	for _, pool := range ds.pools {
		vk.VkDestroyDescriptorPool(ds.dev.Handle, pool, nil)
	}
	ds.pools = nil
	clear(ds.owner)
	vk.VkDestroyDescriptorSetLayout(ds.dev.Handle, ds.Layout, nil)
}
