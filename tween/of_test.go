package tween_test

import (
	"testing"

	"github.com/matjam/bunyip/lin"
	"github.com/matjam/bunyip/tween"
)

func TestOfYoYoMatchesScalar(t *testing.T) {
	for _, tc := range []struct {
		name string
		ease tween.Ease
	}{
		{"linear", nil},
		{"asymmetric easing", tween.InQuad},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scalar := tween.New(2, 10, 1, tc.ease)
			vector := tween.NewVec3(lin.V3(2, 4, 6), lin.V3(10, 20, 30), 1, tc.ease)
			scalar.Repeat, scalar.YoYo, scalar.Delay = 1, true, 0.25
			vector.Repeat, vector.YoYo, vector.Delay = 1, true, 0.25
			for _, dt := range []float32{0.125, 0.375, 0.75, 0.25, 0.25, 0.5, 1} {
				v := scalar.Update(dt)
				want := lin.V3(v, 2*v, 3*v)
				if got := vector.Update(dt); got != want || vector.Done() != scalar.Done() {
					t.Fatalf("after step %g: vector=%v done=%t, want %v done=%t", dt, got, vector.Done(), want, scalar.Done())
				}
			}
			vector.Reset()
			if got := vector.Update(0); got != lin.V3(2, 4, 6) || vector.Done() {
				t.Fatalf("reset: value=%v done=%t", got, vector.Done())
			}
		})
	}
}

func TestOfOnDoneKeepsValueType(t *testing.T) {
	tw := tween.NewVec2(lin.V2(1, 2), lin.V2(3, 4), 1, nil)
	calls := 0
	returned := any(tw.OnDone(func() { calls++ }))
	chained, ok := returned.(*tween.Of[lin.Vec2])
	if !ok || chained != tw {
		t.Fatalf("OnDone returned %T, want the same *Of[lin.Vec2]", returned)
	}
	if got := chained.Update(1); got != lin.V2(3, 4) || calls != 1 {
		t.Fatalf("completed value=%v calls=%d", got, calls)
	}
	chained.Update(1)
	if calls != 1 {
		t.Fatalf("completion callback repeated: %d calls", calls)
	}
	chained.Reset()
	chained.Update(1)
	if calls != 2 {
		t.Fatalf("reset did not rearm callback: %d calls", calls)
	}
}
