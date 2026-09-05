// Package hook is the seam between the engine loop and the public
// packages. The loop has to build a Graphics, open and close frames,
// push platform events into an input State and pull audio out of a
// Mixer, but none of that belongs on the surface a game reads. So gfx,
// input and audio keep those methods unexported, wrap the public value
// in an unexported driver type that implements the interface here, and
// register a constructor from an init. Exported methods on an
// unexported type are unreachable from a game, so no plumbing shows up
// in the documentation.
//
// This package must not import gfx, input or audio, since they import
// it. Parameters are therefore spelled in types it can express on its
// own: a clear colour is four linear floats, a key, modifier set or
// mouse button is the integer the public type is defined as, and the
// driver converts. Game returns the public value the loop hands to the
// game (*gfx.Graphics, *input.State, *audio.Mixer); the loop asserts it
// once at startup. Nothing here is part of the engine's public API.
package hook

import (
	"image"

	"github.com/matjam/bunyip/internal/render"
)

// Graphics is the drawing context as the engine loop drives it.
type Graphics interface {
	// Begin starts a frame cleared to a linear, non-premultiplied RGBA
	// colour. ok is false when the swapchain was rebuilt and the frame
	// should be skipped.
	Begin(clear [4]float32) (ok bool, err error)
	// End flushes queued work, submits and presents. With capture it
	// returns the frame.
	End(capture bool) (*image.RGBA, error)
	// Resize tells the renderer the framebuffer changed size, in pixels.
	Resize(width, height int)
	// SetTime sets the game clock, which shaders read as time().
	SetTime(seconds float64)
	// Destroy releases everything the context created.
	Destroy()
	// Game returns the *gfx.Graphics the game draws with.
	Game() any
}

// Gamepad array sizes, duplicated from input.GamepadButtonCount and
// input.GamepadAxisCount so this package does not import input. The
// arrays are the same types the input package and the platform layer
// declare, so feeding a controller copies rather than allocates. The
// input package asserts the sizes still match at compile time.
const (
	GamepadButtonCount = 15
	GamepadAxisCount   = 6
)

// Input is the input state as the engine loop and the platform layer
// fill it. Key, modifier and mouse button values are the integers
// input.Key, input.Mods and input.MouseButton are defined as.
type Input interface {
	// FeedKey records a key going down or up.
	FeedKey(key uint8, down, repeat bool, mods uint8)
	// FeedChar records a typed character.
	FeedChar(r rune)
	// FeedComposition records the input method's uncommitted text.
	FeedComposition(text string)
	// FeedMouseMove records the pointer position in view units.
	FeedMouseMove(x, y float32)
	// FeedMouseDelta accumulates relative pointer movement.
	FeedMouseDelta(dx, dy float32)
	// FeedMouseButton records a button going down or up at a position.
	FeedMouseButton(button uint8, down bool, x, y float32)
	// FeedScroll accumulates wheel movement in lines.
	FeedScroll(dx, dy float32)
	// FeedFocusLost releases everything, since key-up events stop
	// arriving.
	FeedFocusLost()
	// FeedGamepad replaces controller i's state.
	FeedGamepad(i int, connected bool, name string, buttons [GamepadButtonCount]bool, axes [GamepadAxisCount]float32)
	// EndUpdate clears the per-update transients, first latching them for
	// Draw.
	EndUpdate()
	// EndFrame clears the transients latched for Draw.
	EndFrame()
	// SetDrawing switches the accessors between the update's edges and
	// the frame's.
	SetDrawing(drawing bool)
	// Game returns the *input.State the game reads.
	Game() any
}

// Audio is the mixer as the output device pulls from it.
type Audio interface {
	// Mix writes len(out)/2 stereo frames. The output device calls it
	// from its own thread.
	Mix(out []float32)
	// SetDevice tells the mixer whether the run has an audio device. A
	// headless or silent run has none, and the mixer then refuses to
	// open a capture stream rather than reaching the hardware behind the
	// game's back.
	SetDevice(open bool)
	// Game returns the *audio.Mixer the game plays through.
	Game() any
}

// The constructors, each registered from the owning package's init.
var (
	NewGraphics func(r *render.Renderer) (Graphics, error)
	NewInput    func() Input
	NewMixer    func(rate int) Audio
)
