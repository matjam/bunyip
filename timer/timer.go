// Package timer schedules callbacks on game time, after a delay or on
// every interval. Timers run when the game calls Update, so they pause
// with the game and stay deterministic.
//
// Advance a Scheduler each update with the step. After runs a function
// once, Every runs it repeatedly, and both return a handle to Cancel.
// Countdown is a simpler alternative. Start it, update it, and ask
// whether it is still Running. Because the time is the game's own, a
// paused game stops its timers by not calling Update, a replay reruns
// them identically, and a fast-forward advances them by a larger step.
// Schedulers are not goroutine-safe and fire on the goroutine that
// calls Update, the game loop's, so callbacks may touch game state
// freely.
package timer

// Scheduler runs timers in game time. Its zero value is ready to use.
type Scheduler struct {
	heap     []timer // a min-heap ordered by due time, then by handle
	next     int
	now      float64
	live     int   // timers that have neither fired nor been cancelled
	dead     int   // cancelled timers still sitting in the heap
	firing   timer // the timer whose callback is running
	isFiring bool
}

type timer struct {
	id       Handle
	at       float64
	interval float64
	fn       func()
	dead     bool
}

// before orders timers by due time, then by the order they were
// scheduled, which is the order Update fires them in.
func (t timer) before(u timer) bool {
	if t.at != u.at {
		return t.at < u.at
	}
	return t.id < u.id
}

// Handle identifies a scheduled timer for Cancel.
type Handle int

// After runs a non-nil fn once, seconds from now. A nonpositive delay
// becomes due on the next Update, or in the current Update if scheduled
// by a callback. It does not run fn during After itself.
func (s *Scheduler) After(seconds float64, fn func()) Handle {
	return s.add(seconds, 0, fn)
}

// Every runs fn every interval seconds, starting one interval from now,
// until cancelled. Use a positive interval for repetition; a
// nonpositive interval fires once on the next Update.
func (s *Scheduler) Every(seconds float64, fn func()) Handle {
	return s.add(seconds, seconds, fn)
}

func (s *Scheduler) add(delay, interval float64, fn func()) Handle {
	s.next++
	t := timer{id: Handle(s.next), at: s.now + delay, interval: interval, fn: fn}
	s.push(t)
	s.live++
	return t.id
}

// Cancel stops a timer; cancelling one that already fired is harmless.
func (s *Scheduler) Cancel(h Handle) {
	if s.isFiring && s.firing.id == h && !s.firing.dead {
		s.firing.dead = true // a timer cancelling itself from its callback
		s.live--
		return
	}
	for i := range s.heap {
		if s.heap[i].id == h && !s.heap[i].dead {
			s.heap[i].dead = true
			s.live--
			s.dead++
			if s.dead*2 > len(s.heap) {
				s.compact()
			}
			return
		}
	}
}

// Update advances time by dt seconds and fires what is due, earliest
// first; timers due at the same moment fire in the order they were
// scheduled. Callbacks may schedule or cancel timers.
// Use finite, nonnegative dt. Repeating timers fire once for every
// elapsed interval, including multiple times in a large step. Callbacks
// must return and must not recursively call Update.
func (s *Scheduler) Update(dt float64) {
	s.now += dt
	for len(s.heap) > 0 && s.heap[0].at <= s.now {
		due := s.pop()
		if due.dead {
			continue
		}
		// The timer is off the heap while it runs, so Cancel looks at
		// s.firing to let a callback cancel the timer it belongs to.
		s.firing, s.isFiring = due, true
		due.fn()
		dead := s.firing.dead
		s.firing, s.isFiring = timer{}, false
		switch {
		case dead:
		case due.interval <= 0:
			s.live--
		default:
			due.at += due.interval
			s.push(due)
		}
	}
	// Drop cancelled timers that sank to the front while callbacks ran.
	for len(s.heap) > 0 && s.heap[0].dead {
		s.pop()
	}
}

// Now is the scheduler's elapsed game time in seconds.
func (s *Scheduler) Now() float64 { return s.now }

// Pending counts scheduled timers that have not fired or been cancelled.
func (s *Scheduler) Pending() int { return s.live }

// push adds a timer to the min-heap.
func (s *Scheduler) push(t timer) {
	h := append(s.heap, t)
	i := len(h) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if !t.before(h[parent]) {
			break
		}
		h[i] = h[parent]
		i = parent
	}
	h[i] = t
	s.heap = h
}

// pop removes and returns the earliest timer; the heap must not be empty.
func (s *Scheduler) pop() timer {
	h := s.heap
	top := h[0]
	n := len(h) - 1
	h[0] = h[n]
	h[n] = timer{} // release the callback so it can be collected
	s.heap = h[:n]
	if n > 0 {
		s.siftDown(0)
	}
	if top.dead {
		s.dead--
	}
	return top
}

func (s *Scheduler) siftDown(i int) {
	h := s.heap
	n := len(h)
	t := h[i]
	for {
		left := 2*i + 1
		if left >= n {
			break
		}
		small := left
		if right := left + 1; right < n && h[right].before(h[left]) {
			small = right
		}
		if !h[small].before(t) {
			break
		}
		h[i] = h[small]
		i = small
	}
	h[i] = t
}

// compact drops cancelled timers from the heap, so a game that cancels
// timers due far in the future does not hold their callbacks until they
// come due.
func (s *Scheduler) compact() {
	kept := s.heap[:0]
	for _, t := range s.heap {
		if !t.dead {
			kept = append(kept, t)
		}
	}
	for i := len(kept); i < len(s.heap); i++ {
		s.heap[i] = timer{}
	}
	s.heap = kept
	s.dead = 0
	for i := len(kept)/2 - 1; i >= 0; i-- {
		s.siftDown(i)
	}
}

// Countdown is a simple timer a game polls rather than a callback: set
// it and ask each frame whether it has run out.
type Countdown struct {
	Left float64 // seconds remaining; may become negative on the final Update
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
