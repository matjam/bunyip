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
	padConnected         bool // any controller in gamepads is connected

	// nextFrame is when a paced headless frame is due, one step after the
	// last one was due; zero until the first paced frame.
	nextFrame time.Time
}

// padPollInterval bounds the wait for events while a controller is
// connected. No backend's blocking poll returns for controller input
// (macOS reads GameController.framework, Windows XInput and Linux the
// kernel joystick devices, all after the poll), so a turn-based game
// that waited without a bound would not see a button until some other
// event woke it. Bounding the wait is simpler than having each backend
// call Wake on a controller change, and costs one empty poll and one
// controller read per interval, only while a controller is connected.
const padPollInterval = 10 * time.Millisecond

// headlessBehind is how many steps a paced headless loop may fall behind
// its schedule before it gives up catching up and starts afresh from now.
const headlessBehind = 3

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
	// A new window draws its first frame without waiting for an event. A
	// turn-based one would otherwise start blank on macOS and Windows,
	// where the events a new window raises arrive before the first Poll
	// and are not delivered.
	w.loop.ready = true
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
	f.padConnected = false
	for _, g := range next {
		f.padConnected = f.padConnected || g.Connected
	}
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
		now := time.Now()
		for _, w := range f.windows {
			l := w.loop
			l.wasPaused = l.paused()
			l.ready = l.ready || l.ctx.redraw
			l.ctx.redraw = false
		}
		// Only an event or a Wake makes a waiting turn-based window ready. A
		// poll that returns empty, from a timeout or an event the platform
		// did not translate, runs nothing that is not otherwise due.
		idle := f.idleFor(now)
		switch {
		case idle == 0:
			f.dispatch(f.app.Poll(false))
		case idle < 0:
			f.dispatch(f.app.Poll(true))
		default:
			f.dispatch(f.app.PollTimeout(idle))
		}
		if f.root.ctx.quit || f.root.loop.win.Closed() {
			break
		}
		f.feedGamepads(f.app.Gamepads())
		now = time.Now()
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
			if err := l.advance(now, l.shouldDraw()); err != nil {
				return fmt.Errorf("bunyip: window %q: %w", l.cfg.Title, err)
			}
			if f.root.ctx.quit {
				break
			}
		}
		f.sweep()
		if f.root.loop.cfg.Headless && !f.root.loop.cfg.FixedClock && idle == 0 {
			f.pace()
		} else {
			f.nextFrame = time.Time{}
		}
	}
	return nil
}

// idleFor is how long the next poll may wait for events: zero to not
// wait, negative for as long as it takes, otherwise the time until some
// window next needs the loop. While a controller is connected the wait
// is bounded by padPollInterval, since controller input wakes no poll.
func (f *windowFamily) idleFor(now time.Time) time.Duration {
	idle := time.Duration(-1)
	for _, w := range f.windows {
		d := w.loop.idleFor(now)
		if d == 0 {
			return 0
		}
		if d > 0 && (idle < 0 || d < idle) {
			idle = d
		}
	}
	if f.padConnected && (idle < 0 || idle > padPollInterval) {
		idle = padPollInterval
	}
	return idle
}

// pace holds a headless real-time loop to the shortest real-time step.
// Each frame is due one step after the previous one was due, so the time
// a sleep oversleeps is taken off the next wait rather than added to the
// frame; a loop more than headlessBehind steps late starts again from now
// rather than rushing to catch up.
func (f *windowFamily) pace() {
	step := f.root.loop.cfg.FixedStep
	for _, w := range f.windows {
		if !w.loop.cfg.TurnBased {
			step = min(step, w.loop.cfg.FixedStep)
		}
	}
	now := time.Now()
	next := f.nextFrame.Add(step)
	if f.nextFrame.IsZero() || now.Sub(next) > headlessBehind*step {
		next = now.Add(step)
	}
	f.nextFrame = next
	if rest := next.Sub(now); rest > 0 {
		time.Sleep(rest)
	}
}
