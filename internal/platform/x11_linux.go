package platform

import (
	"errors"
	"fmt"
	"runtime"
	"structs"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/matjam/bunyip/input"
)

// The Linux window layer speaks X11 through libxcb, which every desktop
// provides (Wayland sessions through XWayland). Text input comes from
// xkbcommon so layouts and dead keys behave; without it, only key codes
// are reported.

// xcb protocol constants.
const (
	xcbKeyPress        = 2
	xcbKeyRelease      = 3
	xcbButtonPress     = 4
	xcbButtonRelease   = 5
	xcbMotionNotify    = 6
	xcbEnterNotify     = 7
	xcbLeaveNotify     = 8
	xcbFocusIn         = 9
	xcbFocusOut        = 10
	xcbDestroyNotify   = 17
	xcbConfigureNotify = 22
	xcbClientMessage   = 33
	xcbMappingNotify   = 34

	xcbEventMaskKeyPress      = 1
	xcbEventMaskKeyRelease    = 2
	xcbEventMaskButtonPress   = 4
	xcbEventMaskButtonRelease = 8
	xcbEventMaskEnter         = 0x10
	xcbEventMaskLeave         = 0x20
	xcbEventMaskMotion        = 0x40
	xcbEventMaskExposure      = 0x8000
	xcbEventMaskStructure     = 0x20000
	xcbEventMaskSubRedirect   = 0x100000
	xcbEventMaskSubNotify     = 0x80000
	xcbEventMaskFocus         = 0x200000

	xcbCWBackPixel            = 0x2
	xcbCWEventMask            = 0x800
	xcbCWCursor               = 0x4000
	xcbCopyFromParent         = 0
	xcbWindowClassInputOutput = 1
	xcbPropModeReplace        = 0
	xcbAtomWMName             = 39
	xcbAtomString             = 31
	xcbAtomAtom               = 4
	xcbGrabModeAsync          = 1
	xcbCurrentTime            = 0

	xcbModShift   = 1
	xcbModLock    = 2
	xcbModControl = 4
	xcbModAlt     = 8
	xcbModNumLock = 0x10
	xcbModSuper   = 0x40
)

type xcbCookie struct {
	_        structs.HostLayout
	Sequence uint32
}

type xcbGenericEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Pad          uint8
	Sequence     uint16
	Pad1         [7]uint32
	FullSequence uint32
}

// Key, button, motion, enter and leave events share this layout.
type xcbInputEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Detail       uint8
	Sequence     uint16
	Time         uint32
	Root         uint32
	Event        uint32
	Child        uint32
	RootX        int16
	RootY        int16
	EventX       int16
	EventY       int16
	State        uint16
	SameScreen   uint8
	Pad          uint8
}

type xcbFocusEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Detail       uint8
	Sequence     uint16
	Event        uint32
	Mode         uint8
	Pad          [3]uint8
}

type xcbConfigureEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Pad          uint8
	Sequence     uint16
	Event        uint32
	Window       uint32
	AboveSibling uint32
	X            int16
	Y            int16
	Width        uint16
	Height       uint16
	BorderWidth  uint16
	Override     uint8
	Pad1         uint8
}

type xcbClientMessageEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Format       uint8
	Sequence     uint16
	Window       uint32
	Type         uint32
	Data         [5]uint32
}

type xcbInternAtomReply struct {
	_            structs.HostLayout
	ResponseType uint8
	Pad          uint8
	Sequence     uint16
	Length       uint32
	Atom         uint32
}

type xcbScreen struct {
	_                  structs.HostLayout
	Root               uint32
	DefaultColormap    uint32
	WhitePixel         uint32
	BlackPixel         uint32
	CurrentInputMasks  uint32
	WidthInPixels      uint16
	HeightInPixels     uint16
	WidthInMillimeters uint16
	HeightInMillimeter uint16
	MinInstalledMaps   uint16
	MaxInstalledMaps   uint16
	RootVisual         uint32
	BackingStores      uint8
	SaveUnders         uint8
	RootDepth          uint8
	AllowedDepthsLen   uint8
}

type xcbScreenIterator struct {
	_     structs.HostLayout
	Data  *xcbScreen
	Rem   int32
	Index int32
}

// xlib holds the functions resolved from libxcb and friends.
type xlib struct {
	connect            func(display *byte, screen *int32) unsafe.Pointer
	disconnect         func(c unsafe.Pointer)
	connectionHasError func(c unsafe.Pointer) int32
	getSetup           func(c unsafe.Pointer) unsafe.Pointer
	setupRootsIterator func(setup unsafe.Pointer) xcbScreenIterator
	generateID         func(c unsafe.Pointer) uint32
	createWindow       func(c unsafe.Pointer, depth uint8, wid, parent uint32, x, y int16, w, h, border uint16, class uint16, visual uint32, mask uint32, values *uint32) xcbCookie
	destroyWindow      func(c unsafe.Pointer, w uint32) xcbCookie
	mapWindow          func(c unsafe.Pointer, w uint32) xcbCookie
	flush              func(c unsafe.Pointer) int32
	pollForEvent       func(c unsafe.Pointer) *xcbGenericEvent
	waitForEvent       func(c unsafe.Pointer) *xcbGenericEvent
	internAtom         func(c unsafe.Pointer, onlyIfExists uint8, nameLen uint16, name *byte) xcbCookie
	internAtomReply    func(c unsafe.Pointer, cookie xcbCookie, err unsafe.Pointer) *xcbInternAtomReply
	changeProperty     func(c unsafe.Pointer, mode uint8, w uint32, property, typ uint32, format uint8, dataLen uint32, data unsafe.Pointer) xcbCookie
	sendEvent          func(c unsafe.Pointer, propagate uint8, dest uint32, mask uint32, event *byte) xcbCookie
	grabPointer        func(c unsafe.Pointer, ownerEvents uint8, grabWindow uint32, eventMask uint16, pointerMode, keyboardMode uint8, confineTo, cursor, time uint32) xcbCookie
	ungrabPointer      func(c unsafe.Pointer, time uint32) xcbCookie
	warpPointer        func(c unsafe.Pointer, src, dst uint32, srcX, srcY int16, srcW, srcH uint16, dstX, dstY int16) xcbCookie
	createPixmap       func(c unsafe.Pointer, depth uint8, pid, drawable uint32, w, h uint16) xcbCookie
	freePixmap         func(c unsafe.Pointer, pid uint32) xcbCookie
	createCursor       func(c unsafe.Pointer, cid, source, mask uint32, foreR, foreG, foreB, backR, backG, backB uint16, x, y uint16) xcbCookie
	changeWindowAttrs  func(c unsafe.Pointer, w uint32, mask uint32, values *uint32) xcbCookie
	free               func(p unsafe.Pointer)

	// xkbcommon, optional.
	xkbContextNew       func(flags int32) unsafe.Pointer
	xkbContextUnref     func(ctx unsafe.Pointer)
	xkbSetupExtension   func(c unsafe.Pointer, major, minor uint16, flags int32, maj, min *uint16, base, baseErr *uint8) int32
	xkbCoreDeviceID     func(c unsafe.Pointer) int32
	xkbKeymapFromDevice func(ctx, c unsafe.Pointer, device int32, flags int32) unsafe.Pointer
	xkbKeymapUnref      func(km unsafe.Pointer)
	xkbStateFromDevice  func(km, c unsafe.Pointer, device int32) unsafe.Pointer
	xkbStateUnref       func(st unsafe.Pointer)
	xkbStateKeyGetUTF8  func(st unsafe.Pointer, key uint32, buf *byte, size uintptr) int32
	xkbStateUpdateKey   func(st unsafe.Pointer, key uint32, direction int32) int32
}

func load(lib uintptr, name string, fptr any) error {
	sym, err := purego.Dlsym(lib, name)
	if err != nil {
		return fmt.Errorf("platform: %s: %w", name, err)
	}
	purego.RegisterFunc(fptr, sym)
	return nil
}

func loadX11() (*xlib, error) {
	xcb, err := purego.Dlopen("libxcb.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("platform: libxcb: %w", err)
	}
	libc, err := purego.Dlopen("libc.so.6", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("platform: libc: %w", err)
	}
	x := &xlib{}
	for name, fptr := range map[string]any{
		"xcb_connect": &x.connect, "xcb_disconnect": &x.disconnect, "xcb_connection_has_error": &x.connectionHasError,
		"xcb_get_setup": &x.getSetup, "xcb_setup_roots_iterator": &x.setupRootsIterator, "xcb_generate_id": &x.generateID,
		"xcb_create_window": &x.createWindow, "xcb_destroy_window": &x.destroyWindow, "xcb_map_window": &x.mapWindow,
		"xcb_flush": &x.flush, "xcb_poll_for_event": &x.pollForEvent, "xcb_wait_for_event": &x.waitForEvent,
		"xcb_intern_atom": &x.internAtom, "xcb_intern_atom_reply": &x.internAtomReply, "xcb_change_property": &x.changeProperty,
		"xcb_send_event": &x.sendEvent, "xcb_grab_pointer": &x.grabPointer, "xcb_ungrab_pointer": &x.ungrabPointer,
		"xcb_warp_pointer": &x.warpPointer, "xcb_create_pixmap": &x.createPixmap, "xcb_free_pixmap": &x.freePixmap,
		"xcb_create_cursor": &x.createCursor, "xcb_change_window_attributes": &x.changeWindowAttrs,
	} {
		if err := load(xcb, name, fptr); err != nil {
			return nil, err
		}
	}
	if err := load(libc, "free", &x.free); err != nil {
		return nil, err
	}
	// Text input is optional.
	if xkb, err := purego.Dlopen("libxkbcommon.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
		if xkbx11, err := purego.Dlopen("libxkbcommon-x11.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			ok := true
			for name, fptr := range map[string]any{
				"xkb_context_new": &x.xkbContextNew, "xkb_context_unref": &x.xkbContextUnref, "xkb_keymap_unref": &x.xkbKeymapUnref,
				"xkb_state_unref": &x.xkbStateUnref, "xkb_state_key_get_utf8": &x.xkbStateKeyGetUTF8, "xkb_state_update_key": &x.xkbStateUpdateKey,
			} {
				ok = ok && load(xkb, name, fptr) == nil
			}
			for name, fptr := range map[string]any{
				"xkb_x11_setup_xkb_extension": &x.xkbSetupExtension, "xkb_x11_get_core_keyboard_device_id": &x.xkbCoreDeviceID,
				"xkb_x11_keymap_new_from_device": &x.xkbKeymapFromDevice, "xkb_x11_state_new_from_device": &x.xkbStateFromDevice,
			} {
				ok = ok && load(xkbx11, name, fptr) == nil
			}
			if !ok {
				x.xkbContextNew = nil
			}
		}
	}
	return x, nil
}

func init() {
	// The connection is used from one thread; Wake sends through xcb,
	// which is thread-safe.
	runtime.LockOSThread()
}

// ErrUnsupported is returned when no X server can be reached.
var ErrUnsupported = errors.New("platform: cannot connect to an X server (is DISPLAY set?)")

// App is the connection to the X server. Create one per process.
type App struct {
	x       *xlib
	conn    unsafe.Pointer
	screen  *xcbScreen
	windows map[uint32]*Window
	pending []Event
	mods    Mods
	mu      sync.Mutex

	atomWMProtocols, atomWMDelete, atomNetWMName, atomUTF8, atomNetWMState, atomNetWMFullscreen, atomWake uint32

	xkbCtx, xkbKeymap, xkbState unsafe.Pointer
	xkbDevice                   int32
}

// NewApp connects to the X server.
func NewApp() (*App, error) {
	x, err := loadX11()
	if err != nil {
		return nil, err
	}
	var screenNum int32
	conn := x.connect(nil, &screenNum)
	if conn == nil || x.connectionHasError(conn) != 0 {
		return nil, ErrUnsupported
	}
	it := x.setupRootsIterator(x.getSetup(conn))
	for i := int32(0); i < screenNum && it.Rem > 0; i++ {
		it.Data = (*xcbScreen)(unsafe.Add(unsafe.Pointer(it.Data), unsafe.Sizeof(xcbScreen{})))
		it.Rem--
	}
	a := &App{x: x, conn: conn, screen: it.Data, windows: map[uint32]*Window{}}
	a.atomWMProtocols = a.atom("WM_PROTOCOLS")
	a.atomWMDelete = a.atom("WM_DELETE_WINDOW")
	a.atomNetWMName = a.atom("_NET_WM_NAME")
	a.atomUTF8 = a.atom("UTF8_STRING")
	a.atomNetWMState = a.atom("_NET_WM_STATE")
	a.atomNetWMFullscreen = a.atom("_NET_WM_STATE_FULLSCREEN")
	a.atomWake = a.atom("BUNYIP_WAKE")
	a.setupXKB()
	return a, nil
}

func (a *App) atom(name string) uint32 {
	b := append([]byte(name), 0)
	cookie := a.x.internAtom(a.conn, 0, uint16(len(name)), &b[0])
	reply := a.x.internAtomReply(a.conn, cookie, nil)
	if reply == nil {
		return 0
	}
	atom := reply.Atom
	a.x.free(unsafe.Pointer(reply))
	return atom
}

func (a *App) setupXKB() {
	x := a.x
	if x.xkbContextNew == nil {
		return
	}
	if x.xkbSetupExtension(a.conn, 1, 0, 0, nil, nil, nil, nil) == 0 {
		return
	}
	a.xkbCtx = x.xkbContextNew(0)
	a.xkbDevice = x.xkbCoreDeviceID(a.conn)
	a.refreshKeymap()
}

func (a *App) refreshKeymap() {
	x := a.x
	if a.xkbCtx == nil || a.xkbDevice < 0 {
		return
	}
	if a.xkbState != nil {
		x.xkbStateUnref(a.xkbState)
	}
	if a.xkbKeymap != nil {
		x.xkbKeymapUnref(a.xkbKeymap)
	}
	a.xkbKeymap = x.xkbKeymapFromDevice(a.xkbCtx, a.conn, a.xkbDevice, 0)
	if a.xkbKeymap != nil {
		a.xkbState = x.xkbStateFromDevice(a.xkbKeymap, a.conn, a.xkbDevice)
	}
}

// Window is one X11 window.
type Window struct {
	app       *App
	id        uint32
	width     int
	height    int
	closed    bool
	captured  bool
	fullscr   bool
	cursor    uint32 // the invisible cursor while captured
	inputRect textRect
	mouseX    float64
	mouseY    float64
	warping   bool
}

type textRect struct{ X, Y, W, H float64 }

// NewWindow opens a window and shows it.
func (a *App) NewWindow(cfg Config) (*Window, error) {
	x := a.x
	id := x.generateID(a.conn)
	values := [2]uint32{a.screen.BlackPixel, xcbEventMaskKeyPress | xcbEventMaskKeyRelease | xcbEventMaskButtonPress |
		xcbEventMaskButtonRelease | xcbEventMaskEnter | xcbEventMaskLeave | xcbEventMaskMotion | xcbEventMaskExposure |
		xcbEventMaskStructure | xcbEventMaskFocus}
	x.createWindow(a.conn, xcbCopyFromParent, id, a.screen.Root, 0, 0, uint16(cfg.Width), uint16(cfg.Height), 0,
		xcbWindowClassInputOutput, a.screen.RootVisual, xcbCWBackPixel|xcbCWEventMask, &values[0])
	w := &Window{app: a, id: id, width: cfg.Width, height: cfg.Height}
	a.windows[id] = w
	title := []byte(cfg.Title)
	if len(title) > 0 {
		x.changeProperty(a.conn, xcbPropModeReplace, id, xcbAtomWMName, xcbAtomString, 8, uint32(len(title)), unsafe.Pointer(&title[0]))
		x.changeProperty(a.conn, xcbPropModeReplace, id, a.atomNetWMName, a.atomUTF8, 8, uint32(len(title)), unsafe.Pointer(&title[0]))
	}
	protocols := [1]uint32{a.atomWMDelete}
	x.changeProperty(a.conn, xcbPropModeReplace, id, a.atomWMProtocols, xcbAtomAtom, 32, 1, unsafe.Pointer(&protocols[0]))
	if !cfg.Resizable {
		// WM_NORMAL_HINTS with min == max size.
		hints := [18]uint32{0x30, 0, 0, 0, 0, uint32(cfg.Width), uint32(cfg.Height), uint32(cfg.Width), uint32(cfg.Height)} // PMinSize|PMaxSize
		x.changeProperty(a.conn, xcbPropModeReplace, id, 40 /* WM_NORMAL_HINTS */, 41 /* WM_SIZE_HINTS */, 32, 18, unsafe.Pointer(&hints[0]))
	}
	x.mapWindow(a.conn, id)
	x.flush(a.conn)
	a.push(Event{Kind: EventResize, Window: w, Width: w.width, Height: w.height, PixelW: w.width, PixelH: w.height, Scale: 1})
	return w, nil
}

// Poll drains pending X events into the returned slice, reused by the
// next call. With wait set it blocks until at least one event arrives.
func (a *App) Poll(wait bool) []Event {
	a.pending = a.pending[:0]
	x := a.x
	x.flush(a.conn)
	if wait {
		ev := x.waitForEvent(a.conn)
		if ev == nil {
			for _, w := range a.windows {
				a.push(Event{Kind: EventClose, Window: w})
			}
			return a.pending
		}
		a.handle(ev)
		x.free(unsafe.Pointer(ev))
	}
	for {
		ev := x.pollForEvent(a.conn)
		if ev == nil {
			break
		}
		a.handle(ev)
		x.free(unsafe.Pointer(ev))
	}
	return a.pending
}

func (a *App) push(e Event) { a.pending = append(a.pending, e) }

func modsFromState(state uint16) Mods {
	var m Mods
	if state&xcbModShift != 0 {
		m |= input.ModShift
	}
	if state&xcbModControl != 0 {
		m |= input.ModControl
	}
	if state&xcbModAlt != 0 {
		m |= input.ModAlt
	}
	if state&xcbModSuper != 0 {
		m |= input.ModSuper
	}
	if state&xcbModLock != 0 {
		m |= input.ModCapsLock
	}
	if state&xcbModNumLock != 0 {
		m |= input.ModNumLock
	}
	return m
}

func (a *App) handle(ge *xcbGenericEvent) {
	x := a.x
	switch ge.ResponseType &^ 0x80 {
	case xcbKeyPress, xcbKeyRelease:
		ev := (*xcbInputEvent)(unsafe.Pointer(ge))
		w := a.windows[ev.Event]
		if w == nil {
			return
		}
		a.mods = modsFromState(ev.State)
		key := keyFromX11(ev.Detail)
		if ge.ResponseType&^0x80 == xcbKeyRelease {
			if a.xkbState != nil {
				x.xkbStateUpdateKey(a.xkbState, uint32(ev.Detail), 0)
			}
			a.push(Event{Kind: EventKeyUp, Window: w, Key: key, Mods: a.mods})
			return
		}
		a.push(Event{Kind: EventKeyDown, Window: w, Key: key, Mods: a.mods})
		if a.xkbState != nil && a.mods&(input.ModControl|input.ModSuper) == 0 {
			var buf [16]byte
			n := x.xkbStateKeyGetUTF8(a.xkbState, uint32(ev.Detail), &buf[0], uintptr(len(buf)))
			x.xkbStateUpdateKey(a.xkbState, uint32(ev.Detail), 1)
			for _, r := range string(buf[:max(n, 0)]) {
				if r >= ' ' || r == '\t' || r == '\n' || r == '\r' {
					a.push(Event{Kind: EventChar, Window: w, Rune: r, Mods: a.mods})
				}
			}
		}
	case xcbButtonPress, xcbButtonRelease:
		ev := (*xcbInputEvent)(unsafe.Pointer(ge))
		w := a.windows[ev.Event]
		if w == nil {
			return
		}
		a.mods = modsFromState(ev.State)
		px, py := float64(ev.EventX), float64(ev.EventY)
		down := ge.ResponseType&^0x80 == xcbButtonPress
		switch ev.Detail {
		case 4, 5, 6, 7: // wheel: X reports a press and release per notch
			if !down {
				return
			}
			e := Event{Kind: EventScroll, Window: w, Mods: a.mods}
			switch ev.Detail {
			case 4:
				e.DY = 1
			case 5:
				e.DY = -1
			case 6:
				e.DX = -1
			case 7:
				e.DX = 1
			}
			a.push(e)
			return
		}
		var b MouseButton
		switch ev.Detail {
		case 1:
			b = input.MouseLeft
		case 2:
			b = input.MouseMiddle
		case 3:
			b = input.MouseRight
		case 8:
			b = input.MouseButton4
		case 9:
			b = input.MouseButton5
		default:
			return
		}
		kind := EventMouseUp
		if down {
			kind = EventMouseDown
		}
		a.push(Event{Kind: kind, Window: w, Button: b, X: px, Y: py, Mods: a.mods})
	case xcbMotionNotify:
		ev := (*xcbInputEvent)(unsafe.Pointer(ge))
		w := a.windows[ev.Event]
		if w == nil {
			return
		}
		px, py := float64(ev.EventX), float64(ev.EventY)
		if w.captured {
			cx, cy := w.width/2, w.height/2
			if int(ev.EventX) == cx && int(ev.EventY) == cy {
				return // the warp we asked for
			}
			a.push(Event{Kind: EventMouseMove, Window: w, X: w.mouseX, Y: w.mouseY, DX: px - float64(cx), DY: py - float64(cy), Mods: a.mods})
			x.warpPointer(a.conn, 0, w.id, 0, 0, 0, 0, int16(cx), int16(cy))
			return
		}
		dx, dy := px-w.mouseX, py-w.mouseY
		w.mouseX, w.mouseY = px, py
		a.push(Event{Kind: EventMouseMove, Window: w, X: px, Y: py, DX: dx, DY: dy, Mods: a.mods})
	case xcbEnterNotify, xcbLeaveNotify:
		ev := (*xcbInputEvent)(unsafe.Pointer(ge))
		if w := a.windows[ev.Event]; w != nil {
			kind := EventMouseLeave
			if ge.ResponseType&^0x80 == xcbEnterNotify {
				kind = EventMouseEnter
			}
			a.push(Event{Kind: kind, Window: w})
		}
	case xcbFocusIn, xcbFocusOut:
		ev := (*xcbFocusEvent)(unsafe.Pointer(ge))
		if w := a.windows[ev.Event]; w != nil {
			a.push(Event{Kind: EventFocus, Window: w, Focused: ge.ResponseType&^0x80 == xcbFocusIn})
		}
	case xcbConfigureNotify:
		ev := (*xcbConfigureEvent)(unsafe.Pointer(ge))
		if w := a.windows[ev.Window]; w != nil && (int(ev.Width) != w.width || int(ev.Height) != w.height) {
			w.width, w.height = int(ev.Width), int(ev.Height)
			a.push(Event{Kind: EventResize, Window: w, Width: w.width, Height: w.height, PixelW: w.width, PixelH: w.height, Scale: 1})
		}
	case xcbClientMessage:
		ev := (*xcbClientMessageEvent)(unsafe.Pointer(ge))
		w := a.windows[ev.Window]
		switch {
		case ev.Type == a.atomWake:
			a.push(Event{Kind: EventWake})
		case w != nil && ev.Type == a.atomWMProtocols && ev.Data[0] == a.atomWMDelete:
			a.push(Event{Kind: EventClose, Window: w})
		}
	case xcbDestroyNotify:
		ev := (*xcbConfigureEvent)(unsafe.Pointer(ge)) // window field sits at the same offset
		if w := a.windows[ev.Window]; w != nil {
			w.closed = true
			delete(a.windows, ev.Window)
		}
	case xcbMappingNotify:
		a.refreshKeymap()
	}
}

// Wake sends a client message to the window so a blocked Poll returns
// with EventWake. Safe from any goroutine: xcb serialises its calls.
func (a *App) Wake() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id := range a.windows {
		var ev [32]byte
		msg := (*xcbClientMessageEvent)(unsafe.Pointer(&ev[0]))
		msg.ResponseType, msg.Format, msg.Window, msg.Type = xcbClientMessage, 32, id, a.atomWake
		a.x.sendEvent(a.conn, 0, id, 0, &ev[0])
		a.x.flush(a.conn)
		return
	}
}

// Gamepads reads the Linux joystick devices; see gamepad_linux.go.

// Size is the content size in points (pixels: X11 reports no scale).
func (w *Window) Size() (int, int) { return w.width, w.height }

// PixelSize is the framebuffer size.
func (w *Window) PixelSize() (int, int) { return w.width, w.height }

// Scale is pixels per point.
func (w *Window) Scale() float64 { return 1 }

// Closed reports whether the window was destroyed.
func (w *Window) Closed() bool { return w.closed }

// Close destroys the window.
func (w *Window) Close() {
	if w.closed {
		return
	}
	w.closed = true
	delete(w.app.windows, w.id)
	w.app.x.destroyWindow(w.app.conn, w.id)
	w.app.x.flush(w.app.conn)
}

// Fullscreen reports whether the window manager has the window full screen.
func (w *Window) Fullscreen() bool { return w.fullscr }

// SetFullscreen asks the window manager for _NET_WM_STATE_FULLSCREEN.
func (w *Window) SetFullscreen(on bool) {
	if on == w.fullscr {
		return
	}
	a := w.app
	var ev [32]byte
	msg := (*xcbClientMessageEvent)(unsafe.Pointer(&ev[0]))
	msg.ResponseType, msg.Format, msg.Window, msg.Type = xcbClientMessage, 32, w.id, a.atomNetWMState
	if on {
		msg.Data[0] = 1
	}
	msg.Data[1], msg.Data[3] = a.atomNetWMFullscreen, 1
	a.x.sendEvent(a.conn, 0, a.screen.Root, xcbEventMaskSubRedirect|xcbEventMaskSubNotify, &ev[0])
	a.x.flush(a.conn)
	w.fullscr = on
}

// SetCursorCaptured hides the cursor, confines it to the window and
// reports motion as deltas.
func (w *Window) SetCursorCaptured(on bool) {
	if on == w.captured {
		return
	}
	a, x := w.app, w.app.x
	w.captured = on
	if on {
		pid := x.generateID(a.conn)
		x.createPixmap(a.conn, 1, pid, w.id, 1, 1)
		w.cursor = x.generateID(a.conn)
		x.createCursor(a.conn, w.cursor, pid, pid, 0, 0, 0, 0, 0, 0, 0, 0)
		x.freePixmap(a.conn, pid)
		x.grabPointer(a.conn, 1, w.id, xcbEventMaskButtonPress|xcbEventMaskButtonRelease|xcbEventMaskMotion,
			xcbGrabModeAsync, xcbGrabModeAsync, w.id, w.cursor, xcbCurrentTime)
		x.warpPointer(a.conn, 0, w.id, 0, 0, 0, 0, int16(w.width/2), int16(w.height/2))
	} else {
		x.ungrabPointer(a.conn, xcbCurrentTime)
		values := [1]uint32{0}
		x.changeWindowAttrs(a.conn, w.id, xcbCWCursor, &values[0])
	}
	x.flush(a.conn)
}

// CursorCaptured reports the capture state.
func (w *Window) CursorCaptured() bool { return w.captured }

// SetTextInputRect records where text is entered; X input methods are
// not wired, so it only stores the rectangle.
func (w *Window) SetTextInputRect(x, y, width, height float64) {
	w.inputRect = textRect{X: x, Y: y, W: width, H: height}
}
