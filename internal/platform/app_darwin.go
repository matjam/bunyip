package platform

import (
	"fmt"
	"runtime"

	"github.com/ebitengine/purego/objc"
)

func init() {
	// AppKit delivers events only to the thread that created NSApplication.
	runtime.LockOSThread()
}

// App is the connection to the window system. Create one per process.
type App struct {
	c        *cocoa
	nsApp    objc.ID
	delegate objc.Class
	view     objc.Class // BunyipView: NSView plus NSTextInputClient
	tsel     textSel
	windows  map[objc.ID]*Window
	pending  []Event
	mods     Mods
}

// NewApp initialises AppKit for a windowed application.
func NewApp() (*App, error) {
	c, err := loadCocoa()
	if err != nil {
		return nil, err
	}
	a := &App{c: c, windows: map[objc.ID]*Window{}}
	a.nsApp = objc.ID(c.NSApplication).Send(c.sel.sharedApplication)
	a.nsApp.Send(c.sel.setActivationPolicy, nsApplicationActivationPolicyRegular)
	if a.delegate, err = a.registerDelegateClass(); err != nil {
		return nil, err
	}
	if a.view, err = a.registerViewClass(); err != nil {
		return nil, err
	}
	a.nsApp.Send(c.sel.finishLaunching)
	a.nsApp.Send(c.sel.activateIgnoringOtherApps, true)
	return a, nil
}

// Poll drains the event queue into the returned slice, which is reused by
// the next call. With wait set it blocks until at least one event arrives,
// which is how a turn-based game idles without spinning.
func (a *App) Poll(wait bool) []Event {
	a.pending = a.pending[:0]
	c := a.c
	pool := objc.ID(c.NSAutoreleasePool).Send(c.sel.new)
	defer pool.Send(c.sel.drain)
	until := objc.ID(c.NSDate).Send(c.sel.distantPast)
	if wait {
		until = objc.ID(c.NSDate).Send(c.sel.distantFuture)
	}
	for {
		ev := a.nsApp.Send(c.sel.nextEvent, nsEventMaskAny, until, c.defaultRunLoopMode, true)
		if ev == 0 {
			break
		}
		until = objc.ID(c.NSDate).Send(c.sel.distantPast)
		if a.handleEvent(ev) {
			a.nsApp.Send(c.sel.sendEvent, ev)
		}
	}
	a.nsApp.Send(c.sel.updateWindows)
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
		})
	if err != nil {
		return 0, fmt.Errorf("platform: register window delegate: %w", err)
	}
	return cls, nil
}
