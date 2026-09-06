package gfx

import (
	"testing"

	"github.com/matjam/bunyip/gfx/shaders"
	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/lin"
)

// TestGrowthWithoutWaits drives every per-frame arena past its size each
// frame: distinct shader uniform blocks grow the dynamic uniform buffer,
// a widening joint array grows the joint storage buffer, and the sprites
// and instances grow the vertex streams. Growth allocates new buffers and
// descriptor sets and retires the old ones through the frame ring, so no
// frame idles the GPU.
func TestGrowthWithoutWaits(t *testing.T) {
	g := newHeadless(t, 64, 64)
	// A sprite shader with a uniform block: every distinct value it is
	// given lands in the frame's arena.
	sh := &Shader{g: g, frag: shaders.SpriteFrag, pipes: map[pipeKey]*render.Pipeline{}}
	if _, err := sh.pipeline(pipeKey{}); err != nil {
		t.Fatalf("shader pipeline: %v", err)
	}
	defer sh.Destroy()
	verts := []SkinVertex{
		{Pos: lin.V3(-1, -1, 0), Normal: lin.V3(0, 0, 1), Weights: [4]float32{1}},
		{Pos: lin.V3(1, -1, 0), Normal: lin.V3(0, 0, 1), Weights: [4]float32{1}},
		{Pos: lin.V3(0, 1, 0), Normal: lin.V3(0, 0, 1), Weights: [4]float32{1}},
	}
	mesh, err := g.NewSkinnedMesh(verts, []uint32{0, 1, 2})
	if err != nil {
		t.Fatalf("NewSkinnedMesh: %v", err)
	}
	defer mesh.Destroy()

	type block struct{ Value lin.Vec4 }
	const frames = 5
	for f := range frames {
		n := 256 * (f + 1)
		ok, err := g.begin(Black)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if !ok {
			continue
		}
		g.SetCamera(Camera{Position: lin.V3(0, 0, 5)})
		g.SetShader(sh)
		for i := range n {
			if err := sh.SetUniforms(block{Value: lin.V4(float32(i), 0, 0, 0)}); err != nil {
				t.Fatal(err)
			}
			g.FillRect(float32(i%64), float32(i%64), 1, 1, White)
		}
		g.SetShader(nil)
		// One draw with a growing joint array, so the storage buffer grows
		// without the instance stream having to.
		joints := make([]lin.Mat4, n)
		for i := range joints {
			joints[i] = lin.Identity()
		}
		g.DrawSkinned(mesh, Material{BaseColor: White}, lin.Identity(), joints)
		if _, err := g.end(false); err != nil {
			t.Fatalf("end: %v", err)
		}
		if w := g.Stats().Waits; w != 0 {
			t.Errorf("frame %d: Stats().Waits = %d, want 0", f, w)
		}
	}
}
