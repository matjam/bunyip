package platform

import (
	"github.com/ebitengine/purego/objc"

	"github.com/matjam/bunyip/input"
)

// padStates is the array Gamepads fills and returns, reused every poll.
var padStates [input.MaxGamepads]GamepadState

// padNames caches each controller's vendor name against the controller
// object it came from. Reading the name means an objc_msgSend and a fresh
// Go string, and a controller's name does not change while it is plugged
// in, so it is read once per controller rather than once per frame.
var padNames = map[objc.ID]string{}

// vendorName is the controller's name, from the cache when it is known.
func (a *App) vendorName(ctl objc.ID) string {
	if s, ok := padNames[ctl]; ok {
		return s
	}
	s := ""
	if name := ctl.Send(a.c.sel.vendorName); name != 0 {
		s = objc.Send[string](name, a.c.sel.UTF8String)
	}
	if len(padNames) > 32 {
		// Controllers come and go over a long session; the cache is small
		// and bounded, so it starts over rather than growing without end.
		clear(padNames)
	}
	padNames[ctl] = s
	return s
}

// Gamepads reads every connected controller through GameController.framework.
// Values are read directly from the framework's input objects, so no
// callbacks are registered and the read is safe at any point in the loop.
// The slice it returns is reused by the next call, so read it before
// polling again.
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
	out := padStates[:0]
	for i := range n {
		ctl := list.Send(c.sel.objectAtIndex, uint(i))
		pad := ctl.Send(c.sel.extendedGamepad)
		if pad == 0 {
			continue // micro or directional gamepads are not mapped
		}
		st := GamepadState{Connected: true, Name: a.vendorName(ctl)}
		st.Info = input.GamepadInfo{Name: st.Name, Backend: "gamecontroller"}
		button := func(b input.GamepadButton, sel objc.SEL) {
			if el := pad.Send(sel); el != 0 {
				st.Info.Buttons[b] = true
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
			// A fixed array rather than a slice literal, so the table
			// costs nothing on a path that runs every frame.
			dirs := [4]struct {
				b   input.GamepadButton
				sel objc.SEL
			}{{input.ButtonDpadUp, c.sel.up}, {input.ButtonDpadDown, c.sel.down}, {input.ButtonDpadLeft, c.sel.left}, {input.ButtonDpadRight, c.sel.right}}
			for _, d := range dirs {
				if el := dpad.Send(d.sel); el != 0 {
					st.Info.Buttons[d.b] = true
					st.Buttons[d.b] = objc.Send[bool](el, c.sel.isPressed)
				}
			}
		}
		if stick := pad.Send(c.sel.leftThumbstick); stick != 0 {
			st.Info.Axes[input.AxisLeftX] = stick.Send(c.sel.xAxis) != 0
			st.Info.Axes[input.AxisLeftY] = stick.Send(c.sel.yAxis) != 0
			st.Axes[input.AxisLeftX] = axisOf(stick.Send(c.sel.xAxis))
			st.Axes[input.AxisLeftY] = axisOf(stick.Send(c.sel.yAxis))
		}
		if stick := pad.Send(c.sel.rightThumbstick); stick != 0 {
			st.Info.Axes[input.AxisRightX] = stick.Send(c.sel.xAxis) != 0
			st.Info.Axes[input.AxisRightY] = stick.Send(c.sel.yAxis) != 0
			st.Axes[input.AxisRightX] = axisOf(stick.Send(c.sel.xAxis))
			st.Axes[input.AxisRightY] = axisOf(stick.Send(c.sel.yAxis))
		}
		st.Axes[input.AxisLeftTrigger] = axisOf(pad.Send(c.sel.leftTrigger))
		st.Info.Axes[input.AxisLeftTrigger] = pad.Send(c.sel.leftTrigger) != 0
		st.Axes[input.AxisRightTrigger] = axisOf(pad.Send(c.sel.rightTrigger))
		st.Info.Axes[input.AxisRightTrigger] = pad.Send(c.sel.rightTrigger) != 0
		out = append(out, st)
	}
	return out
}
