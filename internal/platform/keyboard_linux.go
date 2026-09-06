package platform

import (
	"github.com/ebitengine/purego"
	"github.com/matjam/bunyip/input"
	"strings"
	"sync"
	"unsafe"
)

type xkbLayoutAPI struct {
	group       func(unsafe.Pointer, uint32) uint32
	activeGroup func(unsafe.Pointer, uint32) uint32
	syms        func(unsafe.Pointer, uint32, uint32, uint32, **uint32) int32
	name        func(unsafe.Pointer, uint32) string
	utf8        func(uint32, *byte, uintptr) int32
	symName     func(uint32, *byte, uintptr) int32
}

var loadXKBLayout = sync.OnceValues(func() (*xkbLayoutAPI, error) {
	lib, err := purego.Dlopen("libxkbcommon.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, ErrUnsupported
	}
	a := &xkbLayoutAPI{}
	for name, target := range map[string]any{"xkb_state_key_get_layout": &a.group, "xkb_keymap_key_get_syms_by_level": &a.syms, "xkb_keymap_layout_get_name": &a.name, "xkb_keysym_to_utf8": &a.utf8, "xkb_keysym_get_name": &a.symName, "xkb_state_serialize_layout": &a.activeGroup} {
		if err := bindKeyboardFunction(lib, name, target); err != nil {
			return nil, err
		}
	}
	return a, nil
})

func (a *App) KeyboardLayout() (input.KeyboardLayout, error) {
	km, state := a.xkbKeymap, a.xkbState
	if a.wl != nil {
		km, state = a.wl.xkbKeymap, a.wl.xkbState
	}
	if km == nil || state == nil {
		return input.KeyboardLayout{}, ErrUnsupported
	}
	if a.wl == nil && a.x != nil && a.x.xkbStateFromDevice != nil {
		current := a.x.xkbStateFromDevice(km, a.conn, a.xkbDevice)
		if current == nil {
			return input.KeyboardLayout{}, ErrUnsupported
		}
		state = current
		defer a.x.xkbStateUnref(current)
	}
	api, err := loadXKBLayout()
	if err != nil {
		return input.KeyboardLayout{}, err
	}
	return api.snapshot(km, state), nil
}

func (a *xkbLayoutAPI) snapshot(keymap, state unsafe.Pointer) input.KeyboardLayout {
	var out input.KeyboardLayout
	out.Name = a.name(keymap, a.activeGroup(state, 1<<7)) // XKB_STATE_LAYOUT_EFFECTIVE
	for code, key := range evdevTable {
		if key == input.KeyUnknown {
			continue
		}
		native := uint32(code + 8)
		group := a.group(state, native)
		if group == ^uint32(0) {
			continue
		}
		var syms *uint32
		n := a.syms(keymap, native, group, 0, &syms)
		if n <= 0 || n > 16 || syms == nil {
			continue
		}
		var text, label, nativeName string
		dead := false
		for _, sym := range unsafe.Slice(syms, int(n)) {
			var name [64]byte
			if size := a.symName(sym, &name[0], uintptr(len(name))); size > 0 && int(size) < len(name) {
				label = string(name[:size])
				nativeName = label
				if strings.HasPrefix(label, "dead_") {
					dead = true
					text = label
					break
				}
			}
			var bytes [64]byte
			if size := a.utf8(sym, &bytes[0], uintptr(len(bytes))); size > 1 && int(size) <= len(bytes) {
				text += string(bytes[:size-1])
			}
		}
		if !dead && text != "" {
			label = text
		}
		d := keyDescription(key, label, text, dead)
		if !dead && (d.Symbol == "" || strings.HasPrefix(string(d.Symbol), "key:")) && nativeName != "" && nativeName != "NoSymbol" {
			name := strings.TrimPrefix(nativeName, "KP_")
			if canonical := map[string]string{"Return": "Enter", "Prior": "PageUp", "Next": "PageDown", "ISO_Left_Tab": "Tab", "Shift_L": "LeftShift", "Shift_R": "RightShift", "Control_L": "LeftControl", "Control_R": "RightControl", "Alt_L": "LeftAlt", "Alt_R": "RightAlt", "Super_L": "LeftSuper", "Super_R": "RightSuper", "Caps_Lock": "CapsLock", "Num_Lock": "NumLock", "Scroll_Lock": "ScrollLock", "Print": "PrintScreen", "Begin": "Clear"}[name]; canonical != "" {
				name = canonical
			}
			d.Symbol = input.KeySymbol("key:" + name)
		}
		out.Keys[key] = d
	}
	return out
}
