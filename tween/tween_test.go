package tween

import (
	"math"
	"testing"
)

func TestEasesEndpoints(t *testing.T) {
	for name, e := range map[string]Ease{"linear": Linear, "inquad": InQuad, "outquad": OutQuad, "inout": InOutQuad,
		"incubic": InCubic, "outcubic": OutCubic, "inoutcubic": InOutCubic, "insine": InSine, "outsine": OutSine,
		"inoutsine": InOutSine, "smooth": Smoothstep, "back": OutBack, "elastic": OutElastic, "bounce": OutBounce} {
		if v := e(0); math.Abs(float64(v)) > 1e-5 {
			t.Errorf("%s(0) = %v", name, v)
		}
		if v := e(1); math.Abs(float64(v-1)) > 1e-5 {
			t.Errorf("%s(1) = %v", name, v)
		}
	}
}

func TestTween(t *testing.T) {
	done := false
	tw := New(0, 10, 1, nil).OnDone(func() { done = true })
	if v := tw.Update(0.25); v != 2.5 {
		t.Fatalf("value %v", v)
	}
	tw.Update(0.5)
	if tw.Done() {
		t.Fatal("done early")
	}
	if v := tw.Update(1); v != 10 || !tw.Done() || !done {
		t.Fatalf("end: %v done=%v cb=%v", v, tw.Done(), done)
	}
	yo := New(0, 1, 1, nil)
	yo.Repeat, yo.YoYo = 1, true
	yo.Update(1.5)
	if v := yo.Value(); math.Abs(float64(v-0.5)) > 1e-5 || yo.Done() {
		t.Fatalf("yoyo mid second pass %v done=%v", v, yo.Done())
	}
	yo.Update(1)
	if !yo.Done() || yo.Value() != 0 {
		t.Fatalf("yoyo end %v", yo.Value())
	}
	delayed := New(0, 1, 1, nil)
	delayed.Delay = 1
	if v := delayed.Update(0.5); v != 0 {
		t.Fatalf("delay ignored: %v", v)
	}
}

func TestSequence(t *testing.T) {
	s := NewSequence(New(0, 1, 1, nil), New(1, 5, 1, nil))
	s.Update(0.5)
	if v := s.Update(1); math.Abs(float64(v-3)) > 1e-5 { // 1.5 s in: half way through step two
		t.Fatalf("sequence carried overshoot wrongly: %v", v)
	}
	if v := s.Update(5); v != 5 || !s.Done() {
		t.Fatalf("sequence end %v done=%v", v, s.Done())
	}
}
