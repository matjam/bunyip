package platform

import (
	"structs"
	"unsafe"
)

// The protocols that libwayland-client does not export are described here
// and built into C interface tables at run time. libwayland-client exports
// wl_registry_interface, wl_compositor_interface and the rest of the core
// protocol as symbols, so those are read from the library instead; xdg-shell,
// xdg-decoration, relative-pointer, pointer-constraints, fractional-scale,
// viewporter and xdg-toplevel-icon live in wayland-protocols, which ships
// only XML, so their tables are built here.
//
// Every signature string is what wayland-scanner emits for the same message:
// a leading decimal "since" version when the message is not in version one,
// then one character per argument, with "?" before a nullable string or
// object, and "sun" in place of a new_id whose interface is not fixed. The
// types array holds one entry per signature argument, naming the interface
// for object and new_id arguments and nothing for the rest.

// wlMessage mirrors struct wl_message.
type wlMessage struct {
	_         structs.HostLayout
	Name      *byte
	Signature *byte
	Types     **wlInterface
}

// wlInterface mirrors struct wl_interface.
type wlInterface struct {
	_           structs.HostLayout
	Name        *byte
	Version     int32
	MethodCount int32
	Methods     *wlMessage
	EventCount  int32
	Events      *wlMessage
}

// wlArray mirrors struct wl_array, which carries the variable-length
// arguments of xdg_toplevel.configure and wl_keyboard.enter.
type wlArray struct {
	_     structs.HostLayout
	Size  uintptr
	Alloc uintptr
	Data  unsafe.Pointer
}

// u32s reads a wl_array of uint32 values.
func (a *wlArray) u32s() []uint32 {
	if a == nil || a.Data == nil || a.Size < 4 {
		return nil
	}
	return unsafe.Slice((*uint32)(a.Data), a.Size/4)
}

// protoMessage is one request or event before it becomes a wl_message.
type protoMessage struct {
	name  string
	sig   string
	types []string // one entry per signature argument; "" where there is no interface
}

// protoInterface is one interface before it becomes a wl_interface.
type protoInterface struct {
	name    string
	version int32
	methods []protoMessage
	events  []protoMessage
}

// Request opcodes for the protocols built here. The names follow the
// interface and the request, so a call site reads like the protocol XML.
const (
	opXdgWMBaseDestroy          = 0
	opXdgWMBaseCreatePositioner = 1
	opXdgWMBaseGetXdgSurface    = 2
	opXdgWMBasePong             = 3

	opXdgSurfaceDestroy           = 0
	opXdgSurfaceGetToplevel       = 1
	opXdgSurfaceGetPopup          = 2
	opXdgSurfaceSetWindowGeometry = 3
	opXdgSurfaceAckConfigure      = 4

	opXdgToplevelDestroy         = 0
	opXdgToplevelSetParent       = 1
	opXdgToplevelSetTitle        = 2
	opXdgToplevelSetAppID        = 3
	opXdgToplevelSetMaxSize      = 7
	opXdgToplevelSetMinSize      = 8
	opXdgToplevelSetMaximized    = 9
	opXdgToplevelUnsetMaximized  = 10
	opXdgToplevelSetFullscreen   = 11
	opXdgToplevelUnsetFullscreen = 12
	opXdgToplevelSetMinimized    = 13

	opXdgDecorationManagerDestroy  = 0
	opXdgDecorationManagerGetDecor = 1
	opXdgDecorationDestroy         = 0
	opXdgDecorationSetMode         = 1
	opXdgDecorationUnsetMode       = 2

	opRelativePointerManagerDestroy = 0
	opRelativePointerManagerGetPtr  = 1
	opRelativePointerDestroy        = 0

	opToplevelIconMgrDestroy    = 0
	opToplevelIconMgrCreateIcon = 1
	opToplevelIconMgrSetIcon    = 2
	opToplevelIconDestroy       = 0
	opToplevelIconSetName       = 1
	opToplevelIconAddBuffer     = 2

	opFractionalScaleMgrDestroy = 0
	opFractionalScaleMgrGet     = 1
	opFractionalScaleDestroy    = 0

	opViewporterDestroy      = 0
	opViewporterGetViewport  = 1
	opViewportDestroy        = 0
	opViewportSetSource      = 1
	opViewportSetDestination = 2

	opPointerConstraintsDestroy     = 0
	opPointerConstraintsLockPointer = 1
	opPointerConstraintsConfinePtr  = 2
	opLockedPointerDestroy          = 0
	opLockedPointerSetPositionHint  = 1
	opLockedPointerSetRegion        = 2
	opConfinedPointerDestroy        = 0
)

// Protocol enumeration values.
const (
	xdgToplevelStateMaximized  = 1
	xdgToplevelStateFullscreen = 2
	xdgToplevelStateResizing   = 3
	xdgToplevelStateActivated  = 4
	// Suspended arrived in xdg_toplevel version six. A compositor that
	// offers less never sends it, so the window counts as visible.
	xdgToplevelStateSuspended = 9

	xdgDecorationModeClientSide = 1
	xdgDecorationModeServerSide = 2

	pointerConstraintLifetimeOneshot    = 1
	pointerConstraintLifetimePersistent = 2
)

// Request opcodes for the core protocol, whose interface tables come from
// libwayland-client. The clipboard is core: wl_data_device_manager,
// wl_data_device, wl_data_source and wl_data_offer are all exported by
// the library, so only their opcodes are named here.
const (
	opDisplaySync        = 0
	opDisplayGetRegistry = 1

	opRegistryBind = 0

	opCompositorCreateSurface = 0
	opCompositorCreateRegion  = 1

	opSurfaceDestroy         = 0
	opSurfaceAttach          = 1
	opSurfaceDamage          = 2
	opSurfaceFrame           = 3
	opSurfaceSetOpaqueRegion = 4
	opSurfaceSetInputRegion  = 5
	opSurfaceCommit          = 6
	opSurfaceSetBufferScale  = 8
	opSurfaceDamageBuffer    = 9

	opSeatGetPointer  = 0
	opSeatGetKeyboard = 1
	opSeatRelease     = 3

	opPointerSetCursor = 0
	opPointerRelease   = 1

	opKeyboardRelease = 0

	opOutputRelease = 0

	opDataDeviceManagerCreateSource = 0
	opDataDeviceManagerGetDevice    = 1

	opDataSourceOffer   = 0
	opDataSourceDestroy = 1

	opDataDeviceStartDrag    = 0
	opDataDeviceSetSelection = 1
	opDataDeviceRelease      = 2 // the destructor, from version two

	opDataOfferAccept  = 0
	opDataOfferReceive = 1
	opDataOfferDestroy = 2

	opShmCreatePool    = 0
	opShmPoolCreateBuf = 0
	opShmPoolDestroy   = 1
	opShmPoolResize    = 2
	opBufferDestroy    = 0
	opRegionDestroy    = 0
	opRegionAdd        = 1
	opRegionSubtract   = 2
)

// Event opcodes, used to place each handler in a listener array.
const (
	evRegistryGlobal       = 0
	evRegistryGlobalRemove = 1

	evSurfaceEnter                = 0
	evSurfaceLeave                = 1
	evSurfacePreferredBufferScale = 2
	evSurfacePreferredBufferXform = 3

	evSeatCapabilities = 0
	evSeatName         = 1

	evPointerEnter          = 0
	evPointerLeave          = 1
	evPointerMotion         = 2
	evPointerButton         = 3
	evPointerAxis           = 4
	evPointerFrame          = 5
	evPointerAxisSource     = 6
	evPointerAxisStop       = 7
	evPointerAxisDiscrete   = 8
	evPointerAxisValue120   = 9
	evPointerAxisRelDir     = 10
	evPointerWarp           = 11
	evKeyboardKeymap        = 0
	evKeyboardEnter         = 1
	evKeyboardLeave         = 2
	evKeyboardKey           = 3
	evKeyboardModifiers     = 4
	evKeyboardRepeatInfo    = 5
	evDataSourceTarget      = 0
	evDataSourceSend        = 1
	evDataSourceCancelled   = 2
	evDataDeviceDataOffer   = 0
	evDataDeviceEnter       = 1
	evDataDeviceLeave       = 2
	evDataDeviceMotion      = 3
	evDataDeviceDrop        = 4
	evDataDeviceSelection   = 5
	evDataOfferOffer        = 0
	evDataOfferSourceAction = 1
	evDataOfferAction       = 2
	evOutputGeometry        = 0
	evOutputMode            = 1
	evOutputDone            = 2
	evOutputScale           = 3
	evOutputName            = 4
	evOutputDescription     = 5
	evToplevelIconSize      = 0
	evToplevelIconDone      = 1
	evFractionalScalePref   = 0
	evXdgWMBasePing         = 0
	evXdgSurfaceConfigure   = 0
	evXdgToplevelConfigure  = 0
	evXdgToplevelClose      = 1
	evXdgToplevelConfBounds = 2
	evXdgToplevelWMCaps     = 3
	evXdgDecorationConfig   = 0
	evRelativePointerMotion = 0
	evLockedPointerLocked   = 0
	evLockedPointerUnlocked = 1
	evConfinedPointerConf   = 0
	evConfinedPointerUnconf = 1
)

// wlProtocols describes every interface this layer builds itself. The
// message order is the protocol XML's order, because the index is the
// opcode on the wire.
var wlProtocols = []protoInterface{
	{
		name: "xdg_wm_base", version: 7,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "create_positioner", sig: "n", types: []string{"xdg_positioner"}},
			{name: "get_xdg_surface", sig: "no", types: []string{"xdg_surface", "wl_surface"}},
			{name: "pong", sig: "u", types: []string{""}},
		},
		events: []protoMessage{
			{name: "ping", sig: "u", types: []string{""}},
		},
	},
	{
		name: "xdg_positioner", version: 7,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "set_size", sig: "ii", types: []string{"", ""}},
			{name: "set_anchor_rect", sig: "iiii", types: []string{"", "", "", ""}},
			{name: "set_anchor", sig: "u", types: []string{""}},
			{name: "set_gravity", sig: "u", types: []string{""}},
			{name: "set_constraint_adjustment", sig: "u", types: []string{""}},
			{name: "set_offset", sig: "ii", types: []string{"", ""}},
			{name: "set_reactive", sig: "3", types: nil},
			{name: "set_parent_size", sig: "3ii", types: []string{"", ""}},
			{name: "set_parent_configure", sig: "3u", types: []string{""}},
		},
	},
	{
		name: "xdg_surface", version: 7,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "get_toplevel", sig: "n", types: []string{"xdg_toplevel"}},
			{name: "get_popup", sig: "n?oo", types: []string{"xdg_popup", "xdg_surface", "xdg_positioner"}},
			{name: "set_window_geometry", sig: "iiii", types: []string{"", "", "", ""}},
			{name: "ack_configure", sig: "u", types: []string{""}},
		},
		events: []protoMessage{
			{name: "configure", sig: "u", types: []string{""}},
		},
	},
	{
		name: "xdg_toplevel", version: 7,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "set_parent", sig: "?o", types: []string{"xdg_toplevel"}},
			{name: "set_title", sig: "s", types: []string{""}},
			{name: "set_app_id", sig: "s", types: []string{""}},
			{name: "show_window_menu", sig: "ouii", types: []string{"wl_seat", "", "", ""}},
			{name: "move", sig: "ou", types: []string{"wl_seat", ""}},
			{name: "resize", sig: "ouu", types: []string{"wl_seat", "", ""}},
			{name: "set_max_size", sig: "ii", types: []string{"", ""}},
			{name: "set_min_size", sig: "ii", types: []string{"", ""}},
			{name: "set_maximized", sig: "", types: nil},
			{name: "unset_maximized", sig: "", types: nil},
			{name: "set_fullscreen", sig: "?o", types: []string{"wl_output"}},
			{name: "unset_fullscreen", sig: "", types: nil},
			{name: "set_minimized", sig: "", types: nil},
		},
		events: []protoMessage{
			{name: "configure", sig: "iia", types: []string{"", "", ""}},
			{name: "close", sig: "", types: nil},
			{name: "configure_bounds", sig: "4ii", types: []string{"", ""}},
			{name: "wm_capabilities", sig: "5a", types: []string{""}},
		},
	},
	{
		name: "xdg_popup", version: 7,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "grab", sig: "ou", types: []string{"wl_seat", ""}},
			{name: "reposition", sig: "3ou", types: []string{"xdg_positioner", ""}},
		},
		events: []protoMessage{
			{name: "configure", sig: "iiii", types: []string{"", "", "", ""}},
			{name: "popup_done", sig: "", types: nil},
			{name: "repositioned", sig: "3u", types: []string{""}},
		},
	},
	{
		name: "zxdg_decoration_manager_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "get_toplevel_decoration", sig: "no", types: []string{"zxdg_toplevel_decoration_v1", "xdg_toplevel"}},
		},
	},
	{
		name: "zxdg_toplevel_decoration_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "set_mode", sig: "u", types: []string{""}},
			{name: "unset_mode", sig: "", types: nil},
		},
		events: []protoMessage{
			{name: "configure", sig: "u", types: []string{""}},
		},
	},
	{
		name: "xdg_toplevel_icon_manager_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "create_icon", sig: "n", types: []string{"xdg_toplevel_icon_v1"}},
			{name: "set_icon", sig: "o?o", types: []string{"xdg_toplevel", "xdg_toplevel_icon_v1"}},
		},
		events: []protoMessage{
			{name: "icon_size", sig: "i", types: []string{""}},
			{name: "done", sig: "", types: nil},
		},
	},
	{
		name: "xdg_toplevel_icon_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "set_name", sig: "s", types: []string{""}},
			{name: "add_buffer", sig: "oi", types: []string{"wl_buffer", ""}},
		},
	},
	{
		name: "wp_fractional_scale_manager_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "get_fractional_scale", sig: "no", types: []string{"wp_fractional_scale_v1", "wl_surface"}},
		},
	},
	{
		name: "wp_fractional_scale_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
		},
		events: []protoMessage{
			{name: "preferred_scale", sig: "u", types: []string{""}},
		},
	},
	{
		name: "wp_viewporter", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "get_viewport", sig: "no", types: []string{"wp_viewport", "wl_surface"}},
		},
	},
	{
		name: "wp_viewport", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "set_source", sig: "ffff", types: []string{"", "", "", ""}},
			{name: "set_destination", sig: "ii", types: []string{"", ""}},
		},
	},
	{
		name: "zwp_relative_pointer_manager_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "get_relative_pointer", sig: "no", types: []string{"zwp_relative_pointer_v1", "wl_pointer"}},
		},
	},
	{
		name: "zwp_relative_pointer_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
		},
		events: []protoMessage{
			{name: "relative_motion", sig: "uuffff", types: []string{"", "", "", "", "", ""}},
		},
	},
	{
		name: "zwp_pointer_constraints_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "lock_pointer", sig: "noo?ou", types: []string{"zwp_locked_pointer_v1", "wl_surface", "wl_pointer", "wl_region", ""}},
			{name: "confine_pointer", sig: "noo?ou", types: []string{"zwp_confined_pointer_v1", "wl_surface", "wl_pointer", "wl_region", ""}},
		},
	},
	{
		name: "zwp_locked_pointer_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "set_cursor_position_hint", sig: "ff", types: []string{"", ""}},
			{name: "set_region", sig: "?o", types: []string{"wl_region"}},
		},
		events: []protoMessage{
			{name: "locked", sig: "", types: nil},
			{name: "unlocked", sig: "", types: nil},
		},
	},
	{
		name: "zwp_confined_pointer_v1", version: 1,
		methods: []protoMessage{
			{name: "destroy", sig: "", types: nil},
			{name: "set_region", sig: "?o", types: []string{"wl_region"}},
		},
		events: []protoMessage{
			{name: "confined", sig: "", types: nil},
			{name: "unconfined", sig: "", types: nil},
		},
	},
}

// buildProtocols turns wlProtocols into C interface tables. extern holds the
// interfaces libwayland-client exports, which the built tables refer to. The
// memory comes from the C heap and is never freed, because the tables have to
// outlive every proxy that names them.
func buildProtocols(alloc func(n uintptr) unsafe.Pointer, extern map[string]*wlInterface) map[string]*wlInterface {
	table := make(map[string]*wlInterface, len(extern)+len(wlProtocols))
	for name, iface := range extern {
		table[name] = iface
	}
	cstr := func(s string) *byte {
		p := (*byte)(alloc(uintptr(len(s)) + 1))
		copy(unsafe.Slice(p, len(s)+1), s)
		return p
	}
	// First pass: allocate every interface so that the second pass can point
	// at interfaces that appear later in the list, or refer to each other.
	built := make([]*wlInterface, len(wlProtocols))
	for i, d := range wlProtocols {
		iface := (*wlInterface)(alloc(unsafe.Sizeof(wlInterface{})))
		iface.Name = cstr(d.name)
		iface.Version = d.version
		built[i] = iface
		table[d.name] = iface
	}
	fill := func(msgs []protoMessage) *wlMessage {
		if len(msgs) == 0 {
			return nil
		}
		out := (*wlMessage)(alloc(unsafe.Sizeof(wlMessage{}) * uintptr(len(msgs))))
		slice := unsafe.Slice(out, len(msgs))
		for i, m := range msgs {
			slice[i].Name = cstr(m.name)
			slice[i].Signature = cstr(m.sig)
			if len(m.types) == 0 {
				continue
			}
			types := (**wlInterface)(alloc(unsafe.Sizeof((*wlInterface)(nil)) * uintptr(len(m.types))))
			ts := unsafe.Slice(types, len(m.types))
			for j, name := range m.types {
				if name != "" {
					ts[j] = table[name]
				}
			}
			slice[i].Types = types
		}
		return out
	}
	for i, d := range wlProtocols {
		built[i].MethodCount = int32(len(d.methods))
		built[i].Methods = fill(d.methods)
		built[i].EventCount = int32(len(d.events))
		built[i].Events = fill(d.events)
	}
	return table
}
