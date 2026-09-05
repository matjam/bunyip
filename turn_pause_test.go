package bunyip

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
	for _, tc := range []struct {
		name    string
		cfg     Config
		events  []platform.Event
		updates int
	}{
		{"hidden", Config{PauseHidden: true}, []platform.Event{{Kind: platform.EventVisible}}, 0},
		{"unfocused", Config{PauseUnfocused: true}, []platform.Event{{Kind: platform.EventFocus}}, 0},
		{"both", Config{PauseHidden: true, PauseUnfocused: true}, []platform.Event{{Kind: platform.EventVisible}, {Kind: platform.EventFocus}}, 0},
		{"disabled", Config{}, []platform.Event{{Kind: platform.EventVisible}, {Kind: platform.EventFocus}}, 1},
		{"hidden with focus option only", Config{PauseUnfocused: true}, []platform.Event{{Kind: platform.EventVisible}}, 1},
		{"unfocused with hidden option only", Config{PauseHidden: true}, []platform.Event{{Kind: platform.EventFocus}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := tc.cfg
				cfg.TurnBased, cfg.FixedStep = true, 10*time.Millisecond
				g := &pauseGame{}
				l := newRunningPauseLoop(cfg, g)
				polls := 0
				l.app = pauseEvents{poll: func(wait bool) []platform.Event {
					if !wait {
						t.Error("turn-based loop did not wait for events")
					}
					polls++
					time.Sleep(time.Second)
					if polls == 1 {
						return tc.events
					}
					return []platform.Event{{Kind: platform.EventClose}}
				}}
				if err := l.run(); err != nil {
					t.Fatal(err)
				}
				if len(g.deltas) != tc.updates {
					t.Errorf("updates = %d, want %d", len(g.deltas), tc.updates)
				}
				if g.draws != 1 || polls != 2 {
					t.Errorf("draws/polls = %d/%d, want 1/2", g.draws, polls)
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
				batches := []batch{
					{step, nil},
					{step / 2, []platform.Event{{Kind: platform.EventVisible}, {Kind: platform.EventFocus}}},
					{time.Hour, nil}, // waking while paused must not update
					{time.Hour, []platform.Event{{Kind: platform.EventFocus, Focused: true}}},
					{time.Hour, []platform.Event{{Kind: platform.EventVisible, Visible: true}}},
					{step, nil},
					{0, []platform.Event{{Kind: platform.EventClose}}},
				}
				polls := 0
				l.app = pauseEvents{poll: func(wait bool) []platform.Event {
					if wait != mode.turn {
						t.Errorf("Poll wait = %v, want %v", wait, mode.turn)
					}
					b := batches[polls]
					polls++
					time.Sleep(b.delay)
					return b.events
				}}
				if err := l.run(); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(g.deltas, mode.want) {
					t.Errorf("update deltas = %v, want %v", g.deltas, mode.want)
				}
				if g.draws != 6 {
					t.Errorf("draws = %d, want 6", g.draws)
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
		return nil
	}
}

func (a *wakingPauseEvents) Gamepads() []platform.GamepadState { return nil }
func (a *wakingPauseEvents) Wake()                             { a.wake <- struct{}{} }

func TestTurnBasedPausedDrawRedrawWakeAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := &pauseGame{}
		l := newRunningPauseLoop(Config{TurnBased: true, PauseHidden: true,
			HandleClose: true, FixedStep: 10 * time.Millisecond}, g)
		a := &wakingPauseEvents{events: make(chan []platform.Event), wake: make(chan struct{})}
		l.app, l.ctx.app = a, a
		g.draw = func(c *Context) {
			if g.draws == 1 {
				c.RequestRedraw()
			}
			if c.CloseRequested() {
				c.Quit()
			}
		}
		go func() {
			a.events <- []platform.Event{{Kind: platform.EventVisible}}
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
		if g.draws != 4 {
			t.Errorf("draws = %d, want 4", g.draws)
		}
		if !slices.Equal(a.waits, []bool{true, false, true, true}) {
			t.Errorf("Poll waits = %v", a.waits)
		}
		if !l.ctx.CloseRequested() {
			t.Error("handled close was not delivered to Draw")
		}
	})
}
