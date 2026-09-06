package render

import (
	"errors"

	"github.com/matjam/bunyip/internal/vk"
)

// DescriptorSets hands out sets of a fixed layout of image and sampler
// bindings. Sets come from a chain of pools: when the current pool is
// full another of the same capacity is added, so the number of live
// textures, materials and render textures is bounded by memory rather
// than by a starting guess.
type DescriptorSets struct {
	Layout   vk.VkDescriptorSetLayout
	binds    []DescriptorBinding
	paired   bool // callers supply one logical entry per image/sampler pair
	pools    []vk.VkDescriptorPool
	capacity uint32
	owner    map[vk.VkDescriptorSet]vk.VkDescriptorPool // which pool each live set came from
	dev      *Device
}

// DescriptorBinding declares one binding of a set layout.
type DescriptorBinding struct {
	Type vk.VkDescriptorType
	// Count is how many descriptors the binding holds; zero means one.
	Count uint32
	// Stages is which shader stages see it; zero means the fragment
	// stage alone.
	Stages vk.VkShaderStageFlags
	// Immutable bakes samplers into the layout, so no set ever writes
	// them. MoltenVK requires it for comparison samplers, and the mesh
	// material set uses it for its shared samplers.
	Immutable []vk.VkSampler
}

// count is the binding's descriptor count, one by default.
func (b DescriptorBinding) count() uint32 { return max(b.Count, 1) }

// stages is the binding's stage mask, the fragment stage by default.
func (b DescriptorBinding) stages() vk.VkShaderStageFlags {
	if b.Stages == 0 {
		return vk.VK_SHADER_STAGE_FRAGMENT_BIT
	}
	return b.Stages
}

// NewTextureDescriptors creates one image/sampler pair for the 2D path.
func (d *Device) NewTextureDescriptors(capacity uint32) (*DescriptorSets, error) {
	return d.NewSamplerDescriptors(1, capacity)
}

// NewSamplerDescriptors creates a layout with bindings image/sampler pairs
// and a first pool for capacity sets; more pools follow as needed. Logical
// texture N occupies sampled-image binding 2N and sampler binding 2N+1.
func (d *Device) NewSamplerDescriptors(bindings int, capacity uint32) (*DescriptorSets, error) {
	return d.newSamplerDescriptors(bindings, capacity, 0)
}

// NewImmutableSamplerDescriptors is NewSamplerDescriptors with the sampler
// baked into the layout, which MoltenVK requires for comparison samplers.
func (d *Device) NewImmutableSamplerDescriptors(bindings int, capacity uint32, sampler vk.VkSampler) (*DescriptorSets, error) {
	return d.newSamplerDescriptors(bindings, capacity, sampler)
}

func (d *Device) newSamplerDescriptors(bindings int, capacity uint32, immutable vk.VkSampler) (*DescriptorSets, error) {
	binds := make([]DescriptorBinding, bindings*2)
	for i := range bindings {
		binds[2*i] = DescriptorBinding{Type: vk.VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE}
		binds[2*i+1] = DescriptorBinding{Type: vk.VK_DESCRIPTOR_TYPE_SAMPLER}
		if immutable != 0 {
			binds[2*i+1].Immutable = []vk.VkSampler{immutable}
		}
	}
	ds, err := d.NewDescriptors(binds, capacity)
	if err != nil {
		return nil, err
	}
	ds.paired = true
	return ds, nil
}

// NewDescriptors creates a layout from explicit bindings, numbered from
// zero in order, and a first pool for capacity sets; more pools follow
// as needed. Update writes the bindings that are not immutable samplers.
func (d *Device) NewDescriptors(bindings []DescriptorBinding, capacity uint32) (*DescriptorSets, error) {
	ds := &DescriptorSets{dev: d, binds: bindings,
		capacity: max(capacity, 1), owner: map[vk.VkDescriptorSet]vk.VkDescriptorPool{}}
	layoutBindings := make([]vk.VkDescriptorSetLayoutBinding, len(bindings))
	for i, b := range bindings {
		layoutBindings[i] = vk.VkDescriptorSetLayoutBinding{
			Binding:         uint32(i),
			DescriptorType:  b.Type,
			DescriptorCount: b.count(),
			StageFlags:      b.stages(),
		}
		if len(b.Immutable) > 0 {
			layoutBindings[i].PImmutableSamplers = &b.Immutable[0]
		}
	}
	layoutInfo := vk.VkDescriptorSetLayoutCreateInfo{SType: vk.VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, BindingCount: uint32(len(bindings)), PBindings: &layoutBindings[0]}
	if err := vk.Check("vkCreateDescriptorSetLayout", vk.VkCreateDescriptorSetLayout(d.Handle, &layoutInfo, nil, &ds.Layout)); err != nil {
		return nil, err
	}
	if err := ds.addPool(); err != nil {
		vk.VkDestroyDescriptorSetLayout(d.Handle, ds.Layout, nil)
		return nil, err
	}
	return ds, nil
}

// addPool appends a pool of the standard capacity, with room for every
// binding's descriptors in every set it holds.
func (ds *DescriptorSets) addPool() error {
	counts := map[vk.VkDescriptorType]uint32{}
	for _, b := range ds.binds {
		counts[b.Type] += ds.capacity * b.count()
	}
	sizes := make([]vk.VkDescriptorPoolSize, 0, len(counts))
	for t, n := range counts {
		sizes = append(sizes, vk.VkDescriptorPoolSize{Type: t, DescriptorCount: n})
	}
	poolInfo := vk.VkDescriptorPoolCreateInfo{
		SType:         vk.VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO,
		Flags:         vk.VK_DESCRIPTOR_POOL_CREATE_FREE_DESCRIPTOR_SET_BIT,
		MaxSets:       ds.capacity,
		PoolSizeCount: uint32(len(sizes)),
		PPoolSizes:    &sizes[0],
	}
	var pool vk.VkDescriptorPool
	if err := vk.Check("vkCreateDescriptorPool", vk.VkCreateDescriptorPool(ds.dev.Handle, &poolInfo, nil, &pool)); err != nil {
		return err
	}
	ds.pools = append(ds.pools, pool)
	return nil
}

// SamplerBinding supplies an image and sampler for one logical texture in
// a NewSamplerDescriptors layout. In an explicit NewDescriptors layout it
// supplies one actual binding: sampled images use View, samplers use Sampler,
// and combined image samplers use both. Layout applies only to the image.
type SamplerBinding struct {
	View    vk.VkImageView
	Sampler vk.VkSampler
	Layout  vk.VkImageLayout // zero means shader-read-only
}

// Allocate makes a set pointing at view through sampler (one logical texture).
func (ds *DescriptorSets) Allocate(view vk.VkImageView, sampler vk.VkSampler) (vk.VkDescriptorSet, error) {
	return ds.AllocateMany([]SamplerBinding{{View: view, Sampler: sampler}})
}

// AllocateMany makes a set, adding a pool when the current one is full.
// Entries name logical texture pairs for NewSamplerDescriptors layouts,
// or actual bindings for NewDescriptors layouts.
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

// AllocateBuffer makes a set whose one binding is the whole of a buffer,
// for a layout of a single storage buffer made with NewDescriptors. It is
// what data that never changes after it is uploaded is bound through:
// one set per buffer, freed with the buffer, rather than one per frame in
// flight.
func (ds *DescriptorSets) AllocateBuffer(buf *Buffer, size vk.VkDeviceSize) (vk.VkDescriptorSet, error) {
	set, err := ds.allocate(ds.pools[len(ds.pools)-1])
	if poolFull(err) {
		if err = ds.addPool(); err == nil {
			set, err = ds.allocate(ds.pools[len(ds.pools)-1])
		}
	}
	if err != nil {
		return 0, err
	}
	info := vk.VkDescriptorBufferInfo{Buffer: buf.Handle, Range: size}
	write := vk.VkWriteDescriptorSet{
		SType: vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, DstSet: set, DescriptorCount: 1,
		DescriptorType: vk.VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, PBufferInfo: &info,
	}
	vk.VkUpdateDescriptorSets(ds.dev.Handle, 1, &write, 0, nil)
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

// Update rewrites a set's bindings, starting at zero. Entries name logical
// texture pairs for NewSamplerDescriptors layouts, or actual bindings for
// NewDescriptors layouts.
// Bindings past the entries given, and bindings of immutable samplers,
// are left alone. A sampled-image binding takes the entry's view and
// ignores its sampler.
func (ds *DescriptorSets) Update(set vk.VkDescriptorSet, bindings []SamplerBinding) {
	count := len(bindings)
	if ds.paired {
		count *= 2
	}
	infos := make([]vk.VkDescriptorImageInfo, count)
	writes := make([]vk.VkWriteDescriptorSet, 0, count)
	for i := range count {
		entry := i
		if ds.paired {
			entry /= 2
		}
		b := bindings[entry]
		kind := ds.binds[i].Type
		if len(ds.binds[i].Immutable) > 0 && kind == vk.VK_DESCRIPTOR_TYPE_SAMPLER {
			continue
		}
		layout := b.Layout
		if layout == 0 {
			layout = vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL
		}
		sampler := b.Sampler
		if kind == vk.VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE {
			sampler = 0
		}
		view := b.View
		if kind == vk.VK_DESCRIPTOR_TYPE_SAMPLER {
			view, layout = 0, 0
		}
		infos[i] = vk.VkDescriptorImageInfo{Sampler: sampler, ImageView: view, ImageLayout: layout}
		writes = append(writes, vk.VkWriteDescriptorSet{
			SType:           vk.VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET,
			DstSet:          set,
			DstBinding:      uint32(i),
			DescriptorCount: 1,
			DescriptorType:  kind,
			PImageInfo:      &infos[i],
		})
	}
	if len(writes) == 0 {
		return
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
