package gfx

import (
	"sync/atomic"
	"testing"

	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// countGeometryBuffers counts the vertex and index buffers created from
// now until the test ends.
func countGeometryBuffers(t *testing.T) *atomic.Int64 {
	t.Helper()
	var n atomic.Int64
	create := vk.VkCreateBuffer
	vk.VkCreateBuffer = func(device vk.VkDevice, info *vk.VkBufferCreateInfo, alloc *vk.VkAllocationCallbacks, buffer *vk.VkBuffer) vk.VkResult {
		if info.Usage&(vk.VK_BUFFER_USAGE_VERTEX_BUFFER_BIT|vk.VK_BUFFER_USAGE_INDEX_BUFFER_BIT) != 0 && info.Usage&vk.VK_BUFFER_USAGE_TRANSFER_DST_BIT != 0 {
			n.Add(1)
		}
		return create(device, info, alloc, buffer)
	}
	t.Cleanup(func() { vk.VkCreateBuffer = create })
	return &n
}

// sideQuad is a quad on the left or the right half of the test view.
func sideQuad(right bool, jitter float32) ([]Vertex, []uint32) {
	x := float32(-1.1)
	if right {
		x = 0.3
	}
	verts, idx := QuadMesh()
	for i := range verts {
		p := verts[i].Pos
		// QuadMesh is a unit square in the XY plane; place and size it.
		verts[i].Pos = lin.V3(x+(p.X+0.5)*0.8+jitter, p.Y*0.8, 0)
	}
	return verts, idx
}

// TestMeshUpdateReusesBuffers updates a mesh a thousand times, inside a
// frame as a soft body does and between frames as a game's Update does,
// and checks that once the pool has a few buffers no update creates
// another, and that the last frame shows the last geometry.
func TestMeshUpdateReusesBuffers(t *testing.T) {
	for _, inFrame := range []bool{true, false} {
		name := "between_frames"
		if inFrame {
			name = "in_frame"
		}
		t.Run(name, func(t *testing.T) {
			g := newHeadless(t, 64, 64)
			verts, idx := sideQuad(false, 0)
			mesh, err := g.NewMesh(verts, idx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(mesh.Destroy)
			g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
			const updates, warmup = 1000, 10
			var created *atomic.Int64
			var last []Vertex
			for i := range updates {
				if i == warmup {
					created = countGeometryBuffers(t)
				}
				// Alternate sides, and move a little each time, so every
				// update writes different bytes.
				right := i%2 == 1
				v, ix := sideQuad(right, float32(i%7)*0.001)
				last = v
				if !inFrame {
					if err := mesh.Update(v, ix); err != nil {
						t.Fatal(err)
					}
				}
				if ok, err := g.begin(Black); err != nil || !ok {
					t.Fatal(ok, err)
				}
				if inFrame {
					if err := mesh.Update(v, ix); err != nil {
						t.Fatal(err)
					}
				}
				g.SetCamera(Camera{Position: lin.V3(0, 0, 3), Target: lin.V3(0, 0, 0)})
				g.SetLight(Light{Direction: lin.V3(0, 0, -1), Color: White})
				g.DrawMesh(mesh, Material{BaseColor: White, Unlit: true, DoubleSided: true}, lin.Identity())
				img, err := g.end(i == updates-1)
				if err != nil {
					t.Fatal(err)
				}
				if img == nil {
					continue
				}
				// The last update put the quad on the right.
				if !bright(img, 48, 32) || bright(img, 12, 32) {
					t.Errorf("last frame: right %v, left %v; want only the right lit", img.RGBAAt(48, 32), img.RGBAAt(12, 32))
				}
			}
			if n := created.Load(); n != 0 {
				t.Errorf("%d updates after warm-up created %d vertex or index buffers, want 0", updates-warmup, n)
			}
			if got := mesh.Vertices(); len(got) != len(last) || got[0].Pos != last[0].Pos {
				t.Errorf("kept positions do not follow the last update")
			}
		})
	}
}
