package tween_test

import (
	"testing"
	"time"

	"github.com/matjam/bunyip/tween"
)

// A tween of no duration that repeats forever finishes at once rather
// than looping inside Update.
func TestZeroDurationForeverFinishes(t *testing.T) {
	tw := tween.New(0, 1, 0, nil)
	tw.Repeat = -1
	done := make(chan float32, 1)
	go func() { done <- tw.Update(1.0 / 60) }()
	select {
	case v := <-done:
		if v != 1 || !tw.Done() {
			t.Errorf("value %v done %v, want 1 and done", v, tw.Done())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Update never returned")
	}
}

// Reset after an odd number of yo-yo plays starts forwards again.
func TestYoYoResetStartsForwards(t *testing.T) {
	tw := tween.New(0, 10, 1, nil)
	tw.Repeat, tw.YoYo = 1, true
	tw.Update(1.5) // past the first play, heading back
	tw.Update(1)   // done
	tw.Reset()
	if v := tw.Update(0); v != 0 {
		t.Errorf("after Reset the value starts at %v, want 0", v)
	}
	if v := tw.Update(0.5); v != 5 {
		t.Errorf("halfway after Reset is %v, want 5", v)
	}
}
