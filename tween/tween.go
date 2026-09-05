// Package tween animates values over time. A Tween moves a number from
// one value to another with an easing curve, and Sequence chains them.
//
// To make a tween, call New with a start, an end, a duration in seconds
// and an Ease. The usual easing curves are provided, and any
// func(t float32) float32 works. Call Update each step to advance a
// tween, then read Value, or set OnDone to be called when it finishes.
// Use tweens for menus sliding in, health bars draining, cameras easing
// to a target and damage numbers floating up. NewSequence runs several
// in order. For keyframed curves over vectors, colours and component
// fields, see the anim package.
// Tweens and sequences are not safe for concurrent use; advance them
// with finite, nonnegative steps on the game loop goroutine.
package tween

import "math"

// Ease maps progress t in [0,1] to eased progress.
type Ease func(t float32) float32

// Easing curves. In* start slowly, Out* end slowly, InOut* do both.
var (
	Linear     Ease = func(t float32) float32 { return t }
	InQuad     Ease = func(t float32) float32 { return t * t }
	OutQuad    Ease = func(t float32) float32 { return t * (2 - t) }
	InOutQuad  Ease = func(t float32) float32 { return inOut(t, InQuad, OutQuad) }
	InCubic    Ease = func(t float32) float32 { return t * t * t }
	OutCubic   Ease = func(t float32) float32 { u := 1 - t; return 1 - u*u*u }
	InOutCubic Ease = func(t float32) float32 { return inOut(t, InCubic, OutCubic) }
	InSine     Ease = func(t float32) float32 { return 1 - cos(t*math.Pi/2) }
	OutSine    Ease = func(t float32) float32 { return sin(t * math.Pi / 2) }
	InOutSine  Ease = func(t float32) float32 { return (1 - cos(t*math.Pi)) / 2 }
	Smoothstep Ease = func(t float32) float32 { return t * t * (3 - 2*t) }
	OutBack    Ease = func(t float32) float32 {
		const c1, c3 = 1.70158, 2.70158
		u := t - 1
		return 1 + c3*u*u*u + c1*u*u
	}
	OutElastic Ease = func(t float32) float32 {
		if t <= 0 || t >= 1 {
			return clamp01(t)
		}
		const c4 = 2 * math.Pi / 3
		return pow2(-10*t)*sin((t*10-0.75)*c4) + 1
	}
	OutBounce Ease = func(t float32) float32 {
		const n1, d1 = 7.5625, 2.75
		switch {
		case t < 1/d1:
			return n1 * t * t
		case t < 2/d1:
			t -= 1.5 / d1
			return n1*t*t + 0.75
		case t < 2.5/d1:
			t -= 2.25 / d1
			return n1*t*t + 0.9375
		default:
			t -= 2.625 / d1
			return n1*t*t + 0.984375
		}
	}
)

func inOut(t float32, in, out Ease) float32 {
	if t < 0.5 {
		return in(t*2) / 2
	}
	return 0.5 + out(t*2-1)/2
}

func sin(x float32) float32        { return float32(math.Sin(float64(x))) }
func cos(x float32) float32        { return float32(math.Cos(float64(x))) }
func pow2(x float32) float32       { return float32(math.Pow(2, float64(x))) }
func clamp01(t float32) float32    { return max(0, min(1, t)) }
func lerp(a, b, t float32) float32 { return a + (b-a)*t }

// Tween moves a value from From to To over Duration seconds.
type Tween struct {
	From, To float32 // endpoints of the forward play
	Duration float32 // seconds per play; nonpositive completes after Delay
	Ease     Ease    // progress mapping; nil means linear
	Delay    float32 // seconds before movement starts
	Repeat   int     // extra plays after the first; -1 forever
	YoYo     bool    // alternate direction on repeats

	elapsed  float32
	plays    int
	done     bool
	reversed bool    // a YoYo play running from To back to From
	over     float32 // time past the end on the update that finished
	onDone   func()
}

// New makes a tween; a nil ease is linear.
func New(from, to, seconds float32, ease Ease) *Tween {
	if ease == nil {
		ease = Linear
	}
	return &Tween{From: from, To: to, Duration: seconds, Ease: ease}
}

// OnDone replaces the callback called synchronously by the Update that
// finishes the tween. Registration after completion does not invoke it;
// Reset permits it to run again on the next completed playthrough.
func (tw *Tween) OnDone(f func()) *Tween { tw.onDone = f; return tw }

// Update advances by dt seconds and returns the current value.
func (tw *Tween) Update(dt float32) float32 {
	if tw.done {
		return tw.Value()
	}
	tw.elapsed += dt
	if tw.Duration <= 0 {
		// Nothing to play: the tween is at its end as soon as the delay
		// has passed, however often it was asked to repeat.
		if tw.elapsed >= tw.Delay {
			tw.over = tw.elapsed - tw.Delay
			tw.elapsed = tw.Delay
			tw.done = true
			if tw.onDone != nil {
				tw.onDone()
			}
		}
		return tw.Value()
	}
	for tw.elapsed-tw.Delay >= tw.Duration {
		if tw.Repeat >= 0 && tw.plays >= tw.Repeat {
			tw.over = tw.elapsed - tw.Delay - tw.Duration
			tw.elapsed = tw.Delay + tw.Duration
			tw.done = true
			if tw.onDone != nil {
				tw.onDone()
			}
			break
		}
		tw.plays++
		tw.elapsed -= tw.Duration
		if tw.YoYo {
			tw.reversed = !tw.reversed
		}
	}
	return tw.Value()
}

// Progress is eased progress. The input to Ease is clamped to [0,1],
// but curves such as OutBack and OutElastic may overshoot that range.
// A nonpositive Duration reports 1, including while Delay is pending.
func (tw *Tween) Progress() float32 {
	if tw.Duration <= 0 {
		return 1
	}
	t := clamp01((tw.elapsed - tw.Delay) / tw.Duration)
	if tw.Ease == nil {
		return t
	}
	return tw.Ease(t)
}

// Value is the current value.
func (tw *Tween) Value() float32 {
	if tw.reversed {
		return lerp(tw.To, tw.From, tw.Progress())
	}
	return lerp(tw.From, tw.To, tw.Progress())
}

// Done reports whether the tween has finished.
func (tw *Tween) Done() bool { return tw.done }

// Reset starts the tween over, forwards.
func (tw *Tween) Reset() {
	tw.elapsed, tw.plays, tw.done, tw.over = 0, 0, false, 0
	tw.reversed = false
}

// Sequence plays tweens one after another.
type Sequence struct {
	steps []*Tween
	i     int
}

// NewSequence chains tweens.
func NewSequence(steps ...*Tween) *Sequence { return &Sequence{steps: steps} }

// Update advances the sequence and returns the current tween's value.
func (s *Sequence) Update(dt float32) float32 {
	for s.i < len(s.steps) {
		tw := s.steps[s.i]
		v := tw.Update(dt)
		if !tw.Done() {
			return v
		}
		// Carry the overshoot into the next step.
		s.i++
		if s.i < len(s.steps) {
			dt = max(tw.over, 0)
			continue
		}
		return v
	}
	if len(s.steps) == 0 {
		return 0
	}
	return s.steps[len(s.steps)-1].Value()
}

// Done reports whether every step has finished.
func (s *Sequence) Done() bool { return s.i >= len(s.steps) }

// Reset restarts from the first step.
func (s *Sequence) Reset() {
	s.i = 0
	for _, tw := range s.steps {
		tw.Reset()
	}
}
