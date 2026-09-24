package platform

import (
	"fmt"
	"runtime"
	"time"

	"github.com/ebitengine/purego/objc"
)

func init() {
	// AppKit delivers events only to the thread that created NSApplication.
	runtime.LockOSThread()
}

// App is the connection to the window system. Create one per process.
type App struct {
	c           *cocoa
	nsApp       objc.ID
	delegate    objc.Class
	view        objc.Class // BunyipView: NSView plus NSTextInputClient
	tsel        textSel
	windows     map[objc.ID]*Window
	pending     []Event
	mods        Mods
	standalone  bool
	hosted      bool
	views       map[objc.ID]*Window
	mouseTarget *Window
}

// NewApp initialises AppKit for a windowed application.
func NewApp() (*App, error) {
	c, err := loadCocoa()
	if err != nil {
		return nil, err
	}
	a := &App{c: c, windows: map[objc.ID]*Window{}, views: map[objc.ID]*Window{}}
	a.nsApp = objc.ID(c.NSApplication).Send(c.sel.sharedApplication)
	if a.delegate, err = a.registerDelegateClass(); err != nil {
		return nil, err
	}
	if a.view, err = a.registerViewClass(); err != nil {
		return nil, err
	}
	return a, nil
}

// Poll drains the event queue into the returned slice, which is reused by
// the next call. With wait set it blocks until at least one event arrives,
// which is how a turn-based game idles without spinning.
func (a *App) Poll(wait bool) []Event {
	pool := poolPush()
	defer poolPop(pool)
	until := sendID(objc.ID(a.c.NSDate), a.c.sel.distantPast)
	if wait {
		until = sendID(objc.ID(a.c.NSDate), a.c.sel.distantFuture)
	}
	return a.poll(until)
}

// PollTimeout is Poll that waits at most timeout for the first event. A
// timeout of zero or less waits for nothing, as Poll(false) does.
func (a *App) PollTimeout(timeout time.Duration) []Event {
	if timeout <= 0 {
		return a.Poll(false)
	}
	pool := poolPush()
	defer poolPop(pool)
	return a.poll(msgSendIDF64(objc.ID(a.c.NSDate), selDateSinceNow, timeout.Seconds()))
}

// poll drains the queue, waiting until the date until for the first
// event. It runs inside an autorelease pool, which releases the events
// and everything the handlers made while reading them.
func (a *App) poll(until objc.ID) []Event {
	a.pending = a.pending[:0]
	c := a.c
	for {
		ev := objc.ID(call(msgSendAddr, 6, uintptr(a.nsApp), uintptr(c.sel.nextEvent),
			uintptr(nsEventMaskAny), uintptr(until), uintptr(c.defaultRunLoopMode), 1))
		if ev == 0 {
			break
		}
		until = sendID(objc.ID(c.NSDate), c.sel.distantPast)
		if a.handleEvent(ev) {
			sendID1(a.nsApp, c.sel.sendEvent, uintptr(ev))
		}
	}
	sendID(a.nsApp, c.sel.updateWindows)
	a.syncEmbedded()
	return a.pending
}

func (a *App) push(e Event) {
	a.pending = append(a.pending, e)
}

// registerDelegateClass creates the NSWindowDelegate subclass whose methods
// route window notifications back to the owning Window.
func (a *App) registerDelegateClass() (objc.Class, error) {
	c := a.c
	windowOf := func(notification objc.ID) *Window {
		return a.windows[notification.Send(c.sel.object)]
	}
	cls, err := objc.RegisterClass("BunyipWindowDelegate", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("NSWindowDelegate")}, nil,
		[]objc.MethodDef{
			{Cmd: objc.RegisterName("windowShouldClose:"), Fn: func(_ objc.ID, _ objc.SEL, sender objc.ID) bool {
				if w := a.windows[sender]; w != nil {
					a.push(Event{Kind: EventClose, Window: w})
				}
				return false // the game decides when to close
			}},
			{Cmd: objc.RegisterName("windowDidResize:"), Fn: func(_ objc.ID, _ objc.SEL, n objc.ID) {
				if w := windowOf(n); w != nil {
					w.updateGeometry()
				}
			}},
			{Cmd: objc.RegisterName("windowDidChangeBackingProperties:"), Fn: func(_ objc.ID, _ objc.SEL, n objc.ID) {
				if w := windowOf(n); w != nil {
					w.updateGeometry()
				}
			}},
			{Cmd: objc.RegisterName("windowDidBecomeKey:"), Fn: func(_ objc.ID, _ objc.SEL, n objc.ID) {
				if w := windowOf(n); w != nil {
					a.push(Event{Kind: EventFocus, Window: w, Focused: true})
				}
			}},
			{Cmd: objc.RegisterName("windowDidResignKey:"), Fn: func(_ objc.ID, _ objc.SEL, n objc.ID) {
				if w := windowOf(n); w != nil {
					a.push(Event{Kind: EventFocus, Window: w, Focused: false})
				}
			}},
			{Cmd: objc.RegisterName("windowDidMiniaturize:"), Fn: func(_ objc.ID, _ objc.SEL, n objc.ID) {
				if w := windowOf(n); w != nil {
					w.miniaturized = true
					w.updateVisible()
				}
			}},
			{Cmd: objc.RegisterName("windowDidDeminiaturize:"), Fn: func(_ objc.ID, _ objc.SEL, n objc.ID) {
				if w := windowOf(n); w != nil {
					w.miniaturized = false
					w.updateVisible()
				}
			}},
			{Cmd: objc.RegisterName("windowDidChangeOcclusionState:"), Fn: func(_ objc.ID, _ objc.SEL, n objc.ID) {
				if w := windowOf(n); w != nil {
					w.occluded = objc.Send[uint64](w.nsWindow, selOcclusionState)&nsWindowOcclusionStateVisible == 0
					w.updateVisible()
				}
			}},
		})
	if err != nil {
		return 0, fmt.Errorf("platform: register window delegate: %w", err)
	}
	return cls, nil
}
