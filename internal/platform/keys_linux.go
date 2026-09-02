package platform

import "github.com/matjam/bunyip/input"

// evdevTable maps Linux evdev key codes (input-event-codes.h) to keys by
// physical position. X11 key codes are evdev codes plus eight.
var evdevTable = [256]Key{
	1: input.KeyEscape, 2: input.Key1, 3: input.Key2, 4: input.Key3, 5: input.Key4, 6: input.Key5, 7: input.Key6, 8: input.Key7, 9: input.Key8,
	10: input.Key9, 11: input.Key0, 12: input.KeyMinus, 13: input.KeyEqual, 14: input.KeyBackspace, 15: input.KeyTab, 16: input.KeyQ, 17: input.KeyW,
	18: input.KeyE, 19: input.KeyR, 20: input.KeyT, 21: input.KeyY, 22: input.KeyU, 23: input.KeyI, 24: input.KeyO, 25: input.KeyP,
	26: input.KeyLeftBracket, 27: input.KeyRightBracket, 28: input.KeyEnter, 29: input.KeyLeftControl, 30: input.KeyA, 31: input.KeyS, 32: input.KeyD,
	33: input.KeyF, 34: input.KeyG, 35: input.KeyH, 36: input.KeyJ, 37: input.KeyK, 38: input.KeyL, 39: input.KeySemicolon, 40: input.KeyApostrophe,
	41: input.KeyGrave, 42: input.KeyLeftShift, 43: input.KeyBackslash, 44: input.KeyZ, 45: input.KeyX, 46: input.KeyC, 47: input.KeyV, 48: input.KeyB,
	49: input.KeyN, 50: input.KeyM, 51: input.KeyComma, 52: input.KeyPeriod, 53: input.KeySlash, 54: input.KeyRightShift, 55: input.KeyKeypadMultiply,
	56: input.KeyLeftAlt, 57: input.KeySpace, 58: input.KeyCapsLock, 59: input.KeyF1, 60: input.KeyF2, 61: input.KeyF3, 62: input.KeyF4, 63: input.KeyF5,
	64: input.KeyF6, 65: input.KeyF7, 66: input.KeyF8, 67: input.KeyF9, 68: input.KeyF10, 69: input.KeyNumLock, 70: input.KeyScrollLock,
	71: input.KeyKeypad7, 72: input.KeyKeypad8, 73: input.KeyKeypad9, 74: input.KeyKeypadSubtract, 75: input.KeyKeypad4, 76: input.KeyKeypad5,
	77: input.KeyKeypad6, 78: input.KeyKeypadAdd, 79: input.KeyKeypad1, 80: input.KeyKeypad2, 81: input.KeyKeypad3, 82: input.KeyKeypad0,
	83: input.KeyKeypadDecimal, 86: input.KeyWorld1, 87: input.KeyF11, 88: input.KeyF12, 96: input.KeyKeypadEnter, 97: input.KeyRightControl,
	98: input.KeyKeypadDivide, 99: input.KeyPrintScreen, 100: input.KeyRightAlt, 102: input.KeyHome, 103: input.KeyUp, 104: input.KeyPageUp,
	105: input.KeyLeft, 106: input.KeyRight, 107: input.KeyEnd, 108: input.KeyDown, 109: input.KeyPageDown, 110: input.KeyInsert, 111: input.KeyDelete,
	117: input.KeyKeypadEqual, 119: input.KeyPause, 125: input.KeyLeftSuper, 126: input.KeyRightSuper, 127: input.KeyMenu,
	183: input.KeyF13, 184: input.KeyF14, 185: input.KeyF15, 186: input.KeyF16, 187: input.KeyF17, 188: input.KeyF18, 189: input.KeyF19, 190: input.KeyF20,
}

// keyFromX11 maps an X11 key code.
func keyFromX11(code uint8) Key {
	if code < 8 {
		return input.KeyUnknown
	}
	return evdevTable[int(code)-8]
}
