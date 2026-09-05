package gfx

import (
	"bytes"
	"fmt"
	"testing"
	"unsafe"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// TestDenseSphereAllocationRendered holds geometry and buffer sizes fixed
// while selecting the allocator's shared-block or separate-memory path.
func TestDenseSphereAllocationRendered(t *testing.T) {
	for _, dedicated := range []bool{false, true} {
		t.Run(fmt.Sprintf("dedicated=%t", dedicated), func(t *testing.T) {
			g := newHeadless(t, 96, 96)
			geometryAllocationProbe(t, dedicated)
			verts, indices := SphereMesh(32, 64)
			mesh, err := g.NewMesh(verts, indices)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(mesh.Destroy)
			img := renderMaterial(t, g, func() {
				g.DrawMesh(mesh, Material{BaseColor: White, Unlit: true}, lin.Scale(lin.V3(1.3, 1.3, 1.3)))
			})
			if !bright(img, 48, 48) {
				t.Errorf("sphere centre %v, culled %d, draws %d; want visible sphere", img.RGBAAt(48, 48), g.stats.Culled, g.stats.Draws3D)
			}
		})
	}
}

// dedicateGeometryBuffers keeps each geometry VkBuffer unchanged but
// forces its allocation through the separate-memory path, bound at zero.
func dedicateGeometryBuffers(t *testing.T) { geometryAllocationProbe(t, true) }

func geometryAllocationProbe(t *testing.T, dedicated bool) {
	t.Helper()
	create, requirements, bind := vk.VkCreateBuffer, vk.VkGetBufferMemoryRequirements, vk.VkBindBufferMemory
	sizes := make(map[vk.VkBuffer]vk.VkDeviceSize)
	vk.VkCreateBuffer = func(device vk.VkDevice, info *vk.VkBufferCreateInfo, allocator *vk.VkAllocationCallbacks, buffer *vk.VkBuffer) vk.VkResult {
		result := create(device, info, allocator, buffer)
		if result == vk.VK_SUCCESS {
			delete(sizes, *buffer)
			if info.Usage&vk.VK_BUFFER_USAGE_TRANSFER_DST_BIT != 0 && info.Usage&(vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT|vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT) != 0 {
				sizes[*buffer] = info.Size
			}
		}
		return result
	}
	vk.VkGetBufferMemoryRequirements = func(device vk.VkDevice, buffer vk.VkBuffer, req *vk.VkMemoryRequirements) {
		requirements(device, buffer, req)
		if size, ok := sizes[buffer]; ok {
			t.Logf("geometry size=%d required=%d alignment=%d dedicated=%t", size, req.Size, req.Alignment, dedicated)
			if dedicated {
				// This is the allocator's dedicated threshold. Enlarging the
				// allocation is valid; VkBufferCreateInfo.Size stays unchanged.
				req.Size = max(req.Size, 16<<20)
			}
		}
	}
	vk.VkBindBufferMemory = func(device vk.VkDevice, buffer vk.VkBuffer, memory vk.VkDeviceMemory, offset vk.VkDeviceSize) vk.VkResult {
		if size, ok := sizes[buffer]; ok {
			t.Logf("geometry size=%d bound at offset=%d dedicated=%t", size, offset, dedicated)
		}
		return bind(device, buffer, memory, offset)
	}
	t.Cleanup(func() {
		vk.VkCreateBuffer, vk.VkGetBufferMemoryRequirements, vk.VkBindBufferMemory = create, requirements, bind
	})
}

// TestDenseSphereGeometryRendered distinguishes buffer size from indexed
// draw size without changing the geometry visible in the frame.
func TestDenseSphereGeometryRendered(t *testing.T) {
	dense, denseIndices := SphereMesh(32, 64)
	coarse, coarseIndices := SphereMesh(16, 32)
	padded := append(append([]Vertex(nil), coarse...), make([]Vertex, len(dense)-len(coarse))...)
	for _, tc := range []struct {
		name    string
		verts   []Vertex
		indices []uint32
		batch   int
	}{
		{"coarse_padded", padded, coarseIndices, len(coarseIndices)},
		{"dense_whole", dense, denseIndices, len(denseIndices)},
		{"dense_batches", dense, denseIndices, 6 * 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newHeadless(t, 96, 96)
			enableGeometryReadback(t)
			var meshes []*Mesh
			for start := 0; start < len(tc.indices); start += tc.batch {
				mesh, err := g.NewMesh(tc.verts, tc.indices[start:min(start+tc.batch, len(tc.indices))])
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(mesh.Destroy)
				meshes = append(meshes, mesh)
			}
			img := renderMaterial(t, g, func() {
				for _, mesh := range meshes {
					g.DrawMesh(mesh, Material{BaseColor: White, Unlit: true}, lin.Scale(lin.V3(1.3, 1.3, 1.3)))
				}
			})
			covered := 0
			for y := range 96 {
				for x := range 96 {
					if bright(img, x, y) {
						covered++
					}
				}
			}
			if covered < 1000 || !bright(img, 48, 48) {
				t.Errorf("covered %d pixels, centre %v, culled %d, draws %d; want visible sphere", covered, img.RGBAAt(48, 48), g.stats.Culled, g.stats.Draws3D)
			}
			for i, mesh := range meshes {
				packed := make([]gpuVertex, len(mesh.verts))
				for j, v := range mesh.verts {
					packed[j] = v.gpu()
				}
				want := unsafe.Slice((*byte)(unsafe.Pointer(&packed[0])), len(packed)*vertexSize)
				if got := readGeometryBuffer(t, g, mesh.vbuf); !bytes.Equal(got, want) {
					t.Errorf("mesh %d: vertex buffer differs from uploaded bytes", i)
				}
				want = unsafe.Slice((*byte)(unsafe.Pointer(&mesh.indices[0])), len(mesh.indices)*4)
				if got := readGeometryBuffer(t, g, mesh.ibuf); !bytes.Equal(got, want) {
					t.Errorf("mesh %d: index buffer differs from uploaded bytes", i)
				}
			}
		})
	}
}

// TestGeometryUploadBytes verifies both uploads byte for byte on the GPU,
// including dense sphere buffers, independently of the mesh shaders.
func TestGeometryUploadBytes(t *testing.T) {
	verts, indices := SphereMesh(32, 64)
	packed := make([]gpuVertex, len(verts))
	for i, v := range verts {
		packed[i] = v.gpu()
	}
	for _, tc := range []struct {
		name  string
		data  []byte
		usage vk.VkBufferUsageFlags
	}{
		{"vertices", unsafe.Slice((*byte)(unsafe.Pointer(&packed[0])), len(packed)*vertexSize), vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT},
		{"indices", unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*4), vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT},
	} {
		for _, inFrame := range []bool{false, true} {
			mode := "setup"
			if inFrame {
				mode = "in_frame"
			}
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				g := newHeadless(t, 16, 16)
				enableGeometryReadback(t)
				if inFrame {
					if ok, err := g.begin(Black); err != nil || !ok {
						t.Fatalf("begin: %t, %v", ok, err)
					}
				}
				buf, err := g.uploadGeometry(tc.data, tc.usage)
				if err != nil {
					t.Fatal(err)
				}
				defer buf.Destroy()
				if inFrame {
					if _, err := g.end(true); err != nil {
						t.Fatal(err)
					}
				}
				got := readGeometryBuffer(t, g, buf)
				if !bytes.Equal(got, tc.data) {
					for i := range got {
						if got[i] != tc.data[i] {
							t.Fatalf("GPU buffer differs at byte %d of %d: got %#x, want %#x", i, len(got), got[i], tc.data[i])
						}
					}
				}
			})
		}
	}
}

// enableGeometryReadback adds readback capability to the actual buffers
// created by the geometry path, without replacing or moving allocations.
// Call after newHeadless loads the device functions. These tests must stay
// serial because the Vulkan function binding is process-wide.
func enableGeometryReadback(t *testing.T) {
	t.Helper()
	create := vk.VkCreateBuffer
	vk.VkCreateBuffer = func(device vk.VkDevice, info *vk.VkBufferCreateInfo, allocator *vk.VkAllocationCallbacks, buffer *vk.VkBuffer) vk.VkResult {
		copyInfo := *info
		if copyInfo.Usage&(vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT|vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT) != 0 {
			copyInfo.Usage |= vk.VK_BUFFER_USAGE_TRANSFER_SRC_BIT
		}
		return create(device, &copyInfo, allocator, buffer)
	}
	t.Cleanup(func() { vk.VkCreateBuffer = create })
}

func readGeometryBuffer(t *testing.T, g *Graphics, source *render.Buffer) []byte {
	t.Helper()
	d := g.r.Device
	dst, err := d.NewBuffer(source.Size, vk.VK_BUFFER_USAGE_TRANSFER_DST_BIT,
		vk.VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT|vk.VK_MEMORY_PROPERTY_HOST_COHERENT_BIT)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Destroy()
	if err := d.OneShot(func(cb vk.VkCommandBuffer) {
		barrier := vk.VkBufferMemoryBarrier2{
			SType:        vk.VK_STRUCTURE_TYPE_BUFFER_MEMORY_BARRIER_2,
			SrcStageMask: vk.VK_PIPELINE_STAGE_2_ALL_COMMANDS_BIT, SrcAccessMask: vk.VK_ACCESS_2_MEMORY_WRITE_BIT,
			DstStageMask: vk.VK_PIPELINE_STAGE_2_COPY_BIT, DstAccessMask: vk.VK_ACCESS_2_TRANSFER_READ_BIT,
			SrcQueueFamilyIndex: vk.VK_QUEUE_FAMILY_IGNORED, DstQueueFamilyIndex: vk.VK_QUEUE_FAMILY_IGNORED,
			Buffer: source.Handle, Size: source.Size,
		}
		dep := vk.VkDependencyInfo{SType: vk.VK_STRUCTURE_TYPE_DEPENDENCY_INFO, BufferMemoryBarrierCount: 1, PBufferMemoryBarriers: &barrier}
		vk.VkCmdPipelineBarrier2(cb, &dep)
		region := vk.VkBufferCopy{Size: source.Size}
		vk.VkCmdCopyBuffer(cb, source.Handle, dst.Handle, 1, &region)
		barrier.SrcStageMask, barrier.SrcAccessMask = vk.VK_PIPELINE_STAGE_2_COPY_BIT, vk.VK_ACCESS_2_TRANSFER_WRITE_BIT
		barrier.DstStageMask, barrier.DstAccessMask = vk.VK_PIPELINE_STAGE_2_HOST_BIT, vk.VK_ACCESS_2_HOST_READ_BIT
		barrier.Buffer = dst.Handle
		vk.VkCmdPipelineBarrier2(cb, &dep)
	}); err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), dst.Bytes()...)
}
