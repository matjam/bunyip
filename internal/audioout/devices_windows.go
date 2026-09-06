package audioout

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var procCoUninitialize = ole32.NewProc("CoUninitialize")
var procCoTaskMemFree = ole32.NewProc("CoTaskMemFree")
var procPropVariantClear = ole32.NewProc("PropVariantClear")
var freeCOMMemory = func(p uintptr) { procCoTaskMemFree.Call(p) }
var clearProperty = func(p *propVariant) { procPropVariantClear.Call(uintptr(unsafe.Pointer(p))) }
var createWASAPIEnumerator = newEnumerator
var createAudioEvent = func() uintptr { event, _, _ := procCreateEventW.Call(0, 0, 0, 0); return event }
var closeAudioEvent = func(event uintptr) { procCloseHandle.Call(event) }

type propertyKey struct {
	Format guid
	ID     uint32
}
type propVariant struct {
	Kind     uint16
	Reserved [3]uint16
	Data     [2]uintptr
}

var friendlyNameKey = propertyKey{guid{0xa45c254e, 0xdf1c, 0x4efd, [8]byte{0x80, 0x20, 0x67, 0xd1, 0x46, 0xa8, 0x50, 0xe0}}, 14}

func startCOM() error {
	if procCoCreateInstance.Find() != nil {
		return ErrUnsupported
	}
	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	if int32(hr) < 0 {
		return fmt.Errorf("%w: CoInitializeEx: %#x", ErrUnavailable, hr)
	}
	return nil
}

func newEnumerator() (*comObject, error) {
	var enum *comObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll, uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum)))
	if int32(hr) < 0 {
		return nil, fmt.Errorf("%w: MMDeviceEnumerator: %#x", ErrUnavailable, hr)
	}
	return enum, nil
}

func endpoint(enum *comObject, id string, flow uintptr) (*comObject, error) {
	var dev *comObject
	var hr uintptr
	if id == "" {
		hr = enum.call(4, flow, eConsole, uintptr(unsafe.Pointer(&dev)))
	} else {
		wide, err := syscall.UTF16PtrFromString(id)
		if err != nil {
			return nil, err
		}
		hr = enum.call(5, uintptr(unsafe.Pointer(wide)), uintptr(unsafe.Pointer(&dev)))
	}
	if int32(hr) < 0 {
		return nil, fmt.Errorf("%w: audio endpoint %q: %#x", ErrUnavailable, id, hr)
	}
	return dev, nil
}

func wideString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), uintptr(n)*2)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

func endpointID(dev *comObject) (string, error) {
	var p *uint16
	if hr := dev.call(5, uintptr(unsafe.Pointer(&p))); int32(hr) < 0 {
		return "", fmt.Errorf("audioout: GetId: %#x", hr)
	}
	defer freeCOMMemory(uintptr(unsafe.Pointer(p)))
	return wideString(p), nil
}

func endpointInfo(dev *comObject) (DeviceInfo, error) {
	id, err := endpointID(dev)
	if err != nil {
		return DeviceInfo{}, err
	}
	info := DeviceInfo{ID: id, Name: id}
	var props *comObject
	if hr := dev.call(4, 0, uintptr(unsafe.Pointer(&props))); int32(hr) < 0 {
		return info, nil
	}
	defer props.release()
	var value propVariant
	defer clearProperty(&value)
	if hr := props.call(5, uintptr(unsafe.Pointer(&friendlyNameKey)), uintptr(unsafe.Pointer(&value))); int32(hr) >= 0 && value.Kind == 31 {
		if name := wideString(*(**uint16)(unsafe.Pointer(&value.Data[0]))); name != "" {
			info.Name = name
		}
	}
	return info, nil
}

func OutputDevices() ([]DeviceInfo, error) { return wasapiDevices(eRender) }
func InputDevices() ([]DeviceInfo, error)  { return wasapiDevices(eCapture) }

func wasapiDevices(flow uintptr) ([]DeviceInfo, error) {
	// A dedicated thread avoids changing the caller's COM apartment.
	type result struct {
		devices []DeviceInfo
		err     error
	}
	ready := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := startCOM(); err != nil {
			ready <- result{err: err}
			return
		}
		defer procCoUninitialize.Call()
		devices, err := enumerateWASAPI(flow)
		ready <- result{devices, err}
	}()
	r := <-ready
	return r.devices, r.err
}

func enumerateWASAPI(flow uintptr) ([]DeviceInfo, error) {
	enum, err := newEnumerator()
	if err != nil {
		return nil, err
	}
	defer enum.release()
	return enumerateEndpoints(enum, flow)
}

func enumerateEndpoints(enum *comObject, flow uintptr) ([]DeviceInfo, error) {
	defaultID := ""
	if d, err := endpoint(enum, "", flow); err == nil {
		defaultID, _ = endpointID(d)
		d.release()
	}
	var collection *comObject
	if hr := enum.call(3, flow, 1, uintptr(unsafe.Pointer(&collection))); int32(hr) < 0 {
		return nil, fmt.Errorf("%w: EnumAudioEndpoints: %#x", ErrUnavailable, hr)
	}
	defer collection.release()
	var count uint32
	if hr := collection.call(3, uintptr(unsafe.Pointer(&count))); int32(hr) < 0 {
		return nil, fmt.Errorf("audioout: GetCount: %#x", hr)
	}
	out := make([]DeviceInfo, 0, count)
	for i := uint32(0); i < count; i++ {
		var dev *comObject
		if hr := collection.call(4, uintptr(i), uintptr(unsafe.Pointer(&dev))); int32(hr) < 0 {
			continue
		}
		info, err := endpointInfo(dev)
		dev.release()
		if err != nil {
			return nil, err
		}
		info.Default = info.ID == defaultID
		out = append(out, info)
	}
	return out, nil
}
