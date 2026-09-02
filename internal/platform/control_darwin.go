package platform

import (
	"image"
	"image/draw"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// controlSel holds the selectors and classes the window controls use,
// resolved on first use so the main binding table stays as it is.
var controlSel struct {
	once sync.Once

	NSBitmapImageRep, NSImage, NSPasteboard objc.Class

	setContentMinSize, setContentMaxSize, set,
	arrowCursor, pointingHandCursor, IBeamCursor, crosshairCursor, openHandCursor, closedHandCursor,
	resizeLeftRightCursor, resizeUpDownCursor, operationNotAllowedCursor,
	initWithBitmapDataPlanes, bitmapData, initWithSize, addRepresentation, setApplicationIconImage,
	generalPasteboard, clearContents, setStringForType, stringForType objc.SEL
}

func controls() *struct {
	once sync.Once

	NSBitmapImageRep, NSImage, NSPasteboard objc.Class

	setContentMinSize, setContentMaxSize, set,
	arrowCursor, pointingHandCursor, IBeamCursor, crosshairCursor, openHandCursor, closedHandCursor,
	resizeLeftRightCursor, resizeUpDownCursor, operationNotAllowedCursor,
	initWithBitmapDataPlanes, bitmapData, initWithSize, addRepresentation, setApplicationIconImage,
	generalPasteboard, clearContents, setStringForType, stringForType objc.SEL
} {
	s := &controlSel
	s.once.Do(func() {
		s.NSBitmapImageRep = objc.GetClass("NSBitmapImageRep")
		s.NSImage = objc.GetClass("NSImage")
		s.NSPasteboard = objc.GetClass("NSPasteboard")
		for name, dst := range map[string]*objc.SEL{
			"setContentMinSize:": &s.setContentMinSize, "setContentMaxSize:": &s.setContentMaxSize, "set": &s.set,
			"arrowCursor": &s.arrowCursor, "pointingHandCursor": &s.pointingHandCursor, "IBeamCursor": &s.IBeamCursor,
			"crosshairCursor": &s.crosshairCursor, "openHandCursor": &s.openHandCursor, "closedHandCursor": &s.closedHandCursor,
			"resizeLeftRightCursor": &s.resizeLeftRightCursor, "resizeUpDownCursor": &s.resizeUpDownCursor,
			"operationNotAllowedCursor": &s.operationNotAllowedCursor,
			"initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bitmapFormat:bytesPerRow:bitsPerPixel:": &s.initWithBitmapDataPlanes,
			"bitmapData": &s.bitmapData, "initWithSize:": &s.initWithSize, "addRepresentation:": &s.addRepresentation,
			"setApplicationIconImage:": &s.setApplicationIconImage,
			"generalPasteboard":        &s.generalPasteboard, "clearContents": &s.clearContents,
			"setString:forType:": &s.setStringForType, "stringForType:": &s.stringForType,
		} {
			*dst = objc.RegisterName(name)
		}
	})
	return s
}

// SetTitle changes the window's title.
func (w *Window) SetTitle(title string) {
	w.nsWindow.Send(w.app.c.sel.setTitle, w.app.c.nsString(title))
}

// SetSizeLimits bounds the content size in points; zero lifts a bound.
func (w *Window) SetSizeLimits(minW, minH, maxW, maxH int) {
	s := controls()
	lo := nsSize{Width: float64(max(minW, 0)), Height: float64(max(minH, 0))}
	hi := nsSize{Width: 1e7, Height: 1e7}
	if maxW > 0 {
		hi.Width = float64(maxW)
	}
	if maxH > 0 {
		hi.Height = float64(maxH)
	}
	w.nsWindow.Send(s.setContentMinSize, lo)
	w.nsWindow.Send(s.setContentMaxSize, hi)
}

// SetCursorVisible shows or hides the pointer while it is over the app.
func (w *Window) SetCursorVisible(on bool) {
	if w.cursorHidden == !on {
		return
	}
	w.cursorHidden = !on
	c := w.app.c
	if on {
		objc.ID(c.NSCursor).Send(c.sel.unhide)
	} else {
		objc.ID(c.NSCursor).Send(c.sel.hide)
	}
}

// SetCursor picks the pointer's shape. The loop calls it again when the
// pointer re-enters the window, since the system resets the shape.
func (w *Window) SetCursor(shape CursorShape) {
	s := controls()
	sel := s.arrowCursor
	switch shape {
	case CursorHand:
		sel = s.pointingHandCursor
	case CursorIBeam:
		sel = s.IBeamCursor
	case CursorCrosshair:
		sel = s.crosshairCursor
	case CursorResizeH:
		sel = s.resizeLeftRightCursor
	case CursorResizeV:
		sel = s.resizeUpDownCursor
	case CursorGrab:
		sel = s.openHandCursor
	case CursorGrabbing:
		sel = s.closedHandCursor
	case CursorNotAllowed:
		sel = s.operationNotAllowedCursor
	}
	w.shape = shape
	objc.ID(w.app.c.NSCursor).Send(sel).Send(s.set)
}

// SetIcon sets the application's Dock icon from an image.
func (w *Window) SetIcon(img image.Image) {
	if img == nil {
		return
	}
	s := controls()
	b := img.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	rep := objc.ID(s.NSBitmapImageRep).Send(w.app.c.sel.alloc).Send(s.initWithBitmapDataPlanes,
		uintptr(0), b.Dx(), b.Dy(), 8, 4, true, false, w.app.c.nsString("NSDeviceRGBColorSpace"), uint64(0), b.Dx()*4, 32)
	if rep == 0 {
		return
	}
	data := objc.Send[unsafe.Pointer](rep, s.bitmapData)
	if data != nil {
		copy(unsafe.Slice((*byte)(data), len(rgba.Pix)), rgba.Pix)
	}
	icon := objc.ID(s.NSImage).Send(w.app.c.sel.alloc).Send(s.initWithSize, nsSize{Width: float64(b.Dx()), Height: float64(b.Dy())})
	icon.Send(s.addRepresentation, rep)
	w.app.nsApp.Send(s.setApplicationIconImage, icon)
	rep.Send(w.app.c.sel.release)
	icon.Send(w.app.c.sel.release)
}

// Clipboard returns the text on the general pasteboard.
func (a *App) Clipboard() (string, error) {
	s := controls()
	pb := objc.ID(s.NSPasteboard).Send(s.generalPasteboard)
	str := pb.Send(s.stringForType, a.c.nsString("public.utf8-plain-text"))
	if str == 0 {
		return "", nil
	}
	p := objc.Send[unsafe.Pointer](str, a.c.sel.UTF8String)
	if p == nil {
		return "", nil
	}
	return cString(p), nil
}

// SetClipboard replaces the general pasteboard with text.
func (a *App) SetClipboard(text string) error {
	s := controls()
	pb := objc.ID(s.NSPasteboard).Send(s.generalPasteboard)
	pb.Send(s.clearContents)
	pb.Send(s.setStringForType, a.c.nsString(text), a.c.nsString("public.utf8-plain-text"))
	return nil
}

// cString copies a NUL-terminated C string.
func cString(p unsafe.Pointer) string {
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}
