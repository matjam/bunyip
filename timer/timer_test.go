package timer

import "testing"

func TestScheduler(t *testing.T) {
	var s Scheduler
	var log []string
	s.After(1, func() { log = append(log, "after") })
	every := s.Every(0.5, func() { log = append(log, "tick") })
	s.Update(0.4)
	if len(log) != 0 {
		t.Fatal("fired early")
	}
	s.Update(0.7) // 1.1 s: tick at 0.5, then after and tick both at 1.0 in scheduling order
	if len(log) != 3 || log[0] != "tick" || log[1] != "after" || log[2] != "tick" || s.Pending() != 1 {
		t.Fatalf("log %v pending %d", log, s.Pending())
	}
	s.Cancel(every)
	s.Update(5)
	if len(log) != 3 || s.Pending() != 0 {
		t.Fatalf("cancelled timer fired: %v", log)
	}
	// A callback scheduling a timer works.
	s.After(0.1, func() { s.After(0.1, func() { log = append(log, "nested") }) })
	s.Update(0.15)
	s.Update(0.1)
	if log[len(log)-1] != "nested" {
		t.Fatal("nested timer did not fire")
	}
	var c Countdown
	c.Start(1)
	if c.Update(0.5) || !c.Running() {
		t.Fatal("countdown ended early")
	}
	if !c.Update(0.6) || c.Update(1) {
		t.Fatal("countdown should report once")
	}
}
