package platform

import (
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

// A Wayland listener is a C array of function pointers, one per event of the
// interface, so every handler here becomes a callback through purego. The
// callbacks are made once, on the first connection, because a process can
// hold only about two thousand of them and they are never released.
//
// Each handler takes the listener's data pointer and the proxy the event is
// for, then the event's arguments in the C ABI: int and fd as int32, uint as
// uint32, fixed as int32 with eight fractional bits, string as a pointer to
// NUL-terminated bytes, and object and array as pointers. The data pointer is
// unused: there is one app per process and it is in wlCurrent, and every
// proxy that matters is in the app's owner map.

var (
	wlNoopCallback uintptr

	cbRegistryGlobal, cbRegistryGlobalRemove uintptr

	cbSurfaceEnter, cbSurfaceLeave                       uintptr
	cbSurfacePreferredScale, cbSurfacePreferredTransform uintptr

	cbSeatCapabilities, cbSeatName uintptr

	cbPointerEnter, cbPointerLeave, cbPointerMotion, cbPointerButton, cbPointerAxis uintptr
	cbPointerFrame, cbPointerAxisSource, cbPointerAxisStop, cbPointerAxisDiscrete   uintptr
	cbPointerAxisValue120, cbPointerAxisRelDir, cbPointerWarp                       uintptr

	cbKeyboardKeymap, cbKeyboardEnter, cbKeyboardLeave       uintptr
	cbKeyboardKey, cbKeyboardModifiers, cbKeyboardRepeatInfo uintptr

	cbOutputGeometry, cbOutputMode, cbOutputDone     uintptr
	cbOutputScale, cbOutputName, cbOutputDescription uintptr

	cbDataDeviceDataOffer, cbDataDeviceEnter, cbDataDeviceLeave      uintptr
	cbDataDeviceMotion, cbDataDeviceDrop, cbDataDeviceSelection      uintptr
	cbDataSourceTarget, cbDataSourceSend, cbDataSourceCancelled      uintptr
	cbDataSourceDndDrop, cbDataSourceDndFinished, cbDataSourceAction uintptr
	cbDataOfferOffer, cbDataOfferSourceActions, cbDataOfferAction    uintptr

	cbWMBasePing          uintptr
	cbXdgSurfaceConfigure uintptr

	cbToplevelConfigure, cbToplevelClose                uintptr
	cbToplevelConfigureBounds, cbToplevelWMCapabilities uintptr
	cbDecorationConfigure                               uintptr
	cbFractionalScale                                   uintptr
	cbRelativeMotion                                    uintptr
	cbLockedPointerLocked, cbLockedPointerUnlocked      uintptr
)

// wlCallbacksReady guards the one-time creation.
var wlCallbacksReady bool

// wlInitCallbacks makes every listener callback. It runs once.
func wlInitCallbacks() {
	if wlCallbacksReady {
		return
	}
	wlCallbacksReady = true

	wlNoopCallback = purego.NewCallback(func(data, proxy unsafe.Pointer) {})

	// wl_registry.
	cbRegistryGlobal = purego.NewCallback(func(data, proxy unsafe.Pointer, name uint32, iface *byte, version uint32) {
		if a := wlCurrent; a != nil {
			a.onGlobal(name, goString(iface), version)
		}
	})
	cbRegistryGlobalRemove = purego.NewCallback(func(data, proxy unsafe.Pointer, name uint32) {
		if a := wlCurrent; a != nil {
			a.onGlobalRemove(name)
		}
	})

	// wl_surface.
	cbSurfaceEnter = purego.NewCallback(func(data, proxy, output unsafe.Pointer) {
		a := wlCurrent
		if a == nil {
			return
		}
		if w := a.owner[proxy]; w != nil {
			w.onOutputs[output] = true
			w.refreshScale()
		}
	})
	cbSurfaceLeave = purego.NewCallback(func(data, proxy, output unsafe.Pointer) {
		a := wlCurrent
		if a == nil {
			return
		}
		if w := a.owner[proxy]; w != nil {
			delete(w.onOutputs, output)
			w.refreshScale()
		}
	})
	cbSurfacePreferredScale = purego.NewCallback(func(data, proxy unsafe.Pointer, factor int32) {
		a := wlCurrent
		if a == nil || factor < 1 {
			return
		}
		if w := a.owner[proxy]; w != nil {
			w.preferredScale = int(factor)
			w.refreshScale()
		}
	})
	cbSurfacePreferredTransform = purego.NewCallback(func(data, proxy unsafe.Pointer, transform uint32) {})

	// wl_seat.
	cbSeatCapabilities = purego.NewCallback(func(data, proxy unsafe.Pointer, caps uint32) {
		if a := wlCurrent; a != nil {
			a.onSeatCapabilities(caps)
		}
	})
	cbSeatName = purego.NewCallback(func(data, proxy unsafe.Pointer, name *byte) {})

	// wl_pointer.
	cbPointerEnter = purego.NewCallback(func(data, proxy unsafe.Pointer, serial uint32, surface unsafe.Pointer, sx, sy int32) {
		if a := wlCurrent; a != nil {
			a.lastSerial = serial // the earliest serial set_selection can quote
			a.onPointerEnter(serial, surface, sx, sy)
		}
	})
	cbPointerLeave = purego.NewCallback(func(data, proxy unsafe.Pointer, serial uint32, surface unsafe.Pointer) {
		if a := wlCurrent; a != nil {
			a.onPointerLeave(surface)
		}
	})
	cbPointerMotion = purego.NewCallback(func(data, proxy unsafe.Pointer, time uint32, sx, sy int32) {
		if a := wlCurrent; a != nil {
			a.onPointerMotion(sx, sy)
		}
	})
	cbPointerButton = purego.NewCallback(func(data, proxy unsafe.Pointer, serial, time, button, state uint32) {
		if a := wlCurrent; a != nil {
			a.lastSerial = serial // set_selection has to quote an input serial
			a.onPointerButton(button, state)
		}
	})
	cbPointerAxis = purego.NewCallback(func(data, proxy unsafe.Pointer, time, axis uint32, value int32) {
		if a := wlCurrent; a != nil {
			a.onAxis(axis, value)
		}
	})
	cbPointerFrame = purego.NewCallback(func(data, proxy unsafe.Pointer) {
		if a := wlCurrent; a != nil {
			a.flushAxis()
		}
	})
	cbPointerAxisSource = purego.NewCallback(func(data, proxy unsafe.Pointer, source uint32) {})
	cbPointerAxisStop = purego.NewCallback(func(data, proxy unsafe.Pointer, time, axis uint32) {})
	cbPointerAxisDiscrete = purego.NewCallback(func(data, proxy unsafe.Pointer, axis uint32, discrete int32) {
		if a := wlCurrent; a != nil {
			a.onAxisSteps(axis, float64(discrete))
		}
	})
	cbPointerAxisValue120 = purego.NewCallback(func(data, proxy unsafe.Pointer, axis uint32, value120 int32) {
		if a := wlCurrent; a != nil {
			a.onAxisSteps(axis, float64(value120)/120)
		}
	})
	cbPointerAxisRelDir = purego.NewCallback(func(data, proxy unsafe.Pointer, axis, direction uint32) {})
	cbPointerWarp = purego.NewCallback(func(data, proxy unsafe.Pointer, sx, sy int32) {})

	// wl_keyboard.
	cbKeyboardKeymap = purego.NewCallback(func(data, proxy unsafe.Pointer, format uint32, fd int32, size uint32) {
		if a := wlCurrent; a != nil {
			a.onKeymap(format, fd, size)
		}
	})
	cbKeyboardEnter = purego.NewCallback(func(data, proxy unsafe.Pointer, serial uint32, surface, keys unsafe.Pointer) {
		a := wlCurrent
		if a == nil {
			return
		}
		w := a.owner[surface]
		if w == nil {
			return
		}
		a.lastSerial = serial // the earliest serial set_selection can quote
		a.kbFocus = w
		w.setFocused(true)
	})
	cbKeyboardLeave = purego.NewCallback(func(data, proxy unsafe.Pointer, serial uint32, surface unsafe.Pointer) {
		a := wlCurrent
		if a == nil {
			return
		}
		w := a.owner[surface]
		if w == nil {
			return
		}
		if a.kbFocus == w {
			a.kbFocus = nil
		}
		a.repeatKey = 0
		w.setFocused(false)
	})
	cbKeyboardKey = purego.NewCallback(func(data, proxy unsafe.Pointer, serial, time, key, state uint32) {
		if a := wlCurrent; a != nil {
			a.lastSerial = serial // set_selection has to quote an input serial
			a.onKey(key, state)
		}
	})
	cbKeyboardModifiers = purego.NewCallback(func(data, proxy unsafe.Pointer, serial, depressed, latched, locked, group uint32) {
		if a := wlCurrent; a != nil {
			a.onModifiers(depressed, latched, locked, group)
		}
	})
	cbKeyboardRepeatInfo = purego.NewCallback(func(data, proxy unsafe.Pointer, rate, delay int32) {
		a := wlCurrent
		if a == nil {
			return
		}
		a.repeatRate, a.repeatDelay = rate, delay
		if rate <= 0 {
			a.repeatKey = 0
		}
	})

	// wl_data_device, wl_data_source and wl_data_offer: the clipboard.
	// Drag and drop is not handled, so the drag events do nothing.
	cbDataDeviceDataOffer = purego.NewCallback(func(data, proxy, offer unsafe.Pointer) {
		if a := wlCurrent; a != nil {
			a.onDataOffer(offer)
		}
	})
	cbDataDeviceEnter = purego.NewCallback(func(data, proxy unsafe.Pointer, serial uint32, surface unsafe.Pointer, x, y int32, offer unsafe.Pointer) {
	})
	cbDataDeviceLeave = purego.NewCallback(func(data, proxy unsafe.Pointer) {})
	cbDataDeviceMotion = purego.NewCallback(func(data, proxy unsafe.Pointer, time uint32, x, y int32) {})
	cbDataDeviceDrop = purego.NewCallback(func(data, proxy unsafe.Pointer) {})
	cbDataDeviceSelection = purego.NewCallback(func(data, proxy, offer unsafe.Pointer) {
		if a := wlCurrent; a != nil {
			a.onSelection(offer)
		}
	})
	cbDataSourceTarget = purego.NewCallback(func(data, proxy unsafe.Pointer, mime *byte) {})
	cbDataSourceSend = purego.NewCallback(func(data, proxy unsafe.Pointer, mime *byte, fd int32) {
		a := wlCurrent
		if a == nil || proxy != a.clipSource {
			syscall.Close(int(fd))
			return
		}
		a.writeClipboard(int(fd))
	})
	cbDataSourceCancelled = purego.NewCallback(func(data, proxy unsafe.Pointer) {
		a := wlCurrent
		if a != nil && proxy == a.clipSource {
			a.destroySource() // another client took the selection
		}
	})
	cbDataSourceDndDrop = purego.NewCallback(func(data, proxy unsafe.Pointer) {})
	cbDataSourceDndFinished = purego.NewCallback(func(data, proxy unsafe.Pointer) {})
	cbDataSourceAction = purego.NewCallback(func(data, proxy unsafe.Pointer, action uint32) {})
	cbDataOfferOffer = purego.NewCallback(func(data, proxy unsafe.Pointer, mime *byte) {
		a := wlCurrent
		if a == nil {
			return
		}
		if _, ok := a.offerMimes[proxy]; ok {
			a.offerMimes[proxy] = append(a.offerMimes[proxy], goString(mime))
		}
	})
	cbDataOfferSourceActions = purego.NewCallback(func(data, proxy unsafe.Pointer, actions uint32) {})
	cbDataOfferAction = purego.NewCallback(func(data, proxy unsafe.Pointer, action uint32) {})

	// wl_output.
	cbOutputGeometry = purego.NewCallback(func(data, proxy unsafe.Pointer, x, y, physWidth, physHeight, subpixel int32,
		make, model *byte, transform int32) {
	})
	cbOutputMode = purego.NewCallback(func(data, proxy unsafe.Pointer, flags uint32, width, height, refresh int32) {})
	cbOutputDone = purego.NewCallback(func(data, proxy unsafe.Pointer) {
		a := wlCurrent
		if a == nil {
			return
		}
		for _, w := range a.wins {
			if w.onOutputs[proxy] {
				w.refreshScale()
			}
		}
	})
	cbOutputScale = purego.NewCallback(func(data, proxy unsafe.Pointer, factor int32) {
		a := wlCurrent
		if a == nil || factor < 1 {
			return
		}
		if o := a.outputs[proxy]; o != nil {
			o.scale = int(factor)
		}
	})
	cbOutputName = purego.NewCallback(func(data, proxy unsafe.Pointer, name *byte) {})
	cbOutputDescription = purego.NewCallback(func(data, proxy unsafe.Pointer, description *byte) {})

	// xdg_wm_base and the toplevel it makes.
	cbWMBasePing = purego.NewCallback(func(data, proxy unsafe.Pointer, serial uint32) {
		if a := wlCurrent; a != nil {
			a.l.send(proxy, opXdgWMBasePong, uintptr(serial))
			a.l.flush(a.display)
		}
	})
	cbXdgSurfaceConfigure = purego.NewCallback(func(data, proxy unsafe.Pointer, serial uint32) {
		a := wlCurrent
		if a == nil {
			return
		}
		if w := a.owner[proxy]; w != nil {
			w.onSurfaceConfigure(serial)
		}
	})
	cbToplevelConfigure = purego.NewCallback(func(data, proxy unsafe.Pointer, width, height int32, states unsafe.Pointer) {
		a := wlCurrent
		if a == nil {
			return
		}
		if w := a.owner[proxy]; w != nil {
			w.onToplevelConfigure(width, height, (*wlArray)(states))
		}
	})
	cbToplevelClose = purego.NewCallback(func(data, proxy unsafe.Pointer) {
		a := wlCurrent
		if a == nil {
			return
		}
		if w := a.owner[proxy]; w != nil {
			a.push(Event{Kind: EventClose, Window: w.out})
		}
	})
	cbToplevelConfigureBounds = purego.NewCallback(func(data, proxy unsafe.Pointer, width, height int32) {})
	cbToplevelWMCapabilities = purego.NewCallback(func(data, proxy, capabilities unsafe.Pointer) {})

	// zxdg_toplevel_decoration_v1.
	cbDecorationConfigure = purego.NewCallback(func(data, proxy unsafe.Pointer, mode uint32) {
		a := wlCurrent
		if a == nil {
			return
		}
		if w := a.owner[proxy]; w != nil {
			w.serverDecors = mode == xdgDecorationModeServerSide
		}
	})

	// wp_fractional_scale_v1.
	cbFractionalScale = purego.NewCallback(func(data, proxy unsafe.Pointer, scale uint32) {
		a := wlCurrent
		if a == nil {
			return
		}
		if w := a.owner[proxy]; w != nil {
			w.onFractionalScale(scale)
		}
	})

	// zwp_relative_pointer_v1 and zwp_locked_pointer_v1.
	cbRelativeMotion = purego.NewCallback(func(data, proxy unsafe.Pointer, utimeHi, utimeLo uint32,
		dx, dy, dxUnaccel, dyUnaccel int32) {
		if a := wlCurrent; a != nil {
			a.onRelativeMotion(dx, dy)
		}
	})
	cbLockedPointerLocked = purego.NewCallback(func(data, proxy unsafe.Pointer) {})
	cbLockedPointerUnlocked = purego.NewCallback(func(data, proxy unsafe.Pointer) {})
}
