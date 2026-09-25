package engine

import (
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/matjam/bunyip/internal/platform"
)

// orderGraphics records the loop's calls into the graphics driver, and
// waits for the frame slot apart from Begin.
type orderGraphics struct {
	pauseGraphics
	calls *[]string
}

func (g orderGraphics) WaitFrame() error { *g.calls = append(*g.calls, "wait"); return nil }
func (g orderGraphics) Begin([4]float32) (bool, error) {
	*g.calls = append(*g.calls, "begin")
	return true, nil
}
func (g orderGraphics) End(bool) (*image.RGBA, error) {
	*g.calls = append(*g.calls, "end")
	return nil, nil
}

// TestFrameWaitsBeforePoll checks the order of a frame: the wait for the
// GPU's frame slot comes before the poll, so the input the update and the
// draw read is as fresh as the frame can have it, and the frame's own
// Begin does not wait again after the input was read.
func TestFrameWaitsBeforePoll(t *testing.T) {
	var calls []string
	cfg := Config{Title: "order", FixedStep: 10 * time.Millisecond, FixedClock: true}
	g := &pauseGame{}
	l := newRunningPauseLoop(cfg, g)
	l.gfx = orderGraphics{calls: &calls}
	g.draw = func(*Context) { calls = append(calls, "draw") }
	polls := 0
	l.app = pauseEvents{poll: func(bool) []platform.Event {
		calls = append(calls, "poll")
		polls++
		if polls == 4 {
			return []platform.Event{{Kind: platform.EventClose}}
		}
		return nil
	}}
	update := &orderUpdate{pauseGame: g, calls: &calls}
	l.game = update
	if err := l.run(); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(calls, " ")
	frame := "wait poll update begin draw end"
	want := strings.Repeat(frame+" ", 3) + "wait poll"
	if got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
	if i := slices.Index(calls, "poll"); i < 1 || calls[i-1] != "wait" {
		t.Fatalf("the poll did not follow the wait: %v", calls)
	}
}

// orderUpdate records each update into the same call log.
type orderUpdate struct {
	*pauseGame
	calls *[]string
}

func (g *orderUpdate) Update(c *Context) error {
	*g.calls = append(*g.calls, "update")
	return g.pauseGame.Update(c)
}
