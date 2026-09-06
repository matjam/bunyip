package engine

import (
	"fmt"
	"time"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/platform"
)

type windowFamily struct {
	app                  eventSource
	root                 *Window
	windows              []*Window
	closing, audioPaused bool
	gamepads             [input.MaxGamepads]platform.GamepadState
}

func newWindowFamily(l *loop) *windowFamily {
	f := &windowFamily{app: l.app}
	w := &Window{ctx: l.ctx, loop: l, family: f}
	f.root = w
	f.attach(w)
	l.family = f
	l.handle = w
	l.ctx.owner = w
	return f
}
func (f *windowFamily) attach(w *Window) {
	f.windows = append(f.windows, w)
	if native, ok := w.loop.win.(*platform.Window); ok {
		w.loop.eventWindow = native
	} else {
		w.loop.eventWindow = new(platform.Window)
	}
	w.loop.clock.start = time.Now()
	w.loop.clock.last = w.loop.clock.start
	w.ctx.Input.SetStep(float32(w.loop.cfg.FixedStep.Seconds()))
}
func (f *windowFamily) closeChildren(parent *Window) {
	// A deferred continuation drains the rest even if a child's Shutdown or
	// Cleanup panics, matching the cleanup guarantees of a single window.
	defer func() {
		for _, w := range f.windows {
			if w.parent == parent && !w.closed && !w.closing {
				f.closeChildren(parent)
				break
			}
		}
	}()
	for i := len(f.windows) - 1; i >= 0; i-- {
		w := f.windows[i]
		if w.parent == parent {
			w.destroy()
		}
	}
}
func (f *windowFamily) dispatch(events []platform.Event) {
	for _, e := range events {
		if e.Kind == platform.EventWake {
			for _, w := range f.windows {
				if !w.closed {
					w.loop.ready = true
				}
			}
			continue
		}
		var target *Window
		if e.Window == nil {
			target = f.root
		} else {
			for _, w := range f.windows {
				if w.loop.eventWindow == e.Window {
					target = w
					break
				}
			}
		}
		if target == nil || target.closed || target.closing {
			continue
		}
		target.loop.ready = true
		target.loop.handleEvents([]platform.Event{e})
	}
}
func (f *windowFamily) feedGamepads(pads []platform.GamepadState) {
	var next [input.MaxGamepads]platform.GamepadState
	copy(next[:], pads)
	changed := next != f.gamepads
	f.gamepads = next
	for _, w := range f.windows {
		if w.closed {
			continue
		}
		if changed {
			w.loop.ready = true
		}
		for i, g := range next {
			w.loop.input.FeedGamepad(i, g.Connected, g.Name, g.Buttons, g.Axes)
			w.loop.input.FeedGamepadInfo(i, hook.GamepadInfo(g.Info))
		}
	}
}
func (f *windowFamily) applyPause() {
	paused := true
	for _, w := range f.windows {
		if !w.closed && !w.ctx.quit && !w.loop.paused() {
			paused = false
			break
		}
	}
	if paused == f.audioPaused {
		return
	}
	f.audioPaused = paused
	if f.root.ctx.Audio != nil {
		f.root.ctx.Audio.SetPaused(paused)
	}
}
func (f *windowFamily) sweep() {
	for _, w := range f.windows {
		if w != f.root && !w.closed && (w.ctx.quit || w.loop.win.Closed()) {
			w.destroy()
		}
	}
	live := f.windows[:0]
	for _, w := range f.windows {
		if !w.closed {
			live = append(live, w)
		}
	}
	clear(f.windows[len(live):])
	f.windows = live
	f.applyPause()
}
func (f *windowFamily) run() error {
	for !f.root.ctx.quit && !f.root.loop.win.Closed() {
		f.sweep()
		if f.root.ctx.quit || f.root.loop.win.Closed() {
			break
		}
		wait := true
		for _, w := range f.windows {
			l := w.loop
			l.wasPaused = l.paused()
			l.ready = l.ready || l.ctx.redraw
			l.ctx.redraw = false
			if !l.cfg.TurnBased || l.ready {
				wait = false
			}
		}
		events := f.app.Poll(wait)
		f.dispatch(events)
		// A platform can wake its blocking poll without a synthetic EventWake.
		if wait && len(events) == 0 {
			for _, w := range f.windows {
				w.loop.ready = true
			}
		}
		if f.root.ctx.quit || f.root.loop.win.Closed() {
			break
		}
		f.feedGamepads(f.app.Gamepads())
		now := time.Now()
		count := len(f.windows)
		for i := 0; i < count; i++ {
			w := f.windows[i]
			l := w.loop
			if !w.ctx.canCreateWindow() {
				continue
			}
			if l.cfg.TurnBased && !l.ready {
				continue
			}
			l.ready = false
			if err := l.advance(now); err != nil {
				return fmt.Errorf("bunyip: window %q: %w", l.cfg.Title, err)
			}
			if f.root.ctx.quit {
				break
			}
		}
		f.sweep()
		if f.root.loop.cfg.Headless && !f.root.loop.cfg.FixedClock && !wait {
			step := f.root.loop.cfg.FixedStep
			for _, w := range f.windows {
				if !w.loop.cfg.TurnBased {
					step = min(step, w.loop.cfg.FixedStep)
				}
			}
			if rest := step - time.Since(now); rest > 0 {
				time.Sleep(rest)
			}
		}
	}
	return nil
}
