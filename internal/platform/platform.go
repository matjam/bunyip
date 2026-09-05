// Package platform owns the operating system: windows, the event queue,
// keyboard and mouse input, display scaling, window visibility, the
// clipboard and Vulkan surface creation.
//
// Each operating system has its own implementation of App and Window in a
// build-tagged file with an identical method set, so the rest of the engine
// compiles against one API and the linker picks the platform. Events are
// translated into the platform-independent Event type here; nothing above
// this package sees a native handle.
//
// All calls must happen on the main goroutine, which the package pins to the
// main OS thread in init, because window systems require it.
package platform

import (
	"errors"
	"runtime"

	"github.com/matjam/bunyip/input"
)

// backendName is the window system in use. It starts as the operating
// system's name and NewApp replaces it where the platform has more than one
// window system to choose between, which today is Linux.
var backendName = runtime.GOOS

// Backend names the window system the layer opened: "wayland" or "x11" on
// Linux, and the operating system's name elsewhere. The engine logs it at
// startup, because which one a Linux session got is not otherwise visible.
func Backend() string { return backendName }

// Config describes a window to open.
type Config struct {
	Title     string
	Width     int // content size in points (logical pixels)
	Height    int
	Resizable bool
}

// EventKind says what an Event carries.
type EventKind uint8

const (
	EventNone       EventKind = iota
	EventClose                // the user asked the window to close
	EventResize               // content or framebuffer size, or scale, changed
	EventFocus                // Focused says whether the window gained or lost focus
	EventKeyDown              // Key, Mods, Repeat
	EventKeyUp                // Key, Mods
	EventChar                 // Rune: a character of text input
	EventCompose              // Text: the input method's in-progress composition, empty when it ends
	EventMouseMove            // X, Y in points from the top-left of the content area
	EventMouseDown            // Button, X, Y, Mods
	EventMouseUp              // Button, X, Y, Mods
	EventScroll               // DX, DY in lines (or points when Precise)
	EventMouseEnter           // the pointer entered the content area
	EventMouseLeave           // the pointer left the content area
	EventWake                 // App.Wake was called from another goroutine
	EventVisible              // Visible says whether the window became visible or hidden
)

// CursorShape names a system cursor.
type CursorShape uint8

const (
	CursorArrow CursorShape = iota
	CursorHand
	CursorIBeam
	CursorCrosshair
	CursorResizeH
	CursorResizeV
	CursorGrab
	CursorGrabbing
	CursorNotAllowed
	cursorShapeCount
)

// ErrNoClipboard is returned where the platform layer has no clipboard.
var ErrNoClipboard = errors.New("platform: clipboard is not available on this platform")

// GamepadState is one controller's inputs as read this poll.
type GamepadState struct {
	Connected bool
	Name      string
	Buttons   [input.GamepadButtonCount]bool
	Axes      [input.GamepadAxisCount]float32
}

// Event is one thing that happened to a window. Only the fields relevant to
// Kind are set.
type Event struct {
	Kind    EventKind
	Window  *Window
	Key     Key
	Mods    Mods
	Repeat  bool
	Rune    rune
	Text    string
	Button  MouseButton
	X, Y    float64
	DX, DY  float64
	Precise bool
	Focused bool
	Visible bool
	Width   int // content size in points
	Height  int
	PixelW  int // framebuffer size in pixels
	PixelH  int
	Scale   float64
}

var eventNames = [...]string{
	EventNone: "None", EventClose: "Close", EventResize: "Resize", EventFocus: "Focus",
	EventKeyDown: "KeyDown", EventKeyUp: "KeyUp", EventChar: "Char", EventCompose: "Compose", EventMouseMove: "MouseMove",
	EventMouseDown: "MouseDown", EventMouseUp: "MouseUp", EventScroll: "Scroll",
	EventMouseEnter: "MouseEnter", EventMouseLeave: "MouseLeave", EventWake: "Wake",
	EventVisible: "Visible",
}

func (k EventKind) String() string {
	if int(k) < len(eventNames) {
		return eventNames[k]
	}
	return "EventKind(?)"
}
