package particle

import (
	"testing"

	"github.com/matjam/bunyip/lin"
)

// benchEmitter is a two hundred thousand particle snowfall: enough that
// the simulation is the whole cost and nothing dies for the length of a
// run, so each iteration does the same work.
func benchEmitter(n int) Emitter {
	return Emitter{
		Rate:          float32(n),
		Lifetime:      Range{Min: 8, Max: 10},
		Shape:         Rect(1920, 40),
		Direction:     1.4,
		Spread:        0.4,
		Speed:         Range{Min: 90, Max: 160},
		Acceleration:  lin.V2(0, 60),
		Damping:       0.2,
		Size:          Range{Min: 2, Max: 5},
		SizeOverLife:  Linear(1, 0.6),
		AlphaOverLife: Keys(0, 0, 0.1, 1, 1, 0),
		Max:           n,
		Seed:          1,
	}
}

// fill runs a system until it is at its cap.
func fill(s *GPUSystem, n int) {
	for s.Alive() < n {
		s.Update(1.0 / 60)
	}
}

// BenchmarkGPUUpdate measures the simulation alone: ageing, compaction,
// acceleration, damping and integration over two hundred thousand
// particles. Divide ns/op by a million for milliseconds a frame.
func BenchmarkGPUUpdate(b *testing.B) {
	const n = 200_000
	s := NewGPU(benchEmitter(n))
	fill(s, n)
	b.ReportAllocs()
	for b.Loop() {
		s.Update(1.0 / 60)
	}
	b.ReportMetric(float64(n), "particles")
}

// BenchmarkGPUQuads measures building the instance records the upload
// copies: the curve lookups, the palette and the packing.
func BenchmarkGPUQuads(b *testing.B) {
	const n = 200_000
	s := NewGPU(benchEmitter(n))
	fill(s, n)
	b.ReportAllocs()
	for b.Loop() {
		s.buildQuads(lin.Vec2{})
	}
	b.ReportMetric(float64(n), "particles")
}

// BenchmarkGPUFrame measures a whole frame's CPU work for a stateful
// system: one simulation step and one build of the instance records.
func BenchmarkGPUFrame(b *testing.B) {
	const n = 200_000
	s := NewGPU(benchEmitter(n))
	fill(s, n)
	b.ReportAllocs()
	for b.Loop() {
		s.Update(1.0 / 60)
		s.buildQuads(lin.Vec2{})
	}
	b.ReportMetric(float64(n), "particles")
}

// BenchmarkStatelessFrame measures a stateless emitter's whole frame,
// which computes every particle from the clock instead of stepping it.
func BenchmarkStatelessFrame(b *testing.B) {
	const n = 200_000
	e := benchEmitter(n)
	e.Stateless = true
	e.Lifetime = Range{Min: 1, Max: 1} // rate times lifetime is the live count
	e.Rate = n
	s := NewGPU(e)
	s.SetClock(4)
	b.ReportAllocs()
	for b.Loop() {
		s.Update(1.0 / 60)
		s.buildQuads(lin.Vec2{})
	}
	if got := s.Alive(); got < n/2 {
		b.Fatalf("only %d particles alive, want about %d", got, n)
	}
	b.ReportMetric(float64(n), "particles")
}
