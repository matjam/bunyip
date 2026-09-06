package platform

import (
	"errors"
	"unsafe"
)

var procRegisteredRaw = user32.NewProc("GetRegisteredRawInputDevices")

func registeredRawMouse() ([]rawInputDevice, error) {
	var count uint32
	size := unsafe.Sizeof(rawInputDevice{})
	n, _, err := procRegisteredRaw.Call(0, uintptr(unsafe.Pointer(&count)), size)
	if uint32(n) == ^uint32(0) {
		return nil, winControlError("GetRegisteredRawInputDevices", 0, err)
	}
	if count == 0 {
		return nil, nil
	}
	if count > 1024 {
		return nil, errors.New("platform: excessive raw-input registration count")
	}
	devices := make([]rawInputDevice, count)
	n, _, err = procRegisteredRaw.Call(uintptr(unsafe.Pointer(&devices[0])), uintptr(unsafe.Pointer(&count)), size)
	if uint32(n) == ^uint32(0) {
		return nil, winControlError("GetRegisteredRawInputDevices", 0, err)
	}
	return devices[:min(len(devices), int(n))], nil
}

func (a *App) checkHostRawMouse() error {
	if a.rawMouse.registered {
		return nil
	}
	devices, err := registeredRawMouse()
	if err != nil {
		return err
	}
	present, compatible := foregroundMouse(devices)
	if !compatible {
		return errors.New("platform: host raw-mouse registration conflicts with foreground window routing")
	}
	if present {
		a.rawMouse.registered = true
	} // borrowed compatible registration
	return nil
}

func (a *App) releaseRawMouse() {
	if a == nil || len(a.windows) != 0 {
		return
	}
	devices, err := registeredRawMouse()
	if err != nil {
		a.rawMouse = rawMouseRegistration{}
		return
	}
	a.rawMouse.release(devices, func() {
		d := rawInputDevice{UsagePage: 1, Usage: 2, Flags: 1} // RIDEV_REMOVE
		procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&d)), 1, unsafe.Sizeof(d))
	})
}
