package platform

import (
	"structs"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// The benchmarks here measure the Objective-C work the loop does every
// frame, on the main thread and with no window on screen:
//
//	go test -run '^$' -bench . -benchmem ./internal/platform/
//
// Everything that touches AppKit runs through onMain. Nothing inside an
// onMain function may call Skip, Fatal or FailNow, which would end the
// main goroutine; the functions return what the test goroutine checks.

// benchApp returns the test binary's App, made on the main thread, or
// skips when there is no window system to connect to.
func benchApp(tb testing.TB) *App {
	var a *App
	var err error
	onMain(func() { a, err = testApp() })
	if err != nil {
		tb.Skipf("no window system: %v", err)
	}
	return a
}

func BenchmarkPollEmpty(b *testing.B) {
	a := benchApp(b)
	b.ReportAllocs()
	onMain(func() {
		a.Poll(false)
		for b.Loop() {
			a.Poll(false)
		}
	})
}

// BenchmarkGamepadsNone reads the controllers with none connected, which
// is what every frame of a game without a pad costs.
func BenchmarkGamepadsNone(b *testing.B) {
	a := benchApp(b)
	var n int
	onMain(func() { n = len(a.Gamepads()) })
	if n != 0 {
		b.Skipf("%d controllers connected", n)
	}
	b.ReportAllocs()
	onMain(func() {
		for b.Loop() {
			a.Gamepads()
		}
	})
}

// BenchmarkReadPad reads one controller's extended profile from fakePad,
// which answers every selector with native NSNumber methods, so the
// figure is the cost of the message sends and not of the stand-in.
func BenchmarkReadPad(b *testing.B) {
	a := benchApp(b)
	pad := fakePad(b)
	var st GamepadState
	var ok bool
	onMain(func() { st, ok = a.readPad(pad) })
	if !ok || !st.Connected || st.Axes[0] != 0.5 || !st.Info.Buttons[0] || !st.Buttons[0] {
		b.Fatalf("fake pad read as %+v, %v", st, ok)
	}
	b.ReportAllocs()
	onMain(func() {
		for b.Loop() {
			a.readPad(pad)
		}
	})
}

// TestReadPad checks every button and axis readPad maps from an extended
// gamepad, using the fake whose buttons all read pressed and whose axes
// all read 0.5.
func TestReadPad(t *testing.T) {
	a := benchApp(t)
	pad := fakePad(t)
	var st GamepadState
	var ok bool
	onMain(func() { st, ok = a.readPad(pad) })
	if !ok || !st.Connected || st.Name != "0.5" || st.Info.Name != "0.5" || st.Info.Backend != "gamecontroller" {
		t.Fatalf("read %+v, %v", st, ok)
	}
	for b, down := range st.Buttons {
		if !down || !st.Info.Buttons[b] {
			t.Errorf("button %d: down %v, present %v", b, down, st.Info.Buttons[b])
		}
	}
	for x, v := range st.Axes {
		if v != 0.5 || !st.Info.Axes[x] {
			t.Errorf("axis %d: %v, present %v", x, v, st.Info.Axes[x])
		}
	}
}

// BenchmarkMouseMove translates one mouse-moved NSEvent aimed at a window
// that exists but is never put on screen.
func BenchmarkMouseMove(b *testing.B) {
	a := benchApp(b)
	var w *Window
	var ev objc.ID
	var got []Event
	onMain(func() {
		w = offscreenWindow(a)
		ev = mouseEvent(w, nsEventTypeMouseMoved, 50, 40)
		a.pending = a.pending[:0]
		a.handleEvent(ev)
		got = append(got, a.pending...)
	})
	defer onMain(func() { closeOffscreen(a, w) })
	if len(got) != 1 || got[0].Kind != EventMouseMove || got[0].Window != w {
		b.Skipf("the event did not reach the offscreen window: %+v", got)
	}
	b.ReportAllocs()
	onMain(func() {
		for b.Loop() {
			a.pending = a.pending[:0]
			a.handleEvent(ev)
		}
	})
}

// TestMouseMoveTranslation checks the position a mouse-moved event
// carries: points from the top-left of the content view.
func TestMouseMoveTranslation(t *testing.T) {
	a := benchApp(t)
	var w *Window
	var got []Event
	onMain(func() {
		w = offscreenWindow(a)
		a.pending = a.pending[:0]
		a.handleEvent(mouseEvent(w, nsEventTypeMouseMoved, 50, 40))
		got = append(got, a.pending...)
	})
	defer onMain(func() { closeOffscreen(a, w) })
	if len(got) != 1 {
		t.Skipf("the event did not reach the offscreen window: %+v", got)
	}
	if e := got[0]; e.Kind != EventMouseMove || e.Window != w || e.X != 50 || e.Y != float64(w.height)-40 {
		t.Errorf("mouse move = %+v, want (50, %d) on the window", e, w.height-40)
	}
}

// TestPollTimeout checks that a timed poll with nothing to deliver
// returns when its time is up, and that a Wake ends it early.
func TestPollTimeout(t *testing.T) {
	a := benchApp(t)
	const limit = 50 * time.Millisecond
	quiet := false
	for range 5 { // the first polls may pick up the process's own events
		var n int
		var took time.Duration
		onMain(func() {
			start := time.Now()
			n = len(a.PollTimeout(limit))
			took = time.Since(start)
		})
		if n == 0 {
			if took < limit-5*time.Millisecond || took > limit+time.Second {
				t.Errorf("an empty PollTimeout(%v) took %v", limit, took)
			}
			quiet = true
			break
		}
	}
	if !quiet {
		t.Fatal("no poll came back empty")
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		a.Wake()
	}()
	var woke bool
	var took time.Duration
	onMain(func() {
		start := time.Now()
		for !woke && time.Since(start) < 5*time.Second {
			for _, e := range a.PollTimeout(10 * time.Second) {
				woke = woke || e.Kind == EventWake
			}
		}
		took = time.Since(start)
	})
	if !woke || took > 2*time.Second {
		t.Errorf("Wake ended a 10 s PollTimeout: %v, after %v", woke, took)
	}
}

// TestMsgSendShapes calls each registered send shape against AppKit and
// checks what comes back, including the NSRect result that goes through
// objc_msgSend_stret on amd64.
func TestMsgSendShapes(t *testing.T) {
	a := benchApp(t)
	var (
		bounds       nsRect
		scale        float64
		point, local nsPoint
		hit, date    objc.ID
		hidden, kind bool
		count        uint
	)
	onMain(func() {
		pool := poolPush()
		defer poolPop(pool)
		w := offscreenWindow(a)
		defer closeOffscreen(a, w)
		ev := mouseEvent(w, nsEventTypeMouseMoved, 30, 20)
		bounds = msgSendRect(w.view, a.c.sel.bounds)
		scale = msgSendF64(w.nsWindow, a.c.sel.backingScaleFactor)
		point = msgSendPoint(ev, a.c.sel.locationInWindow)
		local = msgSendPointID(w.view, selConvertPointFromView, point, 0)
		hit = msgSendIDPoint(sendID(w.nsWindow, selContentView), selHitTest, nsPoint{X: 10, Y: 10})
		date = msgSendIDF64(objc.ID(a.c.NSDate), selDateSinceNow, 1)
		hidden = sendBool(w.view, selIsHiddenOrHasHiddenAncestor)
		kind = sendBool1(w.view, selIsKindOfClass, uintptr(a.c.NSView))
		count = sendUint(objc.ID(objc.GetClass("NSArray")).Send(objc.RegisterName("array")), a.c.sel.count)
	})
	if bounds.Size.Width != 200 || bounds.Size.Height != 100 || bounds.Origin != (nsPoint{}) {
		t.Errorf("bounds = %+v, want 200 by 100 at the origin", bounds)
	}
	if scale < 1 {
		t.Errorf("backingScaleFactor = %v", scale)
	}
	if point != (nsPoint{X: 30, Y: 20}) || local != point {
		t.Errorf("location %+v, in the view %+v; want (30, 20) for both", point, local)
	}
	if hit == 0 || date == 0 {
		t.Errorf("hitTest: %#x, dateWithTimeIntervalSinceNow: %#x", hit, date)
	}
	if hidden || !kind || count != 0 {
		t.Errorf("hidden %v, isKindOfClass %v, empty count %d", hidden, kind, count)
	}
}

// offscreenWindow builds a Window over an NSWindow that is created with
// its window device but never ordered in, so events can name it and its
// content view can convert points while nothing appears on screen. It
// runs on the main thread.
func offscreenWindow(a *App) *Window {
	c := a.c
	rect := nsRect{Size: nsSize{Width: 200, Height: 100}}
	win := objc.ID(c.NSWindow).Send(c.sel.alloc).Send(c.sel.initWithContentRect, rect,
		uint64(nsWindowStyleMaskTitled), nsBackingStoreBuffered, false)
	w := &Window{app: a, nsWindow: win, visible: true, width: 200, height: 100}
	w.view = objc.ID(a.view).Send(c.sel.alloc).Send(c.sel.initWithFrame, rect)
	win.Send(c.sel.setContentView, w.view)
	a.windows[win] = w
	a.views[w.view] = w
	return w
}

// closeOffscreen undoes offscreenWindow on the main thread.
func closeOffscreen(a *App, w *Window) {
	delete(a.windows, w.nsWindow)
	delete(a.views, w.view)
	w.view.Send(a.c.sel.release)
	w.nsWindow.Send(a.c.sel.close)
}

// mouseEvent makes a retained NSEvent of the given type at (x, y) in the
// window's base coordinates, which start at the bottom-left.
func mouseEvent(w *Window, kind uint, x, y float64) objc.ID {
	sel := objc.RegisterName("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:")
	number := objc.Send[int](w.nsWindow, objc.RegisterName("windowNumber"))
	ev := objc.ID(objc.GetClass("NSEvent")).Send(sel, kind, nsPoint{X: x, Y: y}, uint(0), float64(0), number,
		objc.ID(0), int(0), int(0), float32(0))
	return ev.Send(objc.RegisterName("retain"))
}

// fakePad returns an object that answers every selector readPad sends:
// an NSNumber holding 0.5, given the controller selectors as extra
// methods that reuse native implementations. The element accessors
// return the object itself (-[NSObject self]), isPressed is boolValue,
// value is floatValue and vendorName is stringValue, so no Go callback
// runs during a read.
func fakePad(tb testing.TB) objc.ID {
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		tb.Skip(err)
	}
	var getIMP func(objc.Class, objc.SEL) uintptr
	var addMethod func(objc.Class, objc.SEL, uintptr, string) bool
	purego.RegisterLibFunc(&getIMP, lib, "class_getMethodImplementation")
	purego.RegisterLibFunc(&addMethod, lib, "class_addMethod")
	num := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithFloat:"), float32(0.5))
	num = num.Send(objc.RegisterName("retain"))
	cls := num.Class()
	self := getIMP(objc.GetClass("NSObject"), objc.RegisterName("self"))
	for _, name := range []string{"extendedGamepad", "buttonA", "buttonB", "buttonX", "buttonY",
		"leftShoulder", "rightShoulder", "leftThumbstickButton", "rightThumbstickButton",
		"buttonMenu", "buttonOptions", "buttonHome", "dpad", "up", "down", "left", "right",
		"leftThumbstick", "rightThumbstick", "xAxis", "yAxis", "leftTrigger", "rightTrigger"} {
		addMethod(cls, objc.RegisterName(name), self, "@@:")
	}
	addMethod(cls, objc.RegisterName("isPressed"), getIMP(cls, objc.RegisterName("boolValue")), "c@:")
	addMethod(cls, objc.RegisterName("value"), getIMP(cls, objc.RegisterName("floatValue")), "f@:")
	addMethod(cls, objc.RegisterName("vendorName"), getIMP(cls, objc.RegisterName("stringValue")), "@@:")
	return num
}

// mallocStats mirrors malloc_statistics_t.
type mallocStats struct {
	_                                    structs.HostLayout
	blocksInUse                          uint32
	sizeInUse, maxSizeInUse, sizeAlloced uintptr
}

// mallocStatistics is malloc_zone_statistics from libSystem.
func mallocStatistics(tb testing.TB) func(zone uintptr, out *mallocStats) {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		tb.Skip(err)
	}
	var stats func(zone uintptr, out *mallocStats)
	purego.RegisterLibFunc(&stats, lib, "malloc_zone_statistics")
	return stats
}

// TestGamepadsReleaseAutoreleased checks that reading the controllers
// frees what GameController.framework hands back. The loop reads them
// after Poll has drained its autorelease pool, so without a pool of its
// own every +[GCController controllers] array stays for the life of the
// process: about 55 bytes a frame, 12 MB an hour at 60 Hz, whether or not
// a pad is connected. The C allocator's bytes in use, summed over every
// zone, count what Objective-C objects hold and nothing of Go's heap.
func TestGamepadsReleaseAutoreleased(t *testing.T) {
	a := benchApp(t)
	if a.c.GCController == 0 {
		t.Skip("GameController.framework is unavailable")
	}
	stats := mallocStatistics(t)
	const calls = 100_000
	var before, after mallocStats
	onMain(func() {
		for range 1000 {
			a.Gamepads()
		}
		stats(0, &before)
		for range calls {
			a.Gamepads()
		}
		stats(0, &after)
	})
	grew := int64(after.sizeInUse) - int64(before.sizeInUse)
	t.Logf("malloc in use grew %d bytes over %d calls", grew, calls)
	// Leaking one empty array a call is over 5 MB here; a pooled read
	// leaves allocator noise measured in kilobytes.
	if grew > 1<<20 {
		t.Errorf("malloc in use grew %d bytes over %d Gamepads calls; the autoreleased results are leaking", grew, calls)
	}
}
