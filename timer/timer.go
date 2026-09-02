// Package timer schedules callbacks on game time: after a delay, or
// every interval. Timers run when the game calls Update, so they pause
// with the game and stay deterministic.
package timer

// Scheduler runs timers in game time.
type Scheduler struct {
	timers []*timer
	next   int
	now    float64
}

type timer struct {
	id       Handle
	at       float64
	interval float64
	fn       func()
	dead     bool
}

// Handle identifies a scheduled timer for Cancel.
type Handle int

// After runs fn once, seconds from now.
func (s *Scheduler) After(seconds float64, fn func()) Handle {
	return s.add(seconds, 0, fn)
}

// Every runs fn every interval seconds, starting one interval from now,
// until cancelled.
func (s *Scheduler) Every(seconds float64, fn func()) Handle {
	return s.add(seconds, seconds, fn)
}

func (s *Scheduler) add(delay, interval float64, fn func()) Handle {
	s.next++
	t := &timer{id: Handle(s.next), at: s.now + delay, interval: interval, fn: fn}
	s.timers = append(s.timers, t)
	return t.id
}

// Cancel stops a timer; cancelling one that already fired is harmless.
func (s *Scheduler) Cancel(h Handle) {
	for _, t := range s.timers {
		if t.id == h {
			t.dead = true
		}
	}
}

// Update advances time by dt seconds and fires what is due, earliest
// first; timers due at the same moment fire in the order they were
// scheduled. Callbacks may schedule or cancel timers.
func (s *Scheduler) Update(dt float64) {
	s.now += dt
	for {
		var due *timer
		for _, t := range s.timers {
			if t.dead || t.at > s.now {
				continue
			}
			if due == nil || t.at < due.at || (t.at == due.at && t.id < due.id) {
				due = t
			}
		}
		if due == nil {
			break
		}
		due.fn()
		if due.interval <= 0 {
			due.dead = true
		} else {
			due.at += due.interval
		}
	}
	live := s.timers[:0]
	for _, t := range s.timers {
		if !t.dead {
			live = append(live, t)
		}
	}
	for i := len(live); i < len(s.timers); i++ {
		s.timers[i] = nil
	}
	s.timers = live
}

// Now is the scheduler's elapsed game time in seconds.
func (s *Scheduler) Now() float64 { return s.now }

// Pending counts scheduled timers.
func (s *Scheduler) Pending() int { return len(s.timers) }

// Countdown is a simple timer a game polls rather than a callback: set
// it and ask each frame whether it has run out.
type Countdown struct {
	Left float64
}

// Start sets the countdown.
func (c *Countdown) Start(seconds float64) { c.Left = seconds }

// Update advances the countdown and reports whether it ran out on this
// update (true once).
func (c *Countdown) Update(dt float64) bool {
	if c.Left <= 0 {
		return false
	}
	c.Left -= dt
	return c.Left <= 0
}

// Running reports whether time remains.
func (c *Countdown) Running() bool { return c.Left > 0 }
