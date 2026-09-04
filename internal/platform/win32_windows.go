package platform

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/matjam/bunyip/input"
)

// Win32 constants.
const (
	wsOverlappedWindow = 0x00CF0000
	wsThickFrame       = 0x00040000
	wsMaximizeBox      = 0x00010000
	wsVisible          = 0x10000000
	wsPopup            = 0x80000000
	wsExAppWindow      = 0x00040000
	cwUseDefault       = 0x80000000

	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmClose         = 0x0010
	wmSetFocus      = 0x0007
	wmKillFocus     = 0x0008
	wmKeyDown       = 0x0100
	wmKeyUp         = 0x0101
	wmChar          = 0x0102
	wmSysKeyDown    = 0x0104
	wmSysKeyUp      = 0x0105
	wmSysChar       = 0x0106
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmRButtonDown   = 0x0204
	wmRButtonUp     = 0x0205
	wmMButtonDown   = 0x0207
	wmMButtonUp     = 0x0208
	wmMouseWheel    = 0x020A
	wmXButtonDown   = 0x020B
	wmXButtonUp     = 0x020C
	wmMouseHWheel   = 0x020E
	wmMouseLeave    = 0x02A3
	wmInput         = 0x00FF
	wmDPIChanged    = 0x02E0
	wmUser          = 0x0400
	wmWake          = wmUser + 1
	wmQuit          = 0x0012
	pmRemove        = 0x0001
	swShow          = 5
	swMaximize      = 3
	swRestore       = 9
	gwlStyle        = -16
	swpNoZOrder     = 0x0004
	swpFrameChanged = 0x0020
	swpNoActivate   = 0x0010
	monitorPrimary  = 0x00000001
	idcArrow        = 32512
	cursorSuppress  = 0

	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkCapital = 0x14
	vkNumLock = 0x90
	vkLWin    = 0x5B
	vkRWin    = 0x5C

	ridInput          = 0x10000003
	rimTypeMouse      = 0
	ridevInputSink    = 0x00000100
	hidUsagePageGen   = 0x01
	hidUsageMouse     = 0x02
	dpiAwarenessPerV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procPeekMessageW             = user32.NewProc("PeekMessageW")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procGetClientRect            = user32.NewProc("GetClientRect")
	procAdjustWindowRectEx       = user32.NewProc("AdjustWindowRectEx")
	procSetWindowTextW           = user32.NewProc("SetWindowTextW")
	procLoadCursorW              = user32.NewProc("LoadCursorW")
	procSetCursor                = user32.NewProc("SetCursor")
	procShowCursor               = user32.NewProc("ShowCursor")
	procGetKeyState              = user32.NewProc("GetKeyState")
	procTrackMouseEvent          = user32.NewProc("TrackMouseEvent")
	procGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	procSetProcessDpiAwarenessCt = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procMonitorFromWindow        = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procRegisterRawInputDevices  = user32.NewProc("RegisterRawInputDevices")
	procGetRawInputData          = user32.NewProc("GetRawInputData")
	procClipCursor               = user32.NewProc("ClipCursor")
	procSetCapture               = user32.NewProc("SetCapture")
	procReleaseCapture           = user32.NewProc("ReleaseCapture")
	procGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
)

type wndClassExW struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type point struct{ X, Y int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	_       uint32
}

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type trackMouseEventStruct struct {
	Size      uint32
	Flags     uint32
	Track     uintptr
	HoverTime uint32
}

type rawInputDevice struct {
	UsagePage uint16
	Usage     uint16
	Flags     uint32
	Target    uintptr
}

type rawInputHeader struct {
	Type   uint32
	Size   uint32
	Device uintptr
	WParam uintptr
}

type rawMouse struct {
	Flags            uint16
	_                uint16
	ButtonFlags      uint16
	ButtonData       uint16
	RawButtons       uint32
	LastX            int32
	LastY            int32
	ExtraInformation uint32
}

func init() {
	// Win32 windows must be created and pumped on one thread.
	runtime.LockOSThread()
}

func utf16z(s string) *uint16 {
	u := utf16.Encode([]rune(s + "\x00"))
	return &u[0]
}

// App is the connection to the window system. Create one per process.
type App struct {
	instance uintptr
	class    uint16
	windows  map[uintptr]*Window
	wakeWnd  atomic.Uintptr // the window Wake posts to; read off the main goroutine
	pending  []Event
	mods     Mods
	mu       sync.Mutex // guards nothing hot; Wake posts a message instead
	wndProc  uintptr

	pendingSurrogate rune // high half of a WM_CHAR surrogate pair
}

var theApp *App // the window procedure has no user pointer

// NewApp registers the window class.
func NewApp() (*App, error) {
	procSetProcessDpiAwarenessCt.Call(dpiAwarenessPerV2)
	inst, _, _ := procGetModuleHandleW.Call(0)
	a := &App{instance: inst, windows: map[uintptr]*Window{}}
	a.wndProc = syscall.NewCallback(a.windowProc)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := wndClassExW{
		Size:      uint32(unsafe.Sizeof(wndClassExW{})),
		Style:     0x0003, // CS_HREDRAW | CS_VREDRAW
		WndProc:   a.wndProc,
		Instance:  inst,
		Cursor:    cursor,
		ClassName: utf16z("BunyipWindow"),
	}
	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return nil, fmt.Errorf("platform: RegisterClassEx: %w", err)
	}
	a.class = uint16(atom)
	theApp = a
	return a, nil
}

// Window is one top-level Win32 window.
type Window struct {
	app       *App
	hwnd      uintptr
	width     int
	height    int
	scale     float64
	closed    bool
	captured  bool
	tracking  bool
	fullscr   bool
	savedRect rect
	style     uintptr
	inputRect textRect
	mouseX    float64
	mouseY    float64

	minW, minH, maxW, maxH int // content size limits in points; zero is none
	cursorHidden           bool
	shape                  CursorShape
	hcursor                uintptr
	hicon                  uintptr
}

type textRect struct{ X, Y, W, H float64 }

// NewWindow opens a window and shows it.
func (a *App) NewWindow(cfg Config) (*Window, error) {
	style := uintptr(wsOverlappedWindow)
	if !cfg.Resizable {
		style &^= wsThickFrame | wsMaximizeBox
	}
	r := rect{0, 0, int32(cfg.Width), int32(cfg.Height)}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), style, 0, wsExAppWindow)
	hwnd, _, err := procCreateWindowExW.Call(wsExAppWindow, uintptr(a.class), uintptr(unsafe.Pointer(utf16z(cfg.Title))),
		style|wsVisible, cwUseDefault, cwUseDefault, uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0, 0, a.instance, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("platform: CreateWindowEx: %w", err)
	}
	w := &Window{app: a, hwnd: hwnd, style: style, scale: 1}
	a.windows[hwnd] = w
	a.wakeWnd.CompareAndSwap(0, hwnd)
	// Relative mouse motion arrives as raw input even while captured.
	rid := rawInputDevice{UsagePage: hidUsagePageGen, Usage: hidUsageMouse, Target: hwnd}
	procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&rid)), 1, unsafe.Sizeof(rid))
	procShowWindow.Call(hwnd, swShow)
	w.updateGeometry()
	return w, nil
}

func (w *Window) updateGeometry() {
	var r rect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	dpi, _, _ := procGetDpiForWindow.Call(w.hwnd)
	if dpi == 0 {
		dpi = 96
	}
	w.scale = float64(dpi) / 96
	pw, ph := int(r.Right-r.Left), int(r.Bottom-r.Top)
	w.width, w.height = int(float64(pw)/w.scale+0.5), int(float64(ph)/w.scale+0.5)
	w.app.push(Event{Kind: EventResize, Window: w, Width: w.width, Height: w.height, PixelW: pw, PixelH: ph, Scale: w.scale})
}

// The message loop runs on every frame and once per message, so it calls
// through addresses resolved on first use rather than through
// LazyProc.Call, which allocates a slice for its variadic arguments and
// takes a lock to check the procedure has been found.
var (
	addrPeekMessageW     uintptr
	addrGetMessageW      uintptr
	addrTranslateMessage uintptr
	addrDispatchMessageW uintptr
)

// resolveMessageProcs looks the message-loop entry points up once.
func resolveMessageProcs() {
	if addrPeekMessageW != 0 {
		return
	}
	addrPeekMessageW = procPeekMessageW.Addr()
	addrGetMessageW = procGetMessageW.Addr()
	addrTranslateMessage = procTranslateMessage.Addr()
	addrDispatchMessageW = procDispatchMessageW.Addr()
}

// Poll drains the message queue into the returned slice, reused by the
// next call. With wait set it blocks until a message arrives.
func (a *App) Poll(wait bool) []Event {
	resolveMessageProcs()
	a.pending = a.pending[:0]
	var m msg
	if wait {
		r, _, _ := syscall.SyscallN(addrGetMessageW, uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			return a.pending
		}
		syscall.SyscallN(addrTranslateMessage, uintptr(unsafe.Pointer(&m)))
		syscall.SyscallN(addrDispatchMessageW, uintptr(unsafe.Pointer(&m)))
	}
	for {
		r, _, _ := syscall.SyscallN(addrPeekMessageW, uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove)
		if r == 0 {
			break
		}
		if m.Message == wmQuit {
			for _, w := range a.windows {
				a.push(Event{Kind: EventClose, Window: w})
			}
			break
		}
		syscall.SyscallN(addrTranslateMessage, uintptr(unsafe.Pointer(&m)))
		syscall.SyscallN(addrDispatchMessageW, uintptr(unsafe.Pointer(&m)))
	}
	return a.pending
}

func (a *App) push(e Event) { a.pending = append(a.pending, e) }

// Wake posts a message so a blocked Poll returns with EventWake. Safe
// from any goroutine: the target is read from an atomic, not the window
// map the main goroutine writes.
func (a *App) Wake() {
	if hwnd := a.wakeWnd.Load(); hwnd != 0 {
		procPostMessageW.Call(hwnd, wmWake, 0, 0)
	}
}

func lowWord(v uintptr) int32  { return int32(int16(v & 0xFFFF)) }
func highWord(v uintptr) int32 { return int32(int16((v >> 16) & 0xFFFF)) }

func (a *App) readMods() Mods {
	var m Mods
	down := func(vk uintptr) bool {
		s, _, _ := procGetKeyState.Call(vk)
		return int16(s) < 0
	}
	toggled := func(vk uintptr) bool {
		s, _, _ := procGetKeyState.Call(vk)
		return s&1 != 0
	}
	if down(vkShift) {
		m |= input.ModShift
	}
	if down(vkControl) {
		m |= input.ModControl
	}
	if down(vkMenu) {
		m |= input.ModAlt
	}
	if down(vkLWin) || down(vkRWin) {
		m |= input.ModSuper
	}
	if toggled(vkCapital) {
		m |= input.ModCapsLock
	}
	if toggled(vkNumLock) {
		m |= input.ModNumLock
	}
	return m
}

// windowProc receives every message for every window of the class.
func (a *App) windowProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	w := a.windows[hwnd]
	if w == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
		return r
	}
	switch message {
	case wmGetMinMaxInfo:
		if w.minW > 0 || w.minH > 0 || w.maxW > 0 || w.maxH > 0 {
			w.minMax(*(**minMaxInfo)(unsafe.Pointer(&lparam)))
			return 0
		}
	case wmSetCursor:
		if w.hcursor != 0 && lparam&0xffff == htClient {
			procSetCursor.Call(w.hcursor)
			return 1
		}
	case wmClose:
		a.push(Event{Kind: EventClose, Window: w})
		return 0 // the game decides when to close
	case wmDestroy:
		w.closed = true
		delete(a.windows, hwnd)
		a.wakeWnd.CompareAndSwap(hwnd, 0)
		return 0
	case wmSize, wmDPIChanged:
		if message == wmDPIChanged {
			// lParam points at the suggested new window rectangle (system
			// memory, so reinterpret the variable rather than the value).
			r := *(**rect)(unsafe.Pointer(&lparam))
			procSetWindowPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpNoActivate)
		}
		w.updateGeometry()
		return 0
	case wmSetFocus:
		a.push(Event{Kind: EventFocus, Window: w, Focused: true})
		return 0
	case wmKillFocus:
		a.push(Event{Kind: EventFocus, Window: w, Focused: false})
		return 0
	case wmKeyDown, wmSysKeyDown:
		a.mods = a.readMods()
		repeat := lparam&(1<<30) != 0
		a.push(Event{Kind: EventKeyDown, Window: w, Key: keyFromLParam(lparam), Mods: a.mods, Repeat: repeat})
		if message == wmSysKeyDown && wparam != vkMenu {
			break // let Alt+F4 and friends through to DefWindowProc
		}
		return 0
	case wmKeyUp, wmSysKeyUp:
		a.mods = a.readMods()
		a.push(Event{Kind: EventKeyUp, Window: w, Key: keyFromLParam(lparam), Mods: a.mods})
		return 0
	case wmChar, wmSysChar:
		r := rune(wparam)
		if utf16.IsSurrogate(r) {
			if r < 0xDC00 {
				w.app.pendingSurrogate = r
				return 0
			}
			r = utf16.DecodeRune(w.app.pendingSurrogate, r)
			w.app.pendingSurrogate = 0
		}
		if r >= ' ' || r == '\t' || r == '\n' || r == '\r' {
			a.push(Event{Kind: EventChar, Window: w, Rune: r, Mods: a.mods})
		}
		return 0
	case wmMouseMove:
		x := float64(lowWord(lparam)) / w.scale
		y := float64(highWord(lparam)) / w.scale
		if !w.tracking {
			t := trackMouseEventStruct{Size: uint32(unsafe.Sizeof(trackMouseEventStruct{})), Flags: 0x00000002, Track: hwnd} // TME_LEAVE
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&t)))
			w.tracking = true
			a.push(Event{Kind: EventMouseEnter, Window: w})
		}
		w.mouseX, w.mouseY = x, y
		a.push(Event{Kind: EventMouseMove, Window: w, X: x, Y: y, Mods: a.mods})
		return 0
	case wmMouseLeave:
		w.tracking = false
		a.push(Event{Kind: EventMouseLeave, Window: w})
		return 0
	case wmInput:
		var hdr rawInputHeader
		size := uint32(unsafe.Sizeof(hdr) + unsafe.Sizeof(rawMouse{}) + 16)
		buf := make([]byte, size)
		n, _, _ := procGetRawInputData.Call(lparam, ridInput, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), unsafe.Sizeof(hdr))
		if uint32(n) != 0xFFFFFFFF && n > 0 { // (UINT)-1 means the buffer was too small
			h := (*rawInputHeader)(unsafe.Pointer(&buf[0]))
			if h.Type == rimTypeMouse {
				m := (*rawMouse)(unsafe.Pointer(&buf[unsafe.Sizeof(hdr)]))
				if m.Flags&1 == 0 && (m.LastX != 0 || m.LastY != 0) { // MOUSE_MOVE_RELATIVE
					// Raw counts are device pixels; the other backends
					// report points.
					a.push(Event{Kind: EventMouseMove, Window: w, X: w.mouseX, Y: w.mouseY, DX: float64(m.LastX) / w.scale, DY: float64(m.LastY) / w.scale, Mods: a.mods})
				}
			}
		}
		// The system cleans up after WM_INPUT in DefWindowProc.
		break
	case wmLButtonDown, wmRButtonDown, wmMButtonDown, wmXButtonDown:
		procSetCapture.Call(hwnd)
		a.push(w.mouseButton(message, wparam, lparam, EventMouseDown))
		return 0
	case wmLButtonUp, wmRButtonUp, wmMButtonUp, wmXButtonUp:
		procReleaseCapture.Call()
		a.push(w.mouseButton(message, wparam, lparam, EventMouseUp))
		return 0
	case wmMouseWheel:
		a.push(Event{Kind: EventScroll, Window: w, DY: float64(highWord(wparam)) / 120, Mods: a.mods})
		return 0
	case wmMouseHWheel:
		a.push(Event{Kind: EventScroll, Window: w, DX: float64(highWord(wparam)) / 120, Mods: a.mods})
		return 0
	case wmWake:
		a.push(Event{Kind: EventWake})
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wparam, lparam)
	return r
}

func (w *Window) mouseButton(message uint32, wparam, lparam uintptr, kind EventKind) Event {
	var b MouseButton
	switch message {
	case wmLButtonDown, wmLButtonUp:
		b = input.MouseLeft
	case wmRButtonDown, wmRButtonUp:
		b = input.MouseRight
	case wmMButtonDown, wmMButtonUp:
		b = input.MouseMiddle
	default:
		b = input.MouseButton4
		if highWord(wparam) == 2 {
			b = input.MouseButton5
		}
	}
	return Event{Kind: kind, Window: w, Button: b, X: float64(lowWord(lparam)) / w.scale, Y: float64(highWord(lparam)) / w.scale, Mods: w.app.mods}
}

// Size is the content size in points.
func (w *Window) Size() (int, int) { return w.width, w.height }

// PixelSize is the framebuffer size in pixels.
func (w *Window) PixelSize() (int, int) {
	var r rect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	return int(r.Right - r.Left), int(r.Bottom - r.Top)
}

// Scale is pixels per point.
func (w *Window) Scale() float64 { return w.scale }

// Closed reports whether the window has been destroyed.
func (w *Window) Closed() bool { return w.closed }

// Close destroys the window.
func (w *Window) Close() {
	if !w.closed {
		procDestroyWindow.Call(w.hwnd)
		w.closed = true
	}
}

// Fullscreen reports whether the window covers its monitor.
func (w *Window) Fullscreen() bool { return w.fullscr }

// SetFullscreen switches between a borderless monitor-sized window and
// the previous framed one.
func (w *Window) SetFullscreen(on bool) {
	if on == w.fullscr {
		return
	}
	if on {
		procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&w.savedRect)))
		mon, _, _ := procMonitorFromWindow.Call(w.hwnd, monitorPrimary)
		mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
		procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
		procSetWindowLongPtrW.Call(w.hwnd, uintptr(gwlStyle&0xFFFFFFFF), wsPopup|wsVisible)
		procSetWindowPos.Call(w.hwnd, 0, uintptr(mi.Monitor.Left), uintptr(mi.Monitor.Top),
			uintptr(mi.Monitor.Right-mi.Monitor.Left), uintptr(mi.Monitor.Bottom-mi.Monitor.Top), swpNoZOrder|swpFrameChanged)
	} else {
		procSetWindowLongPtrW.Call(w.hwnd, uintptr(gwlStyle&0xFFFFFFFF), w.style|wsVisible)
		r := w.savedRect
		procSetWindowPos.Call(w.hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpNoZOrder|swpFrameChanged)
	}
	w.fullscr = on
	w.updateGeometry()
}

// SetCursorCaptured hides the cursor and confines it to the window;
// motion then arrives as deltas through raw input.
func (w *Window) SetCursorCaptured(on bool) {
	if on == w.captured {
		return
	}
	w.captured = on
	if on {
		var r rect
		procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
		procClipCursor.Call(uintptr(unsafe.Pointer(&r)))
		procShowCursor.Call(0)
	} else {
		procClipCursor.Call(0)
		procShowCursor.Call(1)
	}
}

// CursorCaptured reports the capture state.
func (w *Window) CursorCaptured() bool { return w.captured }

// SetTextInputRect records where text is entered; Windows IME placement
// is not wired yet, so this only stores the rectangle.
func (w *Window) SetTextInputRect(x, y, width, height float64) {
	w.inputRect = textRect{X: x, Y: y, W: width, H: height}
}

// ErrUnsupported is kept for API parity with other platforms.
var ErrUnsupported = errors.New("platform: unsupported")
