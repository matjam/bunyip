package timer

import "testing"

// TestSchedulerOrder covers what the heap has to preserve: timers due
// at the same moment fire in the order they were scheduled, a callback
// can cancel its own repeating timer, and Pending counts what is left.
func TestSchedulerOrder(t *testing.T) {
	var s Scheduler
	var fired []int
	const n = 64
	for i := range n {
		// Scheduled back to front, all due at the same moment, so only
		// the handle order can produce the scheduling order.
		id := n - 1 - i
		s.After(1, func() { fired = append(fired, id) })
	}
	if s.Pending() != n {
		t.Fatalf("pending %d, want %d", s.Pending(), n)
	}
	s.Update(1)
	if len(fired) != n {
		t.Fatalf("fired %d timers, want %d", len(fired), n)
	}
	for i, id := range fired {
		if want := n - 1 - i; id != want {
			t.Fatalf("fired[%d] is %d, want %d", i, id, want)
		}
	}
	if s.Pending() != 0 {
		t.Fatalf("pending %d after all fired", s.Pending())
	}

	// A repeating timer that cancels itself runs once.
	runs := 0
	var self Handle
	self = s.Every(0.1, func() {
		runs++
		s.Cancel(self)
	})
	s.Update(1)
	if runs != 1 {
		t.Fatalf("self-cancelling timer ran %d times", runs)
	}
	if s.Pending() != 0 {
		t.Fatalf("pending %d after self-cancel", s.Pending())
	}

	// Cancelling timers due far in the future must not leave them in
	// the heap; the scheduler compacts once half of it is cancelled.
	for range 100 {
		s.Cancel(s.After(1e6, func() { t.Fatal("cancelled timer fired") }))
	}
	if s.Pending() != 0 {
		t.Fatalf("pending %d after cancelling every timer", s.Pending())
	}
	s.Update(1)
	if s.Pending() != 0 {
		t.Fatalf("pending %d after update", s.Pending())
	}
}

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
