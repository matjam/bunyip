package particle

import (
	"reflect"
	"testing"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

func TestStatelessStopDrains(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start float32
	}{
		{"at_start", 0},
		{"between_births", 2.25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewGPU(Emitter{Stateless: true, Rate: 10, Lifetime: Range{Min: 1}})
			s.SetClock(tc.start)
			s.buildQuads(lin.Vec2{})
			if s.Alive() == 0 {
				t.Fatal("stream must be populated before Stop")
			}
			s.Stop()
			s.Update(2)
			s.buildQuads(lin.Vec2{})
			if s.Alive() != 0 || !s.Finished() {
				t.Fatalf("after Stop and two lifetimes: alive=%d finished=%v", s.Alive(), s.Finished())
			}
		})
	}
}

func TestStatelessStopKeepsExistingParticles(t *testing.T) {
	for _, tc := range []struct {
		name string
		max  int
		want int
	}{
		{"uncapped", 100, 5},
		{"capped", 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewGPU(Emitter{Stateless: true, Rate: 10, Lifetime: Range{Min: 1}, Speed: Range{Min: 1}, Max: tc.max})
			s.SetClock(2.25)
			s.Stop()
			s.Update(0.5)
			s.buildQuads(lin.Vec2{})
			if s.Alive() != tc.want || s.Finished() {
				t.Fatalf("during drain: alive=%d want=%d finished=%v", s.Alive(), tc.want, s.Finished())
			}
			for _, q := range s.Quads() {
				if q.Pos.X < 0.5 || q.Pos.X >= 1 {
					t.Errorf("particle travelled %v; want age in [0.5, 1)", q.Pos.X)
				}
			}
		})
	}
}

func TestStatelessStopFinishedWithoutDraw(t *testing.T) {
	for _, drawFirst := range []bool{false, true} {
		s := NewGPU(Emitter{Stateless: true, Rate: 10, Lifetime: Range{Min: 1}})
		if drawFirst {
			s.buildQuads(lin.Vec2{})
		}
		s.Stop()
		if s.Finished() {
			t.Fatalf("finished immediately after Stop (drawFirst=%v)", drawFirst)
		}
		s.Update(2)
		if !s.Finished() {
			t.Fatalf("not finished after aging without Draw (drawFirst=%v)", drawFirst)
		}
	}
}

func TestStatelessStopScrubAndRestart(t *testing.T) {
	e := Emitter{Stateless: true, Rate: 10, Lifetime: Range{Min: 1}, Speed: Range{Min: 1}}
	s := NewGPU(e)
	s.SetClock(2.25)
	s.Stop()
	s.SetClock(2.75)
	s.buildQuads(lin.Vec2{})
	want := append([]gfx.ParticleQuad(nil), s.Quads()...)
	s.SetClock(4)
	if !s.Finished() {
		t.Fatal("not finished after scrubbing past final death")
	}
	s.Stop() // Repeated Stop must not move the final birth time forward.
	s.SetClock(2.75)
	s.buildQuads(lin.Vec2{})
	if !reflect.DeepEqual(s.Quads(), want) || s.Finished() {
		t.Fatal("repeated Stop changed the stream when scrubbing backward")
	}
	fresh := NewGPU(e)
	fresh.SetClock(1.25)
	fresh.buildQuads(lin.Vec2{})
	s.SetClock(1.25)
	s.buildQuads(lin.Vec2{})
	if !reflect.DeepEqual(s.Quads(), fresh.Quads()) {
		t.Fatal("scrubbing before Stop does not match the running stream")
	}
	s.Start()
	fresh.Start()
	s.buildQuads(lin.Vec2{})
	fresh.buildQuads(lin.Vec2{})
	if s.Clock() != 0 || !s.Emitting() || !reflect.DeepEqual(s.Quads(), fresh.Quads()) {
		t.Fatal("Start did not restore the initial running stream")
	}
	s.SetClock(5)
	s.buildQuads(lin.Vec2{})
	if s.Alive() == 0 {
		t.Fatal("restarted stream retained the old Stop cutoff")
	}
}

func TestStatelessStopCappedWindow(t *testing.T) {
	s := NewGPU(Emitter{Stateless: true, Rate: 10, Lifetime: Range{Min: 1}, Max: 3})
	s.SetClock(2.25)
	s.Stop()
	for _, tc := range []struct {
		clock float32
		want  int
	}{
		{2.75, 3},
		{3.15, 1},
		{3.25, 0},
		{2.75, 3},
	} {
		s.SetClock(tc.clock)
		s.buildQuads(lin.Vec2{})
		if s.Alive() != tc.want {
			t.Errorf("clock=%v alive=%d want=%d", tc.clock, s.Alive(), tc.want)
		}
	}
}

func TestStatelessStopSampledLifetimes(t *testing.T) {
	for seed := range uint64(20) {
		s := NewGPU(Emitter{
			Stateless: true, Rate: 10, Max: 1, Seed: seed,
			Shape: Circle(10), Spread: 1, Speed: Range{Min: 1, Max: 2},
			Lifetime: Range{Min: 0.125, Max: 0.75},
		})
		s.SetClock(2.25)
		s.Stop()
		for _, clock := range []float32{2.25, 2.5, 2.75, 3.25, 2.25} {
			s.SetClock(clock)
			finished := s.Finished()
			s.buildQuads(lin.Vec2{})
			if want := len(s.Quads()) == 0; finished != want {
				t.Errorf("seed=%d clock=%v finished=%v want=%v", seed, clock, finished, want)
			}
		}
		if allocs := testing.AllocsPerRun(100, func() { s.Finished() }); allocs != 0 {
			t.Errorf("Finished allocates %v times", allocs)
		}
	}
}

func TestStatelessStopInvisibleParticlesStillLive(t *testing.T) {
	s := NewGPU(Emitter{Stateless: true, Rate: 10, Lifetime: Range{Min: 1}, AlphaOverLife: Linear(0, 0)})
	s.buildQuads(lin.Vec2{})
	s.Stop()
	if s.Alive() != 0 || s.Finished() {
		t.Fatal("invisible particles should still be alive until their lifetimes expire")
	}
	s.Update(2)
	if !s.Finished() {
		t.Fatal("invisible particles did not expire")
	}
}

func TestStatefulGPUStopStillDrains(t *testing.T) {
	s := NewGPU(Emitter{Rate: 10, Burst: 1, Lifetime: Range{Min: 1}})
	s.Stop()
	s.Update(0.5)
	if s.Alive() != 1 || s.Finished() {
		t.Fatal("Stop discarded a stateful particle")
	}
	s.Update(1)
	if s.Alive() != 0 || !s.Finished() {
		t.Fatal("stateful particles did not drain")
	}
	s.Start()
	if s.Alive() != 1 || !s.Emitting() {
		t.Fatal("stateful Start did not emit its burst")
	}
}
