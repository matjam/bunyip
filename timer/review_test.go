package timer_test

import (
	"testing"
	"time"

	"github.com/matjam/bunyip/timer"
)

func returns(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Errorf("%s never returned", name)
	}
}

// A looping sequence whose steps take no time goes round at most once per
// update instead of forever.
func TestLoopingSequenceWithoutTimeReturns(t *testing.T) {
	n := 0
	seq := timer.NewSequence().Do(func() { n++ }).Until(func() bool { return true }).Loop()
	returns(t, "Do+Until loop", func() { seq.Update(1.0 / 60) })
	seq2 := timer.NewSequence().Do(func() {}).Wait(0).Loop()
	returns(t, "Do+Wait(0) loop", func() { seq2.Update(1.0 / 60) })
	if n == 0 {
		t.Error("the Do step never ran")
	}
}
