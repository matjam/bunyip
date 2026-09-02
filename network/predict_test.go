package network

import (
	"math"
	"testing"
)

func lerp(a, b float32, k float32) float32 { return a + (b-a)*k }

func TestInterpolator(t *testing.T) {
	var in Interpolator[float32]
	if _, ok := in.At(1, lerp); ok {
		t.Error("empty interpolator returned a value")
	}
	in.Add(0, 0)
	in.Add(2, 20)
	in.Add(1, 10) // out of order
	in.Delay = 0.5
	v, _ := in.At(2, lerp) // 1.5 -> 15
	if v != 15 {
		t.Errorf("At(2) = %v, want 15", v)
	}
	if v, _ := in.At(10, lerp); v != 20 {
		t.Errorf("past the end = %v, want the newest 20", v)
	}
	if v, _ := in.At(0.1, lerp); v != 0 {
		t.Errorf("before the start = %v, want the oldest 0", v)
	}
	in.Keep = 2
	in.Add(3, 30)
	if len(in.snaps) != 2 || in.snaps[0].time != 2 {
		t.Errorf("kept %v, want the newest two", in.snaps)
	}
}

func TestPredictor(t *testing.T) {
	step := func(x float32, dx float32) float32 { return x + dx }
	p := NewPredictor(float32(0), step)
	s1 := p.Apply(1)
	p.Apply(2)
	p.Apply(3)
	if p.State() != 6 || p.Pending() != 3 {
		t.Fatalf("predicted %v with %d pending", p.State(), p.Pending())
	}
	// The server saw the first input and disagrees slightly (it was
	// pushed); the other two replay on top of its state.
	p.Reconcile(s1, 1.5)
	if p.State() != 6.5 || p.Pending() != 2 {
		t.Errorf("after reconcile %v with %d pending, want 6.5 and 2", p.State(), p.Pending())
	}
}

func TestHistoryAndClock(t *testing.T) {
	var h History[int]
	for i := range 10 {
		h.Record(float64(i), i*10)
	}
	if v, ok := h.At(4.5); !ok || v != 40 {
		t.Errorf("At(4.5) = %v %v", v, ok)
	}
	if _, ok := h.At(-1); ok {
		t.Error("history returned a state older than it keeps")
	}
	var c Clock
	if c.Ready() {
		t.Error("clock ready before a sample")
	}
	// The server runs 100 units ahead; a symmetric 0.1 round trip.
	c.Sample(10, 110.05, 10.1)
	if math.Abs(c.ServerTime(20)-120) > 1e-6 || math.Abs(c.RTT()-0.1) > 1e-9 {
		t.Errorf("offset %v rtt %v", c.ServerTime(0), c.RTT())
	}
	c.Sample(20, 120.5, 21) // a slow, less trusted sample
	if d := c.ServerTime(0); d < 99.5 || d > 100.5 {
		t.Errorf("a slow sample moved the offset to %v", d)
	}
}
