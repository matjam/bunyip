package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// BenchmarkPost renders the same scene at 1280 by 720 with one post
// setting changed at a time, so each pass's cost can be read off against
// the "off" case. Frames in flight are bounded, so over many iterations
// the time is the GPU's rather than the recording's.
func BenchmarkPost(b *testing.B) {
	g := benchHeadless(b, 1280, 720)
	cv, ci := CubeMesh()
	cube, err := g.NewMesh(cv, ci)
	if err != nil {
		b.Fatal(err)
	}
	defer cube.Destroy()
	cases := []struct {
		name string
		set  func(p *PostSettings)
	}{
		{"off", func(p *PostSettings) {}},
		{"bloom", func(p *PostSettings) { p.Bloom = 0.3 }},
		{"occlusion", func(p *PostSettings) { p.AmbientOcclusion = 0.6 }},
		{"fxaa", func(p *PostSettings) { p.NoAntiAlias = false }},
		{"temporal", func(p *PostSettings) { p.TemporalAA = true }},
		{"depthOfField", func(p *PostSettings) { p.FocusDistance, p.FocusRange = 8, 3 }},
		{"motionBlur", func(p *PostSettings) { p.MotionBlur = 0.7 }},
		{"godRays", func(p *PostSettings) { p.GodRays = 0.8 }},
		{"lens", func(p *PostSettings) { p.Aberration, p.Distortion, p.Grain, p.Ghosts = 2, 0.3, 0.05, 0.3 }},
	}
	for _, c := range cases {
		p := PostSettings{Exposure: 1, Saturation: 1, Contrast: 1, NoAntiAlias: true}
		c.set(&p)
		b.Run(c.name, func(b *testing.B) {
			g.SetPost(p)
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				ok, err := g.begin(Black)
				if err != nil {
					b.Fatal(err)
				}
				if !ok {
					continue
				}
				g.SetCamera(Camera{Position: lin.V3(0, 1.5, 10), Target: lin.V3(0, 1, 0)})
				g.SetLight(Light{Direction: lin.V3(0, -0.35, 0.94), Color: Color{1, 1, 1, 1}, Ambient: Color{0.2, 0.2, 0.2, 1}})
				x := 0.05 * float32(i%40)
				for k := range 24 {
					at := lin.Translate(lin.V3(float32(k%6)*2-5+x, 0, float32(k/6)*2-3))
					was := lin.Translate(lin.V3(float32(k%6)*2-5+x-0.05, 0, float32(k/6)*2-3))
					g.DrawMeshMoved(cube, Material{BaseColor: White, Roughness: 0.5}, at, was)
				}
				if _, err := g.end(false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
