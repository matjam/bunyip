package platform

import (
	"errors"
	"fmt"
	"image"
	"math"
	"unsafe"

	"github.com/ebitengine/purego"
)

// xControls is loaded once per X11 connection, keeping optional Render support
// separate from the core window connection.
type xControls struct {
	configure        func(unsafe.Pointer, uint32, uint16, *uint32) xcbCookie
	unmap, mapWindow func(unsafe.Pointer, uint32) xcbCookie
	check            func(unsafe.Pointer, xcbCookie) unsafe.Pointer
	createGC         func(unsafe.Pointer, uint32, uint32, uint32, unsafe.Pointer) xcbCookie
	freeGC           func(unsafe.Pointer, uint32) xcbCookie
	putImage         func(unsafe.Pointer, uint8, uint32, uint32, uint16, uint16, int16, int16, uint8, uint8, uint32, *byte) xcbCookie
	formats          func(unsafe.Pointer) xcbCookie
	formatsReply     func(unsafe.Pointer, xcbCookie, unsafe.Pointer) unsafe.Pointer
	createPicture    func(unsafe.Pointer, uint32, uint32, uint32, uint32, unsafe.Pointer) xcbCookie
	freePicture      func(unsafe.Pointer, uint32) xcbCookie
	createCursor     func(unsafe.Pointer, uint32, uint32, uint16, uint16) xcbCookie
	err              error
}

func (a *App) windowControls() (*xControls, error) {
	if a.controls != nil {
		return a.controls, a.controls.err
	}
	c := &xControls{}
	a.controls = c
	lib, err := purego.Dlopen("libxcb.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		c.err = err
		return c, err
	}
	for name, target := range map[string]any{"xcb_configure_window_checked": &c.configure, "xcb_unmap_window_checked": &c.unmap, "xcb_map_window_checked": &c.mapWindow, "xcb_request_check": &c.check, "xcb_create_gc_checked": &c.createGC, "xcb_free_gc": &c.freeGC, "xcb_put_image_checked": &c.putImage} {
		if err := load(lib, name, target); err != nil {
			c.err = err
			return c, err
		}
	}
	if render, err := purego.Dlopen("libxcb-render.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
		for name, target := range map[string]any{"xcb_render_query_pict_formats": &c.formats, "xcb_render_query_pict_formats_reply": &c.formatsReply, "xcb_render_create_picture_checked": &c.createPicture, "xcb_render_free_picture": &c.freePicture, "xcb_render_create_cursor_checked": &c.createCursor} {
			if load(render, name, target) != nil {
				c.formats = nil
				break
			}
		}
	}
	return c, nil
}
func (a *App) checked(c *xControls, cookie xcbCookie) error {
	e := c.check(a.conn, cookie)
	if e == nil {
		return nil
	}
	code := *(*byte)(unsafe.Add(e, 1))
	a.x.free(e)
	return fmt.Errorf("platform: X11 request failed with error %d", code)
}
func (w *Window) Capabilities() WindowCapabilities {
	if w.wl != nil {
		return WindowCapabilities{Resize: true, CursorImage: w.wl.app.shm != nil}
	}
	c, err := w.app.windowControls()
	return WindowCapabilities{Resize: err == nil, Show: err == nil, Hide: err == nil, Focus: true, AlwaysOnTop: true, CursorImage: err == nil && c.formats != nil, PointerPosition: true}
}

func (w *Window) RefreshCursor() {
	if w.wl != nil {
		w.wl.applyCursor()
		w.wl.app.l.flush(w.wl.app.display)
	} else if !w.captured {
		w.applyCursorX11()
	}
}
func (w *Window) SetSize(width, height int) error {
	if width <= 0 || height <= 0 || width > 65535 || height > 65535 {
		return errors.New("platform: window dimensions must be in 1..65535")
	}
	if w.wl != nil {
		v := w.wl
		if v.fullscreen || v.maximized {
			return errors.New("platform: compositor controls the fullscreen or maximized size")
		}
		if v.closed {
			return errors.New("platform: window is closed")
		}
		width = max(width, v.minW)
		height = max(height, v.minH)
		if v.maxW > 0 {
			width = min(width, v.maxW)
		}
		if v.maxH > 0 {
			height = min(height, v.maxH)
		}
		v.width, v.height, v.defW, v.defH = width, height, width, height
		if !v.resizable {
			v.applySizeLimits(width, height, width, height)
		}
		v.applyViewport()
		v.pushResize()
		return nil
	}
	c, err := w.app.windowControls()
	if err != nil {
		return err
	}
	v := [2]uint32{uint32(width), uint32(height)}
	return w.app.checked(c, c.configure(w.app.conn, w.id, 4|8, &v[0]))
}
func (w *Window) Show() error {
	if w.wl != nil {
		return ErrUnsupported
	}
	c, err := w.app.windowControls()
	if err != nil {
		return err
	}
	return w.app.checked(c, c.mapWindow(w.app.conn, w.id))
}
func (w *Window) Hide() error {
	if w.wl != nil {
		return ErrUnsupported
	}
	c, err := w.app.windowControls()
	if err != nil {
		return err
	}
	return w.app.checked(c, c.unmap(w.app.conn, w.id))
}
func (w *Window) RequestFocus() error {
	if w.wl != nil {
		return ErrUnsupported
	}
	a := w.app
	var ev [32]byte
	msg := (*xcbClientMessageEvent)(unsafe.Pointer(&ev[0]))
	msg.ResponseType, msg.Format, msg.Window, msg.Type = xcbClientMessage, 32, w.id, a.atom("_NET_ACTIVE_WINDOW")
	msg.Data[0] = 1 // normal application; focus stealing policy remains with the WM
	a.x.sendEvent(a.conn, 0, a.screen.Root, xcbEventMaskSubRedirect|xcbEventMaskSubNotify, &ev[0])
	a.x.flush(a.conn)
	return nil
}
func (w *Window) SetAlwaysOnTop(on bool) error {
	if w.wl != nil {
		return ErrUnsupported
	}
	a := w.app
	var ev [32]byte
	msg := (*xcbClientMessageEvent)(unsafe.Pointer(&ev[0]))
	msg.ResponseType, msg.Format, msg.Window, msg.Type = xcbClientMessage, 32, w.id, a.atomNetWMState
	if on {
		msg.Data[0] = 1
	}
	msg.Data[1], msg.Data[3] = a.atom("_NET_WM_STATE_ABOVE"), 1
	a.x.sendEvent(a.conn, 0, a.screen.Root, xcbEventMaskSubRedirect|xcbEventMaskSubNotify, &ev[0])
	a.x.flush(a.conn)
	return nil
}
func (w *Window) SetPointerPosition(x, y float64) error {
	if w.wl != nil {
		return ErrUnsupported
	}
	if math.Abs(x) > 32767 || math.Abs(y) > 32767 {
		return errors.New("platform: pointer position exceeds X11 coordinate range")
	}
	w.app.x.warpPointer(w.app.conn, 0, w.id, 0, 0, 0, 0, int16(math.Round(x)), int16(math.Round(y)))
	w.app.x.flush(w.app.conn)
	return nil
}

func (w *Window) SetCursorImage(img image.Image, hotX, hotY int) error {
	pixels, width, height, err := cursorPixels(img, hotX, hotY)
	if err != nil {
		return err
	}
	if w.wl != nil {
		return w.wl.setCursorImage(img, width, height, hotX, hotY)
	}
	return w.setCursorX11(pixels, width, height, hotX, hotY)
}

// setCursorImage keeps one immutable SHM buffer until replacement. Destroying
// the client buffer after replacement is legal: the compositor retains its own
// reference to storage while it finishes displaying the previous cursor.
func (w *wlWindow) setCursorImage(img image.Image, width, height, hotX, hotY int) error {
	if w.app.shm == nil {
		return ErrUnsupported
	}
	buf := w.app.shmBuffer(img)
	if buf == nil {
		return errors.New("platform: allocate Wayland cursor buffer failed")
	}
	if w.cursorSurface == nil {
		w.cursorSurface = w.app.l.marshal(w.app.compositor, opCompositorCreateSurface, w.app.iface["wl_surface"], 0, 0)
		if w.cursorSurface == nil {
			w.app.l.marshal(buf, opBufferDestroy, nil, wlMarshalFlagDestroy)
			return errors.New("platform: create Wayland cursor surface failed")
		}
	}
	w.dropCursorImage()
	w.customCursor = buf
	w.customWidth, w.customHeight, w.hotX, w.hotY = width, height, hotX, hotY
	w.applyCursor()
	w.app.l.flush(w.app.display)
	return nil
}
func (w *wlWindow) dropCursorImage() {
	if w.customCursor != nil {
		w.app.l.marshal(w.customCursor, opBufferDestroy, nil, wlMarshalFlagDestroy)
		w.customCursor = nil
	}
}
func (w *Window) setCursorX11(pixels []byte, width, height, hotX, hotY int) error {
	a := w.app
	c, err := a.windowControls()
	if err != nil {
		return err
	}
	if c.formats == nil {
		return ErrUnsupported
	}
	reply := c.formatsReply(a.conn, c.formats(a.conn), nil)
	if reply == nil {
		return fmt.Errorf("%w: X Render formats unavailable", ErrUnsupported)
	}
	defer a.x.free(reply)
	n := *(*uint32)(unsafe.Add(reply, 8))
	var format uint32
	for i := uint32(0); i < n; i++ {
		p := unsafe.Add(reply, 32+uintptr(i)*28)
		// PICTFORMINFO: direct, depth32, R16/G8/B0/A24 with 8-bit masks.
		d := unsafe.Slice((*uint16)(unsafe.Add(p, 8)), 8)
		if *(*byte)(unsafe.Add(p, 4)) == 1 && *(*byte)(unsafe.Add(p, 5)) == 32 && d[0] == 16 && d[1] == 255 && d[2] == 8 && d[3] == 255 && d[4] == 0 && d[5] == 255 && d[6] == 24 && d[7] == 255 {
			format = *(*uint32)(p)
			break
		}
	}
	if format == 0 {
		return fmt.Errorf("%w: X Render has no ARGB32 format", ErrUnsupported)
	}
	pixmap := a.x.generateID(a.conn)
	a.x.createPixmap(a.conn, 32, pixmap, w.id, uint16(width), uint16(height))
	defer a.x.freePixmap(a.conn, pixmap)
	gc := a.x.generateID(a.conn)
	if err := a.checked(c, c.createGC(a.conn, gc, pixmap, 0, nil)); err != nil {
		return err
	}
	defer c.freeGC(a.conn, gc)
	// Split uploads below the server's request limit, keeping complete scanlines.
	rows := max(1, (int(a.x.maxRequestLength(a.conn))*4-64)/(width*4))
	for y := 0; y < height; y += rows {
		h := min(rows, height-y)
		data := pixels[y*width*4 : (y+h)*width*4]
		if err := a.checked(c, c.putImage(a.conn, 2, pixmap, gc, uint16(width), uint16(h), 0, int16(y), 0, 32, uint32(len(data)), &data[0])); err != nil {
			return err
		}
	}
	picture := a.x.generateID(a.conn)
	if err := a.checked(c, c.createPicture(a.conn, picture, pixmap, format, 0, nil)); err != nil {
		return err
	}
	defer c.freePicture(a.conn, picture)
	cursor := a.x.generateID(a.conn)
	if err := a.checked(c, c.createCursor(a.conn, cursor, picture, uint16(hotX), uint16(hotY))); err != nil {
		return err
	}
	old := w.shapeCursor
	w.shapeCursor = cursor
	if !w.captured {
		w.applyCursorX11()
	}
	if old != 0 {
		a.x.freeCursor(a.conn, old)
	}
	a.x.flush(a.conn)
	return nil
}
