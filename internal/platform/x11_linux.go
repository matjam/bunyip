package platform

import (
	"errors"
	"fmt"
	"structs"
	"time"
	"unicode/utf8"
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
	xcbKeyPress          = 2
	xcbKeyRelease        = 3
	xcbButtonPress       = 4
	xcbButtonRelease     = 5
	xcbMotionNotify      = 6
	xcbEnterNotify       = 7
	xcbLeaveNotify       = 8
	xcbFocusIn           = 9
	xcbFocusOut          = 10
	xcbVisibilityNotify  = 15
	xcbUnmapNotify       = 18
	xcbMapNotify         = 19
	xcbDestroyNotify     = 17
	xcbConfigureNotify   = 22
	xcbPropertyNotify    = 28
	xcbSelectionClear    = 29
	xcbSelectionRequest  = 30
	xcbSelectionNotify   = 31
	xcbClientMessage     = 33
	xcbMappingNotify     = 34
	xcbVisibilityObscure = 2 // VisibilityFullyObscured
	xcbPropertyNewValue  = 0
	xcbPropertyDelete    = 1

	xcbEventMaskKeyPress      = 1
	xcbEventMaskKeyRelease    = 2
	xcbEventMaskButtonPress   = 4
	xcbEventMaskButtonRelease = 8
	xcbEventMaskEnter         = 0x10
	xcbEventMaskLeave         = 0x20
	xcbEventMaskMotion        = 0x40
	xcbEventMaskExposure      = 0x8000
	xcbEventMaskVisibility    = 0x10000
	xcbEventMaskStructure     = 0x20000
	xcbEventMaskSubRedirect   = 0x100000
	xcbEventMaskSubNotify     = 0x80000
	xcbEventMaskFocus         = 0x200000
	xcbEventMaskProperty      = 0x400000

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

// xcbMapEvent is MapNotify and UnmapNotify, which name the window that
// was mapped as well as the window the event was selected on.
type xcbMapEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Pad          uint8
	Sequence     uint16
	Event        uint32
	Window       uint32
	Flag         uint8 // override-redirect on a map, from-configure on an unmap
	Pad1         [3]uint8
}

type xcbVisibilityEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Pad          uint8
	Sequence     uint16
	Window       uint32
	State        uint8
	Pad1         [3]uint8
}

type xcbPropertyEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Pad          uint8
	Sequence     uint16
	Window       uint32
	Atom         uint32
	Time         uint32
	State        uint8
	Pad1         [3]uint8
}

type xcbGetPropertyReply struct {
	_            structs.HostLayout
	ResponseType uint8
	Format       uint8
	Sequence     uint16
	Length       uint32
	Type         uint32
	BytesAfter   uint32
	ValueLen     uint32
	Pad          [12]uint8
}

// The three selection events. A window that owns a selection answers
// requests for it and hears when it loses it; a window that asks for one
// hears the answer.
type xcbSelectionRequestEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Pad          uint8
	Sequence     uint16
	Time         uint32
	Owner        uint32
	Requestor    uint32
	Selection    uint32
	Target       uint32
	Property     uint32
}

type xcbSelectionNotifyEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Pad          uint8
	Sequence     uint16
	Time         uint32
	Requestor    uint32
	Selection    uint32
	Target       uint32
	Property     uint32
}

type xcbSelectionClearEvent struct {
	_            structs.HostLayout
	ResponseType uint8
	Pad          uint8
	Sequence     uint16
	Time         uint32
	Owner        uint32
	Selection    uint32
}

type xcbGetSelectionOwnerReply struct {
	_            structs.HostLayout
	ResponseType uint8
	Pad          uint8
	Sequence     uint16
	Length       uint32
	Owner        uint32
}

type xcbUseExtensionReply struct {
	_            structs.HostLayout
	ResponseType uint8
	Supported    uint8
	Sequence     uint16
	Length       uint32
	ServerMajor  uint16
	ServerMinor  uint16
	Pad          [20]uint8
}

type xcbPerClientFlagsReply struct {
	_               structs.HostLayout
	ResponseType    uint8
	DeviceID        uint8
	Sequence        uint16
	Length          uint32
	Supported       uint32
	Value           uint32
	AutoCtrls       uint32
	AutoCtrlsValues uint32
	Pad             [8]uint8
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
	screenNext         func(it *xcbScreenIterator)
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
	deleteProperty     func(c unsafe.Pointer, w uint32, property uint32) xcbCookie
	getProperty        func(c unsafe.Pointer, del uint8, w uint32, property, typ uint32, offset, length uint32) xcbCookie
	getPropertyReply   func(c unsafe.Pointer, cookie xcbCookie, err unsafe.Pointer) *xcbGetPropertyReply
	getPropertyValue   func(reply *xcbGetPropertyReply) unsafe.Pointer
	getPropertyValueLn func(reply *xcbGetPropertyReply) int32
	setSelectionOwner  func(c unsafe.Pointer, owner, selection, time uint32) xcbCookie
	getSelectionOwner  func(c unsafe.Pointer, selection uint32) xcbCookie
	getSelOwnerReply   func(c unsafe.Pointer, cookie xcbCookie, err unsafe.Pointer) *xcbGetSelectionOwnerReply
	convertSelection   func(c unsafe.Pointer, requestor, selection, target, property, time uint32) xcbCookie
	maxRequestLength   func(c unsafe.Pointer) uint32
	fileDescriptor     func(c unsafe.Pointer) int32
	sendEvent          func(c unsafe.Pointer, propagate uint8, dest uint32, mask uint32, event *byte) xcbCookie
	grabPointer        func(c unsafe.Pointer, ownerEvents uint8, grabWindow uint32, eventMask uint16, pointerMode, keyboardMode uint8, confineTo, cursor, time uint32) xcbCookie
	ungrabPointer      func(c unsafe.Pointer, time uint32) xcbCookie
	warpPointer        func(c unsafe.Pointer, src, dst uint32, srcX, srcY int16, srcW, srcH uint16, dstX, dstY int16) xcbCookie
	createPixmap       func(c unsafe.Pointer, depth uint8, pid, drawable uint32, w, h uint16) xcbCookie
	freePixmap         func(c unsafe.Pointer, pid uint32) xcbCookie
	createCursor       func(c unsafe.Pointer, cid, source, mask uint32, foreR, foreG, foreB, backR, backG, backB uint16, x, y uint16) xcbCookie
	changeWindowAttrs  func(c unsafe.Pointer, w uint32, mask uint32, values *uint32) xcbCookie
	openFont           func(c unsafe.Pointer, fid uint32, nameLen uint16, name *byte) xcbCookie
	closeFont          func(c unsafe.Pointer, fid uint32) xcbCookie
	createGlyphCursor  func(c unsafe.Pointer, cid, sourceFont, maskFont uint32, sourceChar, maskChar uint16, foreR, foreG, foreB, backR, backG, backB uint16) xcbCookie
	freeCursor         func(c unsafe.Pointer, cid uint32) xcbCookie
	free               func(p unsafe.Pointer)
	poll               func(fds *pollFD, nfds uint64, timeout int32) int32

	// libxcb-xkb, optional. It carries the requests that turn detectable
	// auto-repeat on, which is what stops a held key reporting a release
	// between every repeat.
	xkbUseExtension        func(c unsafe.Pointer, major, minor uint16) xcbCookie
	xkbUseExtensionReply   func(c unsafe.Pointer, cookie xcbCookie, err unsafe.Pointer) *xcbUseExtensionReply
	xkbPerClientFlags      func(c unsafe.Pointer, device uint16, change, value, ctrlsToChange, autoCtrls, autoCtrlsValues uint32) xcbCookie
	xkbPerClientFlagsReply func(c unsafe.Pointer, cookie xcbCookie, err unsafe.Pointer) *xcbPerClientFlagsReply

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
	xkbStateUpdateMask  func(st unsafe.Pointer, depressed, latched, locked, baseGroup, latchedGroup, lockedGroup uint32) int32
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
		"xcb_get_setup": &x.getSetup, "xcb_setup_roots_iterator": &x.setupRootsIterator, "xcb_screen_next": &x.screenNext, "xcb_generate_id": &x.generateID,
		"xcb_create_window": &x.createWindow, "xcb_destroy_window": &x.destroyWindow, "xcb_map_window": &x.mapWindow,
		"xcb_flush": &x.flush, "xcb_poll_for_event": &x.pollForEvent, "xcb_wait_for_event": &x.waitForEvent,
		"xcb_intern_atom": &x.internAtom, "xcb_intern_atom_reply": &x.internAtomReply, "xcb_change_property": &x.changeProperty,
		"xcb_delete_property": &x.deleteProperty, "xcb_get_property": &x.getProperty, "xcb_get_property_reply": &x.getPropertyReply,
		"xcb_get_property_value": &x.getPropertyValue, "xcb_get_property_value_length": &x.getPropertyValueLn,
		"xcb_set_selection_owner": &x.setSelectionOwner, "xcb_get_selection_owner": &x.getSelectionOwner,
		"xcb_get_selection_owner_reply": &x.getSelOwnerReply, "xcb_convert_selection": &x.convertSelection,
		"xcb_get_maximum_request_length": &x.maxRequestLength, "xcb_get_file_descriptor": &x.fileDescriptor,
		"xcb_send_event": &x.sendEvent, "xcb_grab_pointer": &x.grabPointer, "xcb_ungrab_pointer": &x.ungrabPointer,
		"xcb_warp_pointer": &x.warpPointer, "xcb_create_pixmap": &x.createPixmap, "xcb_free_pixmap": &x.freePixmap,
		"xcb_create_cursor": &x.createCursor, "xcb_change_window_attributes": &x.changeWindowAttrs,
		"xcb_open_font": &x.openFont, "xcb_close_font": &x.closeFont, "xcb_create_glyph_cursor": &x.createGlyphCursor,
		"xcb_free_cursor": &x.freeCursor,
	} {
		if err := load(xcb, name, fptr); err != nil {
			return nil, err
		}
	}
	for name, fptr := range map[string]any{"free": &x.free, "poll": &x.poll} {
		if err := load(libc, name, fptr); err != nil {
			return nil, err
		}
	}
	// Detectable auto-repeat is optional: without libxcb-xkb the layer
	// falls back to spotting the release and press pair by its timestamp.
	if xkb, err := purego.Dlopen("libxcb-xkb.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
		ok := true
		for name, fptr := range map[string]any{
			"xcb_xkb_use_extension": &x.xkbUseExtension, "xcb_xkb_use_extension_reply": &x.xkbUseExtensionReply,
			"xcb_xkb_per_client_flags": &x.xkbPerClientFlags, "xcb_xkb_per_client_flags_reply": &x.xkbPerClientFlagsReply,
		} {
			ok = ok && load(xkb, name, fptr) == nil
		}
		if !ok {
			x.xkbUseExtension = nil
		}
	}
	// Text input is optional.
	if xkb, err := purego.Dlopen("libxkbcommon.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
		if xkbx11, err := purego.Dlopen("libxkbcommon-x11.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			ok := true
			for name, fptr := range map[string]any{
				"xkb_context_new": &x.xkbContextNew, "xkb_context_unref": &x.xkbContextUnref, "xkb_keymap_unref": &x.xkbKeymapUnref,
				"xkb_state_unref": &x.xkbStateUnref, "xkb_state_key_get_utf8": &x.xkbStateKeyGetUTF8, "xkb_state_update_key": &x.xkbStateUpdateKey,
				"xkb_state_update_mask": &x.xkbStateUpdateMask,
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

// ErrNoX11 is returned when no X server can be reached.
var ErrNoX11 = errors.New("platform: cannot connect to an X server (is DISPLAY set?)")

// connectX11 opens the connection to the X server and fills in the X11 half
// of the App.
func (a *App) connectX11() error {
	x, err := loadX11()
	if err != nil {
		return err
	}
	var screenNum int32
	conn := x.connect(nil, &screenNum)
	if conn == nil || x.connectionHasError(conn) != 0 {
		return ErrNoX11
	}
	it := x.setupRootsIterator(x.getSetup(conn))
	for i := int32(0); i < screenNum && it.Rem > 0; i++ {
		// Each screen is followed by its variable-length depth list, so
		// only xcb knows how far the next one is.
		x.screenNext(&it)
	}
	a.x, a.conn, a.screen, a.windows = x, conn, it.Data, map[uint32]*Window{}
	a.atomWMProtocols = a.atom("WM_PROTOCOLS")
	a.atomWMDelete = a.atom("WM_DELETE_WINDOW")
	a.atomNetWMName = a.atom("_NET_WM_NAME")
	a.atomUTF8 = a.atom("UTF8_STRING")
	a.atomNetWMState = a.atom("_NET_WM_STATE")
	a.atomNetWMFullscreen = a.atom("_NET_WM_STATE_FULLSCREEN")
	a.atomNetWMHidden = a.atom("_NET_WM_STATE_HIDDEN")
	a.atomWake = a.atom("BUNYIP_WAKE")
	a.atomClipboard = a.atom("CLIPBOARD")
	a.atomTargets = a.atom("TARGETS")
	a.atomText = a.atom("TEXT")
	a.atomIncr = a.atom("INCR")
	a.atomSelection = a.atom("BUNYIP_SELECTION")
	a.clipChunk = clipChunk(x.maxRequestLength(conn))
	a.setupXKB()
	a.detectableRepeat = a.setDetectableRepeat()
	return nil
}

// XKB constants for the detectable auto-repeat request.
const (
	xkbUseCoreKbd            = 0x0100
	xkbDetectableAutoRepeat  = 1 << 0
	xkbExtensionMajorVersion = 1
	xkbExtensionMinorVersion = 0
)

// setDetectableRepeat asks the server to deliver a held key as presses
// alone. Without it X sends a release before every repeat, which reads as
// the key having been let go. It reports whether the server agreed;
// keyFromX11 events fall back to matching the release and press by their
// timestamps when it did not.
func (a *App) setDetectableRepeat() bool {
	x := a.x
	if x.xkbUseExtension == nil {
		return false
	}
	// The extension has to be set up on this connection before any of its
	// requests are sent, whether or not xkbcommon already did it.
	use := x.xkbUseExtensionReply(a.conn, x.xkbUseExtension(a.conn, xkbExtensionMajorVersion, xkbExtensionMinorVersion), nil)
	if use == nil {
		return false
	}
	supported := use.Supported != 0
	x.free(unsafe.Pointer(use))
	if !supported {
		return false
	}
	cookie := x.xkbPerClientFlags(a.conn, xkbUseCoreKbd, xkbDetectableAutoRepeat, xkbDetectableAutoRepeat, 0, 0, 0)
	reply := x.xkbPerClientFlagsReply(a.conn, cookie, nil)
	if reply == nil {
		return false
	}
	on := reply.Value&xkbDetectableAutoRepeat != 0
	x.free(unsafe.Pointer(reply))
	return on
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

// xProperty is one reply to a property read.
type xProperty struct {
	Data       []byte
	Type       uint32
	Format     uint8
	BytesAfter uint32
}

// atoms reads a property whose format is 32 as the atom list it is.
func (p xProperty) atoms() []uint32 {
	if p.Format != 32 || len(p.Data) < 4 {
		return nil
	}
	return unsafe.Slice((*uint32)(unsafe.Pointer(&p.Data[0])), len(p.Data)/4)
}

// property reads a window property. offset and length count four-byte
// units, which is what the protocol counts; del deletes the property once
// it has been read, which is how an INCR transfer asks for the next chunk.
// A property that does not exist comes back with no data and type zero.
func (a *App) property(win, prop, typ, offset, length uint32, del bool) xProperty {
	x := a.x
	var d uint8
	if del {
		d = 1
	}
	reply := x.getPropertyReply(a.conn, x.getProperty(a.conn, d, win, prop, typ, offset, length), nil)
	if reply == nil {
		return xProperty{}
	}
	defer x.free(unsafe.Pointer(reply))
	out := xProperty{Type: reply.Type, Format: reply.Format, BytesAfter: reply.BytesAfter}
	if n := int(x.getPropertyValueLn(reply)); n > 0 {
		out.Data = append([]byte(nil), unsafe.Slice((*byte)(x.getPropertyValue(reply)), n)...)
	}
	return out
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

// newX11Window opens an X11 window and shows it.
func (a *App) newX11Window(cfg Config) (*Window, error) {
	x := a.x
	id := x.generateID(a.conn)
	// Visibility and property changes are selected as well as input, so
	// that a window that is minimised, unmapped or covered says so:
	// UnmapNotify and MapNotify come from the structure mask,
	// VisibilityNotify from the visibility mask, and _NET_WM_STATE_HIDDEN
	// from the property mask.
	values := [2]uint32{a.screen.BlackPixel, xcbEventMaskKeyPress | xcbEventMaskKeyRelease | xcbEventMaskButtonPress |
		xcbEventMaskButtonRelease | xcbEventMaskEnter | xcbEventMaskLeave | xcbEventMaskMotion | xcbEventMaskExposure |
		xcbEventMaskVisibility | xcbEventMaskStructure | xcbEventMaskFocus | xcbEventMaskProperty}
	x.createWindow(a.conn, xcbCopyFromParent, id, a.screen.Root, 0, 0, uint16(cfg.Width), uint16(cfg.Height), 0,
		xcbWindowClassInputOutput, a.screen.RootVisual, xcbCWBackPixel|xcbCWEventMask, &values[0])
	// The window is mapped below, so it starts visible; only an unmap, a
	// minimise or full obscuring changes that, and each sends an event.
	w := &Window{app: a, id: id, width: cfg.Width, height: cfg.Height, mapped: true, visible: true}
	a.windows[id] = w
	a.wakeWin.CompareAndSwap(0, id)
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

// x11Poll drains pending X events into the returned slice, reused by the
// next call. With wait set it blocks until at least one event arrives.
func (a *App) x11Poll(wait bool) []Event {
	a.startPoll()
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
		a.dispatch(ev)
	}
	for {
		ev := x.pollForEvent(a.conn)
		if ev == nil {
			break
		}
		a.dispatch(ev)
	}
	a.sweepIncr(time.Now())
	return a.pending
}

// dispatch handles one event and frees it, then handles whatever the key
// repeat lookahead took off the queue behind it.
func (a *App) dispatch(ev *xcbGenericEvent) {
	for ev != nil {
		a.handle(ev)
		a.x.free(unsafe.Pointer(ev))
		ev, a.peeked = a.peeked, nil
	}
}

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

// repeatPress reports whether a key release and the event that follows it
// are one key repeat. Without detectable auto-repeat the server sends a
// repeat as a release and a press for the same key, on the same window,
// carrying the same timestamp; no hand releases and presses a key inside
// the one millisecond an X timestamp counts.
func repeatPress(release *xcbInputEvent, next *xcbGenericEvent) bool {
	if release == nil || next == nil || next.ResponseType&^0x80 != xcbKeyPress {
		return false
	}
	press := (*xcbInputEvent)(unsafe.Pointer(next))
	return press.Detail == release.Detail && press.Time == release.Time && press.Event == release.Event
}

// pushKeyDown reports a key press, or a repeat of one, with the text the
// key produces after it.
func (a *App) pushKeyDown(w *Window, ev *xcbInputEvent, repeat bool) {
	x := a.x
	a.push(Event{Kind: EventKeyDown, Window: w, Key: keyFromX11(ev.Detail), Mods: a.mods, Repeat: repeat})
	if a.xkbState == nil || a.mods&(input.ModControl|input.ModSuper) != 0 {
		return
	}
	// The event carries the server's modifier and group state, which
	// includes keys pressed before this window had focus; tracking presses
	// locally would miss those.
	x.xkbStateUpdateMask(a.xkbState, uint32(ev.State&0xff), 0, 0, 0, 0, uint32(ev.State>>13)&3)
	var buf [16]byte
	n := x.xkbStateKeyGetUTF8(a.xkbState, uint32(ev.Detail), &buf[0], uintptr(len(buf)))
	for _, r := range string(buf[:max(min(int(n), len(buf)-1), 0)]) {
		if r >= ' ' || r == '\t' || r == '\n' || r == '\r' {
			a.push(Event{Kind: EventChar, Window: w, Rune: r, Mods: a.mods})
		}
	}
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
		if ge.ResponseType&^0x80 == xcbKeyRelease {
			if !a.detectableRepeat {
				// Without detectable auto-repeat the server sends a repeat
				// as this release followed by an identical press. Look at
				// the next event: a match is one repeat, and anything else
				// is handed back to the poll loop. The look reads the
				// socket rather than only the queue, because the press is
				// often still in the kernel's buffer when the release is
				// handled, and a queue-only look would miss it.
				next := x.pollForEvent(a.conn)
				if repeatPress(ev, next) {
					x.free(unsafe.Pointer(next))
					a.pushKeyDown(w, ev, true)
					return
				}
				a.peeked = next
			}
			a.keysDown[ev.Detail] = false
			a.push(Event{Kind: EventKeyUp, Window: w, Key: keyFromX11(ev.Detail), Mods: a.mods})
			return
		}
		repeat := a.keysDown[ev.Detail]
		a.keysDown[ev.Detail] = true
		a.pushKeyDown(w, ev, repeat)
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
			focused := ge.ResponseType&^0x80 == xcbFocusIn
			if !focused {
				// A key let go while another window has focus reports
				// nothing here, so forget what was held.
				a.keysDown = [256]bool{}
			}
			a.push(Event{Kind: EventFocus, Window: w, Focused: focused})
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
	case xcbMapNotify, xcbUnmapNotify:
		ev := (*xcbMapEvent)(unsafe.Pointer(ge))
		if w := a.windows[ev.Window]; w != nil {
			w.mapped = ge.ResponseType&^0x80 == xcbMapNotify
			w.updateVisible()
		}
	case xcbVisibilityNotify:
		ev := (*xcbVisibilityEvent)(unsafe.Pointer(ge))
		if w := a.windows[ev.Window]; w != nil {
			w.obscured = ev.State == xcbVisibilityObscure
			w.updateVisible()
		}
	case xcbPropertyNotify:
		ev := (*xcbPropertyEvent)(unsafe.Pointer(ge))
		if w := a.windows[ev.Window]; w != nil && ev.Atom == a.atomNetWMState {
			w.wmHidden = w.netWMHidden()
			w.updateVisible()
		}
		if ev.State == xcbPropertyDelete {
			// A requestor deleting the property is how it asks for the
			// next chunk of an INCR transfer.
			a.sendIncrChunk(ev.Window, ev.Atom)
		}
	case xcbSelectionRequest:
		a.answerSelectionRequest((*xcbSelectionRequestEvent)(unsafe.Pointer(ge)))
	case xcbSelectionClear:
		if ev := (*xcbSelectionClearEvent)(unsafe.Pointer(ge)); ev.Selection == a.atomClipboard {
			a.clipOwned, a.clipText = false, ""
		}
	case xcbDestroyNotify:
		ev := (*xcbConfigureEvent)(unsafe.Pointer(ge)) // window field sits at the same offset
		if w := a.windows[ev.Window]; w != nil {
			w.closed = true
			delete(a.windows, ev.Window)
			a.wakeWin.CompareAndSwap(ev.Window, 0)
		}
		// A requestor that exits in the middle of a transfer will never
		// ask for the rest of it.
		a.forgetRequestor(ev.Window)
	case xcbMappingNotify:
		a.refreshKeymap()
	}
}

// netWMHidden reports whether _NET_WM_STATE on the window carries
// _NET_WM_STATE_HIDDEN, which is how a window manager says the window is
// minimised or shaded. A window manager that does not set the property
// reports nothing, which reads as shown.
func (w *Window) netWMHidden() bool {
	a := w.app
	for _, at := range a.property(w.id, a.atomNetWMState, xcbAtomAtom, 0, 64, false).atoms() {
		if at == a.atomNetWMHidden {
			return true
		}
	}
	return false
}

// updateVisible reports a change in whether the window can be seen. It is
// hidden when it is unmapped, minimised or wholly covered by other
// windows, which is what lets a game stop drawing.
func (w *Window) updateVisible() {
	visible := w.mapped && !w.obscured && !w.wmHidden
	if visible == w.visible {
		return
	}
	w.visible = visible
	w.app.push(Event{Kind: EventVisible, Window: w, Visible: visible})
}

// x11Wake sends a client message to the window so a blocked Poll returns
// with EventWake. Safe from any goroutine: xcb serialises its calls, and
// the target window is read from an atomic rather than the window map,
// which the main goroutine writes without a lock.
func (a *App) x11Wake() {
	id := a.wakeWin.Load()
	if id == 0 {
		return
	}
	var ev [32]byte
	msg := (*xcbClientMessageEvent)(unsafe.Pointer(&ev[0]))
	msg.ResponseType, msg.Format, msg.Window, msg.Type = xcbClientMessage, 32, id, a.atomWake
	a.x.sendEvent(a.conn, 0, id, 0, &ev[0])
	a.x.flush(a.conn)
}

// --- selections ---
//
// X has no clipboard of its own. One client owns the CLIPBOARD selection
// and hands the text to whoever asks, over the same event loop as
// everything else, so the layer both answers requests while it owns the
// selection and asks the current owner when a game reads. Text larger
// than one request goes through INCR, a chunk at a time, with the
// requestor deleting the property to ask for the next.

// incrSend is one INCR transfer this process is handing over.
type incrSend struct {
	requestor uint32
	property  uint32
	target    uint32
	data      []byte
	sent      int
	touched   time.Time // when the requestor last took a chunk
}

// incrIdle is how long a transfer waits for the requestor to ask for the
// next chunk before it is abandoned. A client that exits mid-transfer,
// or never deletes the property, would otherwise hold a copy of the
// clipboard for as long as the game runs.
const incrIdle = 5 * time.Second

// putIncr replaces the transfer for the same requestor and property, or
// appends a new one. Two requests for one property cannot run at once,
// because the property is the whole channel, so the later request is the
// one that counts.
func putIncr(transfers []incrSend, t incrSend) []incrSend {
	for i := range transfers {
		if transfers[i].requestor == t.requestor && transfers[i].property == t.property {
			transfers[i] = t
			return transfers
		}
	}
	return append(transfers, t)
}

// dropStaleIncr splits transfers into those still live at now and those
// whose requestor has said nothing for timeout.
func dropStaleIncr(transfers []incrSend, now time.Time, timeout time.Duration) (kept, stale []incrSend) {
	for _, t := range transfers {
		if now.Sub(t.touched) > timeout {
			stale = append(stale, t)
			continue
		}
		kept = append(kept, t)
	}
	return kept, stale
}

// incrMask counts the transfers that need property changes selected on
// each requestor's window. Two transfers to one window would otherwise
// have the first one's end stop the second one's events.
type incrMask map[uint32]int

// add records one more transfer and reports whether the mask has to be
// selected now.
func (m incrMask) add(win uint32) bool {
	m[win]++
	return m[win] == 1
}

// remove records one fewer and reports whether the mask can be cleared.
func (m incrMask) remove(win uint32) bool {
	n := m[win] - 1
	if n <= 0 {
		delete(m, win)
		return true
	}
	m[win] = n
	return false
}

// clipChunk is the largest property one request can write, from the
// connection's maximum request length in four-byte units. It is kept well
// under the limit to leave room for the request's own header, and capped
// so that a server with BIG-REQUESTS does not make a chunk enormous.
func clipChunk(units uint32) int {
	n := int(units)*4 - 1024
	if n < 4096 {
		return 4096
	}
	if n > 1<<18 {
		return 1 << 18
	}
	return n
}

// clipWindow is the window selections are owned and requested through.
// Zero means no window is open, and the clipboard needs one.
func (a *App) clipWindow() uint32 { return a.wakeWin.Load() }

// setClipboardX11 takes ownership of the CLIPBOARD selection and holds
// the text until another client takes it.
func (a *App) setClipboardX11(text string) error {
	win := a.clipWindow()
	if win == 0 {
		return ErrNoClipboard
	}
	a.clipText, a.clipOwned = text, true
	a.x.setSelectionOwner(a.conn, win, a.atomClipboard, xcbCurrentTime)
	a.x.flush(a.conn)
	return nil
}

// clipboardX11 reads the CLIPBOARD selection. It returns the empty string
// when nobody owns the selection, when the owner has no text to offer and
// when the owner does not answer in time.
func (a *App) clipboardX11() (string, error) {
	if a.clipOwned {
		return a.clipText, nil // no round trip to ourselves
	}
	x := a.x
	win := a.clipWindow()
	if win == 0 {
		return "", ErrNoClipboard
	}
	reply := x.getSelOwnerReply(a.conn, x.getSelectionOwner(a.conn, a.atomClipboard), nil)
	if reply == nil {
		return "", nil
	}
	owner := reply.Owner
	x.free(unsafe.Pointer(reply))
	if owner == 0 {
		return "", nil
	}
	// Events that arrive while the owner is answering are queued for the
	// next Poll; dropping them would lose a key press or a resize.
	a.deferQueue = true
	defer func() { a.deferQueue = false }()
	// UTF8_STRING first. An owner old enough to offer only STRING refuses
	// it by naming no property, and STRING is the fallback every owner
	// has.
	for _, target := range [2]uint32{a.atomUTF8, xcbAtomString} {
		x.deleteProperty(a.conn, win, a.atomSelection)
		x.convertSelection(a.conn, win, a.atomClipboard, target, a.atomSelection, xcbCurrentTime)
		x.flush(a.conn)
		prop := a.waitSelection(win, time.Now().Add(clipboardWait))
		if prop == 0 {
			continue // the owner refused this target, or said nothing in time
		}
		text, typ := a.readSelection(win, prop)
		return decodeSelection(text, typ == xcbAtomString), nil
	}
	return "", nil
}

// decodeSelection turns the bytes an owner wrote into a string. A
// UTF8_STRING is already UTF-8. A STRING is Latin-1 by the protocol, but
// most toolkits write UTF-8 into it anyway, so bytes that are valid UTF-8
// are taken as they are and only the rest are widened from Latin-1, which
// is what keeps both kinds of owner readable.
func decodeSelection(data []byte, latin1 bool) string {
	if !latin1 || utf8.Valid(data) {
		return string(data)
	}
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b)
	}
	return string(runes)
}

// waitSelection waits for the SelectionNotify that answers a convert
// request and returns the property the owner wrote to, or zero when it
// refused or nothing came in time. Every other event is handled as usual.
func (a *App) waitSelection(win uint32, deadline time.Time) uint32 {
	x := a.x
	for {
		for {
			ev := x.pollForEvent(a.conn)
			if ev == nil {
				break
			}
			if ev.ResponseType&^0x80 == xcbSelectionNotify {
				sn := (*xcbSelectionNotifyEvent)(unsafe.Pointer(ev))
				if sn.Requestor == win && sn.Selection == a.atomClipboard {
					prop := sn.Property
					x.free(unsafe.Pointer(ev))
					return prop
				}
			}
			a.dispatch(ev)
		}
		if !a.waitReadable(deadline) {
			return 0
		}
	}
}

// waitProperty waits for the owner to write the next chunk of an INCR
// transfer into the property. It reports whether one arrived in time.
func (a *App) waitProperty(win, prop uint32, deadline time.Time) bool {
	x := a.x
	for {
		for {
			ev := x.pollForEvent(a.conn)
			if ev == nil {
				break
			}
			if ev.ResponseType&^0x80 == xcbPropertyNotify {
				pn := (*xcbPropertyEvent)(unsafe.Pointer(ev))
				if pn.Window == win && pn.Atom == prop && pn.State == xcbPropertyNewValue {
					x.free(unsafe.Pointer(ev))
					return true
				}
			}
			a.dispatch(ev)
		}
		if !a.waitReadable(deadline) {
			return false
		}
	}
}

// waitReadable sleeps until the X connection has something to read or the
// deadline passes, and reports whether it is worth reading again.
func (a *App) waitReadable(deadline time.Time) bool {
	left := time.Until(deadline)
	if left <= 0 {
		return false
	}
	a.x.flush(a.conn)
	fd := pollFD{Fd: a.x.fileDescriptor(a.conn), Events: pollIn}
	a.x.poll(&fd, 1, int32(left/time.Millisecond)+1)
	return true
}

// readSelection reads the bytes the owner wrote and the type it gave
// them, following an INCR transfer where the text was too large for one
// property.
func (a *App) readSelection(win, prop uint32) ([]byte, uint32) {
	x := a.x
	// Read nothing at first, only the type, so that an INCR transfer is
	// not started by a read that throws the property away.
	if head := a.property(win, prop, 0, 0, 0, false); head.Type == a.atomIncr {
		// Deleting the property tells the owner to send the first chunk,
		// and deleting it again after each one asks for the next.
		x.deleteProperty(a.conn, win, prop)
		x.flush(a.conn)
		var out []byte
		typ := uint32(0)
		for {
			if !a.waitProperty(win, prop, time.Now().Add(clipboardWait)) {
				return out, typ
			}
			chunk := a.propertyAll(win, prop, true)
			if len(chunk.Data) == 0 {
				return out, typ // the empty chunk ends the transfer
			}
			typ = chunk.Type
			out = append(out, chunk.Data...)
		}
	}
	all := a.propertyAll(win, prop, true)
	return all.Data, all.Type
}

// propertyAll reads a property whole, following its offset until nothing
// is left, and deletes it afterwards when del is set.
func (a *App) propertyAll(win, prop uint32, del bool) xProperty {
	var out xProperty
	for offset := uint32(0); ; {
		chunk := a.property(win, prop, 0, offset, 1<<16, false)
		out.Type, out.Format = chunk.Type, chunk.Format
		if len(chunk.Data) == 0 {
			break
		}
		out.Data = append(out.Data, chunk.Data...)
		if chunk.BytesAfter == 0 {
			break
		}
		offset += uint32(len(chunk.Data)) / 4
	}
	if del {
		a.x.deleteProperty(a.conn, win, prop)
		a.x.flush(a.conn)
	}
	return out
}

// answerSelectionRequest hands the text to a client that asked for the
// clipboard. A target the layer does not offer is refused with a
// SelectionNotify naming no property, which is what the protocol says.
func (a *App) answerSelectionRequest(req *xcbSelectionRequestEvent) {
	x := a.x
	if !a.clipOwned || req.Selection != a.atomClipboard {
		a.sendSelectionNotify(req, 0)
		return
	}
	prop := req.Property
	if prop == 0 {
		prop = req.Target // a client old enough to leave the property out
	}
	switch req.Target {
	case a.atomTargets:
		targets := [4]uint32{a.atomTargets, a.atomUTF8, xcbAtomString, a.atomText}
		x.changeProperty(a.conn, xcbPropModeReplace, req.Requestor, prop, xcbAtomAtom, 32,
			uint32(len(targets)), unsafe.Pointer(&targets[0]))
	case a.atomUTF8, xcbAtomString, a.atomText:
		data := []byte(a.clipText)
		if len(data) > a.clipChunk {
			a.startIncr(req, prop, data)
			return
		}
		x.changeProperty(a.conn, xcbPropModeReplace, req.Requestor, prop, req.Target, 8,
			uint32(len(data)), bytesPointer(data))
	default:
		a.sendSelectionNotify(req, 0)
		return
	}
	a.sendSelectionNotify(req, prop)
}

// startIncr answers a request for text too large for one property. The
// property is set to the total length with type INCR and the chunks
// follow as the requestor deletes it, so property changes on the
// requestor's window are selected until the transfer ends.
func (a *App) startIncr(req *xcbSelectionRequestEvent, prop uint32, data []byte) {
	x := a.x
	if a.incrMask == nil {
		a.incrMask = incrMask{}
	}
	if a.incrMask.add(req.Requestor) {
		// Property changes are selected once per window, however many
		// transfers it has, and the destroy notify tells us to give up on
		// a requestor that exits in the middle of one.
		mask := [1]uint32{xcbEventMaskProperty | xcbEventMaskStructure}
		x.changeWindowAttrs(a.conn, req.Requestor, xcbCWEventMask, &mask[0])
	}
	total := [1]uint32{uint32(len(data))}
	x.changeProperty(a.conn, xcbPropModeReplace, req.Requestor, prop, a.atomIncr, 32, 1, unsafe.Pointer(&total[0]))
	before := len(a.incr)
	a.incr = putIncr(a.incr, incrSend{requestor: req.Requestor, property: prop, target: req.Target,
		data: data, touched: time.Now()})
	if len(a.incr) == before {
		// The new transfer replaced one for the same property, so the
		// count this window holds has not gone up after all.
		a.incrMask.remove(req.Requestor)
	}
	a.sendSelectionNotify(req, prop)
}

// endIncr forgets one transfer and stops watching its requestor when it
// was the last.
func (a *App) endIncr(i int) {
	win := a.incr[i].requestor
	a.incr = append(a.incr[:i], a.incr[i+1:]...)
	if a.incrMask.remove(win) {
		var none [1]uint32
		a.x.changeWindowAttrs(a.conn, win, xcbCWEventMask, &none[0])
	}
}

// sweepIncr abandons transfers whose requestor has stopped asking. It
// runs once per poll, which is often enough for a timeout counted in
// seconds.
func (a *App) sweepIncr(now time.Time) {
	if len(a.incr) == 0 {
		return
	}
	kept, stale := dropStaleIncr(a.incr, now, incrIdle)
	if len(stale) == 0 {
		return
	}
	a.incr = kept
	for _, t := range stale {
		if a.incrMask.remove(t.requestor) {
			var none [1]uint32
			a.x.changeWindowAttrs(a.conn, t.requestor, xcbCWEventMask, &none[0])
		}
	}
	a.x.flush(a.conn)
}

// forgetRequestor drops every transfer to a window that has gone away.
func (a *App) forgetRequestor(win uint32) {
	for i := len(a.incr) - 1; i >= 0; i-- {
		if a.incr[i].requestor == win {
			a.incr = append(a.incr[:i], a.incr[i+1:]...)
			a.incrMask.remove(win) // the window is gone; nothing to unselect
		}
	}
}

// sendIncrChunk writes the next chunk of a transfer in progress. An empty
// chunk ends it, and the transfer is forgotten.
func (a *App) sendIncrChunk(win, prop uint32) {
	x := a.x
	for i := range a.incr {
		t := &a.incr[i]
		if t.requestor != win || t.property != prop {
			continue
		}
		n := min(len(t.data)-t.sent, a.clipChunk)
		part := t.data[t.sent : t.sent+n]
		x.changeProperty(a.conn, xcbPropModeReplace, win, prop, t.target, 8, uint32(n), bytesPointer(part))
		x.flush(a.conn)
		t.sent += n
		t.touched = time.Now()
		if n == 0 {
			a.endIncr(i) // the empty chunk ended it
		}
		return
	}
}

// sendSelectionNotify tells a requestor where the answer is, or that
// there is none when property is zero.
func (a *App) sendSelectionNotify(req *xcbSelectionRequestEvent, property uint32) {
	var buf [32]byte
	n := (*xcbSelectionNotifyEvent)(unsafe.Pointer(&buf[0]))
	n.ResponseType = xcbSelectionNotify
	n.Time, n.Requestor, n.Selection = req.Time, req.Requestor, req.Selection
	n.Target, n.Property = req.Target, property
	a.x.sendEvent(a.conn, 0, req.Requestor, 0, &buf[0])
	a.x.flush(a.conn)
}

// emptyProperty stands in for the data of a zero-length property, which
// ends an INCR transfer. Nothing reads it; xcb is given an address rather
// than nothing at all.
var emptyProperty byte

// bytesPointer is the address of a byte slice's first element.
func bytesPointer(b []byte) unsafe.Pointer {
	if len(b) == 0 {
		return unsafe.Pointer(&emptyProperty)
	}
	return unsafe.Pointer(&b[0])
}

// Gamepads reads the Linux joystick devices; see gamepad_linux.go.

// closeX11 destroys the X11 window.
func (w *Window) closeX11() {
	if w.closed {
		return
	}
	w.closed = true
	delete(w.app.windows, w.id)
	w.app.x.destroyWindow(w.app.conn, w.id)
	w.app.x.flush(w.app.conn)
}

// setFullscreenX11 asks the window manager for _NET_WM_STATE_FULLSCREEN.
func (w *Window) setFullscreenX11(on bool) {
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

// setCursorCapturedX11 hides the cursor, confines it to the window and
// reports motion as deltas.
func (w *Window) setCursorCapturedX11(on bool) {
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
		if w.cursor != 0 {
			x.freeCursor(a.conn, w.cursor)
			w.cursor = 0
		}
		w.applyCursorX11() // back to the shape or hidden state in force
	}
	x.flush(a.conn)
}
