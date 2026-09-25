package gfx

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/matjam/bunyip/lin"
)

// gpuScene is a lit, shadowed scene with a reflective floor, a ring of
// moving cubes and a few blended spheres: the shape of frame the post
// chain and the scene passes are meant for.
type gpuScene struct {
	cube, sphere, plane *Mesh
	// sunAhead turns the light round so the sun is in front of the
	// camera, where the light shafts pass actually runs.
	sunAhead bool
}

func newGPUScene(tb testing.TB, g *Graphics) *gpuScene {
	tb.Helper()
	cv, ci := CubeMesh()
	sv, si := SphereMesh(24, 32)
	pv, pi := PlaneMesh(1)
	s := &gpuScene{}
	var err error
	if s.cube, err = g.NewMesh(cv, ci); err != nil {
		tb.Fatal(err)
	}
	if s.sphere, err = g.NewMesh(sv, si); err != nil {
		tb.Fatal(err)
	}
	if s.plane, err = g.NewMesh(pv, pi); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { s.cube.Destroy(); s.sphere.Destroy(); s.plane.Destroy() })
	return s
}

func (s *gpuScene) draw(g *Graphics, i int) {
	g.SetCamera(Camera{Position: lin.V3(0, 3, 12), Target: lin.V3(0, 1, 0)})
	dir := lin.V3(-0.3, -0.5, -0.8)
	if s.sunAhead {
		dir = lin.V3(0.1, -0.3, 1)
	}
	g.SetLight(Light{
		Direction: dir, Color: Color{2, 2, 1.9, 1}, Ambient: Color{0.2, 0.2, 0.25, 1},
		Shadows: true, Background: true,
	})
	g.DrawMesh(s.plane, Material{BaseColor: Color{0.5, 0.5, 0.5, 1}, Roughness: 0.1}, lin.Scale(lin.V3(40, 1, 40)))
	x := 0.05 * float32(i%40)
	for k := range 24 {
		at := lin.Translate(lin.V3(float32(k%6)*2-5+x, 0.5, float32(k/6)*2-3))
		was := lin.Translate(lin.V3(float32(k%6)*2-5+x-0.05, 0.5, float32(k/6)*2-3))
		g.DrawMeshMoved(s.cube, Material{BaseColor: White, Roughness: 0.5}, at, was)
	}
	for k := range 4 {
		m := lin.Translate(lin.V3(float32(k)*2.5-4, 2, 2))
		g.DrawMesh(s.sphere, Material{BaseColor: Color{0.2, 0.6, 1, 0.5}, Blend: true, Roughness: 0.2}, m)
	}
}

func (s *gpuScene) frame(tb testing.TB, g *Graphics, i int) {
	ok, err := g.begin(Black)
	if err != nil {
		tb.Fatal(err)
	}
	if !ok {
		return
	}
	s.draw(g, i)
	if _, err := g.end(false); err != nil {
		tb.Fatal(err)
	}
}

// gpuPostCase is one post setting turned on over gpuPostBase.
type gpuPostCase struct {
	name     string
	set      func(p *PostSettings)
	sunAhead bool
}

// gpuPostBase has every effect off, so a case's difference against
// "none" is that effect's cost.
func gpuPostBase() PostSettings {
	return PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true}
}

func gpuAll(p *PostSettings) {
	*p = DefaultPost()
	p.TemporalAA = true
	p.FocusDistance, p.FocusRange = 8, 3
	p.MotionBlur = 0.7
	p.GodRays = 0.8
	p.Aberration, p.Distortion, p.Grain, p.Ghosts = 2, 0.3, 0.05, 0.3
	p.Reflections = 0.6
	p.OrderIndependent = true
}

var gpuPostCases = []gpuPostCase{
	{name: "none", set: func(p *PostSettings) {}},
	{name: "bloom", set: func(p *PostSettings) { p.Bloom = 0.25 }},
	{name: "occlusion", set: func(p *PostSettings) { p.AmbientOcclusion = 0.6 }},
	{name: "fxaa", set: func(p *PostSettings) { p.NoAntiAlias = false }},
	{name: "temporal", set: func(p *PostSettings) { p.TemporalAA = true }},
	{name: "depthOfField", set: func(p *PostSettings) { p.FocusDistance, p.FocusRange = 8, 3 }},
	{name: "motionBlur", set: func(p *PostSettings) { p.MotionBlur = 0.7 }},
	{name: "godRays", set: func(p *PostSettings) { p.GodRays = 0.8 }, sunAhead: true},
	{name: "lens", set: func(p *PostSettings) { p.Aberration, p.Distortion, p.Grain, p.Ghosts = 2, 0.3, 0.05, 0.3 }},
	{name: "reflections", set: func(p *PostSettings) { p.Reflections = 0.6 }},
	{name: "oit", set: func(p *PostSettings) { p.OrderIndependent = true }},
	{name: "msaa4", set: func(p *PostSettings) { p.Samples = 4 }},
	{name: "msaa4+reflections", set: func(p *PostSettings) { p.Samples, p.Reflections = 4, 0.6 }},
	{name: "msaa4+oit", set: func(p *PostSettings) { p.Samples, p.OrderIndependent = 4, true }},
	{name: "default", set: func(p *PostSettings) { *p = DefaultPost() }},
	{name: "all", set: gpuAll},
}

// BenchmarkGPUPostWall measures whole frames of gpuScene with one post
// setting turned on at a time, at 1280x720 and 2560x1440. Frames in
// flight are bounded, so the loop runs at the GPU's pace and the
// difference of a case against "none" is that setting's GPU cost,
// whatever the device's timestamps resolve. Compare runs with benchstat.
func BenchmarkGPUPostWall(b *testing.B) {
	for _, res := range [][2]int{{1280, 720}, {2560, 1440}} {
		b.Run(fmt.Sprintf("%dx%d", res[0], res[1]), func(b *testing.B) {
			g := benchHeadless(b, res[0], res[1])
			sc := newGPUScene(b, g)
			for _, c := range gpuPostCases {
				p := gpuPostBase()
				c.set(&p)
				b.Run(c.name, func(b *testing.B) {
					g.SetPost(p)
					sc.sunAhead = c.sunAhead
					// Warm up: pipelines, optional targets, the history.
					for i := range 8 {
						sc.frame(b, g, i)
					}
					for i := 0; b.Loop(); i++ {
						sc.frame(b, g, i)
					}
				})
			}
		})
	}
}

// BenchmarkGPUPasses renders gpuScene with every effect on and reports
// the mean GPU milliseconds of each timed span, the frame, and the
// share of the frame the spans cover, from the engine's own timestamp
// queries.
func BenchmarkGPUPasses(b *testing.B) {
	for _, res := range [][2]int{{1280, 720}, {2560, 1440}} {
		b.Run(fmt.Sprintf("%dx%d", res[0], res[1]), func(b *testing.B) {
			g := benchHeadless(b, res[0], res[1])
			if g.timestamps == nil {
				b.Skip("no timestamps")
			}
			sc := newGPUScene(b, g)
			for _, c := range []gpuPostCase{gpuPostCases[0], gpuPostCases[len(gpuPostCases)-1]} {
				p := gpuPostBase()
				c.set(&p)
				b.Run(c.name, func(b *testing.B) {
					g.SetPost(p)
					for i := range 8 {
						sc.frame(b, g, i)
					}
					sums := map[string]float64{}
					var frame, covered float64
					n := 0
					for i := 0; b.Loop(); i++ {
						sc.frame(b, g, i)
						st := g.Stats()
						if len(st.GPU) == 0 || st.GPUFrameMS <= 0 {
							continue
						}
						n++
						for _, s := range st.GPU {
							sums[s.Name] += s.MS
							covered += s.MS
						}
						frame += st.GPUFrameMS
					}
					if n == 0 {
						return
					}
					names := make([]string, 0, len(sums))
					for k := range sums {
						names = append(names, k)
					}
					sort.Strings(names)
					for _, k := range names {
						b.ReportMetric(sums[k]/float64(n), strings.ReplaceAll(k, " ", "_")+"-ms")
					}
					b.ReportMetric(frame/float64(n), "gpuframe-ms")
					b.ReportMetric(100*covered/frame, "covered-%")
				})
			}
		})
	}
}
