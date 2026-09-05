package platform

import (
	"errors"
	"fmt"
	"image"
	"math"
	"unsafe"
)

var (
	procCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	procDestroyCursor       = user32.NewProc("DestroyCursor")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procSetCursorPos        = user32.NewProc("SetCursorPos")
)

func winControlError(operation string, result uintptr, err error) error {
	if result != 0 {
		return nil
	}
	return fmt.Errorf("platform: %s failed: %w", operation, err)
}

func (w *Window) Capabilities() WindowCapabilities {
	return WindowCapabilities{Resize: true, Show: true, Hide: true, Focus: true, AlwaysOnTop: true, CursorImage: true, PointerPosition: true}
}

func (w *Window) RefreshCursor() { procSetCursor.Call(w.hcursor) }

func (w *Window) SetSize(width, height int) error {
	if width <= 0 || height <= 0 || float64(width)*w.scale > math.MaxInt32/2 || float64(height)*w.scale > math.MaxInt32/2 {
		return errors.New("platform: invalid window dimensions")
	}
	r := rect{Right: int32(float64(width) * w.scale), Bottom: int32(float64(height) * w.scale)}
	if ok, _, e := procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), w.style, 0, wsExAppWindow); ok == 0 {
		return winControlError("AdjustWindowRectEx", ok, e)
	}
	ok, _, e := procSetWindowPos.Call(w.hwnd, 0, 0, 0, uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0x0002|swpNoZOrder|swpNoActivate)
	return winControlError("SetWindowPos", ok, e)
}
func (w *Window) Show() error { procShowWindow.Call(w.hwnd, 8 /* SW_SHOWNA */); return nil }
func (w *Window) Hide() error { procShowWindow.Call(w.hwnd, 0); return nil }
func (w *Window) RequestFocus() error {
	ok, _, _ := procSetForegroundWindow.Call(w.hwnd)
	if ok == 0 {
		return errors.New("platform: Windows declined the focus request")
	}
	return nil
}
func (w *Window) SetAlwaysOnTop(on bool) error {
	order := ^uintptr(1) // HWND_NOTOPMOST (-2)
	if on {
		order = ^uintptr(0)
	}
	ok, _, e := procSetWindowPos.Call(w.hwnd, order, 0, 0, 0, 0, 0x0001|0x0002|swpNoActivate)
	return winControlError("SetWindowPos always-on-top", ok, e)
}
func (w *Window) SetPointerPosition(x, y float64) error {
	if math.Abs(x*w.scale) > math.MaxInt32 || math.Abs(y*w.scale) > math.MaxInt32 {
		return errors.New("platform: pointer position out of range")
	}
	p := point{X: int32(math.Round(x * w.scale)), Y: int32(math.Round(y * w.scale))}
	if ok, _, e := procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&p))); ok == 0 {
		return winControlError("ClientToScreen", ok, e)
	}
	ok, _, e := procSetCursorPos.Call(uintptr(p.X), uintptr(p.Y))
	return winControlError("SetCursorPos", ok, e)
}

// bitmapInfoHeader is a top-down 32-bit BI_RGB DIB, including its required
// BITMAPINFOHEADER fields; alpha uses premultiplied BGRA.
type bitmapInfoHeader struct {
	Size                         uint32
	Width, Height                int32
	Planes, BitCount             uint16
	Compression, SizeImage       uint32
	XPelsPerMeter, YPelsPerMeter int32
	ClrUsed, ClrImportant        uint32
}

func (w *Window) SetCursorImage(img image.Image, hotX, hotY int) error {
	pixels, width, height, err := cursorPixels(img, hotX, hotY)
	if err != nil {
		return err
	}
	info := bitmapInfoHeader{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	color, _, e := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 {
		return winControlError("CreateDIBSection", color, e)
	}
	defer procDeleteObject.Call(color)
	copy(unsafe.Slice((*byte)(bits), len(pixels)), pixels)
	maskBits := make([]byte, (width+15)/16*2*height)
	mask, _, e := procCreateBitmap.Call(uintptr(width), uintptr(height), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	if mask == 0 {
		return winControlError("CreateBitmap cursor mask", mask, e)
	}
	defer procDeleteObject.Call(mask)
	icon := iconInfo{XHotspot: uint32(hotX), YHotspot: uint32(hotY), Mask: mask, Color: color}
	cursor, _, e := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&icon)))
	if cursor == 0 {
		return winControlError("CreateIconIndirect", cursor, e)
	}
	old := w.customCursor
	w.customCursor, w.hcursor = cursor, cursor
	procSetCursor.Call(cursor)
	if old != 0 {
		procDestroyCursor.Call(old)
	}
	return nil
}
