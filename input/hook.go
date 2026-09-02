package input

import "github.com/matjam/bunyip/internal/hook"

// The gamepad arrays cross the hook boundary as their own types, which
// only works while the sizes there match the ones here.
var (
	_ [hook.GamepadButtonCount]bool  = [GamepadButtonCount]bool{}
	_ [hook.GamepadAxisCount]float32 = [GamepadAxisCount]float32{}
)

// driver is how the engine loop and the platform layer fill a State:
// events in, frame boundaries marked. The methods are exported to
// satisfy hook.Input, but the type is not, so none of this plumbing
// appears on the surface a game reads.
type driver struct{ s *State }

func (d driver) FeedKey(key uint8, down, repeat bool, mods uint8) {
	d.s.feedKey(Key(key), down, repeat, Mods(mods))
}

func (d driver) FeedMouseButton(button uint8, down bool, x, y float32) {
	d.s.feedMouseButton(MouseButton(button), down, x, y)
}

func (d driver) FeedGamepad(i int, connected bool, name string, buttons [hook.GamepadButtonCount]bool, axes [hook.GamepadAxisCount]float32) {
	d.s.feedGamepad(i, connected, name, buttons, axes)
}

func (d driver) FeedChar(r rune)               { d.s.feedChar(r) }
func (d driver) FeedComposition(text string)   { d.s.feedComposition(text) }
func (d driver) FeedMouseMove(x, y float32)    { d.s.feedMouseMove(x, y) }
func (d driver) FeedMouseDelta(dx, dy float32) { d.s.feedMouseDelta(dx, dy) }
func (d driver) FeedScroll(dx, dy float32)     { d.s.feedScroll(dx, dy) }
func (d driver) FeedFocusLost()                { d.s.feedFocusLost() }
func (d driver) EndUpdate()                    { d.s.endUpdate() }
func (d driver) EndFrame()                     { d.s.endFrame() }
func (d driver) SetDrawing(drawing bool)       { d.s.setDrawing(drawing) }
func (d driver) Game() any                     { return d.s }

func init() {
	hook.NewInput = func() hook.Input { return driver{&State{}} }
}
