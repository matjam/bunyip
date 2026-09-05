package input_test

import (
	"fmt"
	"testing"

	"github.com/matjam/bunyip/input"
	"github.com/matjam/bunyip/internal/hook"
)

func TestGamepadFrameEdges(t *testing.T) {
	for button := input.GamepadButton(0); button < input.GamepadButtonCount; button++ {
		for _, updates := range []int{0, 1, 3} {
			for _, transition := range []string{"press", "release", "press and release"} {
				t.Run(fmt.Sprintf("button%d/%s/updates%d", button, transition, updates), func(t *testing.T) {
					d := hook.NewInput()
					state := d.Game().(*input.State)
					var buttons [input.GamepadButtonCount]bool
					var axes [input.GamepadAxisCount]float32
					axes[input.AxisLeftX] = 0.75
					feed := func(down bool) {
						buttons[button] = down
						d.FeedGamepad(0, true, "pad", buttons, axes)
					}
					feed(transition == "release")
					d.EndUpdate()
					d.EndFrame()
					wantPressed := transition != "release"
					wantReleased := transition != "press"
					wantDown := transition == "press"
					if wantPressed {
						feed(true)
					}
					if wantReleased {
						feed(false)
					}
					check := func(phase string, pressed, released bool) *input.Gamepad {
						t.Helper()
						g := state.Gamepad(0)
						if g.Pressed(button) != pressed || g.Released(button) != released || g.Down(button) != wantDown {
							t.Errorf("%s: pressed=%v released=%v down=%v; want %v %v %v", phase, g.Pressed(button), g.Released(button), g.Down(button), pressed, released, wantDown)
						}
						if !g.Connected || g.Name != "pad" || g.Axis(input.AxisLeftX) != 0.75 {
							t.Errorf("%s: controller state changed: %+v", phase, g)
						}
						return g
					}
					check("first update view", wantPressed, wantReleased)
					for update := range updates {
						d.EndUpdate()
						check(fmt.Sprintf("after update %d", update), false, false)
					}
					d.SetDrawing(true)
					first := check("first draw", wantPressed, wantReleased)
					check("repeated query in first draw", wantPressed, wantReleased)
					d.EndFrame()
					d.SetDrawing(false)
					for frame := range 3 {
						// Platform polling supplies held state each frame, even
						// when there are no simulation updates.
						feed(wantDown)
						d.SetDrawing(true)
						check(fmt.Sprintf("later draw %d", frame), false, false)
						d.EndFrame()
						d.SetDrawing(false)
						check("pending update", updates == 0 && wantPressed, updates == 0 && wantReleased)
					}
					if first.Pressed(button) != wantPressed || first.Released(button) != wantReleased {
						t.Error("later frames changed the saved first-draw snapshot")
					}
					d.EndUpdate()
					check("delayed update consumed", false, false)
					d.SetDrawing(true)
					check("draw after delayed update", false, false)
				})
			}
		}
	}
}
