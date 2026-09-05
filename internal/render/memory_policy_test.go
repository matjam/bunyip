package render

import (
	"strings"
	"testing"

	"github.com/matjam/bunyip/internal/vk"
)

// These tests replace Vulkan entry points and must stay serial.
func TestIndexBufferAllocationPolicy(t *testing.T) {
	for _, tc := range []struct {
		name        string
		driver      vk.VkDriverId
		portability bool
		usage       vk.VkBufferUsageFlags
		dedicated   bool
	}{
		{"MoltenVK index", vk.VK_DRIVER_ID_MOLTENVK, true, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT, true},
		{"MoltenVK index and vertex", vk.VK_DRIVER_ID_MOLTENVK, true, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT | vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT, true},
		{"MoltenVK vertex", vk.VK_DRIVER_ID_MOLTENVK, true, vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT, false},
		{"MoltenVK staging", vk.VK_DRIVER_ID_MOLTENVK, true, vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT, false},
		{"other portability driver", 0, true, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT, false},
		{"non-portability driver", 0, false, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT, false},
		{"MoltenVK without portability", vk.VK_DRIVER_ID_MOLTENVK, false, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, calls := mockBufferMemory(t, tc.driver, tc.portability)
			first, err := d.NewBuffer(3500, tc.usage, vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)
			if err != nil {
				t.Fatal(err)
			}
			second, err := d.NewBuffer(3500, tc.usage, vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)
			if err != nil {
				first.Destroy()
				t.Fatal(err)
			}
			if first.Size != 3500 || second.Size != 3500 || calls.created[0] != 3500 || calls.created[1] != 3500 {
				t.Fatal("allocation policy changed the requested buffer size")
			}
			if len(calls.bound) != 2 || calls.bound[0].memory != first.mem.Memory || calls.bound[0].offset != first.mem.Offset || calls.bound[1].memory != second.mem.Memory || calls.bound[1].offset != second.mem.Offset {
				t.Fatal("vkBindBufferMemory did not receive the allocator's memory and offsets")
			}
			if tc.dedicated {
				if len(calls.allocated) != 2 || calls.allocated[0] != 4096 || calls.allocated[1] != 4096 {
					t.Fatalf("allocations %v; want two exact 4096-byte requirements", calls.allocated)
				}
				if first.mem.Memory == second.mem.Memory || first.mem.Offset != 0 || second.mem.Offset != 0 {
					t.Fatal("index buffers must use distinct allocations at offset zero")
				}
				if d.Stats().Dedicated != 2 || d.Stats().Reserved != 8192 {
					t.Fatalf("incorrect dedicated accounting: %+v", d.Stats())
				}
			} else {
				if len(calls.allocated) != 1 || calls.allocated[0] != blockSize {
					t.Fatalf("allocations %v; want one shared block", calls.allocated)
				}
				if first.mem.Memory != second.mem.Memory || first.mem.Offset != 0 || second.mem.Offset != 4096 {
					t.Fatal("ordinary buffers must keep sharing aligned spans")
				}
			}
			first.Destroy()
			second.Destroy()
			if d.Stats().Live != 0 || d.Stats().Used != 0 || d.Stats().Dedicated != 0 {
				t.Fatalf("buffer cleanup leaked allocations: %+v", d.Stats())
			}
			if tc.dedicated && calls.freed != 2 {
				t.Fatalf("freed %d allocations, want 2", calls.freed)
			}
		})
	}
}

func TestDeviceMemoryAllocationLimit(t *testing.T) {
	d, calls := mockBufferMemory(t, vk.VK_DRIVER_ID_MOLTENVK, true)
	d.gpu.props.Limits.MaxMemoryAllocationCount = 2
	// One pooled block and one index allocation together consume the limit.
	vertex, err := d.NewBuffer(3500, vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT, vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)
	if err != nil {
		t.Fatal(err)
	}
	defer vertex.Destroy()
	index, err := d.NewBuffer(3500, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT, vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.NewBuffer(3500, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT, vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT); err == nil || !strings.Contains(err.Error(), "allocation limit reached (2)") {
		t.Fatalf("limit error = %v", err)
	}
	if len(calls.allocated) != 2 || calls.destroyed != 1 {
		t.Fatalf("limit must avoid vkAllocateMemory and destroy the unbound buffer: %+v", calls)
	}
	index.Destroy()
	replacement, err := d.NewBuffer(3500, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT, vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)
	if err != nil {
		t.Fatalf("released allocation did not restore capacity: %v", err)
	}
	replacement.Destroy()
}

func TestIndexBufferAllocationFailures(t *testing.T) {
	for _, bindFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "allocate", true: "bind"}[bindFailure], func(t *testing.T) {
			d, calls := mockBufferMemory(t, vk.VK_DRIVER_ID_MOLTENVK, true)
			calls.failAllocate, calls.failBind = !bindFailure, bindFailure
			if _, err := d.NewBuffer(3500, vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT, vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT); err == nil {
				t.Fatal("driver failure was lost")
			}
			if d.Stats() != (AllocStats{}) || calls.destroyed != 1 {
				t.Fatalf("failed buffer leaked: stats=%+v, destroyed=%d", d.Stats(), calls.destroyed)
			}
			if bindFailure && calls.freed != 1 {
				t.Fatalf("failed binding freed %d allocations, want 1", calls.freed)
			}
		})
	}
}

type bufferMemoryCalls struct {
	created, allocated     []vk.VkDeviceSize
	bound                  []bufferMemoryBinding
	freed, destroyed       int
	failAllocate, failBind bool
}

type bufferMemoryBinding struct {
	memory vk.VkDeviceMemory
	offset vk.VkDeviceSize
}

func mockBufferMemory(t *testing.T, driver vk.VkDriverId, portability bool) (*Device, *bufferMemoryCalls) {
	t.Helper()
	create, requirements, bind := vk.VkCreateBuffer, vk.VkGetBufferMemoryRequirements, vk.VkBindBufferMemory
	allocate, free, destroy := vk.VkAllocateMemory, vk.VkFreeMemory, vk.VkDestroyBuffer
	t.Cleanup(func() {
		vk.VkCreateBuffer, vk.VkGetBufferMemoryRequirements, vk.VkBindBufferMemory = create, requirements, bind
		vk.VkAllocateMemory, vk.VkFreeMemory, vk.VkDestroyBuffer = allocate, free, destroy
	})
	calls := new(bufferMemoryCalls)
	vk.VkCreateBuffer = func(_ vk.VkDevice, info *vk.VkBufferCreateInfo, _ *vk.VkAllocationCallbacks, out *vk.VkBuffer) vk.VkResult {
		calls.created = append(calls.created, info.Size)
		*out = vk.VkBuffer(len(calls.created))
		return vk.VK_SUCCESS
	}
	vk.VkGetBufferMemoryRequirements = func(_ vk.VkDevice, _ vk.VkBuffer, out *vk.VkMemoryRequirements) {
		*out = vk.VkMemoryRequirements{Size: 4096, Alignment: 256, MemoryTypeBits: 1}
	}
	vk.VkAllocateMemory = func(_ vk.VkDevice, info *vk.VkMemoryAllocateInfo, _ *vk.VkAllocationCallbacks, out *vk.VkDeviceMemory) vk.VkResult {
		calls.allocated = append(calls.allocated, info.AllocationSize)
		if calls.failAllocate {
			return vk.VK_ERROR_OUT_OF_DEVICE_MEMORY
		}
		*out = vk.VkDeviceMemory(len(calls.allocated))
		return vk.VK_SUCCESS
	}
	vk.VkBindBufferMemory = func(_ vk.VkDevice, _ vk.VkBuffer, memory vk.VkDeviceMemory, offset vk.VkDeviceSize) vk.VkResult {
		calls.bound = append(calls.bound, bufferMemoryBinding{memory, offset})
		if calls.failBind {
			return vk.VK_ERROR_OUT_OF_DEVICE_MEMORY
		}
		return vk.VK_SUCCESS
	}
	vk.VkFreeMemory = func(_ vk.VkDevice, _ vk.VkDeviceMemory, _ *vk.VkAllocationCallbacks) { calls.freed++ }
	vk.VkDestroyBuffer = func(_ vk.VkDevice, _ vk.VkBuffer, _ *vk.VkAllocationCallbacks) { calls.destroyed++ }
	d := &Device{portability: portability, gpu: &gpu{driverID: driver}}
	d.gpu.props.Limits.MaxMemoryAllocationCount = 128
	d.gpu.memProps.MemoryTypeCount = 1
	d.gpu.memProps.MemoryTypes[0].PropertyFlags = vk.VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT
	d.alloc.dev = d
	t.Cleanup(d.alloc.destroy)
	return d, calls
}
