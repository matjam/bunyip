package gfx

import (
	"bytes"
	"testing"

	"github.com/matjam/bunyip/internal/render"
	"github.com/matjam/bunyip/internal/vk"
	"github.com/matjam/bunyip/lin"
)

// captureReference renders the faces from pos the way bakes did before
// they were batched: one queue, one submission and one readback a face,
// each waited for.
func captureReference(t *testing.T, g *Graphics, size int, scene func(), pos lin.Vec3) *cubeFaces {
	t.Helper()
	extent := vk.VkExtent2D{Width: uint32(size), Height: uint32(size)}
	st, err := g.newSceneTargets(extent, vk.VK_SAMPLE_COUNT_1_BIT)
	if err != nil {
		t.Fatal(err)
	}
	defer st.destroy(g)
	q, err := g.newQueue(float32(size), float32(size))
	if err != nil {
		t.Fatal(err)
	}
	defer q.destroy()
	stats := g.stats
	defer func() { g.stats = stats }()
	prev := g.cur
	g.cur = q
	q.reset()
	scene()
	g.cur = prev
	if err := g.uniforms.Write(0, g.arena.Bytes()); err != nil {
		t.Fatal(err)
	}
	faces := &cubeFaces{side: size}
	for face := range 6 {
		q.camera = faceCamera(pos, face)
		q.hasCam = true
		var inner error
		if err := g.r.Device.OneShot(func(cb vk.VkCommandBuffer) {
			inner = g.renderScene(&render.Frame{CB: cb, Slot: 0, Extent: st.extent}, q, st)
		}); err != nil {
			t.Fatal(err)
		}
		if inner != nil {
			t.Fatal(inner)
		}
		pix, err := g.r.Device.ReadImageRaw(st.hdr.Color, 8)
		if err != nil {
			t.Fatal(err)
		}
		faces.pix[face] = pix
	}
	return faces
}

// TestBatchedBakeMatchesReference bakes a lit, shadowed room from a probe
// and from the cells of a grid with every face of a group in one
// submission, and requires the same face texels as rendering each face
// on its own, so the same cube maps and harmonics come out.
func TestBatchedBakeMatchesReference(t *testing.T) {
	g := newHeadless(t, 32, 32)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	sv, si := SphereMesh(12, 24)
	sphere, err := g.NewMesh(sv, si)
	if err != nil {
		t.Fatal(err)
	}
	defer sphere.Destroy()
	scene := func() {
		g.SetLight(Light{Direction: lin.V3(-0.4, -1, -0.3), Color: Color{1, 0.9, 0.8, 1}, Shadows: true,
			Sky: Sky{Zenith: Color{0.1, 0.2, 0.9, 1}, Horizon: Color{0.8, 0.8, 0.9, 1}}})
		g.AddPointLight(lin.V3(1, 1, 1), Color{0, 1, 0, 1}, 6)
		g.DrawMesh(cube, Material{BaseColor: Color{0.8, 0.2, 0.1, 1}}, lin.Scale(lin.V3(-6, -3, -6)))
		g.DrawMesh(sphere, Material{BaseColor: White, Metallic: 1, Roughness: 0.3}, lin.Translate(lin.V3(2, -1, 0)))
		g.DrawMesh(cube, Material{BaseColor: Color{0.1, 0.9, 0.3, 1}, Emissive: 2}, lin.Translate(lin.V3(-2, 0, 1)))
	}
	const size = 24
	positions := []lin.Vec3{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0.5, Z: -1}, {X: -2, Y: -1, Z: 2}, {X: 3, Y: 1, Z: 0}, {X: 0, Y: 2, Z: 3}}
	for _, together := range []int{1, 4} {
		b, err := g.newBaker(size, together, scene)
		if err != nil {
			t.Fatal(err)
		}
		before := g.r.Device.Waits()
		got, err := b.capture(positions...)
		waits := g.r.Device.Waits() - before
		b.destroy()
		if err != nil {
			t.Fatal(err)
		}
		groups := (len(positions) + together - 1) / together
		if waits != uint64(groups) {
			t.Errorf("%d probes a submission: %d waits for %d probes, want %d", together, waits, len(positions), groups)
		}
		for i, pos := range positions {
			want := captureReference(t, g, size, scene, pos)
			for face := range 6 {
				if !bytes.Equal(got[i].pix[face], want.pix[face]) {
					t.Errorf("%d probes a submission: probe %d face %d differs from the face rendered alone", together, i, face)
				}
			}
			if i == 0 && together == 1 {
				gotSH, wantSH := shProject(got[i].sample, 32, 16), shProject(want.sample, 32, 16)
				if gotSH != wantSH {
					t.Errorf("harmonics %v, want %v", gotSH, wantSH)
				}
				if gotSH[0].X == 0 {
					t.Error("the bake saw nothing")
				}
			}
		}
	}
}
