package platform

import (
	"image"
	"image/draw"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	gdi32                  = syscall.NewLazyDLL("gdi32.dll")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procDestroyIcon        = user32.NewProc("DestroyIcon")
	procSendMessageW       = user32.NewProc("SendMessageW")
	procOpenClipboard      = user32.NewProc("OpenClipboard")
	procCloseClipboard     = user32.NewProc("CloseClipboard")
	procEmptyClipboard     = user32.NewProc("EmptyClipboard")
	procGetClipboardData   = user32.NewProc("GetClipboardData")
	procSetClipboardData   = user32.NewProc("SetClipboardData")
	procGlobalAlloc        = kernel32.NewProc("GlobalAlloc")
	procGlobalLock         = kernel32.NewProc("GlobalLock")
	procGlobalUnlock       = kernel32.NewProc("GlobalUnlock")
)

const (
	wmSetIcon       = 0x0080
	wmGetMinMaxInfo = 0x0024
	wmSetCursor     = 0x0020
	htClient        = 1
	cfUnicodeText   = 13
	gmemMoveable    = 0x0002

	idcIBeam   = 32513
	idcCross   = 32515
	idcSizeWE  = 32644
	idcSizeNS  = 32645
	idcSizeAll = 32646
	idcNo      = 32648
	idcHand    = 32649
)

// minMaxInfo is Win32's MINMAXINFO.
type minMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrack, MaxTrack point
}

// iconInfo is Win32's ICONINFO.
type iconInfo struct {
	Icon     int32
	XHotspot uint32
	YHotspot uint32
	Mask     uintptr
	Color    uintptr
}

// SetTitle changes the window's title.
func (w *Window) SetTitle(title string) {
	p, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	procSetWindowTextW.Call(w.hwnd, uintptr(unsafe.Pointer(p)))
}

// SetSizeLimits bounds the content size in points; zero lifts a bound.
// The limits are applied when Windows asks through WM_GETMINMAXINFO.
func (w *Window) SetSizeLimits(minW, minH, maxW, maxH int) {
	w.minW, w.minH, w.maxW, w.maxH = minW, minH, maxW, maxH
}

// minMax fills a MINMAXINFO from the limits, in pixels including the frame.
func (w *Window) minMax(info *minMaxInfo) {
	frame := func(cw, ch int) (int32, int32) {
		r := rect{Right: int32(float64(cw) * w.scale), Bottom: int32(float64(ch) * w.scale)}
		procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), w.style, 0, 0)
		return r.Right - r.Left, r.Bottom - r.Top
	}
	if w.minW > 0 || w.minH > 0 {
		x, y := frame(max(w.minW, 1), max(w.minH, 1))
		info.MinTrack = point{X: x, Y: y}
	}
	if w.maxW > 0 || w.maxH > 0 {
		x, y := frame(w.maxW, w.maxH)
		if w.maxW <= 0 {
			x = info.MaxTrack.X
		}
		if w.maxH <= 0 {
			y = info.MaxTrack.Y
		}
		info.MaxTrack = point{X: x, Y: y}
	}
}

// SetCursorVisible shows or hides the pointer over the window.
func (w *Window) SetCursorVisible(on bool) {
	if w.cursorHidden == !on {
		return
	}
	w.cursorHidden = !on
	if on {
		procShowCursor.Call(1)
	} else {
		procShowCursor.Call(0)
	}
}

// SetCursor picks the pointer's shape; WM_SETCURSOR re-applies it while
// the pointer is over the client area.
func (w *Window) SetCursor(shape CursorShape) {
	id := uintptr(idcArrow)
	switch shape {
	case CursorHand:
		id = idcHand
	case CursorIBeam:
		id = idcIBeam
	case CursorCrosshair:
		id = idcCross
	case CursorResizeH:
		id = idcSizeWE
	case CursorResizeV:
		id = idcSizeNS
	case CursorGrab, CursorGrabbing:
		id = idcSizeAll
	case CursorNotAllowed:
		id = idcNo
	}
	w.shape = shape
	w.hcursor, _, _ = procLoadCursorW.Call(0, id)
	procSetCursor.Call(w.hcursor)
}

// SetIcon builds an HICON from the image and gives it to the window for
// both its title bar and the taskbar.
// SetPosition, Position, SetAlwaysOnTop and SetCursorImage are not
// implemented on Windows yet; the window stays where the system put it
// and keeps the system pointer.
func (w *Window) SetPosition(x, y int)                 {}
func (w *Window) Position() (int, int)                 { return 0, 0 }
func (w *Window) SetAlwaysOnTop(bool)                  {}
func (w *Window) SetCursorImage(image.Image, int, int) {}

func (w *Window) SetIcon(img image.Image) {
	if img == nil {
		return
	}
	b := img.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	bgra := make([]byte, len(rgba.Pix))
	for i := 0; i+3 < len(rgba.Pix); i += 4 {
		bgra[i], bgra[i+1], bgra[i+2], bgra[i+3] = rgba.Pix[i+2], rgba.Pix[i+1], rgba.Pix[i], rgba.Pix[i+3]
	}
	color, _, _ := procCreateBitmap.Call(uintptr(b.Dx()), uintptr(b.Dy()), 1, 32, uintptr(unsafe.Pointer(&bgra[0])))
	// The mask must be defined even for an icon with alpha; a zeroed one
	// means "draw everywhere" and leaves the alpha channel in charge.
	maskBits := make([]byte, (b.Dx()+15)/16*2*b.Dy())
	mask, _, _ := procCreateBitmap.Call(uintptr(b.Dx()), uintptr(b.Dy()), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	info := iconInfo{Icon: 1, Mask: mask, Color: color}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	procDeleteObject.Call(mask)
	procDeleteObject.Call(color)
	if icon == 0 {
		return
	}
	if w.hicon != 0 {
		procDestroyIcon.Call(w.hicon)
	}
	w.hicon = icon
	procSendMessageW.Call(w.hwnd, wmSetIcon, 1, icon) // ICON_BIG
	procSendMessageW.Call(w.hwnd, wmSetIcon, 0, icon) // ICON_SMALL
}

// Clipboard returns the clipboard's text.
func (a *App) Clipboard() (string, error) {
	if ok, _, _ := procOpenClipboard.Call(0); ok == 0 {
		return "", nil
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", nil
	}
	defer procGlobalUnlock.Call(h)
	// The lock returns system memory, so reinterpret the variable rather
	// than convert the value.
	base := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	var units []uint16
	for i := 0; ; i++ {
		u := *(*uint16)(unsafe.Add(base, i*2))
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	return string(utf16.Decode(units)), nil
}

// SetClipboard replaces the clipboard with text.
func (a *App) SetClipboard(text string) error {
	units := utf16.Encode([]rune(text))
	units = append(units, 0)
	if ok, _, _ := procOpenClipboard.Call(0); ok == 0 {
		return nil
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(units)*2))
	if h == 0 {
		return nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p != 0 {
		base := *(*unsafe.Pointer)(unsafe.Pointer(&p))
		copy(unsafe.Slice((*uint16)(base), len(units)), units)
		procGlobalUnlock.Call(h)
	}
	procSetClipboardData.Call(cfUnicodeText, h) // the system owns h from here
	return nil
}
