package platform

import (
	"errors"
	"math"
	"unsafe"
)

var (
	procIsWindow       = user32.NewProc("IsWindow")
	procWindowThread   = user32.NewProc("GetWindowThreadProcessId")
	procCurrentThread  = kernel32.NewProc("GetCurrentThreadId")
	procCurrentProcess = kernel32.NewProc("GetCurrentProcessId")
	procThreadDPI      = user32.NewProc("GetThreadDpiAwarenessContext")
	procWindowDPI      = user32.NewProc("GetWindowDpiAwarenessContext")
	procEqualDPI       = user32.NewProc("AreDpiAwarenessContextsEqual")
	procSetFocus       = user32.NewProc("SetFocus")
	procGetFocus       = user32.NewProc("GetFocus")
)

func (a *App) newEmbedded(cfg Config) (*Window, error) {
	p := cfg.Parent
	if p.Backend != 1 || p.Handle == 0 {
		return nil, ErrUnsupported
	}
	var pid uint32
	thread, _, _ := procWindowThread.Call(p.Handle, uintptr(unsafe.Pointer(&pid)))
	tid, _, _ := procCurrentThread.Call()
	ownPID, _, _ := procCurrentProcess.Call()
	if thread == 0 || thread != tid || uintptr(pid) != ownPID {
		return nil, errors.New("platform: embedding parent must belong to the current process and UI thread")
	}
	tdpi, _, _ := procThreadDPI.Call()
	wdpi, _, _ := procWindowDPI.Call(p.Handle)
	same, _, _ := procEqualDPI.Call(tdpi, wdpi)
	if same == 0 {
		return nil, errors.New("platform: embedding parent and UI thread have different DPI awareness")
	}
	a.hosted = true
	if err := a.checkHostRawMouse(); err != nil {
		return nil, err
	}
	var bounds rect
	if ok, _, err := procGetClientRect.Call(p.Handle, uintptr(unsafe.Pointer(&bounds))); ok == 0 {
		return nil, winControlError("GetClientRect parent", ok, err)
	}
	if err := a.rawMouse.ensure(func(rid rawInputDevice) error {
		ok, _, err := procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&rid)), 1, unsafe.Sizeof(rid))
		return winControlError("RegisterRawInputDevices", ok, err)
	}); err != nil {
		return nil, err
	}
	const style = 0x40000000 | 0x04000000 // WS_CHILD | WS_CLIPSIBLINGS
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(a.class), 0, style|wsVisible, 0, 0, uintptr(max(1, bounds.Right)), uintptr(max(1, bounds.Bottom)), p.Handle, 0, a.instance, 0)
	if hwnd == 0 {
		a.releaseRawMouse()
		return nil, winControlError("CreateWindowEx child", hwnd, err)
	}
	w := &Window{app: a, hwnd: hwnd, parent: p.Handle, style: style, scale: 1, shown: true, visible: true}
	a.windows[hwnd] = w
	a.wakeWnd.CompareAndSwap(0, hwnd)
	w.updateGeometry()
	return w, nil
}

func (w *Window) SetBounds(x, y, width, height int) error {
	if w.parent == 0 {
		return ErrUnsupported
	}
	if width <= 0 || height <= 0 || math.Abs(float64(x)*w.scale) > math.MaxInt32 || math.Abs(float64(y)*w.scale) > math.MaxInt32 || float64(width)*w.scale > math.MaxInt32 || float64(height)*w.scale > math.MaxInt32 {
		return errors.New("platform: invalid embedded bounds")
	}
	ok, _, err := procSetWindowPos.Call(w.hwnd, 0, uintptr(int32(float64(x)*w.scale)), uintptr(int32(float64(y)*w.scale)), uintptr(float64(width)*w.scale), uintptr(float64(height)*w.scale), swpNoZOrder|swpNoActivate)
	if e := winControlError("SetWindowPos child", ok, err); e != nil {
		return e
	}
	w.manualBounds = true
	return nil
}

func (a *App) syncEmbedded() {
	for _, w := range a.windows {
		if w.parent == 0 || w.closed {
			continue
		}
		focus, _, _ := procGetFocus.Call()
		if focused := focus == w.hwnd; focused != w.embeddedFocus {
			w.embeddedFocus = focused
			a.push(Event{Kind: EventFocus, Window: w, Focused: focused})
		}
		if ok, _, _ := procIsWindow.Call(w.parent); ok == 0 {
			w.hostLost = true
			a.push(Event{Kind: EventClose, Window: w})
			continue
		}
		if w.manualBounds {
			continue
		}
		var r rect
		if ok, _, _ := procGetClientRect.Call(w.parent, uintptr(unsafe.Pointer(&r))); ok == 0 {
			continue
		}
		var current rect
		procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&current)))
		if r.Right != current.Right || r.Bottom != current.Bottom {
			procSetWindowPos.Call(w.hwnd, 0, 0, 0, uintptr(max(1, r.Right)), uintptr(max(1, r.Bottom)), swpNoZOrder|swpNoActivate)
		}
	}
}
