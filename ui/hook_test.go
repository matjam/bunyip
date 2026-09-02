package ui

import (
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/hook"
)

// The tests drive an interface the way the engine loop does: through
// internal/hook, which is where the frame boundaries and the event feed
// live. feeder scripts input, and drivers holds the graphics side of
// each context built by newContext.

// feeder is an input State plus the driver the engine pushes events
// through. The typed methods take input.Key and input.MouseButton, since
// that is what a test writing input has.
type feeder struct {
	hook.Input
	state *input.State
}

func newFeeder() *feeder {
	d := hook.NewInput()
	return &feeder{Input: d, state: d.Game().(*input.State)}
}

func (f *feeder) FeedKey(k input.Key, down, repeat bool, mods input.Mods) {
	f.Input.FeedKey(uint8(k), down, repeat, uint8(mods))
}

func (f *feeder) FeedMouseButton(b input.MouseButton, down bool, x, y float32) {
	f.Input.FeedMouseButton(uint8(b), down, x, y)
}

func (f *feeder) FeedGamepad(i int, connected bool, name string, buttons [input.GamepadButtonCount]bool, axes [input.GamepadAxisCount]float32) {
	f.Input.FeedGamepad(i, connected, name, buttons, axes)
}

// drivers maps each test context to the graphics driver that opens and
// closes its frames.
var drivers = map[*Context]hook.Graphics{}

// beginFrame opens a frame cleared to black; ok is false when the frame
// should be skipped.
func beginFrame(c *Context) (bool, error) { return drivers[c].Begin([4]float32{0, 0, 0, 1}) }

// endFrame submits the frame.
func endFrame(c *Context) error { _, err := drivers[c].End(false); return err }
