package engine

import (
	"image"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/hook"
	"github.com/matjam/bunyip/internal/platform"
)

type pauseEvents struct {
	eventSource
	poll func(bool) []platform.Event
}

func (a pauseEvents) Poll(wait bool) []platform.Event   { return a.poll(wait) }
func (a pauseEvents) Gamepads() []platform.GamepadState { return nil }

type pauseGraphics struct{ hook.Graphics }

func (pauseGraphics) Begin([4]float32) (bool, error) { return true, nil }
func (pauseGraphics) End(bool) (*image.RGBA, error)  { return nil, nil }
func (pauseGraphics) SetTime(float64)                {}

type pauseGame struct {
	deltas []float64
	draws  int
	draw   func(*Context)
}

func (g *pauseGame) Update(c *Context) error {
	g.deltas = append(g.deltas, c.Delta)
	return nil
}

func (g *pauseGame) Draw(c *Context) error {
	g.draws++
	if g.draw != nil {
		g.draw(c)
	}
	return nil
}

func newRunningPauseLoop(cfg Config, game *pauseGame) *loop {
	l := newPauseLoop(cfg)
	l.ctx.Input = l.input.Game().(*input.State)
	l.ctx.Gfx = &gfx.Graphics{}
	l.ctx.timeScale = 1
	l.win = &headlessWindow{w: 100, h: 100}
	l.gfx = pauseGraphics{}
	l.game = game
	return l
}

func TestTurnBasedPauseStopsUpdates(t *testing.T) {
	// A hidden window draws nothing whether or not it is paused; an
	// unfocused one that can still be seen draws.
	for _, tc := range []struct {
		name           string
		cfg            Config
		events         []platform.Event
		updates, draws int
	}{
		{"hidden", Config{PauseHidden: true}, []platform.Event{{Kind: platform.EventVisible}}, 0, 0},
		{"unfocused", Config{PauseUnfocused: true}, []platform.Event{{Kind: platform.EventFocus}}, 0, 1},
		{"both", Config{PauseHidden: true, PauseUnfocused: true}, []platform.Event{{Kind: platform.EventVisible}, {Kind: platform.EventFocus}}, 0, 0},
		{"disabled", Config{}, []platform.Event{{Kind: platform.EventVisible}, {Kind: platform.EventFocus}}, 1, 0},
		{"hidden with focus option only", Config{PauseUnfocused: true}, []platform.Event{{Kind: platform.EventVisible}}, 1, 0},
		{"unfocused with hidden option only", Config{PauseHidden: true}, []platform.Event{{Kind: platform.EventFocus}}, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := tc.cfg
				cfg.TurnBased, cfg.FixedStep = true, 10*time.Millisecond
				g := &pauseGame{}
				l := newRunningPauseLoop(cfg, g)
				polls := 0
				l.app = pauseEvents{poll: func(wait bool) []platform.Event {
					polls++
					if wait != (polls > 1) {
						t.Errorf("poll %d waited %v; only the first frame's poll does not wait", polls, wait)
					}
					time.Sleep(time.Second)
					switch polls {
					case 1:
						return nil // the first frame, drawn unasked
					case 2:
						return tc.events
					}
					return []platform.Event{{Kind: platform.EventClose}}
				}}
				if err := l.run(); err != nil {
					t.Fatal(err)
				}
				// The first frame only draws: no Update.
				if len(g.deltas) != tc.updates {
					t.Errorf("updates = %d, want %d", len(g.deltas), tc.updates)
				}
				if g.draws != 1+tc.draws || polls != 3 {
					t.Errorf("draws/polls = %d/%d, want 1+%d/3", g.draws, polls, tc.draws)
				}
			})
		})
	}
}

func TestLoopPauseExcludesBlockedResumeTime(t *testing.T) {
	const step = 10 * time.Millisecond
	for _, mode := range []struct {
		name        string
		turn, fixed bool
		want        []float64
	}{
		{"turn based", true, false, []float64{0.005, 0, 0.005}},
		{"fixed step", false, false, []float64{0.005, 0.005}},
		{"fixed clock", false, true, []float64{0.005, 0.005, 0.005}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				g := &pauseGame{}
				l := newRunningPauseLoop(Config{TurnBased: mode.turn, FixedClock: mode.fixed,
					FixedStep: step, PauseHidden: true, PauseUnfocused: true}, g)
				l.ctx.SetTimeScale(0.5)
				type batch struct {
					delay  time.Duration
					events []platform.Event
				}
				wake := []platform.Event{{Kind: platform.EventWake}}
				batches := []batch{
					{step, wake},
					{step / 2, []platform.Event{{Kind: platform.EventVisible}, {Kind: platform.EventFocus}}},
					{time.Hour, wake}, // waking while paused must not update
					{time.Hour, []platform.Event{{Kind: platform.EventFocus, Focused: true}}},
					{time.Hour, []platform.Event{{Kind: platform.EventVisible, Visible: true}}},
					{step, wake},
					{0, []platform.Event{{Kind: platform.EventClose}}},
				}
				// A turn-based loop waits from its second poll: the first
				// frame is drawn unasked. A real-time one waits only while it
				// is hidden and paused, with nothing to update or draw.
				wantWaits := []bool{false, false, true, true, true, false, false}
				if mode.turn {
					wantWaits = []bool{false, true, true, true, true, true, true}
				}
				var waits []bool
				polls := 0
				l.app = pauseEvents{poll: func(wait bool) []platform.Event {
					waits = append(waits, wait)
					b := batches[polls]
					polls++
					time.Sleep(b.delay)
					return b.events
				}}
				if err := l.run(); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(waits, wantWaits) {
					t.Errorf("Poll waits = %v, want %v", waits, wantWaits)
				}
				if !slices.Equal(g.deltas, mode.want) {
					t.Errorf("update deltas = %v, want %v", g.deltas, mode.want)
				}
				// Drawn before hiding and after showing; never while hidden.
				if g.draws != 3 {
					t.Errorf("draws = %d, want 3", g.draws)
				}
				wantTime := (3*time.Hour + 2*step + step/2).Seconds()
				if mode.fixed {
					wantTime = 5 * step.Seconds()
				}
				if l.ctx.Time != wantTime {
					t.Errorf("Time = %v, want %v", l.ctx.Time, wantTime)
				}
				if l.ctx.Audio.Paused() {
					t.Error("audio stayed paused after focus and visibility returned")
				}
			})
		})
	}
}

type wakingPauseEvents struct {
	eventSource
	events chan []platform.Event
	wake   chan struct{}
	waits  []bool
}

func (a *wakingPauseEvents) Poll(wait bool) []platform.Event {
	a.waits = append(a.waits, wait)
	if !wait {
		return nil
	}
	select {
	case events := <-a.events:
		return events
	case <-a.wake:
		// Every backend delivers a Wake as an event.
		return []platform.Event{{Kind: platform.EventWake}}
	}
}

func (a *wakingPauseEvents) Gamepads() []platform.GamepadState { return nil }
func (a *wakingPauseEvents) Wake()                             { a.wake <- struct{}{} }

// A paused game that can still be seen draws, redraws on request, wakes
// and sees a close request in Draw.
func TestTurnBasedPausedDrawRedrawWakeAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := &pauseGame{}
		l := newRunningPauseLoop(Config{TurnBased: true, PauseUnfocused: true,
			HandleClose: true, FixedStep: 10 * time.Millisecond}, g)
		a := &wakingPauseEvents{events: make(chan []platform.Event), wake: make(chan struct{})}
		l.app, l.ctx.app = a, a
		g.draw = func(c *Context) {
			if g.draws == 2 { // the first frame after the pause
				c.RequestRedraw()
			}
			if c.CloseRequested() {
				c.Quit()
			}
		}
		go func() {
			a.events <- []platform.Event{{Kind: platform.EventFocus}}
			time.Sleep(time.Second)
			l.ctx.Wake()
			time.Sleep(time.Second)
			a.events <- []platform.Event{{Kind: platform.EventClose}}
		}()
		if err := l.run(); err != nil {
			t.Fatal(err)
		}
		if len(g.deltas) != 0 {
			t.Errorf("paused game updated %d times", len(g.deltas))
		}
		// The first frame, the pause, the redraw, the wake and the close.
		if g.draws != 5 {
			t.Errorf("draws = %d, want 5", g.draws)
		}
		if !slices.Equal(a.waits, []bool{false, true, false, true, true}) {
			t.Errorf("Poll waits = %v", a.waits)
		}
		if !l.ctx.CloseRequested() {
			t.Error("handled close was not delivered to Draw")
		}
	})
}

// A hidden window draws nothing, not for a wake or a redraw request,
// until a close request that a paused game can only see in Draw, and it
// draws again from the first frame it is shown.
func TestHiddenWindowSkipsDrawsUntilShownOrClosing(t *testing.T) {
	g := &pauseGame{}
	l := newRunningPauseLoop(Config{TurnBased: true, PauseHidden: true,
		HandleClose: true, FixedStep: 10 * time.Millisecond}, g)
	g.draw = func(c *Context) {
		if c.CloseRequested() {
			c.Quit()
		}
	}
	var drawsAt []int
	script := []func() []platform.Event{
		func() []platform.Event { return []platform.Event{{Kind: platform.EventVisible}} }, // hidden
		func() []platform.Event {
			l.ctx.RequestRedraw()
			return []platform.Event{{Kind: platform.EventWake}}
		},
		func() []platform.Event {
			drawsAt = append(drawsAt, g.draws)
			return []platform.Event{{Kind: platform.EventVisible, Visible: true}}
		},
		func() []platform.Event {
			drawsAt = append(drawsAt, g.draws)
			return []platform.Event{{Kind: platform.EventVisible}} // hidden again
		},
		func() []platform.Event { return []platform.Event{{Kind: platform.EventClose}} },
	}
	l.app = pauseEvents{poll: func(wait bool) []platform.Event {
		if !wait { // the first frame's turn, then the redraw request's
			return nil
		}
		if len(script) == 0 {
			t.Fatal("the loop kept polling after the close")
		}
		next := script[0]
		script = script[1:]
		return next()
	}}
	if err := l.run(); err != nil {
		t.Fatal(err)
	}
	// The first frame, nothing while hidden, one on showing, one for the
	// close while hidden again.
	if !slices.Equal(drawsAt, []int{1, 2}) || g.draws != 3 {
		t.Errorf("draws while hidden, after showing = %v, total %d; want [1 2], 3", drawsAt, g.draws)
	}
	// Only the turn on showing updates: the first frame just draws, and a
	// hidden paused window updates for nothing.
	if len(g.deltas) != 1 {
		t.Errorf("updates = %d, want 1 (only while shown)", len(g.deltas))
	}
}

// countingEvents records how each poll waited and ends the run after a
// fixed number of polls.
type countingEvents struct {
	eventSource
	waits    []time.Duration // -1 for Poll(true), 0 for Poll(false)
	pads     []platform.GamepadState
	stop     int
	onPoll   func(n int) []platform.Event
	stopWith []platform.Event
}

func (a *countingEvents) record(d time.Duration) []platform.Event {
	a.waits = append(a.waits, d)
	if len(a.waits) >= a.stop {
		return []platform.Event{{Kind: platform.EventClose}}
	}
	if a.onPoll != nil {
		return a.onPoll(len(a.waits))
	}
	return nil
}

func (a *countingEvents) Poll(wait bool) []platform.Event {
	if wait {
		return a.record(-1)
	}
	return a.record(0)
}

func (a *countingEvents) PollTimeout(d time.Duration) []platform.Event {
	time.Sleep(d)
	return a.record(d)
}

func (a *countingEvents) Gamepads() []platform.GamepadState { return a.pads }

// An empty wake, from a timeout or an event the platform did not
// translate, runs no Update and no Draw in a waiting turn-based loop.
func TestTurnBasedEmptyWakesDoNotDraw(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := &pauseGame{}
		l := newRunningPauseLoop(Config{TurnBased: true, FixedStep: 10 * time.Millisecond}, g)
		a := &countingEvents{stop: 20, onPoll: func(n int) []platform.Event {
			time.Sleep(16 * time.Millisecond)
			if n == 10 {
				return []platform.Event{{Kind: platform.EventWake}}
			}
			return nil
		}}
		l.app, l.ctx.app = a, a
		if err := l.run(); err != nil {
			t.Fatal(err)
		}
		// Draws for the first frame and the Wake; only the Wake updates.
		if g.draws != 2 || len(g.deltas) != 1 {
			t.Errorf("draws, updates = %d, %d over 18 empty wakes and one Wake; want 2, 1", g.draws, len(g.deltas))
		}
		if a.waits[0] != 0 || slices.Contains(a.waits[1:], 0) {
			t.Errorf("waits = %v; only the first frame's poll should not wait", a.waits)
		}
	})
}

// A connected controller bounds a turn-based loop's wait, because its
// input wakes no poll; a change of its state runs a turn.
func TestTurnBasedWaitIsBoundedWhileAPadIsConnected(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := &pauseGame{}
		l := newRunningPauseLoop(Config{TurnBased: true, FixedStep: 10 * time.Millisecond}, g)
		a := &countingEvents{stop: 6}
		a.onPoll = func(n int) []platform.Event {
			switch n {
			case 2:
				a.pads = []platform.GamepadState{{Connected: true, Name: "pad"}}
			case 4:
				a.pads[0].Buttons[input.ButtonA] = true
			}
			return nil
		}
		l.app, l.ctx.app = a, a
		if err := l.run(); err != nil {
			t.Fatal(err)
		}
		want := []time.Duration{0, -1, padPollInterval, padPollInterval, padPollInterval, padPollInterval}
		if !slices.Equal(a.waits, want) {
			t.Errorf("waits = %v, want %v", a.waits, want)
		}
		// The first frame, then connecting the pad is one turn and
		// pressing its button another.
		if g.draws != 3 {
			t.Errorf("draws = %d, want 3", g.draws)
		}
	})
}

// A hidden real-time window keeps updating at its step without drawing,
// waiting between updates rather than spinning; paused as well, it waits
// for an event.
func TestHiddenRealtimeWindowWaitsForItsNextUpdate(t *testing.T) {
	const step = 10 * time.Millisecond
	for _, tc := range []struct {
		name  string
		cfg   Config
		waits []time.Duration
		ups   int
	}{
		{"running", Config{FixedStep: step}, []time.Duration{0, step, step, step, step}, 4},
		{"paused", Config{FixedStep: step, PauseHidden: true}, []time.Duration{0, -1, -1, -1, -1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				g := &pauseGame{}
				l := newRunningPauseLoop(tc.cfg, g)
				a := &countingEvents{stop: 6, onPoll: func(n int) []platform.Event {
					if n == 1 {
						return []platform.Event{{Kind: platform.EventVisible}}
					}
					return nil
				}}
				l.app, l.ctx.app = a, a
				if err := l.run(); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(a.waits[:5], tc.waits) {
					t.Errorf("waits = %v, want %v", a.waits[:5], tc.waits)
				}
				if g.draws != 0 || len(g.deltas) != tc.ups {
					t.Errorf("draws, updates = %d, %d; want 0, %d", g.draws, len(g.deltas), tc.ups)
				}
			})
		})
	}
}
