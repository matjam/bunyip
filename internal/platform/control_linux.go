package platform

import (
	"image"
	"image/draw"
	"unsafe"
)

const (
	xcbAtomCardinal = 6
	pMinSize        = 1 << 4
	pMaxSize        = 1 << 5
)

// SetTitle changes the window's title through WM_NAME and _NET_WM_NAME.
func (w *Window) SetTitle(title string) {
	a, x := w.app, w.app.x
	if title == "" {
		title = " "
	}
	b := []byte(title)
	x.changeProperty(a.conn, xcbPropModeReplace, w.id, xcbAtomWMName, xcbAtomString, 8, uint32(len(b)), unsafe.Pointer(&b[0]))
	x.changeProperty(a.conn, xcbPropModeReplace, w.id, a.atomNetWMName, a.atomUTF8, 8, uint32(len(b)), unsafe.Pointer(&b[0]))
	x.flush(a.conn)
}

// SetSizeLimits bounds the content size through WM_NORMAL_HINTS; zero
// lifts a bound.
func (w *Window) SetSizeLimits(minW, minH, maxW, maxH int) {
	w.minW, w.minH, w.maxW, w.maxH = minW, minH, maxW, maxH
	var hints [18]uint32
	if minW > 0 || minH > 0 {
		hints[0] |= pMinSize
		hints[5], hints[6] = uint32(max(minW, 1)), uint32(max(minH, 1))
	}
	if maxW > 0 || maxH > 0 {
		hints[0] |= pMaxSize
		hints[7], hints[8] = uint32(maxW), uint32(maxH)
		if maxW <= 0 {
			hints[7] = 1 << 15
		}
		if maxH <= 0 {
			hints[8] = 1 << 15
		}
	}
	a, x := w.app, w.app.x
	x.changeProperty(a.conn, xcbPropModeReplace, w.id, 40 /* WM_NORMAL_HINTS */, 41 /* WM_SIZE_HINTS */, 32, 18, unsafe.Pointer(&hints[0]))
	x.flush(a.conn)
}

// applyCursor sets the window's cursor attribute from the hidden flag
// and the chosen shape.
func (w *Window) applyCursor() {
	a, x := w.app, w.app.x
	cursor := w.shapeCursor
	if w.cursorHidden {
		if w.blankCursor == 0 {
			pid := x.generateID(a.conn)
			x.createPixmap(a.conn, 1, pid, w.id, 1, 1)
			w.blankCursor = x.generateID(a.conn)
			x.createCursor(a.conn, w.blankCursor, pid, pid, 0, 0, 0, 0, 0, 0, 0, 0)
			x.freePixmap(a.conn, pid)
		}
		cursor = w.blankCursor
	}
	values := [1]uint32{cursor}
	x.changeWindowAttrs(a.conn, w.id, xcbCWCursor, &values[0])
	x.flush(a.conn)
}

// SetCursorVisible shows or hides the pointer over the window.
func (w *Window) SetCursorVisible(on bool) {
	if w.cursorHidden == !on {
		return
	}
	w.cursorHidden = !on
	if !w.captured {
		w.applyCursor()
	}
}

// SetCursor picks the pointer's shape from the X cursor font.
func (w *Window) SetCursor(shape CursorShape) {
	a, x := w.app, w.app.x
	glyph := uint16(68) // XC_left_ptr
	switch shape {
	case CursorHand:
		glyph = 60 // XC_hand2
	case CursorIBeam:
		glyph = 152 // XC_xterm
	case CursorCrosshair:
		glyph = 34 // XC_crosshair
	case CursorResizeH:
		glyph = 108 // XC_sb_h_double_arrow
	case CursorResizeV:
		glyph = 116 // XC_sb_v_double_arrow
	case CursorGrab, CursorGrabbing:
		glyph = 52 // XC_fleur
	case CursorNotAllowed:
		glyph = 0 // XC_X_cursor
	}
	w.shape = shape
	if w.shapeCursor != 0 {
		x.freeCursor(a.conn, w.shapeCursor)
		w.shapeCursor = 0
	}
	if shape != CursorArrow {
		font := x.generateID(a.conn)
		name := []byte("cursor")
		x.openFont(a.conn, font, uint16(len(name)), &name[0])
		w.shapeCursor = x.generateID(a.conn)
		x.createGlyphCursor(a.conn, w.shapeCursor, font, font, glyph, glyph+1, 0, 0, 0, 0xffff, 0xffff, 0xffff)
		x.closeFont(a.conn, font)
	}
	if !w.captured {
		w.applyCursor()
	}
}

// SetIcon sets _NET_WM_ICON: width, height and ARGB pixels as cardinals.
func (w *Window) SetIcon(img image.Image) {
	if img == nil {
		return
	}
	b := img.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	data := make([]uint32, 2+b.Dx()*b.Dy())
	data[0], data[1] = uint32(b.Dx()), uint32(b.Dy())
	for i := 0; i < b.Dx()*b.Dy(); i++ {
		p := rgba.Pix[i*4:]
		data[2+i] = uint32(p[3])<<24 | uint32(p[0])<<16 | uint32(p[1])<<8 | uint32(p[2])
	}
	a, x := w.app, w.app.x
	x.changeProperty(a.conn, xcbPropModeReplace, w.id, a.atom("_NET_WM_ICON"), xcbAtomCardinal, 32, uint32(len(data)), unsafe.Pointer(&data[0]))
	x.flush(a.conn)
}

// Clipboard is not available under X11 yet: serving selections needs a
// request loop the platform layer does not run.
func (a *App) Clipboard() (string, error) { return "", ErrNoClipboard }

// SetClipboard is not available under X11 yet.
func (a *App) SetClipboard(string) error { return ErrNoClipboard }
