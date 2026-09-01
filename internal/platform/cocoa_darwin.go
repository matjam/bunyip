package platform

import (
	"fmt"
	"structs"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Geometry types passed to and from AppKit by value.
type nsPoint struct {
	_    structs.HostLayout
	X, Y float64
}

type nsSize struct {
	_             structs.HostLayout
	Width, Height float64
}

type nsRect struct {
	_      structs.HostLayout
	Origin nsPoint
	Size   nsSize
}

// AppKit constants used by the window layer.
const (
	nsWindowStyleMaskTitled              = 1 << 0
	nsWindowStyleMaskClosable            = 1 << 1
	nsWindowStyleMaskMiniaturizable      = 1 << 2
	nsWindowStyleMaskResizable           = 1 << 3
	nsBackingStoreBuffered               = 2
	nsApplicationActivationPolicyRegular = 0

	nsEventTypeLeftMouseDown     = 1
	nsEventTypeLeftMouseUp       = 2
	nsEventTypeRightMouseDown    = 3
	nsEventTypeRightMouseUp      = 4
	nsEventTypeMouseMoved        = 5
	nsEventTypeLeftMouseDragged  = 6
	nsEventTypeRightMouseDragged = 7
	nsEventTypeMouseEntered      = 8
	nsEventTypeMouseExited       = 9
	nsEventTypeKeyDown           = 10
	nsEventTypeKeyUp             = 11
	nsEventTypeFlagsChanged      = 12
	nsEventTypeScrollWheel       = 22
	nsEventTypeOtherMouseDown    = 25
	nsEventTypeOtherMouseUp      = 26
	nsEventTypeOtherMouseDragged = 27
	nsEventMaskAny               = ^uint64(0)

	nsEventModifierFlagCapsLock = 1 << 16
	nsEventModifierFlagShift    = 1 << 17
	nsEventModifierFlagControl  = 1 << 18
	nsEventModifierFlagOption   = 1 << 19
	nsEventModifierFlagCommand  = 1 << 20
)

// cocoa holds the classes and selectors resolved after the frameworks load.
type cocoa struct {
	NSApplication, NSWindow, NSView, NSString, NSDate, NSAutoreleasePool, CAMetalLayer objc.Class

	sel struct {
		sharedApplication, setActivationPolicy, finishLaunching, activateIgnoringOtherApps,
		nextEvent, sendEvent, updateWindows, distantPast, distantFuture,
		alloc, init, new, release, drain, stringWithUTF8String, UTF8String,
		initWithContentRect, setTitle, center, makeKeyAndOrderFront, setDelegate, setContentView,
		setAcceptsMouseMovedEvents, setRestorable, close, contentView, backingScaleFactor, frame, bounds,
		initWithFrame, setWantsLayer, setLayer, layer, setContentsScale, setDrawableSize,
		eventType, keyCode, modifierFlags, isARepeat, characters, locationInWindow, buttonNumber,
		scrollingDeltaX, scrollingDeltaY, hasPreciseScrollingDeltas, window, object objc.SEL
	}
	defaultRunLoopMode objc.ID
}

// loadCocoa opens AppKit and QuartzCore and resolves everything the window
// layer calls. It runs once, from the first App.
func loadCocoa() (*cocoa, error) {
	for _, fw := range []string{
		"/System/Library/Frameworks/Cocoa.framework/Cocoa",
		"/System/Library/Frameworks/QuartzCore.framework/QuartzCore",
	} {
		if _, err := purego.Dlopen(fw, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return nil, fmt.Errorf("platform: load %s: %w", fw, err)
		}
	}
	foundation, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("platform: load Foundation: %w", err)
	}
	modeSym, err := purego.Dlsym(foundation, "NSDefaultRunLoopMode")
	if err != nil {
		return nil, fmt.Errorf("platform: NSDefaultRunLoopMode: %w", err)
	}
	c := &cocoa{}
	for name, dst := range map[string]*objc.Class{
		"NSApplication": &c.NSApplication, "NSWindow": &c.NSWindow, "NSView": &c.NSView,
		"NSString": &c.NSString, "NSDate": &c.NSDate, "NSAutoreleasePool": &c.NSAutoreleasePool,
		"CAMetalLayer": &c.CAMetalLayer,
	} {
		*dst = objc.GetClass(name)
		if *dst == 0 {
			return nil, fmt.Errorf("platform: class %s not found", name)
		}
	}
	// The symbol is the address of an NSString* global; read the pointer it holds.
	c.defaultRunLoopMode = **(**objc.ID)(unsafe.Pointer(&modeSym))
	s := &c.sel
	for name, dst := range map[string]*objc.SEL{
		"sharedApplication": &s.sharedApplication, "setActivationPolicy:": &s.setActivationPolicy,
		"finishLaunching": &s.finishLaunching, "activateIgnoringOtherApps:": &s.activateIgnoringOtherApps,
		"nextEventMatchingMask:untilDate:inMode:dequeue:": &s.nextEvent, "sendEvent:": &s.sendEvent,
		"updateWindows": &s.updateWindows, "distantPast": &s.distantPast, "distantFuture": &s.distantFuture,
		"alloc": &s.alloc, "init": &s.init, "new": &s.new, "release": &s.release, "drain": &s.drain,
		"stringWithUTF8String:": &s.stringWithUTF8String, "UTF8String": &s.UTF8String,
		"initWithContentRect:styleMask:backing:defer:": &s.initWithContentRect, "setTitle:": &s.setTitle,
		"center": &s.center, "makeKeyAndOrderFront:": &s.makeKeyAndOrderFront, "setDelegate:": &s.setDelegate,
		"setContentView:": &s.setContentView, "setAcceptsMouseMovedEvents:": &s.setAcceptsMouseMovedEvents,
		"setRestorable:": &s.setRestorable, "close": &s.close, "contentView": &s.contentView,
		"backingScaleFactor": &s.backingScaleFactor, "frame": &s.frame, "bounds": &s.bounds,
		"initWithFrame:": &s.initWithFrame, "setWantsLayer:": &s.setWantsLayer, "setLayer:": &s.setLayer,
		"layer": &s.layer, "setContentsScale:": &s.setContentsScale, "setDrawableSize:": &s.setDrawableSize,
		"type": &s.eventType, "keyCode": &s.keyCode, "modifierFlags": &s.modifierFlags, "isARepeat": &s.isARepeat,
		"characters": &s.characters, "locationInWindow": &s.locationInWindow, "buttonNumber": &s.buttonNumber,
		"scrollingDeltaX": &s.scrollingDeltaX, "scrollingDeltaY": &s.scrollingDeltaY,
		"hasPreciseScrollingDeltas": &s.hasPreciseScrollingDeltas, "window": &s.window, "object": &s.object,
	} {
		*dst = objc.RegisterName(name)
	}
	return c, nil
}

func (c *cocoa) nsString(s string) objc.ID {
	return objc.ID(c.NSString).Send(c.sel.stringWithUTF8String, s)
}
