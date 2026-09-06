package platform

// rawInputDevice matches Win32 RAWINPUTDEVICE. A zero Target follows keyboard
// focus; naming one HWND would redirect every window's raw mouse input there.
type rawInputDevice struct {
	UsagePage uint16
	Usage     uint16
	Flags     uint32
	Target    uintptr
}

type rawMouseRegistration struct{ registered, owned bool }

func (r *rawMouseRegistration) ensure(register func(rawInputDevice) error) error {
	if r.registered {
		return nil
	}
	if err := register(rawInputDevice{UsagePage: 1, Usage: 2}); err != nil {
		return err
	}
	r.registered = true
	r.owned = true
	return nil
}

func foregroundMouse(devices []rawInputDevice) (present, compatible bool) {
	for _, d := range devices {
		if d.UsagePage == 1 && d.Usage == 2 {
			return true, d.Target == 0 && d.Flags == 0
		}
	}
	return false, true
}

func (r *rawMouseRegistration) release(current []rawInputDevice, remove func()) {
	defer func() { *r = rawMouseRegistration{} }()
	if present, compatible := foregroundMouse(current); r.owned && present && compatible {
		remove()
	}
}
