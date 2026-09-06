package audioout

import (
	"fmt"
	"structs"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type audioPropertyAddress struct {
	_                        structs.HostLayout
	Selector, Scope, Element uint32
}

const (
	audioSystemObject       = 1
	audioScopeGlobal        = 0x676c6f62 // glob
	audioScopeInput         = 0x696e7074 // inpt
	audioScopeOutput        = 0x6f757470 // outp
	audioPropertyDevices    = 0x64657623 // dev#
	audioPropertyStreams    = 0x73746d23 // stm#
	audioPropertyUID        = 0x75696420 // uid
	audioPropertyName       = 0x6c6e616d // lnam
	audioDefaultInput       = 0x64496e20 // dIn
	audioDefaultOutput      = 0x644f7574 // dOut
	audioQueueCurrentDevice = 0x61716364 // aqcd
	cfUTF8                  = 0x08000100
)

type coreAudioAPI struct {
	size          func(uint32, *audioPropertyAddress, uint32, unsafe.Pointer, *uint32) int32
	data          func(uint32, *audioPropertyAddress, uint32, unsafe.Pointer, *uint32, unsafe.Pointer) int32
	stringLength  func(uintptr) int
	stringMaxSize func(int, uint32) int
	stringCString func(uintptr, *byte, int, uint32) bool
	stringCreate  func(uintptr, *byte, uint32) uintptr
	release       func(uintptr)
}

var coreOnce sync.Once
var coreAPI *coreAudioAPI
var coreErr error

func loadCoreAudio() (*coreAudioAPI, error) {
	coreOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			coreErr = fmt.Errorf("%w: CoreAudio: %v", ErrUnsupported, err)
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			coreErr = fmt.Errorf("%w: CoreFoundation: %v", ErrUnsupported, err)
			return
		}
		a := &coreAudioAPI{}
		purego.RegisterLibFunc(&a.size, lib, "AudioObjectGetPropertyDataSize")
		purego.RegisterLibFunc(&a.data, lib, "AudioObjectGetPropertyData")
		purego.RegisterLibFunc(&a.stringLength, cf, "CFStringGetLength")
		purego.RegisterLibFunc(&a.stringMaxSize, cf, "CFStringGetMaximumSizeForEncoding")
		purego.RegisterLibFunc(&a.stringCString, cf, "CFStringGetCString")
		purego.RegisterLibFunc(&a.stringCreate, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&a.release, cf, "CFRelease")
		coreAPI = a
	})
	return coreAPI, coreErr
}

func (a *coreAudioAPI) stringProperty(device, selector uint32) (string, error) {
	address := audioPropertyAddress{Selector: selector, Scope: audioScopeGlobal}
	var value uintptr
	size := uint32(unsafe.Sizeof(value))
	if st := a.data(device, &address, 0, nil, &size, unsafe.Pointer(&value)); st != 0 {
		return "", fmt.Errorf("audioout: device string: %d", st)
	}
	if value == 0 {
		return "", nil
	}
	defer a.release(value)
	n := a.stringMaxSize(a.stringLength(value), cfUTF8) + 1
	if n <= 0 {
		return "", fmt.Errorf("audioout: invalid device string size")
	}
	buf := make([]byte, n)
	if !a.stringCString(value, &buf[0], len(buf), cfUTF8) {
		return "", fmt.Errorf("audioout: device string conversion failed")
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i]), nil
		}
	}
	return "", fmt.Errorf("audioout: device string missing terminator")
}

func OutputDevices() ([]DeviceInfo, error) { return coreDevices(false) }
func InputDevices() ([]DeviceInfo, error)  { return coreDevices(true) }

func coreDevices(input bool) ([]DeviceInfo, error) {
	a, err := loadCoreAudio()
	if err != nil {
		return nil, err
	}
	return a.devices(input)
}

func (a *coreAudioAPI) devices(input bool) ([]DeviceInfo, error) {
	scope, defaultProperty := uint32(audioScopeOutput), uint32(audioDefaultOutput)
	if input {
		scope, defaultProperty = audioScopeInput, audioDefaultInput
	}
	var defaultDevice uint32
	size := uint32(4)
	address := audioPropertyAddress{Selector: defaultProperty, Scope: audioScopeGlobal}
	a.data(audioSystemObject, &address, 0, nil, &size, unsafe.Pointer(&defaultDevice))
	address.Selector = audioPropertyDevices
	if st := a.size(audioSystemObject, &address, 0, nil, &size); st != 0 {
		return nil, fmt.Errorf("%w: device list size: %d", ErrUnavailable, st)
	}
	if size == 0 {
		return nil, nil
	}
	if size%4 != 0 {
		return nil, fmt.Errorf("audioout: invalid device list size")
	}
	ids := make([]uint32, size/4)
	if st := a.data(audioSystemObject, &address, 0, nil, &size, unsafe.Pointer(&ids[0])); st != 0 {
		return nil, fmt.Errorf("%w: device list: %d", ErrUnavailable, st)
	}
	var out []DeviceInfo
	for _, id := range ids[:min(len(ids), int(size/4))] {
		address = audioPropertyAddress{Selector: audioPropertyStreams, Scope: scope}
		streamSize := uint32(0)
		if a.size(id, &address, 0, nil, &streamSize) != 0 || streamSize == 0 {
			continue
		}
		uid, err := a.stringProperty(id, audioPropertyUID)
		if err != nil {
			return nil, err
		}
		name, err := a.stringProperty(id, audioPropertyName)
		if err != nil {
			return nil, err
		}
		if uid == "" {
			continue
		}
		if name == "" {
			name = uid
		}
		out = append(out, DeviceInfo{ID: uid, Name: name, Default: id == defaultDevice})
	}
	return out, nil
}

func setQueueDevice(queue uintptr, id string, input bool) error {
	if id == "" {
		return nil
	}
	items, err := coreDevices(input)
	if err != nil {
		return err
	}
	found := false
	for _, d := range items {
		if d.ID == id {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: endpoint %q", ErrUnavailable, id)
	}
	a, err := loadCoreAudio()
	if err != nil {
		return err
	}
	text := append([]byte(id), 0)
	uid := a.stringCreate(0, &text[0], cfUTF8)
	if uid == 0 {
		return fmt.Errorf("audioout: device UID allocation failed")
	}
	defer a.release(uid)
	if st := audioQueueSetProperty(queue, audioQueueCurrentDevice, unsafe.Pointer(&uid), uint32(unsafe.Sizeof(uid))); st != 0 {
		return fmt.Errorf("%w: select queue device: %d", ErrUnavailable, st)
	}
	return nil
}
