package platform

import "github.com/matjam/bunyip/input"

// scanTable maps Windows scan codes (set 1, from WM_KEYDOWN's lParam bits
// 16-23) to keys by physical position. Extended keys (bit 24) are looked
// up in extTable.
var scanTable = [128]Key{
	0x01: input.KeyEscape, 0x02: input.Key1, 0x03: input.Key2, 0x04: input.Key3, 0x05: input.Key4, 0x06: input.Key5, 0x07: input.Key6,
	0x08: input.Key7, 0x09: input.Key8, 0x0A: input.Key9, 0x0B: input.Key0, 0x0C: input.KeyMinus, 0x0D: input.KeyEqual, 0x0E: input.KeyBackspace,
	0x0F: input.KeyTab, 0x10: input.KeyQ, 0x11: input.KeyW, 0x12: input.KeyE, 0x13: input.KeyR, 0x14: input.KeyT, 0x15: input.KeyY, 0x16: input.KeyU,
	0x17: input.KeyI, 0x18: input.KeyO, 0x19: input.KeyP, 0x1A: input.KeyLeftBracket, 0x1B: input.KeyRightBracket, 0x1C: input.KeyEnter,
	0x1D: input.KeyLeftControl, 0x1E: input.KeyA, 0x1F: input.KeyS, 0x20: input.KeyD, 0x21: input.KeyF, 0x22: input.KeyG, 0x23: input.KeyH,
	0x24: input.KeyJ, 0x25: input.KeyK, 0x26: input.KeyL, 0x27: input.KeySemicolon, 0x28: input.KeyApostrophe, 0x29: input.KeyGrave,
	0x2A: input.KeyLeftShift, 0x2B: input.KeyBackslash, 0x2C: input.KeyZ, 0x2D: input.KeyX, 0x2E: input.KeyC, 0x2F: input.KeyV, 0x30: input.KeyB,
	0x31: input.KeyN, 0x32: input.KeyM, 0x33: input.KeyComma, 0x34: input.KeyPeriod, 0x35: input.KeySlash, 0x36: input.KeyRightShift,
	0x37: input.KeyKeypadMultiply, 0x38: input.KeyLeftAlt, 0x39: input.KeySpace, 0x3A: input.KeyCapsLock, 0x3B: input.KeyF1, 0x3C: input.KeyF2,
	0x3D: input.KeyF3, 0x3E: input.KeyF4, 0x3F: input.KeyF5, 0x40: input.KeyF6, 0x41: input.KeyF7, 0x42: input.KeyF8, 0x43: input.KeyF9,
	0x44: input.KeyF10, 0x45: input.KeyPause, 0x46: input.KeyScrollLock, 0x47: input.KeyKeypad7, 0x48: input.KeyKeypad8, 0x49: input.KeyKeypad9,
	0x4A: input.KeyKeypadSubtract, 0x4B: input.KeyKeypad4, 0x4C: input.KeyKeypad5, 0x4D: input.KeyKeypad6, 0x4E: input.KeyKeypadAdd,
	0x4F: input.KeyKeypad1, 0x50: input.KeyKeypad2, 0x51: input.KeyKeypad3, 0x52: input.KeyKeypad0, 0x53: input.KeyKeypadDecimal,
	0x56: input.KeyWorld1, 0x57: input.KeyF11, 0x58: input.KeyF12, 0x59: input.KeyKeypadEqual, 0x64: input.KeyF13, 0x65: input.KeyF14,
	0x66: input.KeyF15, 0x67: input.KeyF16, 0x68: input.KeyF17, 0x69: input.KeyF18, 0x6A: input.KeyF19, 0x6B: input.KeyF20,
}

// extTable covers scan codes with the extended bit set.
var extTable = map[uint32]Key{
	0x1C: input.KeyKeypadEnter, 0x1D: input.KeyRightControl, 0x35: input.KeyKeypadDivide, 0x37: input.KeyPrintScreen,
	0x38: input.KeyRightAlt, 0x45: input.KeyNumLock, 0x46: input.KeyPause, 0x47: input.KeyHome, 0x48: input.KeyUp, 0x49: input.KeyPageUp,
	0x4B: input.KeyLeft, 0x4D: input.KeyRight, 0x4F: input.KeyEnd, 0x50: input.KeyDown, 0x51: input.KeyPageDown, 0x52: input.KeyInsert,
	0x53: input.KeyDelete, 0x5B: input.KeyLeftSuper, 0x5C: input.KeyRightSuper, 0x5D: input.KeyMenu,
}

// keyFromLParam decodes a WM_KEYDOWN/WM_KEYUP lParam.
func keyFromLParam(lparam uintptr) Key {
	scan := uint32(lparam>>16) & 0xFF
	if lparam&(1<<24) != 0 {
		if k, ok := extTable[scan]; ok {
			return k
		}
		return input.KeyUnknown
	}
	if scan < uint32(len(scanTable)) {
		return scanTable[scan]
	}
	return input.KeyUnknown
}
