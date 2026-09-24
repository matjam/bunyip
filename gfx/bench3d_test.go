package gfx

import (
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"testing"
	"unsafe"

	"github.com/matjam/bunyip/lin"
)

// bench3DScene is a realistic mixed scene for the 3D frame benchmarks:
// several meshes, a spread of materials over a handful of textures, and
// draws laid on a square of ground around the camera, so a little under
// half of them are in view.
type bench3DScene struct {
	g      *Graphics
	meshes []*Mesh
	mats   []Material
	models []lin.Mat4
	cam    Camera
	tex    []*Texture
}

func newBench3DScene(b *testing.B, n int) *bench3DScene {
	b.Helper()
	g := drawBenchHeadless(b, 320, 180)
	s := &bench3DScene{g: g}
	for _, mk := range []func() ([]Vertex, []uint32){
		CubeMesh,
		func() ([]Vertex, []uint32) { return CylinderMesh(16) },
		func() ([]Vertex, []uint32) { return ConeMesh(16) },
		func() ([]Vertex, []uint32) { return CapsuleMesh(6, 12, 0.5) },
		func() ([]Vertex, []uint32) { return TorusMesh(0.3, 12, 16) },
		QuadMesh,
		func() ([]Vertex, []uint32) { return PlaneMesh(4) },
		func() ([]Vertex, []uint32) { return CylinderMesh(8) },
	} {
		v, i := mk()
		m, err := g.NewMesh(v, i)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(m.Destroy)
		s.meshes = append(s.meshes, m)
	}
	one := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range 8 {
		for p := range 4 {
			one.SetRGBA(p%2, p/2, color.RGBA{uint8(30 * i), 200, 100, 255})
		}
		t, err := g.NewTexture(one, TextureOptions{})
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(t.Destroy)
		s.tex = append(s.tex, t)
	}
	// 32 materials over 8 albedo textures and 4 normal maps; one in eight is
	// blended.
	for i := range 32 {
		m := Material{Texture: s.tex[i%8], BaseColor: RGB(uint8(i*7), 128, 200), Roughness: 0.3 + float32(i%5)*0.1}
		if i%3 == 0 {
			m.NormalTexture = s.tex[(i/3)%4]
		}
		if i%8 == 7 {
			m.Blend = true
			m.BaseColor.A = 0.5
		}
		s.mats = append(s.mats, m)
	}
	r := rand.New(rand.NewSource(3))
	side := float32(200)
	for range n {
		p := lin.V3((r.Float32()-0.5)*side, r.Float32()*2, (r.Float32()-0.5)*side)
		s.models = append(s.models, lin.Translate(p).Mul(lin.Rotate(r.Float32()*6, lin.V3(0, 1, 0))))
	}
	s.cam = Camera{Position: lin.V3(0, 10, 30), Target: lin.V3(0, 0, -20), Far: 200}
	return s
}

// queue queues the scene's draws: draw i takes mesh i%8 and material
// (i/4)%32, so neighbours share state the way a game's loops do.
func (s *bench3DScene) queue(n int) {
	g := s.g
	g.SetCamera(s.cam)
	for i := range n {
		g.DrawMesh(s.meshes[i%len(s.meshes)], s.mats[(i/4)%len(s.mats)], s.models[i])
	}
}

func (s *bench3DScene) lights(shadows bool, points int) {
	g := s.g
	g.SetLight(Light{Direction: lin.V3(-0.4, -1, -0.3), Color: White, Shadows: shadows})
	if shadows {
		for k := range 4 {
			x := float32(k*12 - 18)
			g.AddSpot(SpotLight{Position: lin.V3(x, 8, -10), Direction: lin.V3(0, -1, -0.2), Color: White, Range: 25, OuterAngle: 1, Shadows: true})
			g.AddPoint(PointLight{Position: lin.V3(x, 3, -5), Color: White, Range: 15, Shadows: true})
		}
	}
	r := rand.New(rand.NewSource(5))
	for range points {
		g.AddPoint(PointLight{Position: lin.V3((r.Float32()-0.5)*100, 1+r.Float32()*3, (r.Float32()-0.5)*100), Color: White, Range: 4 + r.Float32()*6})
	}
}

func (s *bench3DScene) frame(b *testing.B, n int, shadows bool, points int, extra func()) {
	g := s.g
	ok, err := g.begin(Black)
	if err != nil || !ok {
		b.Fatalf("begin: %v %v", ok, err)
	}
	s.lights(shadows, points)
	s.queue(n)
	if extra != nil {
		extra()
	}
	if _, err := g.end(false); err != nil {
		b.Fatalf("end: %v", err)
	}
}

// Benchmark3DFrame is a whole frame: queueing, preparation, the shadow
// atlas, the lit pass and submission, with the GPU work of a small
// target overlapping the next frame's CPU work.
func Benchmark3DFrame(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		for _, sh := range []bool{false, true} {
			b.Run(fmt.Sprintf("draws=%d/shadows=%v", n, sh), func(b *testing.B) {
				s := newBench3DScene(b, n)
				s.g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true})
				for range 3 {
					s.frame(b, n, sh, 0, nil)
				}
				for b.Loop() {
					s.frame(b, n, sh, 0, nil)
				}
				st := s.g.Stats()
				b.ReportMetric(float64(st.Draws3D), "calls")
				b.ReportMetric(float64(st.ShadowDraws), "shadowInst")
				b.ReportMetric(float64(st.Culled), "culled")
			})
		}
	}
}

// Benchmark3DVelocity_10k is a frame with temporal anti-aliasing where
// every draw moved, against one where none did.
func Benchmark3DVelocity_10k(b *testing.B) {
	for _, moved := range []bool{false, true} {
		b.Run(fmt.Sprintf("moved=%v", moved), func(b *testing.B) {
			const n = 10000
			s := newBench3DScene(b, n)
			g := s.g
			g.SetPost(PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, TemporalAA: true})
			run := func() {
				ok, err := g.begin(Black)
				if err != nil || !ok {
					b.Fatal(err)
				}
				s.lights(false, 0)
				g.SetCamera(s.cam)
				for i := range n {
					m := s.models[i]
					prev := m
					if moved {
						prev = lin.Translate(lin.V3(0.01, 0, 0)).Mul(m)
					}
					g.DrawMeshMoved(s.meshes[i%len(s.meshes)], s.mats[(i/4)%len(s.mats)], m, prev)
				}
				if _, err := g.end(false); err != nil {
					b.Fatal(err)
				}
			}
			for range 3 {
				run()
			}
			for b.Loop() {
				run()
			}
			b.ReportMetric(float64(g.Stats().Draws3D), "calls")
		})
	}
}

// Benchmark3DQueue_10k measures DrawMesh alone: the copies of Material
// and meshDraw and the ownership checks, per draw.
func Benchmark3DQueue_10k(b *testing.B) {
	s := newBench3DScene(b, 10000)
	g := s.g
	ok, err := g.begin(Black)
	if err != nil || !ok {
		b.Fatal(err)
	}
	for b.Loop() {
		g.cur.draws = g.cur.draws[:0]
		s.queue(10000)
	}
	b.StopTimer()
	g.cur.draws = g.cur.draws[:0]
	g.end(false)
	b.Logf("sizeof Material=%d meshDraw=%d meshInstance=%d", unsafe.Sizeof(Material{}), unsafe.Sizeof(meshDraw{}), unsafe.Sizeof(meshInstance{}))
}

// Benchmark3DPrepare is prepareDraws over the mixed scene.
func Benchmark3DPrepare(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		b.Run(fmt.Sprintf("draws=%d", n), func(b *testing.B) {
			s := newBench3DScene(b, n)
			g := s.g
			ok, err := g.begin(Black)
			if err != nil || !ok {
				b.Fatal(err)
			}
			s.queue(n)
			q := g.cur
			src := append([]meshDraw(nil), q.draws...)
			scene := g.post.main.scene
			for b.Loop() {
				copy(q.draws, src)
				if _, _, _, err := g.prepareDraws(q, g.frame.Slot, scene, 16.0/9); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			q.draws = q.draws[:0]
			g.end(false)
		})
	}
}

// Benchmark3DShadowMasks_10k is the per-map culling the shadow pass
// does: three cascades, four spot maps and twenty-four cube faces.
func Benchmark3DShadowMasks_10k(b *testing.B) {
	s := newBench3DScene(b, 10000)
	g := s.g
	ok, err := g.begin(Black)
	if err != nil || !ok {
		b.Fatal(err)
	}
	s.lights(true, 0)
	s.queue(10000)
	q := g.cur
	if _, _, _, err := g.prepareDraws(q, g.frame.Slot, g.post.main.scene, 16.0/9); err != nil {
		b.Fatal(err)
	}
	q.cascadeMats, _, _ = q.cascades(16.0 / 9)
	for b.Loop() {
		q.cullShadowMaps(true)
	}
	vis := 0
	for _, l := range q.casters.lists {
		vis += len(l)
	}
	b.ReportMetric(float64(vis), "maskedIn")
	b.StopTimer()
	q.draws = q.draws[:0]
	q.points = q.points[:0]
	g.end(false)
}

// Benchmark3DSortDraws is the ordering of a prepared frame's draws: the
// packed keys and their sort.
func Benchmark3DSortDraws(b *testing.B) {
	for _, n := range []int{10000, 50000} {
		b.Run(fmt.Sprintf("draws=%d", n), func(b *testing.B) {
			s := newBench3DScene(b, n)
			g := s.g
			ok, err := g.begin(Black)
			if err != nil || !ok {
				b.Fatal(err)
			}
			s.queue(n)
			q := g.cur
			if _, _, _, err := g.prepareDraws(q, g.frame.Slot, g.post.main.scene, 16.0/9); err != nil {
				b.Fatal(err)
			}
			for b.Loop() {
				q.sortDraws()
			}
			b.StopTimer()
			q.draws = q.draws[:0]
			g.end(false)
		})
	}
}

// Benchmark3DClusters is the light assignment and its upload.
func Benchmark3DClusters(b *testing.B) {
	for _, n := range []int{1, 64, 512} {
		b.Run(fmt.Sprintf("lights=%d", n), func(b *testing.B) {
			s := newBench3DScene(b, 1)
			g := s.g
			ok, err := g.begin(Black)
			if err != nil || !ok {
				b.Fatal(err)
			}
			s.lights(false, n)
			q := g.cur
			q.camera, q.hasCam = s.cam, true
			for b.Loop() {
				if err := q.writeLights(g.frame.Slot, 16.0/9, nil, nil); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(q.clusters.used), "indices")
			b.StopTimer()
			q.points = q.points[:0]
			g.end(false)
		})
	}
}

// Benchmark3DOcclusion_10k is prepareDraws with 16 box occluders.
func Benchmark3DOcclusion_10k(b *testing.B) {
	for _, on := range []bool{false, true} {
		b.Run(fmt.Sprintf("occluders=%v", on), func(b *testing.B) {
			s := newBench3DScene(b, 10000)
			g := s.g
			ok, err := g.begin(Black)
			if err != nil || !ok {
				b.Fatal(err)
			}
			s.queue(10000)
			if on {
				for k := range 16 {
					g.AddOccluder3D(s.meshes[0], lin.Translate(lin.V3(float32(k*6-48), 2, -5)).Mul(lin.Scale(lin.V3(5, 4, 0.5))))
				}
			}
			q := g.cur
			src := append([]meshDraw(nil), q.draws...)
			for b.Loop() {
				copy(q.draws, src)
				if _, _, _, err := g.prepareDraws(q, g.frame.Slot, g.post.main.scene, 16.0/9); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(g.stats.Occluded)/float64(b.N), "occluded")
			b.StopTimer()
			q.draws = q.draws[:0]
			q.meshOccluders = q.meshOccluders[:0]
			g.end(false)
		})
	}
}

// Benchmark3DBatch_20k is a static batch of 20k props: the hierarchy
// walk plus the preparation of what survives, against queueing the same
// props one by one.
func Benchmark3DBatch_20k(b *testing.B) {
	for _, batched := range []bool{true, false} {
		b.Run(fmt.Sprintf("batched=%v", batched), func(b *testing.B) {
			s := newBench3DScene(b, 20000)
			g := s.g
			items := make([]BatchItem, 20000)
			for i := range items {
				items[i] = BatchItem{Mesh: s.meshes[i%len(s.meshes)], Material: s.mats[(i/4)%len(s.mats)], Model: s.models[i]}
			}
			batch := g.NewStaticBatch(items)
			ok, err := g.begin(Black)
			if err != nil || !ok {
				b.Fatal(err)
			}
			q := g.cur
			q.camera, q.hasCam = s.cam, true
			for b.Loop() {
				q.draws = q.draws[:0]
				q.batches = q.batches[:0]
				if batched {
					g.DrawBatch(batch)
				} else {
					s.queue(20000)
				}
				if _, _, _, err := g.prepareDraws(q, g.frame.Slot, g.post.main.scene, 16.0/9); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(len(q.draws)), "draws")
			b.StopTimer()
			q.draws = q.draws[:0]
			q.batches = q.batches[:0]
			g.end(false)
		})
	}
}
