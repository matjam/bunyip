package platform

import (
	"github.com/matjam/bunyip/input"

	"github.com/ebitengine/purego/objc"
)

// Per-key modifier bits in NSEvent.modifierFlags, from IOKit's IOLLEvent.h.
const (
	nxDeviceLCtlKeyMask   = 0x00000001
	nxDeviceLShiftKeyMask = 0x00000002
	nxDeviceRShiftKeyMask = 0x00000004
	nxDeviceLCmdKeyMask   = 0x00000008
	nxDeviceRCmdKeyMask   = 0x00000010
	nxDeviceLAltKeyMask   = 0x00000020
	nxDeviceRAltKeyMask   = 0x00000040
	nxDeviceRCtlKeyMask   = 0x00002000
)

// handleEvent translates one NSEvent. It returns whether AppKit should still
// receive the event: key events are consumed (an unhandled keyDown would beep)
// while mouse and window events are passed on so that dragging, resizing and
// focus keep working.
func (a *App) handleEvent(ev objc.ID) bool {
	c := a.c
	kind := objc.Send[uint](ev, c.sel.eventType)
	if kind == nsEventTypeAppDefined { // Wake: no window attached
		a.push(Event{Kind: EventWake})
		return false
	}
	w := a.windowForEvent(ev, kind)
	if w == nil {
		return true
	}
	switch kind {
	case nsEventTypeKeyDown, nsEventTypeKeyUp:
		return a.handleKey(w, ev, kind == nsEventTypeKeyDown)
	case nsEventTypeFlagsChanged:
		a.handleFlagsChanged(w, ev)
		return true
	case nsEventTypeMouseMoved, nsEventTypeLeftMouseDragged, nsEventTypeRightMouseDragged, nsEventTypeOtherMouseDragged:
		x, y := a.mousePos(w, ev)
		a.push(Event{Kind: EventMouseMove, Window: w, X: x, Y: y, Mods: a.mods,
			DX: objc.Send[float64](ev, c.sel.deltaX), DY: objc.Send[float64](ev, c.sel.deltaY)})
	case nsEventTypeLeftMouseDown, nsEventTypeRightMouseDown, nsEventTypeOtherMouseDown:
		a.pushMouseButton(w, ev, EventMouseDown)
	case nsEventTypeLeftMouseUp, nsEventTypeRightMouseUp, nsEventTypeOtherMouseUp:
		a.pushMouseButton(w, ev, EventMouseUp)
	case nsEventTypeScrollWheel:
		a.push(Event{Kind: EventScroll, Window: w, Mods: a.mods,
			DX:      objc.Send[float64](ev, c.sel.scrollingDeltaX),
			DY:      objc.Send[float64](ev, c.sel.scrollingDeltaY),
			Precise: objc.Send[bool](ev, c.sel.hasPreciseScrollingDeltas)})
	case nsEventTypeMouseEntered:
		a.push(Event{Kind: EventMouseEnter, Window: w})
	case nsEventTypeMouseExited:
		a.push(Event{Kind: EventMouseLeave, Window: w})
	}
	return true
}

// handleKey pushes the key event and reports whether AppKit should still
// see it. Plain key presses go on to the content view, whose
// NSTextInputClient methods turn them into text through the active input
// method (see textinput_darwin.go); shortcuts and releases stop here.
func (a *App) handleKey(w *Window, ev objc.ID, down bool) bool {
	c := a.c
	code := objc.Send[uint16](ev, c.sel.keyCode)
	a.mods = modsFromFlags(objc.Send[uint](ev, c.sel.modifierFlags))
	key := keyFromCode(code)
	if !down {
		a.push(Event{Kind: EventKeyUp, Window: w, Key: key, Mods: a.mods})
		return false
	}
	repeat := objc.Send[bool](ev, c.sel.isARepeat)
	a.push(Event{Kind: EventKeyDown, Window: w, Key: key, Mods: a.mods, Repeat: repeat})
	if a.mods&input.ModSuper != 0 {
		if key == input.KeyQ {
			a.push(Event{Kind: EventClose, Window: w}) // Cmd+Q with no menu bar
		}
		return false
	}
	return a.mods&input.ModControl == 0
}

// handleFlagsChanged reports modifier keys as key events. The key code says
// which modifier moved and the device-specific flag bit says whether it is
// now down; caps lock only reports presses because AppKit reports its state,
// not its travel.
func (a *App) handleFlagsChanged(w *Window, ev objc.ID) {
	c := a.c
	flags := objc.Send[uint](ev, c.sel.modifierFlags)
	a.mods = modsFromFlags(flags)
	code := objc.Send[uint16](ev, c.sel.keyCode)
	key := keyFromCode(code)
	var mask uint
	switch key {
	case input.KeyLeftShift:
		mask = nxDeviceLShiftKeyMask
	case input.KeyRightShift:
		mask = nxDeviceRShiftKeyMask
	case input.KeyLeftControl:
		mask = nxDeviceLCtlKeyMask
	case input.KeyRightControl:
		mask = nxDeviceRCtlKeyMask
	case input.KeyLeftAlt:
		mask = nxDeviceLAltKeyMask
	case input.KeyRightAlt:
		mask = nxDeviceRAltKeyMask
	case input.KeyLeftSuper:
		mask = nxDeviceLCmdKeyMask
	case input.KeyRightSuper:
		mask = nxDeviceRCmdKeyMask
	case input.KeyCapsLock:
		// AppKit reports the lock's state, not the key's travel, so each
		// change is a press and a release; otherwise the key would read
		// as held for the rest of the session.
		a.push(Event{Kind: EventKeyDown, Window: w, Key: key, Mods: a.mods})
		a.push(Event{Kind: EventKeyUp, Window: w, Key: key, Mods: a.mods})
		return
	default:
		return
	}
	kind := EventKeyUp
	if flags&mask != 0 {
		kind = EventKeyDown
	}
	a.push(Event{Kind: kind, Window: w, Key: key, Mods: a.mods})
}

func modsFromFlags(flags uint) Mods {
	var m Mods
	if flags&nsEventModifierFlagShift != 0 {
		m |= input.ModShift
	}
	if flags&nsEventModifierFlagControl != 0 {
		m |= input.ModControl
	}
	if flags&nsEventModifierFlagOption != 0 {
		m |= input.ModAlt
	}
	if flags&nsEventModifierFlagCommand != 0 {
		m |= input.ModSuper
	}
	if flags&nsEventModifierFlagCapsLock != 0 {
		m |= input.ModCapsLock
	}
	return m
}

// mousePos converts an event location to points from the top-left of the
// content view; AppKit's origin is bottom-left.
func (a *App) mousePos(w *Window, ev objc.ID) (float64, float64) {
	p := objc.Send[nsPoint](ev, a.c.sel.locationInWindow)
	p = objc.Send[nsPoint](w.view, objc.RegisterName("convertPoint:fromView:"), p, objc.ID(0))
	return p.X, float64(w.height) - p.Y
}

func (a *App) pushMouseButton(w *Window, ev objc.ID, kind EventKind) {
	x, y := a.mousePos(w, ev)
	// A press in the title bar or resize edge belongs to the window
	// system (dragging, resizing), not to the game.
	if kind == EventMouseDown && (x < 0 || y < 0 || x >= float64(w.width) || y >= float64(w.height)) {
		return
	}
	n := objc.Send[int](ev, a.c.sel.buttonNumber)
	var button MouseButton
	switch n {
	case 0:
		button = input.MouseLeft
	case 1:
		button = input.MouseRight
	case 2:
		button = input.MouseMiddle
	default:
		button = MouseButton(n)
	}
	a.push(Event{Kind: kind, Window: w, Button: button, X: x, Y: y, Mods: a.mods})
}
