package console

import (
	"log/slog"

	"github.com/matjam/bunyip/audio"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/ui"
)

// Frame is the engine state one console frame draws from. The engine
// fills it in from its own context, so a game never builds one; a game
// driving a console outside the engine's loop fills in what it has.
// Every field may be zero, and the console leaves out what it is not
// given: without Screenshot the screenshot command reports an error,
// and without Audio the audio panel says so. Draw does nothing unless
// both Gfx and Input are non-nil.
type Frame struct {
	Gfx   *gfx.Graphics // drawing context; required by Draw
	Input *input.State  // frame input; required by Draw
	Audio *audio.Mixer  // optional mixer for the Audio panel
	Log   *slog.Logger  // host logger, available to console integrations

	// Clipboard is what the panels' text fields cut, copy and paste
	// through; the engine's Context satisfies it.
	Clipboard ui.Clipboard

	// Width and Height are the view's size in view units.
	Width, Height float32
	// Delta is the seconds the last update covered, Time the seconds the
	// game has run, and FrameCount the frames drawn.
	Delta, Time float64
	FrameCount  uint64 // number of frames drawn

	// Stats are the previous frame's timings and counts.
	Stats Stats

	// Screenshot writes the next drawn frame to a PNG at the path.
	Screenshot func(path string)
	// Quit ends the loop.
	Quit func()
	// SetTimeScale and TimeScale drive the timescale command.
	SetTimeScale func(scale float64)
	TimeScale    func() float64 // read the current game-time multiplier
}

// Stats are the frame timings and counts the engine panel shows. They
// mirror engine.Stats, which the engine copies in, so the console does
// not depend on the root package.
type Stats struct {
	FPS       float64 // frames per second over the last second
	FrameMS   float64 // wall time from one frame's start to the next
	UpdateMS  float64 // time spent in Update calls this frame
	DrawMS    float64 // time spent in Draw
	PresentMS float64 // time submitting and waiting on the GPU
	Updates   int     // Update calls this frame
	Scopes    []Scope // profile scopes recorded this frame

	// GPUFrameMS is how long the GPU spent on a recent frame and GPU is
	// that time by pass. Both come from timestamp queries read back a
	// frame or two later, so they lag the frame on screen, and both are
	// zero and empty on a device without timestamp queries.
	GPUFrameMS float64
	GPU        []Scope // GPU duration by pass in milliseconds

	// DrawBudget is the draw call count a frame should stay under, from
	// Config.DrawBudget; zero means no budget was set.
	DrawBudget int
}

// Scope is one timed section of game code, as Context.Profile records it.
type Scope struct {
	Name string  // profile section or GPU pass name
	MS   float64 // duration in milliseconds
}
