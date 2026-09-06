package platform

import (
	"github.com/ebitengine/purego"
	"github.com/matjam/bunyip/input"
	"sync"
	"unicode/utf16"
	"unsafe"
)

type cocoaKeyboardAPI struct {
	current                func() uintptr
	property               func(uintptr, uintptr) uintptr
	release                func(uintptr)
	data                   func(uintptr) unsafe.Pointer
	stringUTF8             func(uintptr, *byte, int64, uint32) bool
	kbdType                func() uint8
	translate              func(unsafe.Pointer, uint16, uint16, uint32, uint32, uint32, *uint32, uint64, *uint64, *uint16) int32
	layoutData, layoutName uintptr
}

var loadCocoaKeyboard = sync.OnceValues(func() (*cocoaKeyboardAPI, error) {
	lib, err := purego.Dlopen("/System/Library/Frameworks/Carbon.framework/Carbon", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, ErrUnsupported
	}
	a := &cocoaKeyboardAPI{}
	for name, target := range map[string]any{"TISCopyCurrentKeyboardLayoutInputSource": &a.current, "TISGetInputSourceProperty": &a.property, "CFRelease": &a.release, "CFDataGetBytePtr": &a.data, "CFStringGetCString": &a.stringUTF8, "LMGetKbdType": &a.kbdType, "UCKeyTranslate": &a.translate} {
		if err := bindKeyboardFunction(lib, name, target); err != nil {
			return nil, err
		}
	}
	for name, dst := range map[string]*uintptr{"kTISPropertyUnicodeKeyLayoutData": &a.layoutData, "kTISPropertyLocalizedName": &a.layoutName} {
		p, err := purego.Dlsym(lib, name)
		if err != nil {
			return nil, err
		}
		// Dlsym returns an address in the retained native framework, not Go
		// memory. Read the CFStringRef stored in that exported variable.
		address := *(*unsafe.Pointer)(unsafe.Pointer(&p))
		*dst = *(*uintptr)(address)
	}
	return a, nil
})

func (a *App) KeyboardLayout() (input.KeyboardLayout, error) {
	api, err := loadCocoaKeyboard()
	if err != nil {
		return input.KeyboardLayout{}, err
	}
	source := api.current()
	if source == 0 {
		return input.KeyboardLayout{}, ErrUnsupported
	}
	defer api.release(source)
	data := api.property(source, api.layoutData)
	if data == 0 {
		return input.KeyboardLayout{}, ErrUnsupported
	}
	keymap := api.data(data)
	if keymap == nil {
		return input.KeyboardLayout{}, ErrUnsupported
	}
	var out input.KeyboardLayout
	if name := api.property(source, api.layoutName); name != 0 {
		var b [512]byte
		if api.stringUTF8(name, &b[0], int64(len(b)), 0x08000100) {
			for i, v := range b {
				if v == 0 {
					out.Name = string(b[:i])
					break
				}
			}
		}
	}
	for code := uint16(0); code < 128; code++ {
		key := keyFromCode(code)
		if key == input.KeyUnknown {
			continue
		}
		var text [16]uint16
		var count uint64
		var dead uint32
		// UCKeyActionDisplay uses a private dead-state value, never live IME state.
		if api.translate(keymap, code, 3, 0, uint32(api.kbdType()), 0, &dead, uint64(len(text)), &count, &text[0]) != 0 {
			continue
		}
		s := string(utf16.Decode(text[:min(int(count), len(text))]))
		if dead != 0 && s == "" {
			// Ask for the display form with no dead-key composition, still local.
			dead = 0
			count = 0
			if api.translate(keymap, code, 3, 0, uint32(api.kbdType()), 1, &dead, uint64(len(text)), &count, &text[0]) == 0 {
				s = string(utf16.Decode(text[:min(int(count), len(text))]))
			}
			out.Keys[key] = keyDescription(key, s, s, true)
		} else {
			out.Keys[key] = keyDescription(key, s, s, dead != 0)
		}
	}
	return out, nil
}
