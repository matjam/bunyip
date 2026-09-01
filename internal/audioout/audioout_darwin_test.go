package audioout

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestOpenPullsFrames opens the default output and waits for Core Audio to
// ask for frames. It skips when no output device exists.
func TestOpenPullsFrames(t *testing.T) {
	var frames atomic.Int64
	d, err := Open(48000, func(out []float32) {
		clear(out)
		frames.Add(int64(len(out) / 2))
	})
	if err != nil {
		t.Skipf("no audio output: %v", err)
	}
	defer d.Close()
	deadline := time.Now().Add(2 * time.Second)
	for frames.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if frames.Load() == 0 {
		t.Fatal("render callback never ran")
	}
	t.Logf("rendered %d frames", frames.Load())
}
