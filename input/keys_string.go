package input

import "strconv"

var keyNames = [...]string{
	KeyUnknown: "Unknown", KeyA: "A", KeyB: "B", KeyC: "C", KeyD: "D", KeyE: "E", KeyF: "F", KeyG: "G",
	KeyH: "H", KeyI: "I", KeyJ: "J", KeyK: "K", KeyL: "L", KeyM: "M", KeyN: "N", KeyO: "O", KeyP: "P",
	KeyQ: "Q", KeyR: "R", KeyS: "S", KeyT: "T", KeyU: "U", KeyV: "V", KeyW: "W", KeyX: "X", KeyY: "Y",
	KeyZ: "Z", Key0: "0", Key1: "1", Key2: "2", Key3: "3", Key4: "4", Key5: "5", Key6: "6", Key7: "7",
	Key8: "8", Key9: "9", KeyF1: "F1", KeyF2: "F2", KeyF3: "F3", KeyF4: "F4", KeyF5: "F5", KeyF6: "F6",
	KeyF7: "F7", KeyF8: "F8", KeyF9: "F9", KeyF10: "F10", KeyF11: "F11", KeyF12: "F12", KeyF13: "F13",
	KeyF14: "F14", KeyF15: "F15", KeyF16: "F16", KeyF17: "F17", KeyF18: "F18", KeyF19: "F19", KeyF20: "F20",
	KeySpace: "Space", KeyEnter: "Enter", KeyEscape: "Escape", KeyTab: "Tab", KeyBackspace: "Backspace",
	KeyDelete: "Delete", KeyInsert: "Insert", KeyHome: "Home", KeyEnd: "End", KeyPageUp: "PageUp",
	KeyPageDown: "PageDown", KeyLeft: "Left", KeyRight: "Right", KeyUp: "Up", KeyDown: "Down",
	KeyMinus: "Minus", KeyEqual: "Equal", KeyLeftBracket: "LeftBracket", KeyRightBracket: "RightBracket",
	KeyBackslash: "Backslash", KeySemicolon: "Semicolon", KeyApostrophe: "Apostrophe", KeyGrave: "Grave",
	KeyComma: "Comma", KeyPeriod: "Period", KeySlash: "Slash", KeyCapsLock: "CapsLock", KeyNumLock: "NumLock",
	KeyScrollLock: "ScrollLock", KeyPrintScreen: "PrintScreen", KeyPause: "Pause", KeyMenu: "Menu",
	KeyLeftShift: "LeftShift", KeyRightShift: "RightShift", KeyLeftControl: "LeftControl",
	KeyRightControl: "RightControl", KeyLeftAlt: "LeftAlt", KeyRightAlt: "RightAlt", KeyLeftSuper: "LeftSuper",
	KeyRightSuper: "RightSuper", KeyFunction: "Function", KeyWorld1: "World1", KeyWorld2: "World2",
	KeyKeypad0: "Keypad0", KeyKeypad1: "Keypad1", KeyKeypad2: "Keypad2", KeyKeypad3: "Keypad3",
	KeyKeypad4: "Keypad4", KeyKeypad5: "Keypad5", KeyKeypad6: "Keypad6", KeyKeypad7: "Keypad7",
	KeyKeypad8: "Keypad8", KeyKeypad9: "Keypad9", KeyKeypadDecimal: "KeypadDecimal",
	KeyKeypadDivide: "KeypadDivide", KeyKeypadMultiply: "KeypadMultiply", KeyKeypadSubtract: "KeypadSubtract",
	KeyKeypadAdd: "KeypadAdd", KeyKeypadEnter: "KeypadEnter", KeyKeypadEqual: "KeypadEqual",
}

func (k Key) String() string {
	if int(k) < len(keyNames) && keyNames[k] != "" {
		return keyNames[k]
	}
	return "Key(" + strconv.Itoa(int(k)) + ")"
}

func (m Mods) String() string {
	s := ""
	for _, f := range []struct {
		bit  Mods
		name string
	}{{ModShift, "Shift"}, {ModControl, "Control"}, {ModAlt, "Alt"}, {ModSuper, "Super"}, {ModCapsLock, "CapsLock"}, {ModNumLock, "NumLock"}} {
		if m&f.bit != 0 {
			if s != "" {
				s += "+"
			}
			s += f.name
		}
	}
	return s
}
