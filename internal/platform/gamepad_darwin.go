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
	if name := sendID(ctl, a.c.sel.vendorName); name != 0 {
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
//
// The loop calls it after Poll has closed its autorelease pool, so it
// opens one of its own: +[GCController controllers] and the element
// accessors return autoreleased objects, which would otherwise stay for
// the life of the process.
func (a *App) Gamepads() []GamepadState {
	c := a.c
	if c.GCController == 0 {
		return nil
	}
	pool := poolPush()
	defer poolPop(pool)
	list := sendID(objc.ID(c.GCController), c.sel.controllers)
	if list == 0 {
		return nil
	}
	n := min(int(sendUint(list, c.sel.count)), input.MaxGamepads)
	out := padStates[:0]
	for i := range n {
		ctl := sendID1(list, c.sel.objectAtIndex, uintptr(i))
		if st, ok := a.readPad(ctl); ok {
			out = append(out, st)
		}
	}
	return out
}

// readPad reads one controller's extended gamepad profile. It reports
// false for a controller without one: micro and directional gamepads are
// not mapped. Every send but the six axis values is integer-only, so a
// read allocates only for those.
func (a *App) readPad(ctl objc.ID) (GamepadState, bool) {
	c := a.c
	pad := sendID(ctl, c.sel.extendedGamepad)
	if pad == 0 {
		return GamepadState{}, false
	}
	st := GamepadState{Connected: true, Name: a.vendorName(ctl)}
	st.Info = input.GamepadInfo{Name: st.Name, Backend: "gamecontroller"}
	// Fixed arrays rather than slice literals, so the tables cost nothing
	// on a path that runs every frame.
	buttons := [...]struct {
		b   input.GamepadButton
		sel objc.SEL
	}{
		{input.ButtonA, c.sel.buttonA}, {input.ButtonB, c.sel.buttonB},
		{input.ButtonX, c.sel.buttonX}, {input.ButtonY, c.sel.buttonY},
		{input.ButtonLeftShoulder, c.sel.leftShoulder}, {input.ButtonRightShoulder, c.sel.rightShoulder},
		{input.ButtonLeftStick, c.sel.leftThumbstickButton}, {input.ButtonRightStick, c.sel.rightThumbstickButton},
		{input.ButtonMenu, c.sel.buttonMenu}, {input.ButtonOptions, c.sel.buttonOptions},
		{input.ButtonHome, c.sel.buttonHome},
	}
	for _, e := range buttons {
		a.readButton(&st, sendID(pad, e.sel), e.b)
	}
	if dpad := sendID(pad, c.sel.dpad); dpad != 0 {
		dirs := [...]struct {
			b   input.GamepadButton
			sel objc.SEL
		}{{input.ButtonDpadUp, c.sel.up}, {input.ButtonDpadDown, c.sel.down}, {input.ButtonDpadLeft, c.sel.left}, {input.ButtonDpadRight, c.sel.right}}
		for _, d := range dirs {
			a.readButton(&st, sendID(dpad, d.sel), d.b)
		}
	}
	if stick := sendID(pad, c.sel.leftThumbstick); stick != 0 {
		a.readAxis(&st, sendID(stick, c.sel.xAxis), input.AxisLeftX)
		a.readAxis(&st, sendID(stick, c.sel.yAxis), input.AxisLeftY)
	}
	if stick := sendID(pad, c.sel.rightThumbstick); stick != 0 {
		a.readAxis(&st, sendID(stick, c.sel.xAxis), input.AxisRightX)
		a.readAxis(&st, sendID(stick, c.sel.yAxis), input.AxisRightY)
	}
	a.readAxis(&st, sendID(pad, c.sel.leftTrigger), input.AxisLeftTrigger)
	a.readAxis(&st, sendID(pad, c.sel.rightTrigger), input.AxisRightTrigger)
	return st, true
}

// readButton records a GCControllerButtonInput: present, and whether it
// is pressed. A zero element is a button the profile lacks.
func (a *App) readButton(st *GamepadState, el objc.ID, b input.GamepadButton) {
	if el == 0 {
		return
	}
	st.Info.Buttons[b] = true
	st.Buttons[b] = sendBool(el, a.c.sel.isPressed)
}

// readAxis records a GCControllerAxisInput or an analogue button's value.
// A zero element is an axis the profile lacks.
func (a *App) readAxis(st *GamepadState, el objc.ID, axis input.GamepadAxis) {
	if el == 0 {
		return
	}
	st.Info.Axes[axis] = true
	st.Axes[axis] = msgSendF32(el, a.c.sel.value)
}
