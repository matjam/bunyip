package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// TestFrame3DAllocs records a settled 3D frame with cascades, shadowed
// spot and point lights, a static batch, moved draws and the GPU timers
// running, and checks that it allocates nothing: the draw queue, the
// material table, the shadow lists, the instance stream and the Vulkan
// calls all reuse what earlier frames grew.
func TestFrame3DAllocs(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector's instrumentation allocates; counted in the ordinary run")
	}
	g := newHeadless(t, 64, 64)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	tex, err := g.NewBlankTexture(2, 2, TextureOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tex.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, TemporalAA: true})
	mats := []Material{
		{Texture: tex, Roughness: 0.5},
		{BaseColor: RGB(200, 40, 40)},
		{BaseColor: Color{0.2, 0.4, 0.9, 0.5}, Blend: true},
	}
	var items []BatchItem
	for i := range 64 {
		items = append(items, BatchItem{Mesh: cube, Material: mats[i%2], Model: lin.Translate(lin.V3(float32(i%8)*3-12, 0, float32(i/8)*3-30))})
	}
	batch := g.NewStaticBatch(items)
	frame := func() {
		ok, err := g.begin(Black)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		g.SetCamera(Camera{Position: lin.V3(0, 6, 12), Target: lin.V3(0, 0, -6)})
		g.SetLight(Light{Direction: lin.V3(-0.4, -1, -0.3), Color: White, Shadows: true})
		g.AddSpot(SpotLight{Position: lin.V3(0, 6, 0), Direction: lin.V3(0, -1, 0), Color: White, Range: 20, OuterAngle: 1, Shadows: true})
		g.AddPoint(PointLight{Position: lin.V3(3, 2, -3), Color: White, Range: 10, Shadows: true})
		g.AddPoint(PointLight{Position: lin.V3(-3, 1, 2), Color: White, Range: 5})
		for i := range 200 {
			m := lin.Translate(lin.V3(float32(i%20)-10, 0.5, -float32(i/20)*2))
			g.DrawMeshMoved(cube, mats[i%len(mats)], m, lin.Translate(lin.V3(0.01, 0, 0)).Mul(m))
		}
		g.DrawBatch(batch)
		if _, err := g.end(false); err != nil {
			t.Fatal(err)
		}
	}
	for range 8 {
		frame()
	}
	if n := testing.AllocsPerRun(20, frame); n != 0 {
		t.Errorf("a settled 3D frame allocates %v times, want 0", n)
	}
}
