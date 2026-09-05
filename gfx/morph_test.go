package gfx

import (
	"image"
	"testing"

	"github.com/matjam/bunyip/gltf"
	"github.com/matjam/bunyip/lin"
)

// morphGridDoc builds a grid of quads with n morph targets, each of which
// lifts one column of vertices. Every target moves normals as well as
// positions, so the two blend paths have both to agree on.
func morphGridDoc(n int) *gltf.Document {
	const cols, rows = 9, 9
	var prim gltf.Primitive
	prim.Material = -1
	for z := range rows {
		for x := range cols {
			fx, fz := float32(x)/(cols-1)-0.5, float32(z)/(rows-1)-0.5
			prim.Positions = append(prim.Positions, lin.V3(fx*2, 0, fz*2))
			prim.Normals = append(prim.Normals, lin.V3(0, 1, 0))
			prim.UVs = append(prim.UVs, lin.V2(fx+0.5, fz+0.5))
		}
	}
	for z := range rows - 1 {
		for x := range cols - 1 {
			a := uint32(z*cols + x)
			b, c, d := a+1, a+cols, a+cols+1
			prim.Indices = append(prim.Indices, a, d, b, a, c, d)
		}
	}
	for k := range n {
		var t gltf.MorphTarget
		t.Positions = make([]lin.Vec3, len(prim.Positions))
		t.Normals = make([]lin.Vec3, len(prim.Positions))
		for i := range t.Positions {
			if i%cols != k%cols {
				continue
			}
			t.Positions[i] = lin.V3(0, 0.5, 0)
			t.Normals[i] = lin.V3(0.2, -0.2, 0)
		}
		prim.Targets = append(prim.Targets, t)
	}
	doc := &gltf.Document{
		Meshes:    []gltf.Mesh{{Name: "grid", Primitives: []gltf.Primitive{prim}}},
		Nodes:     []gltf.Node{{Name: "grid", Parent: -1, Rotation: lin.QuatIdentity(), Scale: lin.V3(1, 1, 1), Mesh: 0, Skin: -1}},
		Instances: []gltf.Instance{{Name: "grid", Mesh: 0, Node: 0, Skin: -1, World: lin.Identity()}},
	}
	return doc
}

// morphScene renders the grid under a weight per target.
func morphScene(t *testing.T, g *Graphics, m *Model, weights []float32) *image.RGBA {
	t.Helper()
	if err := m.SetMorphWeights(0, weights); err != nil {
		t.Fatal(err)
	}
	return frames(t, g, func() {
		g.SetCamera(Camera{Position: lin.V3(0, 1.6, 2.4), Target: lin.V3(0, 0.2, 0)})
		g.SetLight(Light{Direction: lin.V3(-0.4, -1, -0.3), Color: Color{2, 2, 2, 1},
			Sky: Sky{Zenith: Color{0.2, 0.25, 0.35, 1}, Ground: Color{0.1, 0.1, 0.1, 1}}})
		g.DrawModel(m, lin.Identity())
	})
}

// TestMorphOnTheGPUMatchesTheProcessor drives one shape at one set of
// weights through both blends and compares the pixels: the vertex
// shader's, and the same mesh with its deltas taken off the device so
// only the processor's path is left. They shade from the same positions
// and normals, so the frames must agree.
func TestMorphOnTheGPUMatchesTheProcessor(t *testing.T) {
	g := newHeadless(t, 96, 96)
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	weights := []float32{0.9, 0.1, 0.7, 0.3, 0.5, 0.4, 0.6, 0.2}
	m, err := g.LoadModel(morphGridDoc(8))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Destroy()
	mm := m.morphs[0]

	onGPU := morphScene(t, g, m, weights)
	if len(mm.active) != len(weights) {
		t.Fatalf("the shader path took %d targets, want %d", len(mm.active), len(weights))
	}
	if mm.blended {
		t.Fatal("the shader path uploaded a blend")
	}

	mm.gpuBase = -1 // the deltas are unreachable, so the processor blends
	onCPU := morphScene(t, g, m, weights)
	if len(mm.active) != 0 || !mm.blended {
		t.Fatal("with no deltas on the device the processor should have blended")
	}
	// A handful of edge pixels can land either side of a triangle when
	// the same position arrives by two routes, so a few are allowed.
	if diff := imageDiff(onGPU, onCPU); diff > 96*96/50 {
		t.Errorf("the two blends differ in %d pixels of %d", diff, 96*96)
	}
}

// TestMorphFallsBackAndReturns walks a mesh over the cap and back, which
// is the case that has to put the rest pose back before the shader adds
// its deltas to it.
func TestMorphFallsBackAndReturns(t *testing.T) {
	g := newHeadless(t, 64, 64)
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	m, err := g.LoadModel(morphGridDoc(9))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Destroy()
	mm := m.morphs[0]
	rest := m.Parts[0].Mesh.Vertices()[0].Pos

	few := make([]float32, 9)
	few[0] = 1
	first := morphScene(t, g, m, few)
	if len(mm.active) != 1 || mm.blended {
		t.Fatalf("one open target should blend in the shader: %d active, blended %v", len(mm.active), mm.blended)
	}

	all := make([]float32, 9)
	for i := range all {
		all[i] = 0.5
	}
	morphScene(t, g, m, all)
	if len(mm.active) != 0 || !mm.blended {
		t.Fatal("nine open targets should blend on the processor")
	}
	if m.Parts[0].Mesh.Vertices()[0].Pos == rest {
		t.Fatal("the processor's path should have uploaded a blended vertex")
	}

	// Back under the cap: the rest pose must be uploaded again, or the
	// shader adds its deltas to a shape that already has them.
	again := morphScene(t, g, m, few)
	if len(mm.active) != 1 || mm.blended {
		t.Fatalf("back under the cap: %d active, blended %v", len(mm.active), mm.blended)
	}
	if got := m.Parts[0].Mesh.Vertices()[0].Pos; got != rest {
		t.Fatalf("the rest pose was not put back: %v, want %v", got, rest)
	}
	if diff := imageDiff(first, again); diff != 0 {
		t.Errorf("the same weights drew %d pixels differently after the round trip", diff)
	}
}
