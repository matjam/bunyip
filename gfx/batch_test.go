package gfx

import (
	"image"
	"math/rand"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// imageDiff counts the pixels where two frames of the same size differ by
// more than a channel step, which absorbs the rounding two draw orders
// can leave behind.
func imageDiff(a, b *image.RGBA) int {
	if a == nil || b == nil || len(a.Pix) != len(b.Pix) {
		return -1
	}
	diff := 0
	for i := 0; i < len(a.Pix); i += 4 {
		for c := range 4 {
			d := int(a.Pix[i+c]) - int(b.Pix[i+c])
			if d < -1 || d > 1 {
				diff++
				break
			}
		}
	}
	return diff
}

// batchScene lays ten thousand cubes over a long strip of ground, most of
// it behind the camera, and returns the items and the camera that sees
// the near end of it.
func batchScene(mesh *Mesh) ([]BatchItem, Camera) {
	r := rand.New(rand.NewSource(11))
	items := make([]BatchItem, 10000)
	for i := range items {
		p := lin.V3((r.Float32()-0.5)*40, 0, -float32(i)*0.4)
		items[i] = BatchItem{
			Mesh:     mesh,
			Material: Material{BaseColor: RGB(200, 180, 140), Roughness: 1},
			Model:    lin.Translate(p).Mul(lin.Scale(lin.V3(0.4, 0.4, 0.4))),
		}
	}
	return items, Camera{Position: lin.V3(0, 6, 14), Target: lin.V3(0, 0, -10), Far: 60}
}

// TestStaticBatch draws ten thousand cubes both ways: queued one by one,
// and through a static batch. The batch reaches the same pixels while
// testing a fraction of the bounding volumes, because whole subtrees
// behind the camera are rejected at a node.
func TestStaticBatch(t *testing.T) {
	g := newHeadless(t, 96, 96)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
	items, cam := batchScene(cube)
	batch := g.NewStaticBatch(items)
	if batch.Len() != len(items) {
		t.Fatalf("batch holds %d items, want %d", batch.Len(), len(items))
	}
	scene := func(draw func()) {
		g.SetCamera(cam)
		g.SetLight(Light{Direction: lin.V3(-0.3, -1, -0.4), Color: White})
		draw()
	}
	one := frames(t, g, func() {
		scene(func() {
			for _, it := range items {
				g.DrawMesh(it.Mesh, it.Material, it.Model)
			}
		})
	})
	oneStats := g.Stats()
	batched := frames(t, g, func() {
		scene(func() { g.DrawBatch(batch) })
	})
	batchStats := g.Stats()

	if oneStats.CullTests != len(items) {
		t.Errorf("one by one ran %d cull tests, want %d", oneStats.CullTests, len(items))
	}
	if batchStats.CullTests >= len(items)/10 {
		t.Errorf("the batch ran %d cull tests, want far fewer than %d", batchStats.CullTests, len(items))
	}
	if batchStats.Instances != oneStats.Instances {
		t.Errorf("the batch drew %d instances, one by one drew %d", batchStats.Instances, oneStats.Instances)
	}
	if diff := imageDiff(one, batched); diff > 0 {
		t.Errorf("the batch drew %d pixels differently", diff)
	}
	// Something must actually have been drawn, or the comparison is empty.
	if oneStats.Instances == 0 {
		t.Fatal("nothing was drawn")
	}
}

// TestStaticBatchBounds covers the shapes a batch can be built from: an
// empty one, one small enough to be a single leaf, and the bounds it
// reports.
func TestStaticBatchBounds(t *testing.T) {
	g := newHeadless(t, 32, 32)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	if b := g.NewStaticBatch(nil); b.Len() != 0 {
		t.Errorf("an empty batch holds %d items", b.Len())
	}
	if b := g.NewStaticBatch([]BatchItem{{Model: lin.Identity()}}); b.Len() != 0 {
		t.Errorf("an item with no mesh was kept")
	}
	var items []BatchItem
	for i := range 3 {
		items = append(items, BatchItem{Mesh: cube, Model: lin.Translate(lin.V3(float32(i)*10, 0, 0))})
	}
	b := g.NewStaticBatch(items)
	lo, hi := b.Bounds()
	if lo.X != -0.5 || hi.X != 20.5 || lo.Y != -0.5 || hi.Y != 0.5 {
		t.Errorf("bounds are %v to %v, want -0.5,-0.5,-0.5 to 20.5,0.5,0.5", lo, hi)
	}
	// Every item must land in exactly one leaf, whatever the tree shape.
	seen := 0
	for _, n := range b.nodes {
		if n.right == 0 {
			seen += int(n.total)
		}
	}
	if seen != b.Len() {
		t.Errorf("the leaves hold %d items, the batch holds %d", seen, b.Len())
	}
}

// TestStaticBatchLeaves builds a batch big enough to be several levels
// deep and checks that the leaves partition the items exactly once each.
func TestStaticBatchLeaves(t *testing.T) {
	g := newHeadless(t, 32, 32)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		t.Fatal(err)
	}
	defer cube.Destroy()
	r := rand.New(rand.NewSource(3))
	var items []BatchItem
	for range 500 {
		p := lin.V3(r.Float32()*100, r.Float32()*20, r.Float32()*100)
		items = append(items, BatchItem{Mesh: cube, Model: lin.Translate(p)})
	}
	b := g.NewStaticBatch(items)
	covered := make([]int, b.Len())
	for _, n := range b.nodes {
		if n.right != 0 {
			continue
		}
		if n.total > batchLeaf {
			t.Fatalf("a leaf holds %d items, more than %d", n.total, batchLeaf)
		}
		for i := n.start; i < n.start+n.total; i++ {
			covered[i]++
		}
	}
	for i, c := range covered {
		if c != 1 {
			t.Fatalf("item %d is in %d leaves, want 1", i, c)
		}
	}
	// Every node's box must hold its items' boxes.
	for k, n := range b.nodes {
		for i := n.start; i < n.start+n.total; i++ {
			lo, hi := itemBox(&b.items[i])
			if lo.X < n.lo.X || lo.Y < n.lo.Y || lo.Z < n.lo.Z || hi.X > n.hi.X || hi.Y > n.hi.Y || hi.Z > n.hi.Z {
				t.Fatalf("node %d does not hold item %d", k, i)
			}
		}
	}
}

// BenchmarkStaticBatch compares the culling cost of ten thousand draws
// queued one by one against the same draws behind a hierarchy.
func BenchmarkStaticBatch(b *testing.B) {
	g := &Graphics{}
	cv, ci := CubeMesh()
	cube := &Mesh{verts: cv, indices: ci, IndexCount: uint32(len(ci))}
	for _, v := range cv {
		cube.Min, cube.Max = cube.Min.Min(v.Pos), cube.Max.Max(v.Pos)
	}
	items, cam := batchScene(cube)
	frustum := cam.Frustum(16.0 / 9)
	batch := &StaticBatch{}
	for _, it := range items {
		batch.items = append(batch.items, meshDraw{mesh: it.Mesh, mat: it.Material, model: it.Model})
	}
	los := make([]lin.Vec3, len(batch.items))
	his := make([]lin.Vec3, len(batch.items))
	for i := range batch.items {
		los[i], his[i] = itemBox(&batch.items[i])
	}
	batch.build(los, his, 0, len(batch.items))
	q := &drawQueue{}
	b.Run("one by one", func(b *testing.B) {
		for range b.N {
			kept := 0
			for i := range batch.items {
				c, r := batch.items[i].mesh.boundingSphere(batch.items[i].model)
				if frustum.ContainsSphere(c, r) {
					kept++
				}
			}
			_ = kept
		}
	})
	b.Run("hierarchy", func(b *testing.B) {
		for range b.N {
			q.draws = q.draws[:0]
			g.stats = FrameStats{}
			g.walkBatch(q, batch, frustum, lin.Identity(), false, 0)
		}
		b.ReportMetric(float64(g.stats.CullTests), "tests/frame")
		b.ReportMetric(float64(len(q.draws)), "queued/frame")
	})
}
