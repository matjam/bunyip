package bunyip

import (
	"math"
	"testing"
	"time"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/internal/vk"
)

// clockGame records the clock and the delta of every frame, and how many
// updates ran between draws.
type clockGame struct {
	frames  int
	updates int
	// perFrame is the update count at each draw, times is the clock at
	// each draw, and alphas the interpolation fraction.
	perFrame []int
	times    []float64
	deltas   []float64
	alphas   []float32
	stop     int
}

func (g *clockGame) Update(ctx *Context) error {
	g.updates++
	return nil
}

func (g *clockGame) Draw(ctx *Context) error {
	g.frames++
	g.perFrame = append(g.perFrame, g.updates)
	g.times = append(g.times, ctx.Time)
	g.deltas = append(g.deltas, ctx.Delta)
	g.alphas = append(g.alphas, ctx.Alpha)
	ctx.Gfx.FillRect(0, 0, ctx.Width, ctx.Height, gfx.RGB(0, 128, 200))
	if g.frames >= g.stop {
		ctx.Quit()
	}
	return nil
}

// TestFixedClockCountsFrames checks the property the golden screenshots
// rest on: with FixedClock the clock is the frame count times the step,
// one update runs per frame, and nothing depends on how long the machine
// took to draw.
func TestFixedClockCountsFrames(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	const frames = 12
	step := time.Second / 50
	g := &clockGame{stop: frames}
	cfg := Config{Width: 64, Height: 64, Headless: true, NoAudio: true, FixedClock: true, FixedStep: step}
	if err := Run(cfg, g); err != nil {
		t.Fatal(err)
	}
	if g.frames != frames {
		t.Fatalf("drew %d frames, want %d", g.frames, frames)
	}
	if g.updates != frames {
		t.Errorf("ran %d updates over %d frames; FixedClock runs exactly one per frame", g.updates, frames)
	}
	for i, at := range g.perFrame {
		if at != i+1 {
			t.Fatalf("frame %d drew after %d updates, want %d", i, at, i+1)
		}
	}
	for i, when := range g.times {
		want := float64(i) * step.Seconds()
		if math.Abs(when-want) > 1e-9 {
			t.Errorf("frame %d at t = %v, want exactly %v", i, when, want)
		}
	}
	for i, d := range g.deltas {
		if math.Abs(d-step.Seconds()) > 1e-9 {
			t.Errorf("frame %d delta = %v, want the step %v", i, d, step.Seconds())
		}
	}
	for i, a := range g.alphas {
		if a != 0 {
			t.Errorf("frame %d alpha = %v; a fixed clock leaves nothing to interpolate", i, a)
		}
	}
}

// TestWallClockVariesWithTheMachine records why FixedClock exists: the
// default clock reads the wall clock, so the number of updates before a
// given frame depends on how long the frames took and a screenshot taken
// at a frame number is not reproducible.
func TestWallClockVariesWithTheMachine(t *testing.T) {
	if err := vk.Load(); err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	g := &clockGame{stop: 8}
	cfg := Config{Width: 64, Height: 64, Headless: true, NoAudio: true}
	if err := Run(cfg, g); err != nil {
		t.Fatal(err)
	}
	if len(g.times) < 2 {
		t.Fatal("too few frames")
	}
	// The first frame runs no update at all, because no time has passed
	// for the accumulator to spend. That alone is the difference from a
	// fixed clock.
	if g.perFrame[0] != 0 {
		t.Errorf("the first wall-clock frame ran %d updates, want none", g.perFrame[0])
	}
	if g.times[0] == g.times[1] {
		t.Error("the wall clock did not advance between frames")
	}
}
