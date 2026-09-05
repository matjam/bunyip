package gfx

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// benchQuads builds n instance records spread over the view, the shape
// the particle package hands to DrawParticles.
func benchQuads(n int, w, h float32) []ParticleQuad {
	quads := make([]ParticleQuad, n)
	for i := range quads {
		// A cheap spread; the numbers only have to differ.
		x := float32(i%977) / 977 * w
		y := float32(i%733) / 733 * h
		quads[i] = ParticleQuad{
			Pos: lin.V3(x, y, 0), Size: lin.V2(3, 3), UV1: lin.V2(1, 1),
			Rotation: float32(i%64) * 0.1, Color: Color{R: 1, G: 0.6, B: 0.2, A: 0.5},
		}
	}
	return quads
}

// BenchmarkDrawParticles measures a whole frame that draws two hundred
// thousand instanced particles: the copy into the stream, the upload
// into the frame's buffer and the one draw call. Subtract
// BenchmarkDrawParticlesEmpty to get the cost of the particles alone.
func BenchmarkDrawParticles(b *testing.B) {
	const n = 200_000
	g := drawBenchHeadless(b, 1280, 720)
	quads := benchQuads(n, 1280, 720)
	b.ReportAllocs()
	for b.Loop() {
		ok, err := g.begin(Black)
		if err != nil {
			b.Fatalf("begin: %v", err)
		}
		if !ok {
			continue
		}
		g.DrawParticles(nil, quads)
		if _, err := g.end(false); err != nil {
			b.Fatalf("end: %v", err)
		}
	}
	b.ReportMetric(float64(n), "particles")
}

// BenchmarkDrawParticlesEmpty is the same frame with nothing in it, the
// baseline the particle frame is measured against.
func BenchmarkDrawParticlesEmpty(b *testing.B) {
	g := drawBenchHeadless(b, 1280, 720)
	b.ReportAllocs()
	for b.Loop() {
		ok, err := g.begin(Black)
		if err != nil {
			b.Fatalf("begin: %v", err)
		}
		if !ok {
			continue
		}
		if _, err := g.end(false); err != nil {
			b.Fatalf("end: %v", err)
		}
	}
}

// BenchmarkDrawSprites draws the same count through the sprite stream,
// for the comparison that says when the instanced path is worth it.
func BenchmarkDrawSprites(b *testing.B) {
	const n = 200_000
	g := drawBenchHeadless(b, 1280, 720)
	quads := benchQuads(n, 1280, 720)
	b.ReportAllocs()
	for b.Loop() {
		ok, err := g.begin(Black)
		if err != nil {
			b.Fatalf("begin: %v", err)
		}
		if !ok {
			continue
		}
		for _, q := range quads {
			g.Draw(nil, Sprite{
				Pos: lin.V2(q.Pos.X, q.Pos.Y), Size: q.Size, Origin: lin.V2(0.5, 0.5),
				Rotation: q.Rotation, Color: q.Color,
			})
		}
		if _, err := g.end(false); err != nil {
			b.Fatalf("end: %v", err)
		}
	}
	b.ReportMetric(float64(n), "particles")
}
