package platform

import (
	"github.com/matjam/bunyip/input"
	"runtime"
	"strconv"
	"unicode/utf16"
	"unsafe"
)

type windowsKeyQuery struct {
	key      input.Key
	vk, scan uintptr
	label    string
}

var (
	procKeyboardLayout     = user32.NewProc("GetKeyboardLayout")
	procKeyboardLayoutName = user32.NewProc("GetKeyboardLayoutNameW")
	procMapVirtualKeyEx    = user32.NewProc("MapVirtualKeyExW")
	procToUnicodeEx        = user32.NewProc("ToUnicodeEx")
	procKeyNameText        = user32.NewProc("GetKeyNameTextW")
)

func (a *App) KeyboardLayout() (input.KeyboardLayout, error) {
	hkl, _, _ := procKeyboardLayout.Call(0)
	if hkl == 0 {
		return input.KeyboardLayout{}, ErrUnsupported
	}
	var out input.KeyboardLayout
	var queries []windowsKeyQuery
	var name [9]uint16
	if ok, _, _ := procKeyboardLayoutName.Call(uintptr(unsafe.Pointer(&name[0]))); ok != 0 {
		out.Name = string(utf16.Decode(name[:8]))
	}
	read := func(key input.Key, scan uint32, extended bool) {
		if key == input.KeyUnknown {
			return
		}
		lparam := uintptr(scan) << 16
		native := scan
		if extended {
			lparam |= 1 << 24
			native |= 0xe000
		}
		vk, _, _ := procMapVirtualKeyEx.Call(uintptr(native), 3, hkl)
		if vk == 0 {
			return
		}
		var label [128]uint16
		n, _, _ := procKeyNameText.Call(lparam, uintptr(unsafe.Pointer(&label[0])), uintptr(len(label)))
		queries = append(queries, windowsKeyQuery{key, vk, uintptr(native), string(utf16.Decode(label[:min(int(n), len(label))]))})
	}
	for scan, key := range scanTable {
		read(key, uint32(scan), false)
	}
	for scan, key := range extTable {
		read(key, scan, true)
	}
	out.Keys = isolatedWindowsKeys(hkl, queries, windowsUnicode)
	return out, nil
}

func windowsUnicode(hkl, vk, scan, flags uintptr) (string, bool) {
	var text [16]uint16
	var state [256]byte
	n, _, _ := procToUnicodeEx.Call(vk, scan, uintptr(unsafe.Pointer(&state[0])), uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), flags, hkl)
	count := int(int32(n))
	dead := count < 0
	if dead {
		count = -count
	}
	return string(utf16.Decode(text[:min(count, len(text))])), dead
}

func isolatedWindowsKeys(hkl uintptr, queries []windowsKeyQuery, translate func(uintptr, uintptr, uintptr, uintptr) (string, bool)) [input.KeyCount]input.KeyDescription {
	result := make(chan [input.KeyCount]input.KeyDescription, 1)
	go func() {
		// The UI thread is already locked. Do not attach its input queue: bit 2
		// avoids writing dead-key state but still reads pending composition.
		runtime.LockOSThread()
		// Clear only this worker's private buffer. Exit while locked so Go
		// retires this OS thread instead of lending its keyboard state onward.
		cleared := false
		for range 16 {
			if _, dead := translate(hkl, 0x20, 0x39, 0); !dead {
				cleared = true
				break
			}
		}
		var keys [input.KeyCount]input.KeyDescription
		if !cleared {
			result <- keys
			return
		}
		for _, q := range queries {
			if name := windowsNamedKey(q.vk, q.key); name != "" {
				keys[q.key] = input.KeyDescription{Label: q.label, Symbol: input.KeySymbol("key:" + name)}
				continue
			}
			text, dead := translate(hkl, q.vk, q.scan, 4)
			keys[q.key] = keyDescription(q.key, q.label, text, dead)
		}
		result <- keys
	}()
	return <-result
}

func windowsNamedKey(vk uintptr, physical input.Key) string {
	// ToUnicodeEx ignores NumLock. Resolve the native locks-off keypad
	// navigation before translation, rather than reporting numeric VK text.
	if physical >= input.KeyKeypad0 && physical <= input.KeyKeypad9 {
		return [...]string{"Insert", "End", "Down", "PageDown", "Left", "Clear", "Right", "Home", "Up", "PageUp"}[physical-input.KeyKeypad0]
	}
	if physical == input.KeyKeypadDecimal {
		return "Delete"
	}
	if vk >= 0x70 && vk <= 0x87 {
		return "F" + strconv.Itoa(int(vk-0x70)+1)
	}
	return map[uintptr]string{0x08: "Backspace", 0x09: "Tab", 0x0c: "Clear", 0x0d: "Enter", 0x13: "Pause", 0x14: "CapsLock", 0x1b: "Escape", 0x21: "PageUp", 0x22: "PageDown", 0x23: "End", 0x24: "Home", 0x25: "Left", 0x26: "Up", 0x27: "Right", 0x28: "Down", 0x2c: "PrintScreen", 0x2d: "Insert", 0x2e: "Delete", 0x5b: "LeftSuper", 0x5c: "RightSuper", 0x5d: "Menu", 0x90: "NumLock", 0x91: "ScrollLock", 0xa0: "LeftShift", 0xa1: "RightShift", 0xa2: "LeftControl", 0xa3: "RightControl", 0xa4: "LeftAlt", 0xa5: "RightAlt"}[vk]
}
