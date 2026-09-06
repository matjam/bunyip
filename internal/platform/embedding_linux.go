package platform

import (
	"errors"
	"math"
	"structs"
	"unsafe"
)

type embeddedGeometry struct {
	_                     structs.HostLayout
	Response, Depth       uint8
	Sequence              uint16
	Length, Root          uint32
	X, Y                  int16
	Width, Height, Border uint16
	Pad                   [2]byte
}

func (a *App) parentGeometry(c *xControls, parent uint32) (embeddedGeometry, error) {
	r := c.geometryReply(a.conn, c.geometry(a.conn, parent), nil)
	if r == nil {
		return embeddedGeometry{}, errors.New("platform: X11 embedding parent no longer exists")
	}
	defer a.x.free(unsafe.Pointer(r))
	if r.Root != a.screen.Root {
		return embeddedGeometry{}, errors.New("platform: X11 parent belongs to another screen")
	}
	return *r, nil
}

func (a *App) newEmbedded(cfg Config) (*Window, error) {
	p := cfg.Parent
	if a.wl != nil || p.Backend != 3 || p.Handle == 0 || p.Handle > math.MaxUint32 {
		return nil, ErrUnsupported
	}
	c, err := a.windowControls()
	if err != nil {
		return nil, err
	}
	parent := uint32(p.Handle)
	g, err := a.parentGeometry(c, parent)
	if err != nil {
		return nil, err
	}
	if err := a.watchParent(c, parent); err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			a.unwatchParent(parent)
		}
	}()
	id := a.x.generateID(a.conn)
	mask := uint32(xcbEventMaskKeyPress | xcbEventMaskKeyRelease | xcbEventMaskButtonPress | xcbEventMaskButtonRelease | xcbEventMaskEnter | xcbEventMaskLeave | xcbEventMaskMotion | xcbEventMaskExposure | xcbEventMaskVisibility | xcbEventMaskStructure | xcbEventMaskFocus)
	// Depth and visual inherit the parent's values.
	if err := a.checked(c, c.createChild(a.conn, 0, id, parent, 0, 0, max(1, g.Width), max(1, g.Height), 0, xcbWindowClassInputOutput, 0, xcbCWEventMask, &mask)); err != nil {
		return nil, err
	}
	w := &Window{app: a, id: id, parent: parent, width: int(g.Width), height: int(g.Height), mapped: true, visible: true}
	transferred = true
	a.windows[id] = w
	a.wakeWin.CompareAndSwap(0, id)
	if err := a.checked(c, c.mapWindow(a.conn, id)); err != nil {
		w.Close()
		return nil, err
	}
	a.x.flush(a.conn)
	return w, nil
}

func (w *Window) SetBounds(x, y, width, height int) error {
	if w.parent == 0 {
		return ErrUnsupported
	}
	if width <= 0 || height <= 0 || width > math.MaxUint16 || height > math.MaxUint16 || x < math.MinInt16 || x > math.MaxInt16 || y < math.MinInt16 || y > math.MaxInt16 {
		return errors.New("platform: embedded bounds exceed X11 limits")
	}
	c, err := w.app.windowControls()
	if err != nil {
		return err
	}
	values := [4]uint32{uint32(x), uint32(y), uint32(width), uint32(height)}
	if err := w.app.checked(c, c.configure(w.app.conn, w.id, 15, &values[0])); err != nil {
		return err
	}
	w.manualBounds = true
	w.app.x.flush(w.app.conn)
	return nil
}

func (a *App) syncEmbedded() {
	for _, w := range a.windows {
		if w.parent == 0 || w.closed {
			continue
		}
		c, err := a.windowControls()
		if err != nil {
			continue
		}
		g, err := a.parentGeometry(c, w.parent)
		if err != nil {
			w.hostLost = true
			a.push(Event{Kind: EventClose, Window: w})
			continue
		}
		if !w.manualBounds && (w.width != int(g.Width) || w.height != int(g.Height)) {
			v := [4]uint32{0, 0, uint32(max(1, g.Width)), uint32(max(1, g.Height))}
			if err := a.checked(c, c.configure(a.conn, w.id, 15, &v[0])); err == nil {
				a.x.flush(a.conn)
			}
		}
	}
}
