package platform

import (
	"github.com/ebitengine/purego/objc"

	"github.com/matjam/bunyip/input"
)

// Gamepads reads every connected controller through GameController.framework.
// Values are read directly from the framework's input objects, so no
// callbacks are registered and the read is safe at any point in the loop.
func (a *App) Gamepads() []GamepadState {
	c := a.c
	if c.GCController == 0 {
		return nil
	}
	list := objc.ID(c.GCController).Send(c.sel.controllers)
	if list == 0 {
		return nil
	}
	n := min(int(objc.Send[uint](list, c.sel.count)), input.MaxGamepads)
	out := make([]GamepadState, 0, n)
	for i := range n {
		ctl := list.Send(c.sel.objectAtIndex, uint(i))
		pad := ctl.Send(c.sel.extendedGamepad)
		if pad == 0 {
			continue // micro or directional gamepads are not mapped
		}
		st := GamepadState{Connected: true}
		if name := ctl.Send(c.sel.vendorName); name != 0 {
			st.Name = objc.Send[string](name, c.sel.UTF8String)
		}
		button := func(b input.GamepadButton, sel objc.SEL) {
			if el := pad.Send(sel); el != 0 {
				st.Buttons[b] = objc.Send[bool](el, c.sel.isPressed)
			}
		}
		axisOf := func(el objc.ID) float32 {
			if el == 0 {
				return 0
			}
			return objc.Send[float32](el, c.sel.value)
		}
		button(input.ButtonA, c.sel.buttonA)
		button(input.ButtonB, c.sel.buttonB)
		button(input.ButtonX, c.sel.buttonX)
		button(input.ButtonY, c.sel.buttonY)
		button(input.ButtonLeftShoulder, c.sel.leftShoulder)
		button(input.ButtonRightShoulder, c.sel.rightShoulder)
		button(input.ButtonLeftStick, c.sel.leftThumbstickButton)
		button(input.ButtonRightStick, c.sel.rightThumbstickButton)
		button(input.ButtonMenu, c.sel.buttonMenu)
		button(input.ButtonOptions, c.sel.buttonOptions)
		button(input.ButtonHome, c.sel.buttonHome)
		if dpad := pad.Send(c.sel.dpad); dpad != 0 {
			for _, d := range []struct {
				b   input.GamepadButton
				sel objc.SEL
			}{{input.ButtonDpadUp, c.sel.up}, {input.ButtonDpadDown, c.sel.down}, {input.ButtonDpadLeft, c.sel.left}, {input.ButtonDpadRight, c.sel.right}} {
				if el := dpad.Send(d.sel); el != 0 {
					st.Buttons[d.b] = objc.Send[bool](el, c.sel.isPressed)
				}
			}
		}
		if stick := pad.Send(c.sel.leftThumbstick); stick != 0 {
			st.Axes[input.AxisLeftX] = axisOf(stick.Send(c.sel.xAxis))
			st.Axes[input.AxisLeftY] = axisOf(stick.Send(c.sel.yAxis))
		}
		if stick := pad.Send(c.sel.rightThumbstick); stick != 0 {
			st.Axes[input.AxisRightX] = axisOf(stick.Send(c.sel.xAxis))
			st.Axes[input.AxisRightY] = axisOf(stick.Send(c.sel.yAxis))
		}
		st.Axes[input.AxisLeftTrigger] = axisOf(pad.Send(c.sel.leftTrigger))
		st.Axes[input.AxisRightTrigger] = axisOf(pad.Send(c.sel.rightTrigger))
		out = append(out, st)
	}
	return out
}
