package platform

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"os"
	"strings"
	"structs"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/matjam/bunyip/input"
)

// The Wayland window layer talks to the compositor through
// libwayland-client, which every Wayland session provides. libwayland
// exports the core protocol's interface tables as symbols; the xdg-shell,
// xdg-decoration, relative-pointer and pointer-constraints tables are built
// in wayland_proto_linux.go because wayland-protocols ships only XML.
//
// libwayland 1.20 or later is required, for wl_proxy_marshal_flags. Text
// input and key repeat come from xkbcommon and cursor shapes from
// libwayland-cursor; both are optional, and without them the layer reports
// key codes only and leaves the pointer as the compositor drew it.

// Flags for wl_proxy_marshal_flags.
const wlMarshalFlagDestroy = 1 << 0

// Interface versions this layer implements. A global is bound at the lowest
// of the compositor's version, the local libwayland's version and this cap,
// so the compositor never sends an event that either side cannot decode.
const (
	compositorVersionCap = 6 // wl_surface.preferred_buffer_scale
	seatVersionCap       = 8 // wl_pointer.axis_value120
	outputVersionCap     = 4
	shmVersionCap        = 1
	wmBaseVersionCap     = 7
	dataDeviceVersionCap = 3 // wl_data_source.set_actions
)

// evdev button codes, from input-event-codes.h.
const (
	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112
	btnSide   = 0x113
	btnExtra  = 0x114
)

const (
	pollIn  = 0x001
	pollOut = 0x004

	xkbKeymapFormatTextV1 = 1
	xkbStateModsEffective = 1 << 3
)

// ErrNoWayland is returned when no Wayland compositor can be reached.
var ErrNoWayland = errors.New("platform: cannot connect to a Wayland compositor")

// pollFD mirrors struct pollfd.
type pollFD struct {
	_       structs.HostLayout
	Fd      int32
	Events  int16
	Revents int16
}

// wllib holds the functions resolved from libwayland-client, libc and the
// optional libwayland-cursor and libxkbcommon.
type wllib struct {
	connect         func(name *byte) unsafe.Pointer
	disconnect      func(d unsafe.Pointer)
	getFD           func(d unsafe.Pointer) int32
	dispatch        func(d unsafe.Pointer) int32
	dispatchPending func(d unsafe.Pointer) int32
	flush           func(d unsafe.Pointer) int32
	roundtrip       func(d unsafe.Pointer) int32
	prepareRead     func(d unsafe.Pointer) int32
	readEvents      func(d unsafe.Pointer) int32
	cancelRead      func(d unsafe.Pointer)
	getError        func(d unsafe.Pointer) int32

	// wl_proxy_marshal_flags is variadic, so it is registered once per
	// argument count. Every argument is an integer or a pointer, which is
	// what the System V and AArch64 variadic conventions expect here.
	marshal0 func(p unsafe.Pointer, opcode uint32, iface unsafe.Pointer, version, flags uint32) unsafe.Pointer
	marshal1 func(p unsafe.Pointer, opcode uint32, iface unsafe.Pointer, version, flags uint32, a1 uintptr) unsafe.Pointer
	marshal2 func(p unsafe.Pointer, opcode uint32, iface unsafe.Pointer, version, flags uint32, a1, a2 uintptr) unsafe.Pointer
	marshal3 func(p unsafe.Pointer, opcode uint32, iface unsafe.Pointer, version, flags uint32, a1, a2, a3 uintptr) unsafe.Pointer
	marshal4 func(p unsafe.Pointer, opcode uint32, iface unsafe.Pointer, version, flags uint32, a1, a2, a3, a4 uintptr) unsafe.Pointer
	marshal5 func(p unsafe.Pointer, opcode uint32, iface unsafe.Pointer, version, flags uint32, a1, a2, a3, a4, a5 uintptr) unsafe.Pointer
	marshal6 func(p unsafe.Pointer, opcode uint32, iface unsafe.Pointer, version, flags uint32, a1, a2, a3, a4, a5, a6 uintptr) unsafe.Pointer

	addListener func(p unsafe.Pointer, impl unsafe.Pointer, data unsafe.Pointer) int32
	destroy     func(p unsafe.Pointer)
	getVersion  func(p unsafe.Pointer) uint32

	// libc.
	calloc func(n, size uintptr) unsafe.Pointer
	cfree  func(p unsafe.Pointer)
	poll   func(fds *pollFD, nfds uint64, timeout int32) int32
	errno  func() *int32

	// libwayland-cursor, optional.
	cursorThemeLoad      func(name *byte, size int32, shm unsafe.Pointer) unsafe.Pointer
	cursorThemeDestroy   func(theme unsafe.Pointer)
	cursorThemeGetCursor func(theme unsafe.Pointer, name *byte) *wlCursor
	cursorImageGetBuffer func(image *wlCursorImage) unsafe.Pointer

	// libxkbcommon, optional.
	xkbContextNew      func(flags int32) unsafe.Pointer
	xkbContextUnref    func(ctx unsafe.Pointer)
	xkbKeymapFromStr   func(ctx unsafe.Pointer, s *byte, format, flags uint32) unsafe.Pointer
	xkbKeymapUnref     func(km unsafe.Pointer)
	xkbKeymapRepeats   func(km unsafe.Pointer, key uint32) int32
	xkbStateNew        func(km unsafe.Pointer) unsafe.Pointer
	xkbStateUnref      func(st unsafe.Pointer)
	xkbStateUpdateMask func(st unsafe.Pointer, depressed, latched, locked, depGroup, latGroup, lockGroup uint32) uint32
	xkbStateKeyGetUTF8 func(st unsafe.Pointer, key uint32, buf *byte, size uintptr) int32
	xkbModActive       func(st unsafe.Pointer, name *byte, component uint32) int32
}

// wlCursor mirrors struct wl_cursor from libwayland-cursor.
type wlCursor struct {
	_          structs.HostLayout
	ImageCount uint32
	Images     **wlCursorImage
	Name       *byte
}

// wlCursorImage mirrors struct wl_cursor_image.
type wlCursorImage struct {
	_        structs.HostLayout
	Width    uint32
	Height   uint32
	HotspotX uint32
	HotspotY uint32
	Delay    uint32
}

// wlOutput is one display the compositor advertised.
type wlOutput struct {
	proxy       unsafe.Pointer
	name        uint32
	scale       int
	description string
	current     VideoMode
	modes       []VideoMode
}

// wlApp is the Wayland half of App. Exactly one exists per process, in
// wlCurrent, because the listener callbacks are C function pointers with no
// room for a Go receiver.
type wlApp struct {
	out *App
	l   *wllib

	display  unsafe.Pointer
	registry unsafe.Pointer
	iface    map[string]*wlInterface

	compositor     unsafe.Pointer
	compositorVer  uint32
	shm            unsafe.Pointer
	seat           unsafe.Pointer
	seatVer        uint32
	wmBase         unsafe.Pointer
	decorManager   unsafe.Pointer
	relPointerMgr  unsafe.Pointer
	ptrConstraints unsafe.Pointer
	dataMgr        unsafe.Pointer
	dataMgrVer     uint32
	fracScaleMgr   unsafe.Pointer
	viewporter     unsafe.Pointer
	iconMgr        unsafe.Pointer

	// The clipboard. dataDevice is the seat's; clipSource is the source
	// this process offered and clipText what it offers, so a read of our
	// own selection needs no pipe. selection is the offer the compositor
	// last handed over and offerMimes the types each live offer carries;
	// newOffer holds an offer the compositor has made but not yet used.
	dataDevice unsafe.Pointer
	clipSource unsafe.Pointer
	clipText   string
	selection  unsafe.Pointer
	newOffer   unsafe.Pointer
	offerMimes map[unsafe.Pointer][]string
	// lastSerial is the most recent serial from a key or button, which is
	// what wl_data_device.set_selection has to quote.
	lastSerial uint32

	keyboard unsafe.Pointer
	pointer  unsafe.Pointer
	outputs  map[unsafe.Pointer]*wlOutput
	// owner maps every proxy a window owns, its wl_surface included, back to
	// the window, because an event arrives with only the proxy it is for.
	owner map[unsafe.Pointer]*wlWindow
	wins  []*wlWindow

	cursorTheme     unsafe.Pointer
	cursorThemeSize int32

	// Keyboard state.
	xkbCtx, xkbKeymap, xkbState unsafe.Pointer
	mods                        Mods
	repeatRate                  int32 // keys per second; zero disables repeat
	repeatDelay                 int32 // milliseconds before the first repeat
	repeatKey                   uint32
	repeatDue                   time.Time
	repeatWindow                *wlWindow

	// Pointer state, accumulated between wl_pointer.frame events.
	focus        *wlWindow // the surface the pointer is over
	kbFocus      *wlWindow
	enterSerial  uint32
	axisX, axisY float64 // pending scroll in points
	stepX, stepY float64 // pending scroll in lines
	haveAxis     bool
	haveStep     bool

	wakeR, wakeW int
	listeners    map[unsafe.Pointer]unsafe.Pointer // proxy to its C listener array
}

// wlCurrent is the process's Wayland app. The listener callbacks are made
// once and find their app here.
var wlCurrent *wlApp

// wlWindow is the Wayland half of Window.
type wlWindow struct {
	app *wlApp
	out *Window

	surface     unsafe.Pointer
	xdgSurface  unsafe.Pointer
	xdgToplevel unsafe.Pointer
	decoration  unsafe.Pointer

	width, height int // content size in points
	pendW, pendH  int // the size the last xdg_toplevel.configure asked for
	defW, defH    int // the size Config asked for, used when the compositor leaves it to us

	scale          int // integer buffer scale in force
	preferredScale int // wl_surface.preferred_buffer_scale, or zero
	onOutputs      map[unsafe.Pointer]bool

	// Fractional scale. fracScale is wp_fractional_scale_v1's preferred
	// scale in 120ths, or zero where the compositor does not offer the
	// protocol; the viewport is what maps the buffer back onto the
	// window's logical size.
	fracScale int
	fracObj   unsafe.Pointer
	viewport  unsafe.Pointer

	configured bool
	closed     bool

	fullscreen, pendFullscreen bool
	maximized, pendMaximized   bool
	activated, pendActivated   bool
	pendSuspended              bool
	visible                    bool // false only while the compositor suspends the window

	resizable              bool
	minW, minH, maxW, maxH int

	title *byte // C strings owned by this window
	appID *byte

	cursorHidden                          bool
	shape                                 CursorShape
	cursorSurface                         unsafe.Pointer
	cursorScale                           int
	customCursor                          unsafe.Pointer
	customWidth, customHeight, hotX, hotY int

	icon       unsafe.Pointer // the xdg_toplevel_icon_v1 in force, or nil
	iconBuffer unsafe.Pointer // the shm buffer it holds, kept alive with it

	captured     bool
	lockedPtr    unsafe.Pointer
	relPointer   unsafe.Pointer
	mouseX       float64
	mouseY       float64
	serverDecors bool

	inputRect textRect
}

// waylandAvailable reports whether a Wayland socket is worth trying. It
// looks at WAYLAND_DISPLAY first, then the default socket under
// XDG_RUNTIME_DIR, so a session that sets neither falls straight to X11.
func waylandAvailable() bool {
	if d := os.Getenv("WAYLAND_DISPLAY"); d != "" {
		return true
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return false
	}
	_, err := os.Stat(dir + "/wayland-0")
	return err == nil
}

// loadWayland opens libwayland-client and resolves everything this layer
// calls, including the core protocol's exported interface tables.
func loadWayland() (*wllib, map[string]*wlInterface, error) {
	lib, err := purego.Dlopen("libwayland-client.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, nil, fmt.Errorf("platform: libwayland-client: %w", err)
	}
	libc, err := purego.Dlopen("libc.so.6", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, nil, fmt.Errorf("platform: libc: %w", err)
	}
	l := &wllib{}
	for name, fptr := range map[string]any{
		"wl_display_connect": &l.connect, "wl_display_disconnect": &l.disconnect,
		"wl_display_get_fd": &l.getFD, "wl_display_dispatch": &l.dispatch,
		"wl_display_dispatch_pending": &l.dispatchPending, "wl_display_flush": &l.flush,
		"wl_display_roundtrip": &l.roundtrip, "wl_display_prepare_read": &l.prepareRead,
		"wl_display_read_events": &l.readEvents, "wl_display_cancel_read": &l.cancelRead,
		"wl_display_get_error":  &l.getError,
		"wl_proxy_add_listener": &l.addListener, "wl_proxy_destroy": &l.destroy,
		"wl_proxy_get_version": &l.getVersion,
	} {
		if err := load(lib, name, fptr); err != nil {
			return nil, nil, err
		}
	}
	// wl_proxy_marshal_flags arrived in libwayland 1.20. Older libraries
	// need wl_proxy_marshal_constructor_versioned, which takes its arguments
	// in a different order; rather than carry two paths, say what is needed.
	sym, err := purego.Dlsym(lib, "wl_proxy_marshal_flags")
	if err != nil {
		return nil, nil, fmt.Errorf("platform: libwayland-client is older than 1.20 (no wl_proxy_marshal_flags)")
	}
	for _, fptr := range []any{&l.marshal0, &l.marshal1, &l.marshal2, &l.marshal3, &l.marshal4, &l.marshal5, &l.marshal6} {
		purego.RegisterFunc(fptr, sym)
	}
	for name, fptr := range map[string]any{
		"calloc": &l.calloc, "free": &l.cfree, "poll": &l.poll, "__errno_location": &l.errno,
	} {
		if err := load(libc, name, fptr); err != nil {
			return nil, nil, err
		}
	}
	// The core protocol's interface tables are data symbols, not functions.
	extern := map[string]*wlInterface{}
	for _, name := range []string{
		"wl_display", "wl_registry", "wl_callback", "wl_compositor", "wl_surface", "wl_region",
		"wl_seat", "wl_keyboard", "wl_pointer", "wl_output", "wl_shm", "wl_shm_pool", "wl_buffer",
		"wl_data_device_manager", "wl_data_device", "wl_data_source", "wl_data_offer",
	} {
		sym, err := purego.Dlsym(lib, name+"_interface")
		if err != nil {
			return nil, nil, fmt.Errorf("platform: %s_interface: %w", name, err)
		}
		extern[name] = (*wlInterface)(symbolPointer(sym))
	}
	table := buildProtocols(func(n uintptr) unsafe.Pointer { return l.calloc(1, n) }, extern)

	// libwayland-cursor gives the pointer its standard shapes. Without it
	// the compositor's own cursor stays as it is.
	if cur, err := purego.Dlopen("libwayland-cursor.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
		ok := true
		for name, fptr := range map[string]any{
			"wl_cursor_theme_load": &l.cursorThemeLoad, "wl_cursor_theme_destroy": &l.cursorThemeDestroy,
			"wl_cursor_theme_get_cursor": &l.cursorThemeGetCursor, "wl_cursor_image_get_buffer": &l.cursorImageGetBuffer,
		} {
			ok = ok && load(cur, name, fptr) == nil
		}
		if !ok {
			l.cursorThemeLoad = nil
		}
	}
	// xkbcommon turns key codes into text and says which keys repeat.
	if xkb, err := purego.Dlopen("libxkbcommon.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
		ok := true
		for name, fptr := range map[string]any{
			"xkb_context_new": &l.xkbContextNew, "xkb_context_unref": &l.xkbContextUnref,
			"xkb_keymap_new_from_string": &l.xkbKeymapFromStr, "xkb_keymap_unref": &l.xkbKeymapUnref,
			"xkb_keymap_key_repeats": &l.xkbKeymapRepeats, "xkb_state_new": &l.xkbStateNew,
			"xkb_state_unref": &l.xkbStateUnref, "xkb_state_update_mask": &l.xkbStateUpdateMask,
			"xkb_state_key_get_utf8": &l.xkbStateKeyGetUTF8, "xkb_state_mod_name_is_active": &l.xkbModActive,
		} {
			ok = ok && load(xkb, name, fptr) == nil
		}
		if !ok {
			l.xkbContextNew = nil
		}
	}
	return l, table, nil
}

// alloc returns zeroed C memory. Everything the protocol tables and the
// listener arrays hold lives there, because C keeps pointers into it.
func (l *wllib) alloc(n uintptr) unsafe.Pointer { return l.calloc(1, n) }

// cstring copies s into C memory with a terminating NUL.
func (l *wllib) cstring(s string) *byte {
	p := (*byte)(l.alloc(uintptr(len(s)) + 1))
	copy(unsafe.Slice(p, len(s)+1), s)
	return p
}

// symbolPointer turns the address purego.Dlsym returns into a pointer. The
// address names a data symbol in a library the process has loaded, so it is
// not memory the collector knows about and reading it back as a pointer is
// safe; the round trip through a variable is what keeps vet's unsafeptr
// check satisfied.
func symbolPointer(sym uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&sym))
}

// goString reads a NUL-terminated C string.
func goString(p *byte) string {
	if p == nil {
		return ""
	}
	var b []byte
	for i := 0; ; i++ {
		c := *(*byte)(unsafe.Add(unsafe.Pointer(p), i))
		if c == 0 {
			return string(b)
		}
		b = append(b, c)
	}
}

// marshal sends one request. iface is the interface of the object the
// request creates, or nil when it creates none, and the variadic arguments
// are the request's own, with a zero standing in for a new_id or a null
// object.
func (l *wllib) marshal(proxy unsafe.Pointer, opcode uint32, iface *wlInterface, flags uint32, args ...uintptr) unsafe.Pointer {
	v := l.getVersion(proxy)
	ip := unsafe.Pointer(iface)
	switch len(args) {
	case 0:
		return l.marshal0(proxy, opcode, ip, v, flags)
	case 1:
		return l.marshal1(proxy, opcode, ip, v, flags, args[0])
	case 2:
		return l.marshal2(proxy, opcode, ip, v, flags, args[0], args[1])
	case 3:
		return l.marshal3(proxy, opcode, ip, v, flags, args[0], args[1], args[2])
	case 4:
		return l.marshal4(proxy, opcode, ip, v, flags, args[0], args[1], args[2], args[3])
	case 5:
		return l.marshal5(proxy, opcode, ip, v, flags, args[0], args[1], args[2], args[3], args[4])
	case 6:
		return l.marshal6(proxy, opcode, ip, v, flags, args[0], args[1], args[2], args[3], args[4], args[5])
	}
	panic(fmt.Sprintf("platform: request %d has %d arguments, more than marshal sends", opcode, len(args)))
}

// send is marshal for a request that creates nothing.
func (l *wllib) send(proxy unsafe.Pointer, opcode uint32, args ...uintptr) {
	l.marshal(proxy, opcode, nil, 0, args...)
}

// fixed converts a wl_fixed_t, which is 24.8 signed fixed point.
func fixed(v int32) float64 { return float64(v) / 256 }

// listen attaches a listener to a proxy. The array is sized from the
// interface the local libwayland knows, so a handler this layer does not
// have, or an event a newer library added, lands on a callback that does
// nothing.
func (a *wlApp) listen(proxy unsafe.Pointer, iface *wlInterface, handlers []uintptr) {
	n := int(iface.EventCount)
	if n == 0 {
		return
	}
	impl := a.l.alloc(unsafe.Sizeof(uintptr(0)) * uintptr(n))
	slots := unsafe.Slice((*uintptr)(impl), n)
	for i := range slots {
		slots[i] = wlNoopCallback
		if i < len(handlers) && handlers[i] != 0 {
			slots[i] = handlers[i]
		}
	}
	a.l.addListener(proxy, impl, nil)
	a.listeners[proxy] = impl
}

// freeListener releases the listener array of a proxy that has been
// destroyed. libwayland reads the array only while the proxy is alive.
func (a *wlApp) freeListener(proxy unsafe.Pointer) {
	if impl, ok := a.listeners[proxy]; ok {
		delete(a.listeners, proxy)
		a.l.cfree(impl)
	}
}

// newWaylandApp connects to the compositor and binds the globals it needs.
func newWaylandApp(out *App) (*wlApp, error) {
	if wlCurrent != nil {
		return nil, errors.New("platform: the Wayland app already exists")
	}
	l, table, err := loadWayland()
	if err != nil {
		return nil, err
	}
	display := l.connect(nil)
	if display == nil {
		return nil, ErrNoWayland
	}
	a := &wlApp{
		out: out, l: l, display: display, iface: table,
		outputs: map[unsafe.Pointer]*wlOutput{}, owner: map[unsafe.Pointer]*wlWindow{},
		listeners: map[unsafe.Pointer]unsafe.Pointer{}, offerMimes: map[unsafe.Pointer][]string{},
		wakeR: -1, wakeW: -1,
	}
	wlCurrent = a
	wlInitCallbacks()

	var fds [2]int
	if err := syscall.Pipe2(fds[:], syscall.O_CLOEXEC|syscall.O_NONBLOCK); err != nil {
		a.close()
		return nil, fmt.Errorf("platform: wake pipe: %w", err)
	}
	a.wakeR, a.wakeW = fds[0], fds[1]

	// The xkb context has to exist before the registry round trips, because
	// binding the seat brings a wl_keyboard.keymap event with it.
	if l.xkbContextNew != nil {
		a.xkbCtx = l.xkbContextNew(0)
	}
	a.registry = l.marshal(display, opDisplayGetRegistry, table["wl_registry"], 0, 0)
	a.listen(a.registry, table["wl_registry"], []uintptr{cbRegistryGlobal, cbRegistryGlobalRemove})
	// The first round trip delivers the globals, the second the events the
	// bound globals send straight away, such as wl_seat.capabilities and
	// wl_output.scale.
	l.roundtrip(display)
	l.roundtrip(display)
	if a.compositor == nil || a.wmBase == nil {
		a.close()
		return nil, fmt.Errorf("platform: the compositor has no %s", missingGlobal(a))
	}
	// The seat and the data device manager arrive as globals in whichever
	// order the compositor lists them, so the clipboard's data device is
	// made once both are in hand. A third round trip brings the selection
	// the compositor is already holding.
	if a.dataMgr != nil && a.seat != nil {
		iface := table["wl_data_device"]
		a.dataDevice = l.marshal(a.dataMgr, opDataDeviceManagerGetDevice, iface, 0, 0, uintptr(a.seat))
		a.listen(a.dataDevice, iface, []uintptr{
			cbDataDeviceDataOffer, cbDataDeviceEnter, cbDataDeviceLeave,
			cbDataDeviceMotion, cbDataDeviceDrop, cbDataDeviceSelection,
		})
		l.roundtrip(display)
	}
	return a, nil
}

func missingGlobal(a *wlApp) string {
	if a.compositor == nil {
		return "wl_compositor"
	}
	return "xdg_wm_base"
}

// bindVersion is the version to bind a global at: the lowest of what the
// compositor offers, what the local libwayland can decode and what this
// layer implements.
func (a *wlApp) bindVersion(advertised uint32, iface *wlInterface, limit uint32) uint32 {
	v := advertised
	if u := uint32(iface.Version); u < v {
		v = u
	}
	if limit < v {
		v = limit
	}
	return v
}

// bind binds a registry global. It does not go through marshal, because
// wl_registry.bind is the one request whose version argument is the version
// to create the new proxy at rather than the version of the proxy the
// request is sent to.
func (a *wlApp) bind(name uint32, iface *wlInterface, version uint32) unsafe.Pointer {
	return a.l.marshal4(a.registry, opRegistryBind, unsafe.Pointer(iface), version, 0,
		uintptr(name), uintptr(unsafe.Pointer(iface.Name)), uintptr(version), 0)
}

// close tears the connection down.
func (a *wlApp) close() {
	l := a.l
	a.destroySource()
	a.dropOffer(a.selection)
	a.dropOffer(a.newOffer)
	if a.dataDevice != nil {
		// release is the destructor from version two; an older device is
		// only destroyed locally, which is all its version allows.
		if a.dataMgrVer >= 2 {
			l.marshal(a.dataDevice, opDataDeviceRelease, nil, wlMarshalFlagDestroy)
		} else {
			l.destroy(a.dataDevice)
		}
		a.freeListener(a.dataDevice)
		a.dataDevice = nil
	}
	if a.xkbState != nil {
		l.xkbStateUnref(a.xkbState)
	}
	if a.xkbKeymap != nil {
		l.xkbKeymapUnref(a.xkbKeymap)
	}
	if a.xkbCtx != nil {
		l.xkbContextUnref(a.xkbCtx)
	}
	if a.cursorTheme != nil && l.cursorThemeDestroy != nil {
		l.cursorThemeDestroy(a.cursorTheme)
	}
	if a.wakeR >= 0 {
		syscall.Close(a.wakeR)
	}
	if a.wakeW >= 0 {
		syscall.Close(a.wakeW)
	}
	if a.display != nil {
		l.disconnect(a.display)
	}
	a.display = nil
	if wlCurrent == a {
		wlCurrent = nil
	}
}

func (a *wlApp) push(e Event) { a.out.push(e) }

// --- registry ---

func (a *wlApp) onGlobal(name uint32, ifaceName string, version uint32) {
	switch ifaceName {
	case "wl_compositor":
		iface := a.iface["wl_compositor"]
		a.compositorVer = a.bindVersion(version, iface, compositorVersionCap)
		a.compositor = a.bind(name, iface, a.compositorVer)
	case "wl_shm":
		iface := a.iface["wl_shm"]
		a.shm = a.bind(name, iface, a.bindVersion(version, iface, shmVersionCap))
	case "wl_seat":
		if a.seat != nil {
			return // one seat is enough; the rest are other users of the session
		}
		iface := a.iface["wl_seat"]
		a.seatVer = a.bindVersion(version, iface, seatVersionCap)
		a.seat = a.bind(name, iface, a.seatVer)
		a.listen(a.seat, iface, []uintptr{cbSeatCapabilities, cbSeatName})
	case "wl_output":
		iface := a.iface["wl_output"]
		proxy := a.bind(name, iface, a.bindVersion(version, iface, outputVersionCap))
		a.outputs[proxy] = &wlOutput{proxy: proxy, name: name, scale: 1}
		a.listen(proxy, iface, []uintptr{cbOutputGeometry, cbOutputMode, cbOutputDone, cbOutputScale, cbOutputName, cbOutputDescription})
	case "xdg_wm_base":
		iface := a.iface["xdg_wm_base"]
		a.wmBase = a.bind(name, iface, a.bindVersion(version, iface, wmBaseVersionCap))
		a.listen(a.wmBase, iface, []uintptr{cbWMBasePing})
	case "wl_data_device_manager":
		iface := a.iface["wl_data_device_manager"]
		a.dataMgrVer = a.bindVersion(version, iface, dataDeviceVersionCap)
		a.dataMgr = a.bind(name, iface, a.dataMgrVer)
	case "zxdg_decoration_manager_v1":
		iface := a.iface["zxdg_decoration_manager_v1"]
		a.decorManager = a.bind(name, iface, 1)
	case "xdg_toplevel_icon_manager_v1":
		iface := a.iface["xdg_toplevel_icon_manager_v1"]
		a.iconMgr = a.bind(name, iface, 1)
		// The manager lists the icon sizes it likes and says when the
		// list ends; the layer sends whatever image the game gave it, so
		// neither event needs a handler.
		a.listen(a.iconMgr, iface, nil)
	case "wp_fractional_scale_manager_v1":
		a.fracScaleMgr = a.bind(name, a.iface["wp_fractional_scale_manager_v1"], 1)
	case "wp_viewporter":
		a.viewporter = a.bind(name, a.iface["wp_viewporter"], 1)
	case "zwp_relative_pointer_manager_v1":
		a.relPointerMgr = a.bind(name, a.iface["zwp_relative_pointer_manager_v1"], 1)
	case "zwp_pointer_constraints_v1":
		a.ptrConstraints = a.bind(name, a.iface["zwp_pointer_constraints_v1"], 1)
	}
}

func (a *wlApp) onGlobalRemove(name uint32) {
	for proxy, o := range a.outputs {
		if o.name != name {
			continue
		}
		delete(a.outputs, proxy)
		for _, w := range a.wins {
			if w.onOutputs[proxy] {
				delete(w.onOutputs, proxy)
				w.refreshScale()
			}
		}
		a.l.destroy(proxy)
		return
	}
}

// --- seat ---

func (a *wlApp) onSeatCapabilities(caps uint32) {
	const (
		capPointer  = 1
		capKeyboard = 2
	)
	l := a.l
	if caps&capPointer != 0 && a.pointer == nil {
		iface := a.iface["wl_pointer"]
		a.pointer = l.marshal(a.seat, opSeatGetPointer, iface, 0, 0)
		a.listen(a.pointer, iface, []uintptr{
			cbPointerEnter, cbPointerLeave, cbPointerMotion, cbPointerButton, cbPointerAxis,
			cbPointerFrame, cbPointerAxisSource, cbPointerAxisStop, cbPointerAxisDiscrete,
			cbPointerAxisValue120, cbPointerAxisRelDir, cbPointerWarp,
		})
	} else if caps&capPointer == 0 && a.pointer != nil {
		a.releasePointer()
	}
	if caps&capKeyboard != 0 && a.keyboard == nil {
		iface := a.iface["wl_keyboard"]
		a.keyboard = l.marshal(a.seat, opSeatGetKeyboard, iface, 0, 0)
		a.listen(a.keyboard, iface, []uintptr{
			cbKeyboardKeymap, cbKeyboardEnter, cbKeyboardLeave, cbKeyboardKey,
			cbKeyboardModifiers, cbKeyboardRepeatInfo,
		})
	} else if caps&capKeyboard == 0 && a.keyboard != nil {
		if a.seatVer >= 3 {
			l.marshal(a.keyboard, opKeyboardRelease, nil, wlMarshalFlagDestroy)
		} else {
			l.destroy(a.keyboard)
		}
		a.keyboard = nil
		a.repeatKey = 0
	}
}

func (a *wlApp) releasePointer() {
	if a.pointer == nil {
		return
	}
	if a.seatVer >= 3 {
		a.l.marshal(a.pointer, opPointerRelease, nil, wlMarshalFlagDestroy)
	} else {
		a.l.destroy(a.pointer)
	}
	a.pointer = nil
	a.focus = nil
}

// --- keyboard ---

func (a *wlApp) onKeymap(format uint32, fd int32, size uint32) {
	defer syscall.Close(int(fd))
	l := a.l
	if a.xkbCtx == nil || format != xkbKeymapFormatTextV1 || size == 0 {
		return
	}
	data, err := syscall.Mmap(int(fd), 0, int(size), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		return
	}
	defer syscall.Munmap(data)
	if data[len(data)-1] != 0 {
		return // the protocol says the keymap is NUL terminated
	}
	km := l.xkbKeymapFromStr(a.xkbCtx, &data[0], xkbKeymapFormatTextV1, 0)
	if km == nil {
		return
	}
	st := l.xkbStateNew(km)
	if st == nil {
		l.xkbKeymapUnref(km)
		return
	}
	if a.xkbState != nil {
		l.xkbStateUnref(a.xkbState)
	}
	if a.xkbKeymap != nil {
		l.xkbKeymapUnref(a.xkbKeymap)
	}
	a.xkbKeymap, a.xkbState = km, st
}

// modNames are the xkbcommon modifier names, allocated once because they
// are passed to C on every modifiers event.
type modName struct {
	c *byte
	m Mods
}

var wlModNames []modName

func (a *wlApp) onModifiers(depressed, latched, locked, group uint32) {
	l := a.l
	if a.xkbState == nil {
		return
	}
	l.xkbStateUpdateMask(a.xkbState, depressed, latched, locked, 0, 0, group)
	if wlModNames == nil {
		for _, m := range []struct {
			name string
			mod  Mods
		}{
			{"Shift", input.ModShift}, {"Control", input.ModControl}, {"Mod1", input.ModAlt},
			{"Mod4", input.ModSuper}, {"Lock", input.ModCapsLock}, {"Mod2", input.ModNumLock},
		} {
			wlModNames = append(wlModNames, modName{c: l.cstring(m.name), m: m.mod})
		}
	}
	var mods Mods
	for _, m := range wlModNames {
		if l.xkbModActive(a.xkbState, m.c, xkbStateModsEffective) == 1 {
			mods |= m.m
		}
	}
	a.mods = mods
	// Wayland sends modifiers after the key event that changed them, and
	// also permits updates without keyboard focus (for pointer input).
	w := a.kbFocus
	if w == nil {
		w = a.focus
	}
	e := Event{Kind: EventModifiers, Mods: mods}
	if w != nil {
		e.Window = w.out
	}
	a.push(e)
}

func (a *wlApp) onKeyboardLeave(w *wlWindow) {
	if a.kbFocus == w {
		a.kbFocus = nil
		a.mods = 0
		if a.xkbState != nil {
			a.l.xkbStateUpdateMask(a.xkbState, 0, 0, 0, 0, 0, 0)
		}
		// Leave resets the keyboard's logical state. A configure may have
		// already reported focus loss, so explicitly clear modifiers too.
		a.push(Event{Kind: EventModifiers, Window: w.out})
	}
	a.repeatWindow, a.repeatKey = nil, 0
	w.setFocused(false)
}

// onKey turns one wl_keyboard.key event into events. code is the evdev key
// code; X11 key codes, and so keys_linux.go, are that plus eight.
func (a *wlApp) onKey(code, state uint32) {
	w := a.kbFocus
	if w == nil {
		return
	}
	key := keyFromX11(uint8(min(code+8, 255)))
	if state == 0 {
		if a.repeatKey == code {
			a.repeatKey = 0
		}
		a.push(Event{Kind: EventKeyUp, Window: w.out, Key: key, Mods: a.mods})
		return
	}
	a.push(Event{Kind: EventKeyDown, Window: w.out, Key: key, Mods: a.mods})
	a.pushText(w, code)
	if a.repeatRate > 0 && a.repeats(code) {
		a.repeatKey, a.repeatWindow = code, w
		a.repeatDue = time.Now().Add(time.Duration(a.repeatDelay) * time.Millisecond)
	}
}

// repeats says whether the keymap marks a key as repeating. Without
// xkbcommon every key repeats, which is what the compositor assumes.
func (a *wlApp) repeats(code uint32) bool {
	if a.xkbKeymap == nil || a.l.xkbKeymapRepeats == nil {
		return true
	}
	return a.l.xkbKeymapRepeats(a.xkbKeymap, code+8) == 1
}

// pushText emits the characters a key produces. Wayland sends no text of
// its own, so the layer asks xkbcommon.
func (a *wlApp) pushText(w *wlWindow, code uint32) {
	if a.xkbState == nil || a.mods&(input.ModControl|input.ModSuper) != 0 {
		return
	}
	var buf [16]byte
	n := a.l.xkbStateKeyGetUTF8(a.xkbState, code+8, &buf[0], uintptr(len(buf)))
	for _, r := range string(buf[:max(min(int(n), len(buf)-1), 0)]) {
		if r >= ' ' || r == '\t' || r == '\n' || r == '\r' {
			a.push(Event{Kind: EventChar, Window: w.out, Rune: r, Mods: a.mods})
		}
	}
}

// pumpRepeats emits the key repeats that have come due. Wayland sends no
// repeat events, only the rate and the delay, so the layer makes them.
func (a *wlApp) pumpRepeats(now time.Time) {
	if a.repeatKey == 0 || a.repeatRate <= 0 || a.repeatWindow == nil {
		return
	}
	interval := time.Second / time.Duration(a.repeatRate)
	if interval <= 0 {
		interval = time.Millisecond
	}
	for n := 0; !now.Before(a.repeatDue); n++ {
		if n == 8 {
			// A frame that took a long time should not deliver a burst of
			// backlog; start counting again from now.
			a.repeatDue = now.Add(interval)
			break
		}
		key := keyFromX11(uint8(min(a.repeatKey+8, 255)))
		a.push(Event{Kind: EventKeyDown, Window: a.repeatWindow.out, Key: key, Mods: a.mods, Repeat: true})
		a.pushText(a.repeatWindow, a.repeatKey)
		a.repeatDue = a.repeatDue.Add(interval)
	}
}

// repeatWait is how long Poll may block before the next repeat is due, in
// milliseconds, or -1 when no key is repeating.
func (a *wlApp) repeatWait(now time.Time) int32 {
	if a.repeatKey == 0 || a.repeatRate <= 0 {
		return -1
	}
	d := a.repeatDue.Sub(now)
	if d < 0 {
		return 0
	}
	return int32(d/time.Millisecond) + 1
}

// --- pointer ---

func (a *wlApp) onPointerEnter(serial uint32, surface unsafe.Pointer, sx, sy int32) {
	w := a.owner[surface]
	if w == nil {
		return
	}
	a.focus, a.enterSerial = w, serial
	w.mouseX, w.mouseY = fixed(sx), fixed(sy)
	w.applyCursor()
	a.push(Event{Kind: EventMouseEnter, Window: w.out})
	a.push(Event{Kind: EventMouseMove, Window: w.out, X: w.mouseX, Y: w.mouseY, Mods: a.mods})
}

func (a *wlApp) onPointerLeave(surface unsafe.Pointer) {
	w := a.owner[surface]
	if w == nil {
		return
	}
	if a.focus == w {
		a.focus = nil
	}
	a.push(Event{Kind: EventMouseLeave, Window: w.out})
}

func (a *wlApp) onPointerMotion(sx, sy int32) {
	w := a.focus
	if w == nil {
		return
	}
	x, y := fixed(sx), fixed(sy)
	if w.captured {
		// With the pointer locked there is no absolute motion; without the
		// lock, report the movement as a delta and leave the position where
		// capture began.
		if w.lockedPtr != nil {
			return
		}
		a.push(Event{Kind: EventMouseMove, Window: w.out, X: w.mouseX, Y: w.mouseY, DX: x - w.mouseX, DY: y - w.mouseY, Mods: a.mods})
		return
	}
	dx, dy := x-w.mouseX, y-w.mouseY
	w.mouseX, w.mouseY = x, y
	a.push(Event{Kind: EventMouseMove, Window: w.out, X: x, Y: y, DX: dx, DY: dy, Mods: a.mods})
}

func (a *wlApp) onRelativeMotion(dx, dy int32) {
	w := a.focus
	if w == nil || !w.captured {
		return
	}
	a.push(Event{Kind: EventMouseMove, Window: w.out, X: w.mouseX, Y: w.mouseY, DX: fixed(dx), DY: fixed(dy), Mods: a.mods})
}

func (a *wlApp) onPointerButton(button, state uint32) {
	w := a.focus
	if w == nil {
		return
	}
	var b MouseButton
	switch button {
	case btnLeft:
		b = input.MouseLeft
	case btnRight:
		b = input.MouseRight
	case btnMiddle:
		b = input.MouseMiddle
	case btnSide:
		b = input.MouseButton4
	case btnExtra:
		b = input.MouseButton5
	default:
		return
	}
	kind := EventMouseUp
	if state == 1 {
		kind = EventMouseDown
	}
	a.push(Event{Kind: kind, Window: w.out, Button: b, X: w.mouseX, Y: w.mouseY, Mods: a.mods})
}

// onAxis records a smooth scroll. The sign is flipped on the vertical axis
// because Wayland counts down as positive and the Event counts up.
func (a *wlApp) onAxis(axis uint32, value int32) {
	if axis == 0 {
		a.axisY -= fixed(value)
	} else {
		a.axisX += fixed(value)
	}
	a.haveAxis = true
	if a.seatVer < 5 {
		a.flushAxis() // without wl_pointer.frame there is nothing to batch
	}
}

// onAxisSteps records a notched scroll, from axis_discrete (version five) or
// axis_value120 (version eight), in whole lines.
func (a *wlApp) onAxisSteps(axis uint32, lines float64) {
	if axis == 0 {
		a.stepY -= lines
	} else {
		a.stepX += lines
	}
	a.haveStep = true
}

// flushAxis emits the scroll gathered since the last frame. Notched steps
// win over the smooth value, because a wheel sends both.
func (a *wlApp) flushAxis() {
	w := a.focus
	if w != nil {
		switch {
		case a.haveStep:
			a.push(Event{Kind: EventScroll, Window: w.out, DX: a.stepX, DY: a.stepY, Mods: a.mods})
		case a.haveAxis:
			a.push(Event{Kind: EventScroll, Window: w.out, DX: a.axisX, DY: a.axisY, Precise: true, Mods: a.mods})
		}
	}
	a.axisX, a.axisY, a.stepX, a.stepY = 0, 0, 0, 0
	a.haveAxis, a.haveStep = false, false
}

// --- windows ---

// newWindow opens a toplevel and waits for its first configure, so that the
// size and scale are settled before the renderer asks for them.
func (a *wlApp) newWindow(out *Window, cfg Config) (*wlWindow, error) {
	l := a.l
	w := &wlWindow{
		app: a, out: out,
		width: cfg.Width, height: cfg.Height, defW: cfg.Width, defH: cfg.Height,
		scale: 1, resizable: cfg.Resizable, onOutputs: map[unsafe.Pointer]bool{},
		cursorScale: 1, visible: true,
	}
	w.surface = l.marshal(a.compositor, opCompositorCreateSurface, a.iface["wl_surface"], 0, 0)
	if w.surface == nil {
		return nil, ErrNoWayland
	}
	a.wins = append(a.wins, w)
	a.owner[w.surface] = w
	a.listen(w.surface, a.iface["wl_surface"], []uintptr{
		cbSurfaceEnter, cbSurfaceLeave, cbSurfacePreferredScale, cbSurfacePreferredTransform,
	})
	// A fractional scale is only useful with a viewport to map the buffer
	// back onto the window's logical size, so both or neither.
	if a.fracScaleMgr != nil && a.viewporter != nil {
		w.viewport = l.marshal(a.viewporter, opViewporterGetViewport, a.iface["wp_viewport"], 0, 0, uintptr(w.surface))
		w.fracObj = l.marshal(a.fracScaleMgr, opFractionalScaleMgrGet, a.iface["wp_fractional_scale_v1"], 0, 0, uintptr(w.surface))
		a.owner[w.fracObj] = w
		a.listen(w.fracObj, a.iface["wp_fractional_scale_v1"], []uintptr{cbFractionalScale})
	}
	w.xdgSurface = l.marshal(a.wmBase, opXdgWMBaseGetXdgSurface, a.iface["xdg_surface"], 0, 0, uintptr(w.surface))
	a.owner[w.xdgSurface] = w
	a.listen(w.xdgSurface, a.iface["xdg_surface"], []uintptr{cbXdgSurfaceConfigure})
	w.xdgToplevel = l.marshal(w.xdgSurface, opXdgSurfaceGetToplevel, a.iface["xdg_toplevel"], 0, 0)
	a.owner[w.xdgToplevel] = w
	a.listen(w.xdgToplevel, a.iface["xdg_toplevel"], []uintptr{
		cbToplevelConfigure, cbToplevelClose, cbToplevelConfigureBounds, cbToplevelWMCapabilities,
	})
	w.setTitle(cfg.Title)
	w.appID = l.cstring(appID(cfg.Title))
	l.send(w.xdgToplevel, opXdgToplevelSetAppID, uintptr(unsafe.Pointer(w.appID)))
	if !cfg.Resizable {
		w.applySizeLimits(cfg.Width, cfg.Height, cfg.Width, cfg.Height)
	}
	// Server-side decorations if the compositor manages them. Without the
	// protocol the window has no title bar, because drawing one is the
	// client's job and this layer does not draw.
	if a.decorManager != nil {
		w.decoration = l.marshal(a.decorManager, opXdgDecorationManagerGetDecor,
			a.iface["zxdg_toplevel_decoration_v1"], 0, 0, uintptr(w.xdgToplevel))
		a.owner[w.decoration] = w
		a.listen(w.decoration, a.iface["zxdg_toplevel_decoration_v1"], []uintptr{cbDecorationConfigure})
		l.send(w.decoration, opXdgDecorationSetMode, uintptr(xdgDecorationModeServerSide))
	}
	// xdg-shell requires one commit with no buffer attached before the
	// first configure. The renderer attaches the first buffer through the
	// Vulkan swapchain, which is what maps the window.
	l.send(w.surface, opSurfaceCommit)
	for i := 0; i < 8 && !w.configured; i++ {
		if l.roundtrip(a.display) < 0 {
			break
		}
	}
	if !w.configured {
		w.destroy()
		return nil, fmt.Errorf("platform: the compositor never configured the window")
	}
	w.pushResize()
	return w, nil
}

// appID derives an xdg app id from the title. Compositors use it to group
// windows and to find a desktop entry.
func appID(title string) string {
	id := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return '-'
	}, title)
	id = strings.Trim(id, "-")
	if id == "" {
		id = "bunyip"
	}
	return id
}

func (w *wlWindow) setTitle(title string) {
	l := w.app.l
	if w.title != nil {
		l.cfree(unsafe.Pointer(w.title))
	}
	w.title = l.cstring(title)
	l.send(w.xdgToplevel, opXdgToplevelSetTitle, uintptr(unsafe.Pointer(w.title)))
}

func (w *wlWindow) applySizeLimits(minW, minH, maxW, maxH int) {
	l := w.app.l
	l.send(w.xdgToplevel, opXdgToplevelSetMinSize, uintptr(uint32(max(minW, 0))), uintptr(uint32(max(minH, 0))))
	l.send(w.xdgToplevel, opXdgToplevelSetMaxSize, uintptr(uint32(max(maxW, 0))), uintptr(uint32(max(maxH, 0))))
}

// onToplevelConfigure records what the compositor asked for. Nothing is
// applied until the matching xdg_surface.configure, which is the event that
// carries the serial to acknowledge.
func (w *wlWindow) onToplevelConfigure(width, height int32, states *wlArray) {
	w.pendW, w.pendH = int(width), int(height)
	w.pendFullscreen, w.pendMaximized, w.pendActivated, w.pendSuspended = false, false, false, false
	for _, s := range states.u32s() {
		switch s {
		case xdgToplevelStateFullscreen:
			w.pendFullscreen = true
		case xdgToplevelStateMaximized:
			w.pendMaximized = true
		case xdgToplevelStateActivated:
			w.pendActivated = true
		case xdgToplevelStateSuspended:
			w.pendSuspended = true
		}
	}
}

// onSurfaceConfigure acknowledges the configure and applies it.
func (w *wlWindow) onSurfaceConfigure(serial uint32) {
	a := w.app
	a.l.send(w.xdgSurface, opXdgSurfaceAckConfigure, uintptr(serial))
	width, height := w.pendW, w.pendH
	if width == 0 || height == 0 {
		// Zero means the size is the client's to pick. Keep what the window
		// already has, which starts as the size Config asked for.
		width, height = w.defW, w.defH
		if w.configured {
			width, height = w.width, w.height
		}
	}
	if !w.resizable && !w.pendFullscreen && !w.pendMaximized {
		// A fixed-size window keeps its size, except when the compositor
		// is showing it full screen or maximised, where the whole screen
		// is the engine's to letterbox as on the other platforms.
		width, height = w.defW, w.defH
	}
	sizeChanged := width != w.width || height != w.height
	w.width, w.height = width, height
	w.applyViewport()
	w.fullscreen, w.maximized = w.pendFullscreen, w.pendMaximized
	w.setFocused(w.pendActivated)
	w.setVisible(!w.pendSuspended)
	first := !w.configured
	w.configured = true
	if sizeChanged && !first {
		w.pushResize()
	}
}

// setFocused reports a change of keyboard focus. Two events say the window
// has it, the activated state in the configure and wl_keyboard.enter, so the
// flag is what makes one event out of them.
func (w *wlWindow) setFocused(on bool) {
	if on == w.activated {
		return
	}
	w.activated = on
	if !on {
		w.app.repeatKey = 0
	}
	w.app.push(Event{Kind: EventFocus, Window: w.out, Focused: on})
}

// setVisible reports a change in whether the window can be seen. The
// compositor says so with the suspended state of xdg_toplevel version
// six, which it sends when the window is minimised or wholly covered; a
// compositor that offers an earlier version never sends it, so the window
// stays visible.
func (w *wlWindow) setVisible(on bool) {
	if on == w.visible {
		return
	}
	w.visible = on
	w.app.push(Event{Kind: EventVisible, Window: w.out, Visible: on})
}

// pushResize reports the content and framebuffer size.
func (w *wlWindow) pushResize() {
	pw, ph := w.pixelSize()
	w.app.push(Event{Kind: EventResize, Window: w.out, Width: w.width, Height: w.height,
		PixelW: pw, PixelH: ph, Scale: w.scaleFactor()})
}

// pixelSize is the buffer size in pixels: the window's logical size times
// the fractional scale where the compositor sends one, and times the
// integer buffer scale otherwise.
func (w *wlWindow) pixelSize() (int, int) {
	if w.fracScale > 0 {
		return scale120(w.width, w.fracScale), scale120(w.height, w.fracScale)
	}
	return w.width * w.scale, w.height * w.scale
}

// scaleFactor is pixels per point, fractional where the compositor sends
// a fractional scale.
func (w *wlWindow) scaleFactor() float64 {
	if w.fracScale > 0 {
		return float64(w.fracScale) / 120
	}
	return float64(w.scale)
}

// scale120 multiplies a length by a scale counted in 120ths, rounding to
// the nearest pixel, which is what the fractional scale protocol asks
// for.
func scale120(n, scale int) int { return (n*scale + 60) / 120 }

// pointerScale is the buffer scale the cursor is drawn at. A fractional
// scale rounds up, because a cursor buffer's scale is a whole number and
// too large is sharper than too small.
func (w *wlWindow) pointerScale() int {
	if w.fracScale > 0 {
		return max((w.fracScale+119)/120, 1)
	}
	return w.scale
}

// onFractionalScale takes the scale the compositor prefers, in 120ths.
// The buffer is sized by it and the viewport maps the buffer back onto
// the window's logical size, so the integer buffer scale goes to one.
func (w *wlWindow) onFractionalScale(scale uint32) {
	if scale == 0 || int(scale) == w.fracScale {
		return
	}
	w.fracScale = int(scale)
	if w.scale != 1 {
		w.scale = 1
		if w.app.compositorVer >= 3 {
			w.app.l.send(w.surface, opSurfaceSetBufferScale, 1)
		}
	}
	w.applyViewport()
	w.applyCursor()
	if w.configured {
		w.pushResize()
	}
}

// applyViewport tells the compositor that the buffer covers the window's
// logical size however many pixels it holds, which is what lets the
// buffer be a fractional multiple of that size.
func (w *wlWindow) applyViewport() {
	if w.viewport == nil || w.fracScale == 0 {
		return
	}
	w.app.l.send(w.viewport, opViewportSetDestination, uintptr(int32(w.width)), uintptr(int32(w.height)))
}

// refreshScale recomputes the buffer scale from the outputs the surface is
// on, or from wl_surface.preferred_buffer_scale where the compositor sends
// it, and tells the compositor what the buffer scale now is.
func (w *wlWindow) refreshScale() {
	if w.fracScale > 0 {
		return // the viewport carries the scale and the buffer scale stays one
	}
	scale := w.preferredScale
	if scale == 0 {
		scale = 1
		for proxy := range w.onOutputs {
			if o := w.app.outputs[proxy]; o != nil && o.scale > scale {
				scale = o.scale
			}
		}
	}
	if scale < 1 {
		scale = 1
	}
	if scale == w.scale {
		return
	}
	w.scale = scale
	if w.app.compositorVer >= 3 {
		w.app.l.send(w.surface, opSurfaceSetBufferScale, uintptr(int32(scale)))
	}
	w.applyCursor()
	if w.configured {
		w.pushResize()
	}
}

func (w *wlWindow) destroy() {
	a, l := w.app, w.app.l
	w.setCaptured(false)
	w.dropIcon()
	w.dropCursorImage()
	if w.cursorSurface != nil {
		l.marshal(w.cursorSurface, opSurfaceDestroy, nil, wlMarshalFlagDestroy)
		w.cursorSurface = nil
	}
	// The protocol destroys the objects a toplevel is built from in reverse
	// order: the decoration, then the role object, then the xdg_surface, then
	// the wl_surface.
	for _, p := range []struct {
		proxy  unsafe.Pointer
		opcode uint32
	}{
		{w.fracObj, opFractionalScaleDestroy},
		{w.viewport, opViewportDestroy},
		{w.decoration, opXdgDecorationDestroy},
		{w.xdgToplevel, opXdgToplevelDestroy},
		{w.xdgSurface, opXdgSurfaceDestroy},
		{w.surface, opSurfaceDestroy},
	} {
		if p.proxy == nil {
			continue
		}
		delete(a.owner, p.proxy)
		l.marshal(p.proxy, p.opcode, nil, wlMarshalFlagDestroy)
		a.freeListener(p.proxy)
	}
	for i, other := range a.wins {
		if other == w {
			a.wins = append(a.wins[:i], a.wins[i+1:]...)
			break
		}
	}
	if w.title != nil {
		l.cfree(unsafe.Pointer(w.title))
		w.title = nil
	}
	if w.appID != nil {
		l.cfree(unsafe.Pointer(w.appID))
		w.appID = nil
	}
	if a.focus == w {
		a.focus = nil
	}
	if a.kbFocus == w {
		a.kbFocus = nil
	}
	if a.repeatWindow == w {
		a.repeatWindow, a.repeatKey = nil, 0
	}
	w.fracObj, w.viewport = nil, nil
	w.decoration, w.xdgToplevel, w.xdgSurface, w.surface = nil, nil, nil, nil
	w.closed = true
	l.flush(a.display)
}

// --- the window icon ---

// shmFormatARGB8888 is wl_shm's premultiplied 32-bit format, which on a
// little-endian machine is blue, green, red then alpha in memory.
const shmFormatARGB8888 = 0

// setIcon hands the compositor an icon through xdg-toplevel-icon-v1. A
// compositor without the protocol keeps the icon from the desktop entry
// whose name matches the app id, which is all a client can do there, so
// this does nothing.
func (w *wlWindow) setIcon(img image.Image) {
	a, l := w.app, w.app.l
	if a.iconMgr == nil || a.shm == nil || w.xdgToplevel == nil {
		return
	}
	w.dropIcon()
	if img == nil {
		l.send(a.iconMgr, opToplevelIconMgrSetIcon, uintptr(w.xdgToplevel), 0)
		w.commitIcon()
		return
	}
	// The protocol requires a square buffer and raises invalid_buffer on
	// anything else, which would disconnect the client; a non-square icon
	// is legal on X11 and Windows, so pad it here.
	buf := a.shmBuffer(squareIcon(img))
	if buf == nil {
		return
	}
	icon := l.marshal(a.iconMgr, opToplevelIconMgrCreateIcon, a.iface["xdg_toplevel_icon_v1"], 0, 0)
	if icon == nil {
		l.marshal(buf, opBufferDestroy, nil, wlMarshalFlagDestroy)
		return
	}
	// One buffer at scale one. The image is sent as the game gave it,
	// rather than resampled to each size the compositor listed, because
	// the compositor scales an icon it does not have at the size it wants.
	l.send(icon, opToplevelIconAddBuffer, uintptr(buf), 1)
	l.send(a.iconMgr, opToplevelIconMgrSetIcon, uintptr(w.xdgToplevel), uintptr(icon))
	w.icon, w.iconBuffer = icon, buf
	w.commitIcon()
}

// commitIcon applies a set_icon. The request is double-buffered on the
// toplevel's surface, so without a commit the icon changes only at the
// next frame the game draws, and never at all while it is hidden.
func (w *wlWindow) commitIcon() {
	l := w.app.l
	l.send(w.surface, opSurfaceCommit)
	l.flush(w.app.display)
}

// squareIcon pads an image out to a square with transparent edges,
// centred, because xdg-toplevel-icon-v1 takes square buffers only. An
// image that is already square is returned as it is.
func squareIcon(img image.Image) image.Image {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	if width == height || width <= 0 || height <= 0 {
		return img
	}
	side := max(width, height)
	out := image.NewNRGBA(image.Rect(0, 0, side, side))
	at := image.Pt((side-width)/2, (side-height)/2)
	draw.Draw(out, image.Rectangle{Min: at, Max: at.Add(image.Pt(width, height))}, img, b.Min, draw.Src)
	return out
}

// dropIcon releases the icon in force. Both the icon and its buffer are
// kept until the icon is replaced, because the compositor reads them for
// as long as the toplevel wears it.
func (w *wlWindow) dropIcon() {
	l := w.app.l
	if w.icon != nil {
		l.marshal(w.icon, opToplevelIconDestroy, nil, wlMarshalFlagDestroy)
		w.icon = nil
	}
	if w.iconBuffer != nil {
		l.marshal(w.iconBuffer, opBufferDestroy, nil, wlMarshalFlagDestroy)
		w.iconBuffer = nil
	}
}

// shmBuffer copies an image into shared memory the compositor can read
// and returns the wl_buffer over it, or nil where the memory could not be
// made. The caller owns the buffer and destroys it.
func (a *wlApp) shmBuffer(img image.Image) unsafe.Pointer {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	if width <= 0 || height <= 0 {
		return nil
	}
	// image.RGBA is premultiplied, which is what the format wants; only
	// the channel order changes.
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	pix := make([]byte, len(rgba.Pix))
	for i := 0; i+3 < len(pix); i += 4 {
		pix[i], pix[i+1], pix[i+2], pix[i+3] = rgba.Pix[i+2], rgba.Pix[i+1], rgba.Pix[i], rgba.Pix[i+3]
	}
	f, err := shmFile(len(pix))
	if err != nil {
		return nil
	}
	defer f.Close()
	if _, err := f.Write(pix); err != nil {
		return nil
	}
	l := a.l
	pool := l.marshal(a.shm, opShmCreatePool, a.iface["wl_shm_pool"], 0, 0,
		uintptr(f.Fd()), uintptr(int32(len(pix))))
	if pool == nil {
		return nil
	}
	// create_buffer takes the new id and the offset into the pool, both
	// zero here, then the size, the stride and the format.
	buf := l.marshal(pool, opShmPoolCreateBuf, a.iface["wl_buffer"], 0, 0, 0,
		uintptr(int32(width)), uintptr(int32(height)), uintptr(int32(width*4)), uintptr(uint32(shmFormatARGB8888)))
	// The pool can go as soon as the buffer exists, and the file has to
	// reach the compositor before this process closes its descriptor.
	l.marshal(pool, opShmPoolDestroy, nil, wlMarshalFlagDestroy)
	a.flushDisplay()
	return buf
}

// shmFile makes a file the compositor can map. It goes in
// XDG_RUNTIME_DIR, where the socket already is, and is unlinked at once,
// so it lives only as long as the two descriptors do.
func shmFile(size int) (*os.File, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, "bunyip-shm-")
	if err != nil {
		return nil, err
	}
	os.Remove(f.Name())
	if err := f.Truncate(int64(size)); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// --- cursor ---

// cursorNames lists the names to try for each shape, newest naming first.
var cursorNames = [cursorShapeCount][]string{
	CursorArrow:      {"default", "left_ptr"},
	CursorHand:       {"pointer", "hand2", "hand1"},
	CursorIBeam:      {"text", "xterm"},
	CursorCrosshair:  {"crosshair"},
	CursorResizeH:    {"ew-resize", "sb_h_double_arrow"},
	CursorResizeV:    {"ns-resize", "sb_v_double_arrow"},
	CursorGrab:       {"grab", "openhand"},
	CursorGrabbing:   {"grabbing", "closedhand"},
	CursorNotAllowed: {"not-allowed", "crossed_circle"},
}

// loadCursorTheme opens the theme at the scale the window is drawn at.
func (a *wlApp) loadCursorTheme(scale int) unsafe.Pointer {
	l := a.l
	if l.cursorThemeLoad == nil || a.shm == nil {
		return nil
	}
	size := int32(24 * scale)
	if s := os.Getenv("XCURSOR_SIZE"); s != "" {
		var n int32
		if _, err := fmt.Sscan(s, &n); err == nil && n > 0 {
			size = n * int32(scale)
		}
	}
	if a.cursorTheme != nil && a.cursorThemeSize == size {
		return a.cursorTheme
	}
	if a.cursorTheme != nil {
		l.cursorThemeDestroy(a.cursorTheme)
		a.cursorTheme = nil
	}
	var name *byte
	if t := os.Getenv("XCURSOR_THEME"); t != "" {
		name = l.cstring(t)
		defer l.cfree(unsafe.Pointer(name))
	}
	a.cursorTheme = l.cursorThemeLoad(name, size, a.shm)
	a.cursorThemeSize = size
	return a.cursorTheme
}

// applyCursor sets the pointer over this window from the hidden flag and the
// chosen shape. It does nothing until the pointer has entered the surface,
// because wl_pointer.set_cursor needs that event's serial.
func (w *wlWindow) applyCursor() {
	a, l := w.app, w.app.l
	if a.pointer == nil || a.focus != w {
		return
	}
	if w.cursorHidden || w.captured {
		l.send(a.pointer, opPointerSetCursor, uintptr(a.enterSerial), 0, 0, 0)
		return
	}
	if w.customCursor != nil {
		if w.cursorSurface == nil {
			w.cursorSurface = l.marshal(a.compositor, opCompositorCreateSurface, a.iface["wl_surface"], 0, 0)
		}
		if a.compositorVer >= 3 {
			l.send(w.cursorSurface, opSurfaceSetBufferScale, 1)
			w.cursorScale = 1
		}
		l.send(w.cursorSurface, opSurfaceAttach, uintptr(w.customCursor), 0, 0)
		l.send(w.cursorSurface, opSurfaceDamage, 0, 0, uintptr(w.customWidth), uintptr(w.customHeight))
		l.send(w.cursorSurface, opSurfaceCommit)
		l.send(a.pointer, opPointerSetCursor, uintptr(a.enterSerial), uintptr(w.cursorSurface), uintptr(w.hotX), uintptr(w.hotY))
		return
	}
	scale := w.pointerScale()
	theme := a.loadCursorTheme(scale)
	if theme == nil {
		return
	}
	var img *wlCursorImage
	for _, name := range cursorNames[w.shape] {
		c := l.cstring(name)
		cursor := l.cursorThemeGetCursor(theme, c)
		l.cfree(unsafe.Pointer(c))
		if cursor != nil && cursor.ImageCount > 0 {
			img = *(**wlCursorImage)(unsafe.Pointer(cursor.Images))
			break
		}
	}
	if img == nil {
		return
	}
	buf := l.cursorImageGetBuffer(img)
	if buf == nil {
		return
	}
	if w.cursorSurface == nil {
		w.cursorSurface = l.marshal(a.compositor, opCompositorCreateSurface, a.iface["wl_surface"], 0, 0)
	}
	if a.compositorVer >= 3 && w.cursorScale != scale {
		l.send(w.cursorSurface, opSurfaceSetBufferScale, uintptr(int32(scale)))
		w.cursorScale = scale
	}
	l.send(w.cursorSurface, opSurfaceAttach, uintptr(buf), 0, 0)
	l.send(w.cursorSurface, opSurfaceDamage, 0, 0, uintptr(int32(img.Width)), uintptr(int32(img.Height)))
	l.send(w.cursorSurface, opSurfaceCommit)
	l.send(a.pointer, opPointerSetCursor, uintptr(a.enterSerial), uintptr(w.cursorSurface),
		uintptr(int32(img.HotspotX)/int32(scale)), uintptr(int32(img.HotspotY)/int32(scale)))
}

// setCaptured locks the pointer to the window and switches motion to
// deltas. Without pointer-constraints and relative-pointer the pointer is
// only hidden and the deltas come from absolute motion, so it can leave.
func (w *wlWindow) setCaptured(on bool) {
	a, l := w.app, w.app.l
	if on == w.captured {
		return
	}
	w.captured = on
	if !on {
		if w.lockedPtr != nil {
			delete(a.owner, w.lockedPtr)
			l.marshal(w.lockedPtr, opLockedPointerDestroy, nil, wlMarshalFlagDestroy)
			a.freeListener(w.lockedPtr)
			w.lockedPtr = nil
		}
		if w.relPointer != nil {
			delete(a.owner, w.relPointer)
			l.marshal(w.relPointer, opRelativePointerDestroy, nil, wlMarshalFlagDestroy)
			a.freeListener(w.relPointer)
			w.relPointer = nil
		}
		w.applyCursor()
		l.flush(a.display)
		return
	}
	if a.pointer != nil && a.ptrConstraints != nil {
		w.lockedPtr = l.marshal(a.ptrConstraints, opPointerConstraintsLockPointer,
			a.iface["zwp_locked_pointer_v1"], 0, 0, uintptr(w.surface), uintptr(a.pointer), 0,
			uintptr(pointerConstraintLifetimePersistent))
		a.owner[w.lockedPtr] = w
		a.listen(w.lockedPtr, a.iface["zwp_locked_pointer_v1"], []uintptr{cbLockedPointerLocked, cbLockedPointerUnlocked})
	}
	if a.pointer != nil && a.relPointerMgr != nil {
		w.relPointer = l.marshal(a.relPointerMgr, opRelativePointerManagerGetPtr,
			a.iface["zwp_relative_pointer_v1"], 0, 0, uintptr(a.pointer))
		a.owner[w.relPointer] = w
		a.listen(w.relPointer, a.iface["zwp_relative_pointer_v1"], []uintptr{cbRelativeMotion})
	}
	w.applyCursor()
	l.flush(a.display)
}

func (w *wlWindow) setFullscreen(on bool) {
	l := w.app.l
	if on {
		l.send(w.xdgToplevel, opXdgToplevelSetFullscreen, 0)
	} else {
		l.send(w.xdgToplevel, opXdgToplevelUnsetFullscreen)
	}
	l.flush(w.app.display)
}

// --- the event loop ---

// poll drains what the compositor has sent. With wait set it blocks until
// an event, a Wake or a due key repeat.
func (a *wlApp) poll(wait bool) []Event {
	l := a.l
	a.out.startPoll()
	a.pumpRepeats(time.Now())
	if l.dispatchPending(a.display) < 0 {
		return a.closeAll()
	}
	// Zero blocks for no time at all, which is how a poll that does not wait
	// still picks up what the compositor has already sent.
	timeout := int32(0)
	if wait {
		if len(a.out.pending) > 0 {
			a.flushDisplay()
			return a.out.pending
		}
		timeout = a.repeatWait(time.Now())
	}
	// wl_display_prepare_read reserves the connection so that the read below
	// races no other reader; it fails while the queue still holds events,
	// which dispatch_pending clears.
	for l.prepareRead(a.display) != 0 {
		if l.dispatchPending(a.display) < 0 {
			return a.closeAll()
		}
	}
	if wait && len(a.out.pending) > 0 {
		l.cancelRead(a.display)
		a.flushDisplay()
		return a.out.pending
	}
	if !a.flushDisplay() {
		l.cancelRead(a.display)
		return a.closeAll()
	}
	fds := [2]pollFD{{Fd: l.getFD(a.display), Events: pollIn}, {Fd: int32(a.wakeR), Events: pollIn}}
	n := l.poll(&fds[0], 2, timeout)
	if n <= 0 || fds[0].Revents&pollIn == 0 {
		l.cancelRead(a.display)
	} else if l.readEvents(a.display) < 0 {
		return a.closeAll()
	}
	if n > 0 && fds[1].Revents&pollIn != 0 {
		var buf [64]byte
		for {
			if _, err := syscall.Read(a.wakeR, buf[:]); err != nil {
				break
			}
		}
		a.push(Event{Kind: EventWake})
	}
	if l.dispatchPending(a.display) < 0 {
		return a.closeAll()
	}
	a.pumpRepeats(time.Now())
	return a.out.pending
}

// flushDisplay sends what is queued, waiting for room when the socket is
// full. It reports whether the connection is still good.
func (a *wlApp) flushDisplay() bool {
	l := a.l
	for {
		if l.flush(a.display) >= 0 {
			return true
		}
		if *l.errno() != int32(syscall.EAGAIN) {
			return false
		}
		fd := pollFD{Fd: l.getFD(a.display), Events: pollOut}
		if l.poll(&fd, 1, -1) < 0 && *l.errno() != int32(syscall.EINTR) {
			return false
		}
	}
}

// closeAll reports every window closed, which is what a lost connection
// means to the loop above.
func (a *wlApp) closeAll() []Event {
	for _, w := range a.wins {
		if !w.closed {
			a.push(Event{Kind: EventClose, Window: w.out})
		}
	}
	return a.out.pending
}

// --- the clipboard ---
//
// A Wayland selection is a live offer from whichever client owns it: the
// compositor names the types on offer and a reader asks for one over a
// pipe the owner writes into. Only the focused client is told what the
// selection holds, which is why a read outside focus finds nothing.

// clipMimes are the types this process offers when it owns the
// selection, best first. Everything is UTF-8, whatever the type is
// called, because the older names have no encoding of their own.
var clipMimes = []string{"text/plain;charset=utf-8", "text/plain", "UTF8_STRING", "TEXT"}

// pickTextMime chooses the type to ask an offer for: the best of the ones
// this layer names, or any other text type the owner offers.
func pickTextMime(offered []string) string {
	for _, want := range clipMimes {
		for _, have := range offered {
			if have == want {
				return have
			}
		}
	}
	for _, have := range offered {
		if strings.HasPrefix(have, "text/") {
			return have
		}
	}
	return ""
}

// clipboard reads the selection the compositor is holding. It returns
// what this process put there without a round trip, and empty text when
// the selection holds nothing this layer can read or the owner does not
// write in time.
func (a *wlApp) clipboard() (string, error) {
	if a.dataDevice == nil {
		return "", ErrNoClipboard
	}
	if a.clipSource != nil {
		return a.clipText, nil
	}
	if a.selection == nil {
		return "", nil
	}
	mime := pickTextMime(a.offerMimes[a.selection])
	if mime == "" {
		return "", nil
	}
	var fds [2]int
	if err := syscall.Pipe2(fds[:], syscall.O_CLOEXEC); err != nil {
		return "", fmt.Errorf("platform: clipboard pipe: %w", err)
	}
	l := a.l
	c := l.cstring(mime)
	l.send(a.selection, opDataOfferReceive, uintptr(unsafe.Pointer(c)), uintptr(fds[1]))
	l.cfree(unsafe.Pointer(c))
	// The owner is handed the write end only once the request reaches the
	// compositor, and the read below ends at end of file only once every
	// copy of that end is closed, so flush first and close ours second.
	a.flushDisplay()
	syscall.Close(fds[1])
	// Only the read end is made non-blocking, so that the read below can
	// wait on the display as well; the owner writes into a pipe that
	// behaves as it expects.
	if err := syscall.SetNonblock(fds[0], true); err != nil {
		syscall.Close(fds[0])
		return "", fmt.Errorf("platform: clipboard pipe: %w", err)
	}
	defer syscall.Close(fds[0])
	// A read that stops early keeps what it has: a truncated paste is
	// better than none, and an owner that says nothing gives none.
	return string(a.readOffer(fds[0], time.Now().Add(clipboardWait))), nil
}

// readOffer reads the pipe the selection's owner writes into, until the
// owner closes it or the deadline passes. It waits on the display as well
// as the pipe and dispatches what arrives, because a compositor that goes
// unanswered for a second may take the window for hung, and because the
// owner is sometimes this process, whose source cannot write until its
// send event is dispatched. Events that arrive are queued for the next
// Poll rather than added to the slice the last one returned.
func (a *wlApp) readOffer(fd int, deadline time.Time) []byte {
	l := a.l
	a.out.deferQueue = true
	defer func() { a.out.deferQueue = false }()
	var out []byte
	buf := make([]byte, 4096)
	for {
		for {
			n, err := syscall.Read(fd, buf)
			if n > 0 {
				out = append(out, buf[:n]...)
				continue
			}
			if err == syscall.EINTR {
				continue
			}
			if err != syscall.EAGAIN {
				return out // end of file, or a pipe that broke
			}
			break
		}
		left := time.Until(deadline)
		if left <= 0 {
			return out
		}
		if l.dispatchPending(a.display) < 0 {
			return out
		}
		// prepare_read reserves the connection so the read below races no
		// other reader; it fails while the queue still holds events.
		for l.prepareRead(a.display) != 0 {
			if l.dispatchPending(a.display) < 0 {
				return out
			}
		}
		if !a.flushDisplay() {
			l.cancelRead(a.display)
			return out
		}
		fds := [2]pollFD{{Fd: l.getFD(a.display), Events: pollIn}, {Fd: int32(fd), Events: pollIn}}
		n := l.poll(&fds[0], 2, int32(left/time.Millisecond)+1)
		if n <= 0 || fds[0].Revents&pollIn == 0 {
			l.cancelRead(a.display)
		} else if l.readEvents(a.display) < 0 {
			return out
		}
		if n < 0 || l.dispatchPending(a.display) < 0 {
			return out
		}
	}
}

// setClipboard offers the text as the selection. The compositor keeps the
// offer alive until another client takes the selection or this one goes
// away, and asks for the text over a pipe whenever someone pastes.
func (a *wlApp) setClipboard(text string) error {
	if a.dataDevice == nil {
		return ErrNoClipboard
	}
	if a.lastSerial == 0 {
		// set_selection has to quote the serial of an input event, and a
		// window that has never been touched has none to quote. A
		// compositor that checks would drop the request in silence.
		return ErrNoInputYet
	}
	l := a.l
	a.destroySource()
	src := l.marshal(a.dataMgr, opDataDeviceManagerCreateSource, a.iface["wl_data_source"], 0, 0)
	if src == nil {
		return ErrNoClipboard
	}
	a.clipSource, a.clipText = src, text
	a.listen(src, a.iface["wl_data_source"], []uintptr{
		cbDataSourceTarget, cbDataSourceSend, cbDataSourceCancelled,
		cbDataSourceDndDrop, cbDataSourceDndFinished, cbDataSourceAction,
	})
	for _, m := range clipMimes {
		c := l.cstring(m)
		l.send(src, opDataSourceOffer, uintptr(unsafe.Pointer(c)))
		l.cfree(unsafe.Pointer(c))
	}
	l.send(a.dataDevice, opDataDeviceSetSelection, uintptr(src), uintptr(a.lastSerial))
	a.flushDisplay()
	return nil
}

// writeClipboard hands the text to a client that asked for it. The write
// runs on another goroutine because a pipe blocks once it is full, and
// the event loop cannot wait for whoever is reading.
func (a *wlApp) writeClipboard(fd int) {
	text := a.clipText
	go func() {
		f := os.NewFile(uintptr(fd), "wayland-clipboard")
		defer f.Close()
		f.WriteString(text) // the reader may leave early, which is not an error here
	}()
}

// destroySource drops the selection this process offered.
func (a *wlApp) destroySource() {
	if a.clipSource == nil {
		return
	}
	a.l.marshal(a.clipSource, opDataSourceDestroy, nil, wlMarshalFlagDestroy)
	a.freeListener(a.clipSource)
	a.clipSource, a.clipText = nil, ""
}

// onDataOffer records a new offer from the compositor and starts
// collecting the types it carries. It is not the selection until a
// selection event says so, and an offer that never becomes one, such as
// a drag this layer does not handle, is dropped when the next arrives.
func (a *wlApp) onDataOffer(offer unsafe.Pointer) {
	if offer == nil {
		return
	}
	a.dropOffer(a.newOffer)
	a.newOffer = offer
	a.offerMimes[offer] = nil
	a.listen(offer, a.iface["wl_data_offer"], []uintptr{cbDataOfferOffer, cbDataOfferSourceActions, cbDataOfferAction})
}

// onSelection takes the offer the compositor says is the selection, or
// nothing when the selection was cleared.
func (a *wlApp) onSelection(offer unsafe.Pointer) {
	// An announced offer that turns out not to be the selection is this
	// layer's to destroy, and a cleared selection leaves one behind every
	// time; without this its proxy and listener live to the next offer.
	if a.newOffer != nil && a.newOffer != offer {
		a.dropOffer(a.newOffer)
	}
	a.newOffer = nil
	if a.selection != nil && a.selection != offer {
		a.dropOffer(a.selection)
	}
	a.selection = offer
}

// dropOffer destroys an offer this layer will not read.
func (a *wlApp) dropOffer(offer unsafe.Pointer) {
	if offer == nil {
		return
	}
	if a.selection == offer {
		a.selection = nil
	}
	if a.newOffer == offer {
		a.newOffer = nil
	}
	delete(a.offerMimes, offer)
	a.l.marshal(offer, opDataOfferDestroy, nil, wlMarshalFlagDestroy)
	a.freeListener(offer)
}

// wake writes to the pipe Poll waits on. Safe from any goroutine.
func (a *wlApp) wake() {
	if a.wakeW >= 0 {
		syscall.Write(a.wakeW, []byte{1})
	}
}
