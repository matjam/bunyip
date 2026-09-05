package network

import "sort"

// Interpolator smooths remote state between the snapshots a server
// sends: it holds timestamped values and returns the value at a time a
// little in the past, blended between the two snapshots around it, so
// other players' ships move at the server's rate without stutter
// whatever the packet timing. T is whatever the game sends (a position,
// a whole entity state); the blend function says how to mix two of
// them.
type Interpolator[T any] struct {
	// Delay is subtracted from the time passed to At, in the snapshots'
	// time units. Two or three send intervals hide most jitter.
	// Nonpositive values mean one tenth of a unit.
	Delay float64
	// Keep is how many snapshots to keep; zero means 32.
	Keep  int
	snaps []snapshot[T]
}

type snapshot[T any] struct {
	time  float64
	value T
}

// Add records a snapshot taken at a time. Snapshots may arrive out of
// order; they are kept sorted.
func (in *Interpolator[T]) Add(time float64, value T) {
	i := sort.Search(len(in.snaps), func(i int) bool { return in.snaps[i].time > time })
	in.snaps = append(in.snaps, snapshot[T]{})
	copy(in.snaps[i+1:], in.snaps[i:])
	in.snaps[i] = snapshot[T]{time, value}
	keep := in.Keep
	if keep <= 0 {
		keep = 32
	}
	if len(in.snaps) > keep {
		in.snaps = in.snaps[len(in.snaps)-keep:]
	}
}

// At returns the value at a time: the blend of the snapshots either side
// of time - Delay, the newest snapshot when time is past them all (no
// extrapolation), and false with the zero value before any snapshot.
// blend mixes two values, k from 0 (a) to 1 (b).
func (in *Interpolator[T]) At(time float64, blend func(a, b T, k float32) T) (T, bool) {
	var zero T
	if len(in.snaps) == 0 {
		return zero, false
	}
	delay := in.Delay
	if delay <= 0 {
		delay = 0.1
	}
	t := time - delay
	if t <= in.snaps[0].time {
		return in.snaps[0].value, true
	}
	last := in.snaps[len(in.snaps)-1]
	if t >= last.time {
		return last.value, true
	}
	i := sort.Search(len(in.snaps), func(i int) bool { return in.snaps[i].time > t })
	a, b := in.snaps[i-1], in.snaps[i]
	k := float32((t - a.time) / (b.time - a.time))
	return blend(a.value, b.value, k), true
}

// Latest returns the newest snapshot, for things that should not be
// blended.
func (in *Interpolator[T]) Latest() (T, bool) {
	var zero T
	if len(in.snaps) == 0 {
		return zero, false
	}
	return in.snaps[len(in.snaps)-1].value, true
}

// Predictor runs the local player's inputs ahead of the server so
// controls feel instant, then reconciles when the server's state
// arrives: it rewinds to the server's state, replays the inputs the
// server has not seen yet, and the result is where the player should
// be. S is the player's state and I one update's input; Step applies an
// input to a state for one fixed step and must be the same function the
// server runs.
type Predictor[S, I any] struct {
	Step    func(state S, in I) S // non-nil deterministic update, matching the server
	state   S
	pending []stamped[I]
	next    uint32
}

type stamped[I any] struct {
	seq uint32
	in  I
}

// NewPredictor starts prediction from a state with a step function.
func NewPredictor[S, I any](state S, step func(S, I) S) *Predictor[S, I] {
	return &Predictor[S, I]{Step: step, state: state, next: 1}
}

// Apply runs one input locally and returns its sequence number, which
// the game sends to the server with the input.
func (p *Predictor[S, I]) Apply(in I) (seq uint32) {
	seq = p.next
	p.next++
	p.pending = append(p.pending, stamped[I]{seq, in})
	p.state = p.Step(p.state, in)
	return seq
}

// Reconcile takes the server's state after it applied the input with
// sequence ack, drops the inputs up to it, and replays the rest on top,
// so the local state agrees with the server plus what it has not yet
// seen.
func (p *Predictor[S, I]) Reconcile(ack uint32, server S) {
	i := 0
	for i < len(p.pending) && p.pending[i].seq <= ack {
		i++
	}
	p.pending = p.pending[i:]
	p.state = server
	for _, s := range p.pending {
		p.state = p.Step(p.state, s.in)
	}
}

// State is the predicted state right now.
func (p *Predictor[S, I]) State() S { return p.state }

// Pending is how many inputs the server has not acknowledged, a measure
// of the round trip in steps.
func (p *Predictor[S, I]) Pending() int { return len(p.pending) }

// History keeps past states by tick for lag compensation: when a
// client's shot arrives, the server rewinds the targets to where that
// client saw them (its interpolation time) before testing the hit.
type History[T any] struct {
	// Keep is how many ticks to keep; zero means 64.
	Keep  int
	items []snapshot[T]
}

// Record stores the state at a tick or time. Record times must be
// nondecreasing; At uses binary search over insertion order. Values
// are shallow copies, so do not mutate referenced data in a saved state.
func (h *History[T]) Record(time float64, value T) {
	h.items = append(h.items, snapshot[T]{time, value})
	keep := h.Keep
	if keep <= 0 {
		keep = 64
	}
	if len(h.items) > keep {
		h.items = h.items[len(h.items)-keep:]
	}
}

// At returns the state recorded at or just before a time, false when
// nothing that old is kept.
func (h *History[T]) At(time float64) (T, bool) {
	var zero T
	i := sort.Search(len(h.items), func(i int) bool { return h.items[i].time > time })
	if i == 0 {
		return zero, false
	}
	return h.items[i-1].value, true
}

// Clock estimates the server's time from ping replies, so a client can
// timestamp what it sends and interpolate on the server's timeline.
// Feed it the local time a ping was sent, the server's time in the
// reply, and the local time the reply arrived.
type Clock struct {
	offset  float64 // server time minus local time
	rtt     float64
	samples int
}

// Sample records one ping round trip; the offset is smoothed over the
// samples, weighting quick replies more since they are more accurate.
func (c *Clock) Sample(sent, serverTime, received float64) {
	rtt := received - sent
	offset := serverTime - (sent + rtt/2)
	c.samples++
	if c.samples == 1 {
		c.offset, c.rtt = offset, rtt
		return
	}
	// A quicker round trip than usual is a better sample.
	w := 0.1
	if rtt < c.rtt {
		w = 0.5
	}
	c.offset += (offset - c.offset) * w
	c.rtt += (rtt - c.rtt) * 0.2
}

// ServerTime converts a local time to the server's.
func (c *Clock) ServerTime(local float64) float64 { return local + c.offset }

// RTT is the smoothed round-trip time.
func (c *Clock) RTT() float64 { return c.rtt }

// Ready reports whether the clock has at least one sample.
func (c *Clock) Ready() bool { return c.samples > 0 }
