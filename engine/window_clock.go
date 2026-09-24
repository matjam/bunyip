package engine

import "time"

type windowClock struct {
	start, last time.Time
	accumulator time.Duration
	frames      int64
}

func (l *loop) resetClock() {
	now := time.Now()
	l.clock = windowClock{start: now, last: now}
}

// advance owns one window's clock. The family polls events once and invokes it
// only for ready turn-based windows, or on every cycle for real-time windows.
// It updates as the clock says and then draws, unless draw is false: a
// window nobody can see still keeps its game time but renders nothing.
func (l *loop) advance(now time.Time, draw bool) error {
	l.overlay.toggle(l.ctx.Input)
	clock := &l.clock
	elapsed := now.Sub(clock.last)
	clock.last = now
	paused := l.paused()
	if l.wasPaused || paused {
		elapsed = 0
		clock.accumulator = 0
	}
	step := l.cfg.FixedStep
	catchUp := l.cfg.MaxCatchUp
	if catchUp <= 0 {
		catchUp = 250 * time.Millisecond
	}
	catchUp = max(catchUp, step)
	l.beginFrame(now)
	l.ctx.Time = now.Sub(clock.start).Seconds()
	if l.cfg.FixedClock && !l.cfg.TurnBased {
		l.ctx.Time = float64(clock.frames) * step.Seconds()
		l.ctx.Delta = step.Seconds() * l.ctx.timeScale
		clock.frames++
		if !paused {
			if err := l.update(); err != nil {
				return err
			}
		}
		l.ctx.Alpha = 0
		return l.drawIf(draw)
	}
	if l.cfg.TurnBased {
		l.ctx.Delta = elapsed.Seconds() * l.ctx.timeScale
		if !paused {
			if err := l.update(); err != nil {
				return err
			}
		}
		l.ctx.Alpha = 1
	} else {
		clock.accumulator = min(clock.accumulator+elapsed, catchUp)
		l.ctx.Delta = step.Seconds() * l.ctx.timeScale
		for steps := 0; clock.accumulator >= step; steps++ {
			if l.cfg.MaxSteps > 0 && steps >= l.cfg.MaxSteps {
				clock.accumulator = 0
				break
			}
			clock.accumulator -= step
			if err := l.update(); err != nil {
				return err
			}
		}
		l.ctx.Alpha = float32(clock.accumulator) / float32(step)
	}
	return l.drawIf(draw)
}

func (l *loop) drawIf(draw bool) error {
	if !draw {
		return nil
	}
	return l.draw()
}

// shouldDraw reports whether this window renders its next frame: when it
// can be seen, or when a close request is waiting for a paused game's
// Draw to see it. A hidden or minimised window skips the whole frame,
// presenting included, and draws again from the first frame it is seen.
func (l *loop) shouldDraw() bool {
	return l.ctx.visible || l.ctx.closeReq
}

// idleFor reports how long the loop may wait for events before this
// window needs it back: zero to not wait, negative for as long as it
// takes. A turn-based window waits for an event unless it is ready. A
// real-time window that is drawing never waits, since presenting paces
// it. A hidden real-time window waits for its next update, or for an
// event when it is paused and has none to run.
func (l *loop) idleFor(now time.Time) time.Duration {
	switch {
	case l.cfg.TurnBased:
		if l.ready {
			return 0
		}
		return -1
	case l.shouldDraw():
		return 0
	case l.paused():
		return -1
	case l.cfg.FixedClock:
		return l.cfg.FixedStep
	}
	return max(l.cfg.FixedStep-l.clock.accumulator-now.Sub(l.clock.last), 0)
}
