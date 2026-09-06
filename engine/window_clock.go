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
func (l *loop) advance(now time.Time) error {
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
		return l.draw()
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
	return l.draw()
}
