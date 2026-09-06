package platform

import (
	"errors"
	"slices"
	"testing"
	"unsafe"
)

func TestEmbeddedX11OwnsOnlyChildAndFitsParent(t *testing.T) {
	geometry := embeddedGeometry{Root: 1, Width: 120, Height: 90}
	var destroyed []uint32
	var configured [][4]uint32
	parentMask := uint32(0)
	a := &App{screen: &xcbScreen{Root: 1}, windows: map[uint32]*Window{}}
	a.x = &xlib{generateID: func(unsafe.Pointer) uint32 { return 8 }, free: func(unsafe.Pointer) {}, flush: func(unsafe.Pointer) int32 { return 1 }, destroyWindow: func(_ unsafe.Pointer, id uint32) xcbCookie { destroyed = append(destroyed, id); return xcbCookie{} }}
	a.controls = &xControls{
		getAttributes: func(unsafe.Pointer, uint32) xcbCookie { return xcbCookie{} },
		attributesReply: func(unsafe.Pointer, xcbCookie, unsafe.Pointer) *embeddedAttributes {
			return &embeddedAttributes{YourEvents: parentMask}
		},
		geometry: func(_ unsafe.Pointer, id uint32) xcbCookie {
			if id != 7 {
				t.Error("wrong parent geometry")
			}
			return xcbCookie{}
		},
		geometryReply: func(unsafe.Pointer, xcbCookie, unsafe.Pointer) *embeddedGeometry { return &geometry },
		check:         func(unsafe.Pointer, xcbCookie) unsafe.Pointer { return nil },
		attributes: func(_ unsafe.Pointer, id, mask uint32, v *uint32) xcbCookie {
			if id != 7 || mask != xcbCWEventMask || (*v != xcbEventMaskStructure && *v != 0) {
				t.Error("parent event subscription replaced unrelated state")
			}
			parentMask = *v
			return xcbCookie{}
		},
		createChild: func(_ unsafe.Pointer, depth uint8, id, parent uint32, x, y int16, width, height, border, class uint16, visual, mask uint32, values *uint32) xcbCookie {
			if id != 8 || parent != 7 || width != 120 || height != 90 || visual != 0 || depth != 0 {
				t.Error("child did not inherit parent geometry/visual")
			}
			return xcbCookie{}
		},
		mapWindow: func(_ unsafe.Pointer, id uint32) xcbCookie {
			if id != 8 {
				t.Error("mapped borrowed parent")
			}
			return xcbCookie{}
		},
		configure: func(_ unsafe.Pointer, id uint32, mask uint16, v *uint32) xcbCookie {
			if id != 8 || mask != 15 {
				t.Error("modified borrowed parent")
			}
			configured = append(configured, *(*[4]uint32)(unsafe.Pointer(v)))
			return xcbCookie{}
		},
	}
	w, err := a.NewWindow(Config{Parent: &NativeParent{Backend: 3, Handle: 7}})
	if err != nil {
		t.Fatal(err)
	}
	geometry.Width = 200
	a.syncEmbedded()
	if !slices.Equal(configured, [][4]uint32{{0, 0, 200, 90}}) {
		t.Fatalf("auto-fit=%v", configured)
	}
	if err := w.SetBounds(10, 15, 80, 60); err != nil {
		t.Fatal(err)
	}
	geometry.Width = 300
	a.syncEmbedded()
	if len(configured) != 2 || configured[1] != [4]uint32{10, 15, 80, 60} {
		t.Fatalf("manual bounds=%v", configured)
	}
	if !w.Capabilities().EmbeddedBounds || w.Capabilities().AlwaysOnTop || w.Capabilities().Resize {
		t.Fatal("embedded capabilities expose host controls")
	}
	w.Close()
	w.Close()
	if parentMask != 0 {
		t.Error("parent notification subscription survived last child")
	}
	if !slices.Equal(destroyed, []uint32{8}) || len(a.windows) != 0 {
		t.Fatalf("detach destroyed=%v windows=%v", destroyed, a.windows)
	}
}

func TestEmbeddedParentSubscriptionLivesUntilLastChild(t *testing.T) {
	const parent = 7
	current := uint32(xcbEventMaskKeyPress)
	a := &App{windows: map[uint32]*Window{}, x: &xlib{free: func(unsafe.Pointer) {}, destroyWindow: func(unsafe.Pointer, uint32) xcbCookie { return xcbCookie{} }, flush: func(unsafe.Pointer) int32 { return 1 }}}
	c := &xControls{check: func(unsafe.Pointer, xcbCookie) unsafe.Pointer { return nil }, getAttributes: func(unsafe.Pointer, uint32) xcbCookie { return xcbCookie{} }, attributesReply: func(unsafe.Pointer, xcbCookie, unsafe.Pointer) *embeddedAttributes {
		return &embeddedAttributes{YourEvents: current}
	}, attributes: func(_ unsafe.Pointer, id, mask uint32, v *uint32) xcbCookie {
		if id != parent || mask != xcbCWEventMask {
			t.Fatal("wrong parent mask request")
		}
		current = *v
		return xcbCookie{}
	}}
	a.controls = c
	for _, id := range []uint32{8, 9} {
		if err := a.watchParent(c, parent); err != nil {
			t.Fatal(err)
		}
		a.windows[id] = &Window{app: a, id: id, parent: parent}
	}
	if current != xcbEventMaskKeyPress|xcbEventMaskStructure {
		t.Fatal("prior parent events erased")
	}
	a.windows[8].Close()
	if current&xcbEventMaskStructure == 0 || a.parentWatches[parent].refs != 1 {
		t.Fatal("first child removed sibling subscription")
	}
	current |= xcbEventMaskProperty // another consumer adds events while children live
	a.windows[9].Close()
	if current != xcbEventMaskKeyPress|xcbEventMaskProperty || len(a.parentWatches) != 0 {
		t.Fatalf("last close restored mask incorrectly: %x", current)
	}
	current |= xcbEventMaskStructure
	if err := a.watchParent(c, parent); err != nil {
		t.Fatal(err)
	}
	a.unwatchParent(parent)
	if current&xcbEventMaskStructure == 0 {
		t.Fatal("removed pre-existing parent subscription")
	}
	var reply embeddedAttributes
	if unsafe.Sizeof(reply) != 44 || unsafe.Offsetof(reply.YourEvents) != 36 {
		t.Fatal("X11 window attributes ABI mismatch")
	}
}

func TestEmbeddedX11RejectsForeignScreenAndWayland(t *testing.T) {
	a := &App{wl: &wlApp{}}
	if _, err := a.NewWindow(Config{Parent: &NativeParent{Backend: 3, Handle: 1}}); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	a = &App{screen: &xcbScreen{Root: 1}, x: &xlib{free: func(unsafe.Pointer) {}}}
	c := &xControls{geometry: func(unsafe.Pointer, uint32) xcbCookie { return xcbCookie{} }, geometryReply: func(unsafe.Pointer, xcbCookie, unsafe.Pointer) *embeddedGeometry { return &embeddedGeometry{Root: 2} }}
	if _, err := a.parentGeometry(c, 1); err == nil {
		t.Fatal("foreign screen accepted")
	}
	c.geometryReply = func(unsafe.Pointer, xcbCookie, unsafe.Pointer) *embeddedGeometry { return nil }
	if _, err := a.parentGeometry(c, 1); err == nil {
		t.Fatal("destroyed parent accepted")
	}
	var g embeddedGeometry
	if unsafe.Sizeof(g) != 24 || unsafe.Offsetof(g.Root) != 8 || unsafe.Offsetof(g.Width) != 16 {
		t.Fatal("xcb geometry reply ABI mismatch")
	}
}
