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

// SetTitle changes the window's title, through xdg_toplevel.set_title under
// Wayland and WM_NAME and _NET_WM_NAME under X11.
func (w *Window) SetTitle(title string) {
	if w.wl != nil {
		w.wl.setTitle(title)
		w.wl.app.l.flush(w.wl.app.display)
		return
	}
	a, x := w.app, w.app.x
	if title == "" {
		title = " "
	}
	b := []byte(title)
	x.changeProperty(a.conn, xcbPropModeReplace, w.id, xcbAtomWMName, xcbAtomString, 8, uint32(len(b)), unsafe.Pointer(&b[0]))
	x.changeProperty(a.conn, xcbPropModeReplace, w.id, a.atomNetWMName, a.atomUTF8, 8, uint32(len(b)), unsafe.Pointer(&b[0]))
	x.flush(a.conn)
}

// SetSizeLimits bounds the content size, through xdg_toplevel.set_min_size
// and set_max_size under Wayland and WM_NORMAL_HINTS under X11; zero lifts a
// bound.
func (w *Window) SetSizeLimits(minW, minH, maxW, maxH int) {
	if w.wl != nil {
		w.wl.minW, w.wl.minH, w.wl.maxW, w.wl.maxH = minW, minH, maxW, maxH
		w.wl.applySizeLimits(minW, minH, maxW, maxH)
		w.wl.app.l.flush(w.wl.app.display)
		return
	}
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

// applyCursorX11 sets the window's cursor attribute from the hidden flag
// and the chosen shape.
func (w *Window) applyCursorX11() {
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
	if w.wl != nil {
		if w.wl.cursorHidden == !on {
			return
		}
		w.wl.cursorHidden = !on
		w.wl.applyCursor()
		w.wl.app.l.flush(w.wl.app.display)
		return
	}
	if w.cursorHidden == !on {
		return
	}
	w.cursorHidden = !on
	if !w.captured {
		w.applyCursorX11()
	}
}

// SetCursor picks the pointer's shape, from the cursor theme under Wayland
// and from the X cursor font under X11.
func (w *Window) SetCursor(shape CursorShape) {
	if w.wl != nil {
		if shape >= cursorShapeCount {
			shape = CursorArrow
		}
		w.wl.shape = shape
		w.wl.applyCursor()
		w.wl.app.l.flush(w.wl.app.display)
		return
	}
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
		w.applyCursorX11()
	}
}

// SetPosition, Position, SetAlwaysOnTop and SetCursorImage are not
// implemented on either Linux backend. Wayland has no protocol that gives a
// client its own position or puts a window above others, and the X11 layer
// leaves placement to the window manager.
func (w *Window) SetPosition(x, y int)                 {}
func (w *Window) Position() (int, int)                 { return 0, 0 }
func (w *Window) SetAlwaysOnTop(bool)                  {}
func (w *Window) SetCursorImage(image.Image, int, int) {}

// SetIcon sets _NET_WM_ICON under X11: width, height and ARGB pixels as
// cardinals. Under Wayland it sends the pixels through
// xdg-toplevel-icon-v1, and does nothing where the compositor lacks that
// protocol, because the icon then comes from the desktop entry whose name
// matches the app id.
func (w *Window) SetIcon(img image.Image) {
	if w.wl != nil {
		w.wl.setIcon(img)
		return
	}
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

// Clipboard returns the system clipboard's text, empty when it holds
// none. Under X11 it asks whoever owns the CLIPBOARD selection and waits
// about a second for the answer, and under Wayland it reads the offer the
// compositor gave the seat. Text this process put there is returned
// without asking anyone. It fails only where the window system has no
// clipboard for a client with no window, or where the compositor has no
// wl_data_device_manager.
func (a *App) Clipboard() (string, error) {
	if a.wl != nil {
		return a.wl.clipboard()
	}
	return a.clipboardX11()
}

// SetClipboard puts text on the system clipboard. Both backends hand the
// text over when another client asks for it, so it stays available for as
// long as this process owns the selection and no longer. Under Wayland it
// returns ErrNoInputYet before the window has had any input, because
// changing the selection quotes the serial of an input event.
func (a *App) SetClipboard(text string) error {
	if a.wl != nil {
		return a.wl.setClipboard(text)
	}
	return a.setClipboardX11(text)
}
