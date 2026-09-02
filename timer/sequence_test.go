package timer

import "testing"

func TestSequence(t *testing.T) {
	var log []string
	ready := false
	walked := float32(0)
	seq := NewSequence().
		Do(func() { log = append(log, "start") }).
		Wait(1).
		Do(func() { log = append(log, "after wait") }).
		Until(func() bool { return ready }).
		Run(func(dt float32) bool { walked += dt; return walked >= 0.5 }).
		Do(func() { log = append(log, "end") })
	if seq.Update(0.4) || len(log) != 1 {
		t.Fatalf("after 0.4s: log %v", log)
	}
	// 0.7 more finishes the wait with 0.1 to spare, runs the Do, and
	// stalls on Until.
	if seq.Update(0.7) || len(log) != 2 || log[1] != "after wait" {
		t.Fatalf("after 1.1s: log %v", log)
	}
	seq.Update(0.2)
	if len(log) != 2 {
		t.Fatal("Until let the sequence through")
	}
	ready = true
	for i := 0; i < 10 && !seq.Done(); i++ {
		seq.Update(0.2)
	}
	if !seq.Done() || log[len(log)-1] != "end" || walked < 0.5 {
		t.Errorf("done %v log %v walked %v", seq.Done(), log, walked)
	}
	seq.Reset()
	if seq.Done() {
		t.Error("Reset did not restart")
	}
	seq.Skip()
	if !seq.Done() {
		t.Error("Skip did not finish")
	}

	// A loop never finishes and runs its Do steps again.
	count := 0
	loop := NewSequence().Do(func() { count++ }).Wait(0.5).Loop()
	for range 10 {
		loop.Update(0.25)
	}
	if loop.Done() || count < 5 {
		t.Errorf("loop done %v ran %d times", loop.Done(), count)
	}
}
