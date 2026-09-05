package platform

import (
	"errors"
	"math"
	"testing"
	"unsafe"
)

func TestRandRModeLayoutAndRefresh(t *testing.T) {
	var m randrMode
	if unsafe.Sizeof(m) != 32 || unsafe.Offsetof(m.DotClock) != 8 || unsafe.Offsetof(m.HTotal) != 16 || unsafe.Offsetof(m.VTotal) != 24 || unsafe.Offsetof(m.Flags) != 28 {
		t.Fatal("RandR mode layout differs from xcb/randr.h")
	}
	m = randrMode{Width: 1920, Height: 1080, DotClock: 148500000, HTotal: 2200, VTotal: 1125}
	for _, tc := range []struct {
		flags uint32
		hz    float64
	}{{0, 60}, {0x10, 120}, {0x20, 30}} {
		m.Flags = tc.flags
		v := m.videoMode()
		if math.Abs(v.RefreshHz-tc.hz) > 1e-9 {
			t.Fatalf("flags %x rate %g", tc.flags, v.RefreshHz)
		}
	}
	m.HTotal = 0
	if m.videoMode().RefreshHz != 0 {
		t.Fatal("missing timing fabricated a rate")
	}
}
func TestWaylandDisplaySnapshot(t *testing.T) {
	p := new(byte)
	o := &wlOutput{name: 4, description: "Reported panel", scale: 2, current: VideoMode{Width: 1920, Height: 1080, RefreshHz: 60}, modes: []VideoMode{{Width: 1920, Height: 1080, RefreshHz: 60}}}
	a := &App{wl: &wlApp{outputs: map[unsafe.Pointer]*wlOutput{unsafe.Pointer(p): o}}}
	d, err := a.Displays()
	if err != nil || len(d) != 1 {
		t.Fatalf("displays %v %v", d, err)
	}
	if d[0].BoundsKnown || d[0].Scale != 2 || d[0].Name != o.description {
		t.Fatalf("display metadata %v", d[0])
	}
	d[0].Modes[0].Width = 1
	if o.modes[0].Width != 1920 {
		t.Fatal("snapshot aliases platform modes")
	}
}
func TestWaylandUnsupportedControls(t *testing.T) {
	w := &Window{wl: &wlWindow{app: &wlApp{}}}
	for _, f := range []func() error{w.Show, w.Hide, w.RequestFocus, func() error { return w.SetAlwaysOnTop(true) }, func() error { return w.SetPointerPosition(10, 20) }} {
		if !errors.Is(f(), ErrUnsupported) {
			t.Fatal("unsupported Wayland request accepted")
		}
	}
}

func TestWaylandCustomCursorProtocol(t *testing.T) {
	var tokens [4]byte
	ptr := func(i int) unsafe.Pointer { return unsafe.Pointer(&tokens[i]) }
	var operations []uint32
	var cursorArgs [4]uintptr
	l := &wllib{getVersion: func(unsafe.Pointer) uint32 { return 4 }}
	l.marshal0 = func(p unsafe.Pointer, op uint32, _ unsafe.Pointer, _, flags uint32) unsafe.Pointer {
		operations = append(operations, op)
		return nil
	}
	l.marshal1 = func(p unsafe.Pointer, op uint32, _ unsafe.Pointer, _, _ uint32, arg uintptr) unsafe.Pointer {
		operations = append(operations, op)
		if op == opSurfaceSetBufferScale && arg != 1 {
			t.Errorf("custom cursor scale %d", arg)
		}
		return nil
	}
	l.marshal3 = func(p unsafe.Pointer, op uint32, _ unsafe.Pointer, _, _ uint32, a, b, c uintptr) unsafe.Pointer {
		operations = append(operations, op)
		if op == opSurfaceAttach && a != uintptr(ptr(2)) {
			t.Error("wrong cursor buffer attached")
		}
		return nil
	}
	l.marshal4 = func(p unsafe.Pointer, op uint32, _ unsafe.Pointer, _, _ uint32, a, b, c, d uintptr) unsafe.Pointer {
		operations = append(operations, op)
		if op == opPointerSetCursor {
			cursorArgs = [4]uintptr{a, b, c, d}
		}
		return nil
	}
	a := &wlApp{l: l, pointer: ptr(0), enterSerial: 42, compositorVer: 4}
	w := &wlWindow{app: a, cursorSurface: ptr(1), customCursor: ptr(2), customWidth: 32, customHeight: 24, hotX: 7, hotY: 9}
	a.focus = w
	w.applyCursor()
	if cursorArgs != [4]uintptr{42, uintptr(ptr(1)), 7, 9} {
		t.Fatalf("set_cursor arguments %v", cursorArgs)
	}
	if len(operations) != 5 {
		t.Fatalf("cursor request sequence %v", operations)
	}
	w.captured = true
	operations = nil
	w.applyCursor()
	if len(operations) != 1 || cursorArgs[1] != 0 {
		t.Fatal("capture did not hide custom cursor")
	}
	w.dropCursorImage()
	if w.customCursor != nil {
		t.Fatal("custom cursor reference retained")
	}
}

func TestWaylandResizeUsesNormalResizeEvents(t *testing.T) {
	a := &App{}
	wa := &wlApp{out: a}
	w := &Window{app: a}
	v := &wlWindow{app: wa, out: w, width: 640, height: 480, scale: 2, resizable: true, minW: 100, maxW: 900}
	w.wl = v
	if err := w.SetSize(1200, 600); err != nil {
		t.Fatal(err)
	}
	if v.width != 900 || v.height != 600 || len(a.pending) != 1 {
		t.Fatalf("resize state %dx%d events %v", v.width, v.height, a.pending)
	}
	e := a.pending[0]
	if e.Kind != EventResize || e.Width != 900 || e.PixelW != 1800 {
		t.Fatalf("resize event %v", e)
	}
	v.fullscreen = true
	if err := w.SetSize(200, 100); err == nil {
		t.Fatal("fullscreen size changed without compositor")
	}
}
