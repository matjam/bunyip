package bunyip

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/platform"
)

func testWindowFamily() (*windowFamily, *Window, *pauseGame, *pauseGame) {
	cfg := Config{Title: "test", TurnBased: true, FixedStep: time.Second / 60, PauseHidden: true}
	rg, cg := &pauseGame{}, &pauseGame{}
	r, c := newRunningPauseLoop(cfg, rg), newRunningPauseLoop(cfg, cg)
	f := newWindowFamily(r)
	w := &Window{ctx: c.ctx, loop: c, family: f, parent: f.root}
	c.family, c.handle, c.ctx.owner, c.ctx.Audio = f, w, w, r.ctx.Audio
	f.attach(w)
	return f, w, rg, cg
}

func TestWindowFamilyRoutesHeadlessEventsAndPollsOnce(t *testing.T) {
	f, child, rootGame, childGame := testWindowFamily()
	if child.loop.eventWindow == f.root.loop.eventWindow {
		t.Fatal("headless outputs share an event identity")
	}
	polls := 0
	f.app = pauseEvents{poll: func(wait bool) []platform.Event {
		if !wait {
			t.Error("idle turn-based windows did not wait")
		}
		polls++
		switch polls {
		case 1:
			return []platform.Event{{Window: child.loop.eventWindow, Kind: platform.EventKeyDown, Key: platform.Key(input.KeyA)}}
		case 2:
			if f.root.ctx.Input.KeyDown(input.KeyA) || !child.ctx.Input.KeyDown(input.KeyA) {
				t.Error("key was delivered to another window")
			}
			return []platform.Event{{Window: child.loop.eventWindow, Kind: platform.EventClose}}
		case 3:
			if !child.Closed() {
				t.Error("child was not torn down before next poll")
			}
			return []platform.Event{{Window: f.root.loop.eventWindow, Kind: platform.EventKeyDown, Key: platform.Key(input.KeyB)}}
		default:
			return []platform.Event{{Window: f.root.loop.eventWindow, Kind: platform.EventClose}}
		}
	}}
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	if polls != 4 || rootGame.draws != 1 || childGame.draws != 1 || len(rootGame.deltas) != 1 || len(childGame.deltas) != 1 {
		t.Fatalf("polls=%d, root=%+v, child=%+v", polls, rootGame, childGame)
	}
}

func TestWindowFamilyDisconnectAndPause(t *testing.T) {
	f, child, _, _ := testWindowFamily()
	pads := make([]platform.GamepadState, input.MaxGamepads)
	pads[3].Connected, pads[3].Name, pads[3].Buttons[input.ButtonA] = true, "controller", true
	pads[3].Info = input.GamepadInfo{Name: "controller", Backend: "test", VendorID: 0x1234}
	pads[3].Info.Buttons[input.ButtonA] = true
	f.feedGamepads(pads)
	for _, w := range f.windows {
		if !w.ctx.Input.Gamepad(3).Connected || !w.ctx.Input.Gamepad(3).Down(input.ButtonA) || w.ctx.Input.Gamepad(3).Info != pads[3].Info {
			t.Fatal("missing controller")
		}
		w.loop.input.EndUpdate()
	}
	f.feedGamepads(nil)
	for _, w := range f.windows {
		g := w.ctx.Input.Gamepad(3)
		if g.Connected || g.Down(input.ButtonA) || !g.JustDisconnected() || g.Info != (input.GamepadInfo{}) {
			t.Errorf("stale disconnected controller: %+v", g)
		}
	}
	f.dispatch([]platform.Event{{Window: f.root.loop.eventWindow, Kind: platform.EventVisible}})
	if f.root.ctx.Audio.Paused() {
		t.Fatal("one hidden window paused another's audio")
	}
	f.dispatch([]platform.Event{{Window: child.loop.eventWindow, Kind: platform.EventVisible}})
	if !f.root.ctx.Audio.Paused() {
		t.Fatal("all hidden windows did not pause audio")
	}
	f.dispatch([]platform.Event{{Window: child.loop.eventWindow, Kind: platform.EventVisible, Visible: true}})
	if f.root.ctx.Audio.Paused() {
		t.Fatal("visible child did not resume audio")
	}
}

func TestWindowFamilyCleanupRejectsCreationAndDrainsPanic(t *testing.T) {
	f, child, _, _ := testWindowFamily()
	var order []int
	child.release = func() {
		if _, err := child.ctx.NewWindow(Config{}, GameFuncs{}); err == nil {
			t.Error("creation during cleanup succeeded")
		}
		order = append(order, 1)
		panic("cleanup")
	}
	l := newRunningPauseLoop(f.root.loop.cfg, &pauseGame{})
	sibling := &Window{ctx: l.ctx, loop: l, family: f, parent: f.root, release: func() { order = append(order, 2) }}
	f.attach(sibling)
	func() {
		defer func() {
			if recover() != "cleanup" {
				t.Error("missing cleanup panic")
			}
		}()
		f.closeChildren(f.root)
	}()
	if !child.Closed() || !sibling.Closed() || !slices.Equal(order, []int{2, 1}) {
		t.Fatalf("cleanup order=%v", order)
	}
	f.root.ctx.Quit()
	if _, err := f.root.ctx.NewWindow(Config{}, GameFuncs{}); err == nil {
		t.Fatal("creation after Quit succeeded")
	}
}

func TestWindowFamilyCleanupQuitDoesNotPoll(t *testing.T) {
	f, child, _, _ := testWindowFamily()
	child.release = f.root.ctx.Quit
	child.Close()
	f.app = pauseEvents{poll: func(bool) []platform.Event { t.Fatal("polled after cleanup quit the application"); return nil }}
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	if !child.Closed() {
		t.Fatal("child teardown was incomplete")
	}
}

func TestWindowFamilyClosePreventsDescendantCallbacks(t *testing.T) {
	f, child, rootGame, childGame := testWindowFamily()
	grandGame := &pauseGame{}
	l := newRunningPauseLoop(child.loop.cfg, grandGame)
	w := &Window{ctx: l.ctx, loop: l, family: f, parent: child}
	l.family, l.handle, l.ctx.owner = f, w, w
	f.attach(w)
	childGame.draw = func(*Context) { child.Close() }
	polls := 0
	f.app = pauseEvents{poll: func(bool) []platform.Event {
		polls++
		if polls == 1 {
			return []platform.Event{{Kind: platform.EventWake}}
		}
		return []platform.Event{{Window: f.root.loop.eventWindow, Kind: platform.EventClose}}
	}}
	if err := f.run(); err != nil {
		t.Fatal(err)
	}
	if rootGame.draws != 1 || childGame.draws != 1 || grandGame.draws != 0 || len(grandGame.deltas) != 0 || !w.Closed() {
		t.Fatalf("root/child/grandchild draws=%d/%d/%d updates=%d closed=%v", rootGame.draws, childGame.draws, grandGame.draws, len(grandGame.deltas), w.Closed())
	}
}

func TestWindowClockExcludesInitialization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := &pauseGame{}
		l := newRunningPauseLoop(Config{TurnBased: true, FixedStep: time.Second / 60}, g)
		newWindowFamily(l)
		time.Sleep(time.Hour) // initialization work before entering the loop
		polls := 0
		l.family.app = pauseEvents{poll: func(bool) []platform.Event {
			polls++
			time.Sleep(time.Millisecond)
			if polls == 1 {
				return nil
			}
			return []platform.Event{{Kind: platform.EventClose}}
		}}
		if err := l.run(); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(g.deltas, []float64{0.001}) || l.ctx.Time != 0.001 {
			t.Fatalf("initialization advanced clock: deltas=%v time=%v", g.deltas, l.ctx.Time)
		}
	})
}
