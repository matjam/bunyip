package timer_test

import (
	"testing"

	"github.com/matjam/bunyip/timer"
)

// BenchmarkSchedulerUpdate keeps a thousand timers pending and fires a
// hundred of them per update, the shape a game with many delayed effects
// has.
func BenchmarkSchedulerUpdate(b *testing.B) {
	const (
		total = 1000
		fires = 100
		step  = 1.0 / 60
	)
	fired := 0
	hit := func() { fired++ }

	var s timer.Scheduler
	// The first hundred come due on every update; the rest sit far out.
	for range fires {
		s.Every(step, hit)
	}
	for range total - fires {
		s.After(1e6, hit)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s.Update(step)
	}
	b.StopTimer()
	if fired == 0 {
		b.Fatal("nothing fired")
	}
}

// BenchmarkSchedulerAdd measures scheduling itself, which a game does
// every time it starts a delayed effect.
func BenchmarkSchedulerAdd(b *testing.B) {
	var s timer.Scheduler
	fn := func() {}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		s.After(1, fn)
		if s.Pending() >= 4096 {
			b.StopTimer()
			s.Update(2)
			b.StartTimer()
		}
	}
}
