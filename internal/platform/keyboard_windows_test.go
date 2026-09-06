package platform

import (
	"github.com/matjam/bunyip/input"
	"runtime"
	"slices"
	"testing"
	"unsafe"
)

func TestWindowsKeyboardWorkerAndKeypadPolicy(t *testing.T) {
	for vk, want := range map[uintptr]string{0x83: "F20", 0x84: "F21", 0x87: "F24"} {
		if got := windowsNamedKey(vk, input.KeyF1); got != want {
			t.Errorf("VK %#x = %q, want %q", vk, got, want)
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tid, _, _ := procCurrentThread.Call()
	var wrongThread, wrongFlags bool
	queries := []windowsKeyQuery{{input.KeyA, 0x41, 0x1e, "native A"}, {input.KeyKeypad1, 0x61, 0x4f, "native keypad"}, {input.KeyEnd, 0x23, 0xe04f, "End"}}
	keys := isolatedWindowsKeys(7, queries, func(hkl, vk, scan, flags uintptr) (string, bool) {
		worker, _, _ := procCurrentThread.Call()
		wrongThread = wrongThread || worker == tid
		wrongFlags = wrongFlags || hkl != 7 || (vk == 0x20 && flags != 0) || (vk != 0x20 && flags != 4)
		return "a", false
	})
	l := input.KeyboardLayout{Keys: keys}
	if wrongThread || wrongFlags || l.Symbol(input.KeyA) != input.TextSymbol("a") || l.Label(input.KeyA) != "native A" || !slices.Equal(l.KeysFor("key:End"), []input.Key{input.KeyEnd, input.KeyKeypad1}) || len(l.KeysFor(input.TextSymbol("1"))) != 0 {
		t.Fatalf("worker or locks-off reverse lookup failed: %+v", l)
	}
}

func TestWindowsKeyboardSnapshotPreservesPendingDeadKey(t *testing.T) {
	// Never load or activate a layout. The test owns and retires its thread;
	// use only existing layouts and keep all synthetic composition private.
	type result struct {
		available bool
		err       string
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		list := user32.NewProc("GetKeyboardLayoutList")
		n, _, _ := list.Call(0, 0)
		if n == 0 || n > 256 {
			done <- result{}
			return
		}
		layouts := make([]uintptr, n)
		n, _, _ = list.Call(n, uintptr(unsafe.Pointer(&layouts[0])))
		for _, hkl := range layouts[:min(int(n), len(layouts))] {
			windowsUnicode(hkl, 0x20, 0x39, 0)
			for scan, physical := range scanTable {
				if physical == input.KeyUnknown {
					continue
				}
				vk, _, _ := procMapVirtualKeyEx.Call(uintptr(scan), 3, hkl)
				if _, dead := windowsUnicode(hkl, vk, uintptr(scan), 4); !dead {
					continue
				}
				plainVK, _, _ := procMapVirtualKeyEx.Call(0x1e, 3, hkl)
				plain, _ := windowsUnicode(hkl, plainVK, 0x1e, 4)
				windowsUnicode(hkl, vk, uintptr(scan), 0)
				composed, _ := windowsUnicode(hkl, plainVK, 0x1e, 4)
				if composed == plain {
					windowsUnicode(hkl, 0x20, 0x39, 0)
					continue
				}
				queries := []windowsKeyQuery{{input.KeyA, plainVK, 0x1e, "A"}}
				keys := isolatedWindowsKeys(hkl, queries, windowsUnicode)
				after, _ := windowsUnicode(hkl, plainVK, 0x1e, 4)
				if keys[input.KeyA].Symbol != input.TextSymbol(plain) || composed != after {
					done <- result{true, "snapshot read or changed caller composition"}
					return
				}
				done <- result{available: true}
				return
			}
		}
		done <- result{}
	}()
	r := <-done
	if r.err != "" {
		t.Fatal(r.err)
	}
	if !r.available {
		t.Skip("no already-loaded layout has an unmodified dead key")
	}
}
