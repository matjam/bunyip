package timer

// Sequence runs steps one after another on game time, the way a
// coroutine would: wait a while, do something, wait until a condition
// holds, run a function every update until it says it is done. It is
// how a cutscene, a turn's animation, a boss's attack pattern or a
// tutorial is written as a list rather than a state machine:
//
//	seq := timer.NewSequence().
//		Do(func() { camera.PanTo(door) }).
//		Wait(1).
//		Until(func() bool { return camera.Arrived() }).
//		Do(func() { door.Open() }).
//		Run(func(dt float32) bool { return hero.WalkTowards(door, dt) })
//
// The game calls Update each step; Done reports when the last step has
// finished. Steps run on the goroutine that calls Update.
type Sequence struct {
	steps []seqStep
	i     int
	wait  float32
	loop  bool
}

type seqStep struct {
	wait  float32
	do    func()
	until func() bool
	run   func(dt float32) bool
}

// NewSequence starts an empty sequence; add steps with the builder
// methods, which return the sequence for chaining.
func NewSequence() *Sequence { return &Sequence{} }

// Wait pauses for a number of seconds.
func (s *Sequence) Wait(seconds float32) *Sequence {
	s.steps = append(s.steps, seqStep{wait: seconds})
	return s
}

// Do runs a function once and moves on in the same update.
func (s *Sequence) Do(fn func()) *Sequence {
	s.steps = append(s.steps, seqStep{do: fn})
	return s
}

// Until waits, checking each update, until the condition holds.
func (s *Sequence) Until(cond func() bool) *Sequence {
	s.steps = append(s.steps, seqStep{until: cond})
	return s
}

// Run calls the function every update with the step until it returns
// true: a movement to carry out, an animation to play through.
func (s *Sequence) Run(fn func(dt float32) bool) *Sequence {
	s.steps = append(s.steps, seqStep{run: fn})
	return s
}

// Loop makes the sequence start over when it ends, for patrols and
// idle behaviours.
func (s *Sequence) Loop() *Sequence { s.loop = true; return s }

// Update advances the sequence by dt seconds, running as many steps as
// finish within it. It reports whether the sequence is done.
// Wait carries leftover time into following steps. A completed Run
// consumes the entire remaining step, so later Run steps in the same
// call receive zero dt. Use finite, nonnegative dt and do not call Update
// recursively from a step.
func (s *Sequence) Update(dt float32) bool {
	wrapped := false // a looping sequence goes round at most once per update
	for s.i < len(s.steps) {
		st := &s.steps[s.i]
		switch {
		case st.do != nil:
			st.do()
		case st.until != nil:
			if !st.until() {
				return false
			}
		case st.run != nil:
			if !st.run(dt) {
				return false
			}
			dt = 0 // the rest of this update belongs to the step that ran
		default:
			s.wait += dt
			if s.wait < st.wait {
				return false
			}
			dt = s.wait - st.wait // carry what is left into the next step
			s.wait = 0
		}
		s.i++
		if s.i >= len(s.steps) && s.loop {
			s.i = 0
			if dt <= 0 || wrapped {
				// Steps that take no time would otherwise loop forever
				// inside one update.
				return false
			}
			wrapped = true
		}
	}
	return true
}

// Done reports whether every step has finished; a looping sequence is
// never done unless Skip was called or it has no steps.
func (s *Sequence) Done() bool { return s.i >= len(s.steps) }

// Reset starts the sequence over from its first step.
func (s *Sequence) Reset() { s.i, s.wait = 0, 0 }

// Skip jumps to the end, for a player who presses through a cutscene;
// Do steps that have not run yet do not run.
func (s *Sequence) Skip() { s.i, s.wait = len(s.steps), 0 }
