package engine

import (
	"testing"
	"time"
)

// TestHeadlessPacingHoldsTheRate runs a headless real-time loop at 60 Hz
// on the wall clock for two seconds. Each frame is due a step after the
// last one was due, so a sleep that oversleeps shortens the next wait
// instead of stretching every frame: the count is 120 give or take a
// frame at either end, where a relative sleep each frame came to about
// 109 on macOS.
func TestHeadlessPacingHoldsTheRate(t *testing.T) {
	const run = 2 * time.Second
	g := &pauseGame{}
	l := newRunningPauseLoop(Config{Headless: true, FixedStep: time.Second / 60}, g)
	l.app = &headlessApp{step: l.cfg.FixedStep}
	l.ctx.app = l.app
	var start time.Time
	frames := 0
	g.draw = func(c *Context) {
		now := time.Now()
		if start.IsZero() {
			start = now
			return
		}
		if now.Sub(start) >= run {
			c.Quit()
			return
		}
		frames++
	}
	if err := l.run(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d frames in %v", frames, run)
	if want := int(run / l.cfg.FixedStep); frames < want-3 || frames > want+3 {
		t.Errorf("%d frames in %v at 60 Hz, want %d ± 3", frames, run, want)
	}
}
