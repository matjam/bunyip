package platform

import "github.com/matjam/bunyip/input"

// keyTable maps macOS virtual key codes (kVK_* in HIToolbox/Events.h) to keys.
var keyTable = [128]Key{
	0x00: input.KeyA, 0x01: input.KeyS, 0x02: input.KeyD, 0x03: input.KeyF, 0x04: input.KeyH, 0x05: input.KeyG, 0x06: input.KeyZ, 0x07: input.KeyX,
	0x08: input.KeyC, 0x09: input.KeyV, 0x0A: input.KeyWorld1, 0x0B: input.KeyB, 0x0C: input.KeyQ, 0x0D: input.KeyW, 0x0E: input.KeyE, 0x0F: input.KeyR,
	0x10: input.KeyY, 0x11: input.KeyT, 0x12: input.Key1, 0x13: input.Key2, 0x14: input.Key3, 0x15: input.Key4, 0x16: input.Key6, 0x17: input.Key5,
	0x18: input.KeyEqual, 0x19: input.Key9, 0x1A: input.Key7, 0x1B: input.KeyMinus, 0x1C: input.Key8, 0x1D: input.Key0, 0x1E: input.KeyRightBracket,
	0x1F: input.KeyO, 0x20: input.KeyU, 0x21: input.KeyLeftBracket, 0x22: input.KeyI, 0x23: input.KeyP, 0x24: input.KeyEnter, 0x25: input.KeyL,
	0x26: input.KeyJ, 0x27: input.KeyApostrophe, 0x28: input.KeyK, 0x29: input.KeySemicolon, 0x2A: input.KeyBackslash, 0x2B: input.KeyComma,
	0x2C: input.KeySlash, 0x2D: input.KeyN, 0x2E: input.KeyM, 0x2F: input.KeyPeriod, 0x30: input.KeyTab, 0x31: input.KeySpace, 0x32: input.KeyGrave,
	0x33: input.KeyBackspace, 0x35: input.KeyEscape, 0x36: input.KeyRightSuper, 0x37: input.KeyLeftSuper, 0x38: input.KeyLeftShift,
	0x39: input.KeyCapsLock, 0x3A: input.KeyLeftAlt, 0x3B: input.KeyLeftControl, 0x3C: input.KeyRightShift, 0x3D: input.KeyRightAlt,
	0x3E: input.KeyRightControl, 0x3F: input.KeyFunction, 0x40: input.KeyF17, 0x41: input.KeyKeypadDecimal, 0x43: input.KeyKeypadMultiply,
	0x45: input.KeyKeypadAdd, 0x47: input.KeyNumLock, 0x4B: input.KeyKeypadDivide, 0x4C: input.KeyKeypadEnter, 0x4E: input.KeyKeypadSubtract,
	0x4F: input.KeyF18, 0x50: input.KeyF19, 0x51: input.KeyKeypadEqual, 0x52: input.KeyKeypad0, 0x53: input.KeyKeypad1, 0x54: input.KeyKeypad2,
	0x55: input.KeyKeypad3, 0x56: input.KeyKeypad4, 0x57: input.KeyKeypad5, 0x58: input.KeyKeypad6, 0x59: input.KeyKeypad7, 0x5A: input.KeyF20,
	0x5B: input.KeyKeypad8, 0x5C: input.KeyKeypad9, 0x60: input.KeyF5, 0x61: input.KeyF6, 0x62: input.KeyF7, 0x63: input.KeyF3, 0x64: input.KeyF8,
	0x65: input.KeyF9, 0x67: input.KeyF11, 0x69: input.KeyF13, 0x6A: input.KeyF16, 0x6B: input.KeyF14, 0x6D: input.KeyF10, 0x6E: input.KeyMenu,
	0x6F: input.KeyF12, 0x71: input.KeyF15, 0x72: input.KeyInsert, 0x73: input.KeyHome, 0x74: input.KeyPageUp, 0x75: input.KeyDelete, 0x76: input.KeyF4,
	0x77: input.KeyEnd, 0x78: input.KeyF2, 0x79: input.KeyPageDown, 0x7A: input.KeyF1, 0x7B: input.KeyLeft, 0x7C: input.KeyRight, 0x7D: input.KeyDown,
	0x7E: input.KeyUp,
}

func keyFromCode(code uint16) Key {
	if int(code) < len(keyTable) {
		return keyTable[code]
	}
	return input.KeyUnknown
}
