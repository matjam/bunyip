// Package platform owns the operating system: windows, the event queue,
// keyboard and mouse input, display scaling and Vulkan surface creation.
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

import "github.com/matjam/bunyip/input"

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
	EventMouseMove            // X, Y in points from the top-left of the content area
	EventMouseDown            // Button, X, Y, Mods
	EventMouseUp              // Button, X, Y, Mods
	EventScroll               // DX, DY in lines (or points when Precise)
	EventMouseEnter           // the pointer entered the content area
	EventMouseLeave           // the pointer left the content area
)

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
	Button  MouseButton
	X, Y    float64
	DX, DY  float64
	Precise bool
	Focused bool
	Width   int // content size in points
	Height  int
	PixelW  int // framebuffer size in pixels
	PixelH  int
	Scale   float64
}

var eventNames = [...]string{
	EventNone: "None", EventClose: "Close", EventResize: "Resize", EventFocus: "Focus",
	EventKeyDown: "KeyDown", EventKeyUp: "KeyUp", EventChar: "Char", EventMouseMove: "MouseMove",
	EventMouseDown: "MouseDown", EventMouseUp: "MouseUp", EventScroll: "Scroll",
	EventMouseEnter: "MouseEnter", EventMouseLeave: "MouseLeave",
}

func (k EventKind) String() string {
	if int(k) < len(eventNames) {
		return eventNames[k]
	}
	return "EventKind(?)"
}
