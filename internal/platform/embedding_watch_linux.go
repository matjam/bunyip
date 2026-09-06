package platform

import (
	"errors"
	"structs"
	"unsafe"
)

type embeddedAttributes struct {
	_                     structs.HostLayout
	Prefix                [32]byte
	AllEvents, YourEvents uint32
	DoNotPropagate        uint16
	Pad                   [2]byte
}
type parentWatch struct {
	refs     int
	original uint32
}

func (a *App) parentMask(c *xControls, parent uint32) (uint32, error) {
	r := c.attributesReply(a.conn, c.getAttributes(a.conn, parent), nil)
	if r == nil {
		return 0, errors.New("platform: X11 parent attributes unavailable")
	}
	defer a.x.free(unsafe.Pointer(r))
	return r.YourEvents, nil
}

func (a *App) watchParent(c *xControls, parent uint32) error {
	if watch := a.parentWatches[parent]; watch != nil {
		watch.refs++
		return nil
	}
	original, err := a.parentMask(c, parent)
	if err != nil {
		return err
	}
	mask := original | xcbEventMaskStructure
	if mask != original {
		if err := a.checked(c, c.attributes(a.conn, parent, xcbCWEventMask, &mask)); err != nil {
			return err
		}
	}
	if a.parentWatches == nil {
		a.parentWatches = make(map[uint32]*parentWatch)
	}
	a.parentWatches[parent] = &parentWatch{refs: 1, original: original}
	return nil
}

func (a *App) unwatchParent(parent uint32) {
	w := a.parentWatches[parent]
	if w == nil {
		return
	}
	w.refs--
	if w.refs > 0 {
		return
	}
	delete(a.parentWatches, parent)
	c, err := a.windowControls()
	if err != nil {
		return
	}
	current, err := a.parentMask(c, parent)
	if err != nil {
		return
	} // already destroyed
	// Remove only the bit this subscription added; retain subsequent mask edits.
	mask := current &^ (xcbEventMaskStructure &^ w.original)
	if mask != current {
		_ = a.checked(c, c.attributes(a.conn, parent, xcbCWEventMask, &mask))
	}
}
