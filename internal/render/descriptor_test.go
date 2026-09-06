package render

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/matjam/bunyip/internal/vk"
)

// Inspect native calls while their pointer arguments are live. These tests
// replace shared entry points and must not run in parallel.
func TestDescriptorTexturePairs(t *testing.T) {
	oldLayout, oldPool := vk.VkCreateDescriptorSetLayout, vk.VkCreateDescriptorPool
	oldAllocate, oldUpdate := vk.VkAllocateDescriptorSets, vk.VkUpdateDescriptorSets
	t.Cleanup(func() {
		vk.VkCreateDescriptorSetLayout, vk.VkCreateDescriptorPool = oldLayout, oldPool
		vk.VkAllocateDescriptorSets, vk.VkUpdateDescriptorSets = oldAllocate, oldUpdate
	})
	for _, immutable := range []vk.VkSampler{0, 77} {
		t.Run(map[bool]string{true: "immutable comparison", false: "mutable"}[immutable != 0], func(t *testing.T) {
			vk.VkCreateDescriptorSetLayout = func(_ vk.VkDevice, info *vk.VkDescriptorSetLayoutCreateInfo, _ *vk.VkAllocationCallbacks, out *vk.VkDescriptorSetLayout) vk.VkResult {
				if info.BindingCount != 4 {
					t.Fatalf("layout has %d bindings, want two pairs", info.BindingCount)
				}
				for i, b := range unsafe.Slice(info.PBindings, info.BindingCount) {
					kind := vk.VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE
					if i%2 != 0 {
						kind = vk.VK_DESCRIPTOR_TYPE_SAMPLER
					}
					if b.Binding != uint32(i) || b.DescriptorType != kind || b.DescriptorCount != 1 || b.StageFlags != vk.VK_SHADER_STAGE_FRAGMENT_BIT {
						t.Fatalf("binding %d: %+v", i, b)
					}
					if immutable != 0 && i%2 != 0 {
						if b.PImmutableSamplers == nil || *b.PImmutableSamplers != immutable {
							t.Fatalf("sampler %d is not immutable", i)
						}
					} else if b.PImmutableSamplers != nil {
						t.Fatalf("unexpected immutable sampler at %d", i)
					}
				}
				*out = 1
				return vk.VK_SUCCESS
			}
			vk.VkCreateDescriptorPool = func(_ vk.VkDevice, info *vk.VkDescriptorPoolCreateInfo, _ *vk.VkAllocationCallbacks, out *vk.VkDescriptorPool) vk.VkResult {
				counts := map[vk.VkDescriptorType]uint32{}
				for _, size := range unsafe.Slice(info.PPoolSizes, info.PoolSizeCount) {
					counts[size.Type] = size.DescriptorCount
				}
				if !reflect.DeepEqual(counts, map[vk.VkDescriptorType]uint32{vk.VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE: 6, vk.VK_DESCRIPTOR_TYPE_SAMPLER: 6}) || info.MaxSets != 3 {
					t.Fatalf("pool capacity: %+v, sets %d", counts, info.MaxSets)
				}
				*out = 2
				return vk.VK_SUCCESS
			}
			vk.VkAllocateDescriptorSets = func(_ vk.VkDevice, info *vk.VkDescriptorSetAllocateInfo, out *vk.VkDescriptorSet) vk.VkResult {
				if info.DescriptorPool != 2 || *info.PSetLayouts != 1 {
					t.Fatal("allocation lost layout/pool")
				}
				*out = 3
				return vk.VK_SUCCESS
			}
			type write struct {
				binding uint32
				kind    vk.VkDescriptorType
				info    vk.VkDescriptorImageInfo
			}
			var got []write
			vk.VkUpdateDescriptorSets = func(_ vk.VkDevice, count uint32, writes *vk.VkWriteDescriptorSet, _ uint32, _ *vk.VkCopyDescriptorSet) {
				got = nil
				for _, w := range unsafe.Slice(writes, count) {
					if w.DstSet != 3 || w.DescriptorCount != 1 {
						t.Fatalf("bad write: %+v", w)
					}
					got = append(got, write{w.DstBinding, w.DescriptorType, *w.PImageInfo})
				}
			}
			ds, err := (&Device{}).newSamplerDescriptors(2, 3, immutable)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ds.AllocateMany([]SamplerBinding{{View: 11, Sampler: 21}, {View: 12, Sampler: 22, Layout: vk.VK_IMAGE_LAYOUT_DEPTH_STENCIL_READ_ONLY_OPTIMAL}})
			if err != nil {
				t.Fatal(err)
			}
			want := []write{
				{0, vk.VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE, vk.VkDescriptorImageInfo{ImageView: 11, ImageLayout: vk.VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL}},
				{2, vk.VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE, vk.VkDescriptorImageInfo{ImageView: 12, ImageLayout: vk.VK_IMAGE_LAYOUT_DEPTH_STENCIL_READ_ONLY_OPTIMAL}},
			}
			if immutable == 0 {
				want = []write{want[0], {1, vk.VK_DESCRIPTOR_TYPE_SAMPLER, vk.VkDescriptorImageInfo{Sampler: 21}}, want[1], {3, vk.VK_DESCRIPTOR_TYPE_SAMPLER, vk.VkDescriptorImageInfo{Sampler: 22}}}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("writes = %+v, want %+v", got, want)
			}
			// A partial update changes only the first logical pair.
			ds.Update(3, []SamplerBinding{{View: 11, Sampler: 21}})
			n := 1
			if immutable == 0 {
				n = 2
			}
			if !reflect.DeepEqual(got, want[:n]) {
				t.Fatalf("partial writes = %+v", got)
			}
		})
	}
}

func TestDescriptorExplicitBindings(t *testing.T) {
	old := vk.VkUpdateDescriptorSets
	t.Cleanup(func() { vk.VkUpdateDescriptorSets = old })
	ds := &DescriptorSets{dev: &Device{}, binds: []DescriptorBinding{
		{Type: vk.VK_DESCRIPTOR_TYPE_SAMPLED_IMAGE},
		{Type: vk.VK_DESCRIPTOR_TYPE_SAMPLER, Immutable: []vk.VkSampler{7}},
		{Type: vk.VK_DESCRIPTOR_TYPE_SAMPLER},
	}}
	called := false
	vk.VkUpdateDescriptorSets = func(_ vk.VkDevice, count uint32, writes *vk.VkWriteDescriptorSet, _ uint32, _ *vk.VkCopyDescriptorSet) {
		called = true
		got := unsafe.Slice(writes, count)
		if len(got) != 2 || got[0].DstBinding != 0 || got[1].DstBinding != 2 || got[0].PImageInfo.ImageView != 11 || got[0].PImageInfo.Sampler != 0 || got[1].PImageInfo.Sampler != 22 || got[1].PImageInfo.ImageView != 0 {
			t.Fatalf("explicit layout was paired or immutable sampler overwritten: %+v", got)
		}
	}
	ds.Update(3, []SamplerBinding{{View: 11, Sampler: 99}, {Sampler: 88}, {View: 99, Sampler: 22}})
	if !called {
		t.Fatal("no writes")
	}
	called = false
	ds.Update(3, nil)
	if called {
		t.Fatal("empty update made native call")
	}
}
