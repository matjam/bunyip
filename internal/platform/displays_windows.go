package platform

import (
	"encoding/binary"
	"fmt"
	"image"
	"syscall"
	"unsafe"
)

var (
	procEnumDisplayDevicesW  = user32.NewProc("EnumDisplayDevicesW")
	procEnumDisplaySettingsW = user32.NewProc("EnumDisplaySettingsW")
)

type displayDevice struct {
	Size        uint32
	Name        [32]uint16
	Description [128]uint16
	Flags       uint32
	ID, Key     [128]uint16
}

// Displays enumerates active desktop adapters. Windows reports bounds in
// physical desktop pixels for this DPI-aware process. Per-display logical
// scaling is left unknown rather than inferred from a window on another screen.
func (a *App) Displays() ([]Display, error) {
	var out []Display
	for index := uint32(0); ; index++ {
		device := displayDevice{Size: uint32(unsafe.Sizeof(displayDevice{}))}
		if ok, _, _ := procEnumDisplayDevicesW.Call(0, uintptr(index), uintptr(unsafe.Pointer(&device)), 0); ok == 0 {
			break
		}
		if device.Flags&1 == 0 || device.Flags&8 != 0 {
			continue
		}
		read := func(index uint32) (VideoMode, image.Rectangle, bool) {
			var mode [220]byte // DEVMODEW, including the display union
			binary.LittleEndian.PutUint16(mode[68:], uint16(len(mode)))
			ok, _, _ := procEnumDisplaySettingsW.Call(uintptr(unsafe.Pointer(&device.Name[0])), uintptr(index), uintptr(unsafe.Pointer(&mode[0])))
			if ok == 0 {
				return VideoMode{}, image.Rectangle{}, false
			}
			v := VideoMode{Width: int(binary.LittleEndian.Uint32(mode[172:])), Height: int(binary.LittleEndian.Uint32(mode[176:]))}
			rate := binary.LittleEndian.Uint32(mode[184:])
			if rate > 1 {
				v.RefreshHz = float64(rate)
			}
			x, y := int(int32(binary.LittleEndian.Uint32(mode[76:]))), int(int32(binary.LittleEndian.Uint32(mode[80:])))
			return v, image.Rect(x, y, x+v.Width, y+v.Height), true
		}
		current, bounds, ok := read(^uint32(0))
		if !ok {
			return nil, fmt.Errorf("platform: current display mode query failed for %s", syscall.UTF16ToString(device.Name[:]))
		}
		d := Display{Name: syscall.UTF16ToString(device.Description[:]), Bounds: bounds, BoundsKnown: true, Current: current}
		for i := uint32(0); ; i++ {
			mode, _, ok := read(i)
			if !ok {
				break
			}
			found := false
			for _, m := range d.Modes {
				if m == mode {
					found = true
					break
				}
			}
			if !found {
				d.Modes = append(d.Modes, mode)
			}
		}
		out = append(out, d)
	}
	return out, nil
}
