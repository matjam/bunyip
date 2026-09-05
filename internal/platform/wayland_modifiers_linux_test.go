package platform

import (
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/matjam/bunyip/input"
)

// The XKB boundary is replaced here; no display or native library is opened.
func modifierTestApp(t *testing.T) (*wlApp, *wlWindow) {
	t.Helper()
	old := wlModNames
	t.Cleanup(func() { wlModNames = old })
	wlModNames = nil
	for _, mod := range []Mods{input.ModShift, input.ModControl, input.ModAlt, input.ModSuper, input.ModCapsLock, input.ModNumLock} {
		name := byte(mod)
		wlModNames = append(wlModNames, modName{c: &name, m: mod})
	}
	var effective uint32
	l := &wllib{
		xkbStateUpdateMask: func(_ unsafe.Pointer, depressed, latched, locked, _, _, _ uint32) uint32 {
			effective = depressed | latched | locked
			return 0
		},
		xkbModActive: func(_ unsafe.Pointer, name *byte, component uint32) int32 {
			if component != xkbStateModsEffective {
				t.Errorf("modifier component = %d, want effective", component)
			}
			if effective&uint32(*name) != 0 {
				return 1
			}
			return 0
		},
	}
	a := &wlApp{out: &App{}, l: l, xkbState: unsafe.Pointer(&effective)}
	w := &wlWindow{app: a, out: &Window{}, activated: true}
	a.kbFocus = w
	return a, w
}

func TestWaylandModifierReleaseFollowsKeyEvent(t *testing.T) {
	a, w := modifierTestApp(t)
	a.mods = input.ModShift
	// wl_keyboard.key precedes the resulting wl_keyboard.modifiers event.
	a.onKey(42, 0) // evdev KEY_LEFTSHIFT
	a.onModifiers(0, 0, 0, 0)
	events := a.out.pending
	if len(events) != 2 {
		t.Fatalf("Shift release delivered %d events, want key release then modifier state: %+v", len(events), events)
	}
	if events[0].Kind != EventKeyUp || events[0].Key != input.KeyLeftShift || events[0].Mods != input.ModShift {
		t.Errorf("key release = %+v", events[0])
	}
	if events[1].Kind != EventModifiers || events[1].Window != w.out || events[1].Mods != 0 {
		t.Errorf("modifier release = %+v, want focused window with no modifiers", events[1])
	}
}

func TestWaylandModifiersWithoutKeys(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		depressed, latched, locked  Mods
		keyboardFocus, pointerFocus bool
	}{
		{"depressed", input.ModShift | input.ModControl, 0, 0, true, false},
		{"latched", 0, input.ModAlt | input.ModSuper, 0, true, false},
		{"locks", 0, 0, input.ModCapsLock | input.ModNumLock, true, false},
		{"combined", input.ModShift, input.ModAlt, input.ModCapsLock, true, true},
		{"pointer focus only", input.ModControl, 0, input.ModNumLock, false, true},
		{"no focused surface", input.ModShift, 0, 0, false, false},
		{"no focused surface reset", 0, 0, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, w := modifierTestApp(t)
			var wantWindow *Window
			if !tc.keyboardFocus {
				a.kbFocus = nil
			} else {
				wantWindow = w.out
			}
			if tc.pointerFocus {
				a.focus = &wlWindow{app: a, out: &Window{}}
				if wantWindow == nil {
					wantWindow = a.focus.out
				}
			}
			due := time.Unix(123, 0)
			a.repeatKey, a.repeatWindow, a.repeatDue = 30, w, due
			update := a.l.xkbStateUpdateMask
			a.l.xkbStateUpdateMask = func(st unsafe.Pointer, depressed, latched, locked, depGroup, latGroup, lockGroup uint32) uint32 {
				if depressed != uint32(tc.depressed) || latched != uint32(tc.latched) || locked != uint32(tc.locked) || depGroup != 0 || latGroup != 0 || lockGroup != 2 {
					t.Errorf("XKB masks/groups = %v", []uint32{depressed, latched, locked, depGroup, latGroup, lockGroup})
				}
				return update(st, depressed, latched, locked, depGroup, latGroup, lockGroup)
			}
			a.onModifiers(uint32(tc.depressed), uint32(tc.latched), uint32(tc.locked), 2)
			wantMods := tc.depressed | tc.latched | tc.locked
			want := []Event{{Kind: EventModifiers, Window: wantWindow, Mods: wantMods}}
			if !reflect.DeepEqual(a.out.pending, want) || a.mods != wantMods {
				t.Errorf("modifiers=%v events=%+v, want %+v", a.mods, a.out.pending, want)
			}
			if a.repeatKey != 30 || a.repeatWindow != w || a.repeatDue != due {
				t.Error("modifier update changed key repeat scheduling")
			}
		})
	}
	if EventModifiers.String() != "Modifiers" {
		t.Errorf("modifier event name = %q", EventModifiers.String())
	}
}

func TestWaylandKeyboardLeaveResetsModifiers(t *testing.T) {
	for _, alreadyUnfocused := range []bool{false, true} {
		t.Run(map[bool]string{false: "keyboard leave first", true: "configure focus loss first"}[alreadyUnfocused], func(t *testing.T) {
			a, w := modifierTestApp(t)
			a.onModifiers(uint32(input.ModShift), 0, uint32(input.ModCapsLock), 0)
			a.repeatKey, a.repeatWindow = 30, w
			if alreadyUnfocused {
				w.setFocused(false)
			}
			a.out.pending = nil
			// Drive the listener entry point to cover its delegation and the
			// deduplicated configure/keyboard focus notifications together.
			wlInitCallbacks()
			old := wlCurrent
			wlCurrent = a
			t.Cleanup(func() { wlCurrent = old })
			var surface byte
			a.owner = map[unsafe.Pointer]*wlWindow{unsafe.Pointer(&surface): w}
			var leave func(unsafe.Pointer, unsafe.Pointer, uint32, unsafe.Pointer)
			purego.RegisterFunc(&leave, cbKeyboardLeave)
			leave(nil, nil, 1, unsafe.Pointer(&surface))
			if a.mods != 0 || a.kbFocus != nil || a.repeatKey != 0 || a.repeatWindow != nil {
				t.Error("keyboard leave retained modifier, focus or repeat state")
			}
			for _, m := range wlModNames {
				if a.l.xkbModActive(a.xkbState, m.c, xkbStateModsEffective) != 0 {
					t.Errorf("keyboard leave retained XKB modifier %v", m.m)
				}
			}
			want := []Event{{Kind: EventModifiers, Window: w.out}}
			if !alreadyUnfocused {
				want = append(want, Event{Kind: EventFocus, Window: w.out})
			}
			if !reflect.DeepEqual(a.out.pending, want) {
				t.Errorf("leave events=%+v, want %+v", a.out.pending, want)
			}
			a.out.pending = nil
			var enter func(unsafe.Pointer, unsafe.Pointer, uint32, unsafe.Pointer, unsafe.Pointer)
			purego.RegisterFunc(&enter, cbKeyboardEnter)
			enter(nil, nil, 2, unsafe.Pointer(&surface), nil)
			a.onModifiers(0, 0, uint32(input.ModCapsLock), 0)
			want = []Event{{Kind: EventFocus, Window: w.out, Focused: true}, {Kind: EventModifiers, Window: w.out, Mods: input.ModCapsLock}}
			if !reflect.DeepEqual(a.out.pending, want) {
				t.Errorf("reenter events=%+v, want %+v", a.out.pending, want)
			}
		})
	}
}

func TestWaylandModifierChangePreservesRepeatingText(t *testing.T) {
	a, w := modifierTestApp(t)
	a.l.xkbStateKeyGetUTF8 = func(_ unsafe.Pointer, key uint32, buf *byte, _ uintptr) int32 {
		if key != 38 {
			t.Errorf("text key=%d, want XKB A (38)", key)
		}
		*buf = 'a'
		if a.mods&input.ModShift != 0 {
			*buf = 'A'
		}
		return 1
	}
	a.repeatRate, a.repeatDelay = 20, 500
	a.onKey(30, 1)
	a.onModifiers(uint32(input.ModShift), 0, 0, 0)
	a.pumpRepeats(a.repeatDue)
	a.onKey(30, 0)
	want := []Event{
		{Kind: EventKeyDown, Window: w.out, Key: input.KeyA},
		{Kind: EventChar, Window: w.out, Rune: 'a'},
		{Kind: EventModifiers, Window: w.out, Mods: input.ModShift},
		{Kind: EventKeyDown, Window: w.out, Key: input.KeyA, Mods: input.ModShift, Repeat: true},
		{Kind: EventChar, Window: w.out, Rune: 'A', Mods: input.ModShift},
		{Kind: EventKeyUp, Window: w.out, Key: input.KeyA, Mods: input.ModShift},
	}
	if !reflect.DeepEqual(a.out.pending, want) {
		t.Errorf("key/text events=%+v, want %+v", a.out.pending, want)
	}
	if a.repeatKey != 0 {
		t.Error("key release did not cancel repeat")
	}
}
