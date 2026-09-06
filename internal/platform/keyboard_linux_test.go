package platform

import (
	"github.com/matjam/bunyip/input"
	"slices"
	"testing"
	"unsafe"
)

func TestX11KeymapRefreshFailureClearsReleasedState(t *testing.T) {
	for _, failMap := range []bool{true, false} {
		t.Run(map[bool]string{true: "keymap", false: "state"}[failMap], func(t *testing.T) {
			var oldMap, oldState, newMap, ctx byte
			mapFreed, stateFreed := false, false
			x := &xlib{
				xkbKeymapFromDevice: func(unsafe.Pointer, unsafe.Pointer, int32, int32) unsafe.Pointer {
					if failMap {
						return nil
					}
					return unsafe.Pointer(&newMap)
				},
				xkbStateFromDevice: func(unsafe.Pointer, unsafe.Pointer, int32) unsafe.Pointer { return nil },
				xkbKeymapUnref: func(p unsafe.Pointer) {
					if p != unsafe.Pointer(&oldMap) {
						t.Fatal("released replacement")
					}
					mapFreed = true
				},
				xkbStateUnref: func(p unsafe.Pointer) {
					if p != unsafe.Pointer(&oldState) {
						t.Fatal("released replacement")
					}
					stateFreed = true
				},
			}
			a := &App{x: x, xkbCtx: unsafe.Pointer(&ctx), xkbKeymap: unsafe.Pointer(&oldMap), xkbState: unsafe.Pointer(&oldState)}
			a.refreshKeymap()
			if !mapFreed || !stateFreed || a.xkbState != nil || a.xkbKeymap == unsafe.Pointer(&oldMap) {
				t.Fatal("failed refresh retained released state")
			}
			if _, err := a.KeyboardLayout(); err != ErrUnsupported {
				t.Fatal("failed replacement used stale snapshot")
			}
		})
	}
}

func TestXKBLayoutUsesActiveGroupAndUnmodifiedLevel(t *testing.T) {
	syms := []uint32{1, 2, 3, 4, 5, 6, 7}
	a := xkbLayoutAPI{
		activeGroup: func(_ unsafe.Pointer, mask uint32) uint32 {
			if mask != 1<<7 {
				t.Fatal("wrong layout component")
			}
			return 1
		},
		group: func(_ unsafe.Pointer, key uint32) uint32 {
			if key == 24 || key == 38 || key == 49 || key == 87 || key == 115 || key == 56 || key == 54 {
				return 1
			}
			return ^uint32(0)
		},
		name: func(_ unsafe.Pointer, group uint32) string {
			if group != 1 {
				t.Fatal("reported wrong layout name")
			}
			return "active layout"
		},
		syms: func(_ unsafe.Pointer, key, group, level uint32, out **uint32) int32 {
			if group != 1 || level != 0 {
				t.Fatal("snapshot used modifiers or wrong group")
			}
			index := map[uint32]int{24: 0, 38: 1, 49: 2, 87: 3, 115: 4, 56: 5, 54: 6}[key]
			*out = &syms[index]
			return 1
		},
		utf8: func(sym uint32, b *byte, size uintptr) int32 {
			s := map[uint32]string{1: "a", 2: "й", 6: "\r"}[sym]
			if s == "" {
				return 0
			}
			copy(unsafe.Slice(b, int(size)), s+"\x00")
			return int32(len(s) + 1)
		},
		symName: func(sym uint32, b *byte, size uintptr) int32 {
			s := map[uint32]string{1: "a", 2: "Cyrillic_shorti", 3: "dead_acute", 4: "KP_End", 5: "End", 6: "Return"}[sym]
			copy(unsafe.Slice(b, int(size)), s)
			return int32(len(s))
		},
	}
	l := a.snapshot(nil, nil)
	if l.Symbol(input.KeyB) != "key:Enter" || l.Symbol(input.KeyC) != "" {
		t.Fatal("remapped named/unknown keys used physical position")
	}
	if !slices.Equal(l.KeysFor("key:End"), []input.Key{input.KeyEnd, input.KeyKeypad1}) || len(l.KeysFor(input.TextSymbol("1"))) != 0 {
		t.Fatal("keypad level-zero navigation reverse lookup failed")
	}
	if l.Name != "active layout" || l.Symbol(input.KeyQ) != input.TextSymbol("a") || l.Symbol(input.KeyA) != input.TextSymbol("й") || l.Symbol(input.KeyGrave) != "dead:dead_acute" || l.Symbol(input.KeyZ) != "" {
		t.Fatalf("layout=%+v", l)
	}
	if _, err := (&App{}).KeyboardLayout(); err != ErrUnsupported {
		t.Fatal("missing map did not report unsupported")
	}
}

func TestJoystickUsesKernelMapsAndOnlyAdvertisesMappedControls(t *testing.T) {
	js := &joystick{axisCount: 4, buttonCount: 3, state: GamepadState{Name: "native"}}
	js.axisMap = [64]uint8{4, 16, 2, 40}
	js.buttonMap = [512]uint16{0x131, 0x130, 0x2ff}
	i := js.mappedInfo()
	if !i.HasButton(input.ButtonA) || !i.HasButton(input.ButtonB) || i.HasButton(input.ButtonX) || !i.HasButton(input.ButtonDpadLeft) || !i.HasAxis(input.AxisRightY) || i.HasAxis(input.AxisLeftX) || i.VendorID != 0 {
		t.Fatalf("mapped capabilities=%+v", i)
	}
	js.event(jsEventButton, 0, 1)
	js.event(jsEventAxis, 0, -32768)
	js.event(jsEventAxis, 1, -32768)
	js.event(jsEventAxis, 2, -32768)
	if !js.state.Buttons[input.ButtonB] || js.state.Buttons[input.ButtonA] || js.state.Axes[input.AxisRightY] != 1 || !js.state.Buttons[input.ButtonDpadLeft] || js.state.Axes[input.AxisLeftTrigger] != 0 {
		t.Fatalf("kernel-mapped state=%+v", js.state)
	}
	before := js.state
	js.event(jsEventButton, 2, 1)
	js.event(jsEventAxis, 3, 123)
	if js.state != before {
		t.Fatal("unmapped hardware input changed standard controls")
	}
}
