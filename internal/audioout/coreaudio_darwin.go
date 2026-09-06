package audioout

import (
	"fmt"
	"structs"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Core Audio types, laid out as in AudioToolbox headers.
type audioComponentDescription struct {
	_                                             structs.HostLayout
	Type, SubType, Manufacturer, Flags, FlagsMask uint32
}

type audioStreamBasicDescription struct {
	_                structs.HostLayout
	SampleRate       float64
	FormatID         uint32
	FormatFlags      uint32
	BytesPerPacket   uint32
	FramesPerPacket  uint32
	BytesPerFrame    uint32
	ChannelsPerFrame uint32
	BitsPerChannel   uint32
	Reserved         uint32
}

type auRenderCallbackStruct struct {
	_      structs.HostLayout
	Proc   uintptr
	RefCon uintptr // opaque registry ID, not a Go pointer
}

type audioBuffer struct {
	_              structs.HostLayout
	NumberChannels uint32
	DataByteSize   uint32
	Data           unsafe.Pointer
}

type audioBufferList struct {
	_             structs.HostLayout
	NumberBuffers uint32
	_             uint32
	Buffers       [1]audioBuffer
}

const (
	kAudioUnitType_Output                = 0x61756F75 // 'auou'
	kAudioUnitSubType_DefaultOutput      = 0x64656620 // 'def '
	kAudioUnitManufacturer_Apple         = 0x6170706C // 'appl'
	kAudioFormatLinearPCM                = 0x6C70636D // 'lpcm'
	kAudioFormatFlagIsFloat              = 1 << 0
	kAudioFormatFlagIsPacked             = 1 << 3
	kAudioUnitProperty_StreamFormat      = 8
	kAudioUnitProperty_SetRenderCallback = 23
	kAudioUnitScope_Input                = 1
)

var (
	loadOnce                      sync.Once
	loadErr                       error
	audioComponentFindNext        func(comp uintptr, desc *audioComponentDescription) uintptr
	audioComponentInstanceNew     func(comp uintptr, out *uintptr) int32
	audioComponentInstanceDispose func(unit uintptr) int32
	audioUnitSetProperty          func(unit uintptr, id, scope, element uint32, data unsafe.Pointer, size uint32) int32
	audioUnitInitialize           func(unit uintptr) int32
	audioUnitUninitialize         func(unit uintptr) int32
	audioOutputUnitStart          func(unit uintptr) int32
	audioOutputUnitStop           func(unit uintptr) int32
	renderCallback                uintptr

	// devices lets the C callback find its Device without passing Go
	// pointers through C memory.
	devicesMu sync.Mutex
	devices           = map[uintptr]*Device{}
	nextID    uintptr = 1
)

func loadAudioToolbox() error {
	loadOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("%w: load AudioToolbox: %v", ErrUnsupported, err)
			return
		}
		purego.RegisterLibFunc(&audioComponentFindNext, lib, "AudioComponentFindNext")
		purego.RegisterLibFunc(&audioComponentInstanceNew, lib, "AudioComponentInstanceNew")
		purego.RegisterLibFunc(&audioComponentInstanceDispose, lib, "AudioComponentInstanceDispose")
		purego.RegisterLibFunc(&audioUnitSetProperty, lib, "AudioUnitSetProperty")
		purego.RegisterLibFunc(&audioUnitInitialize, lib, "AudioUnitInitialize")
		purego.RegisterLibFunc(&audioUnitUninitialize, lib, "AudioUnitUninitialize")
		purego.RegisterLibFunc(&audioOutputUnitStart, lib, "AudioOutputUnitStart")
		purego.RegisterLibFunc(&audioOutputUnitStop, lib, "AudioOutputUnitStop")
		renderCallback = purego.NewCallback(onRender)
	})
	return loadErr
}

// Device is a running default-output audio unit.
type Device struct {
	closeOnce    sync.Once
	queue        uintptr
	queueMu      sync.Mutex
	queueStopped bool
	Rate         int
	unit         uintptr
	id           uintptr
	callback     Callback
}

// Open starts the default output at rate Hz, stereo float32.
func openDevice(id string, rate int, cb Callback) (*Device, error) {
	if id != "" {
		return openQueueOutput(id, rate, cb)
	}
	if err := loadAudioToolbox(); err != nil {
		return nil, err
	}
	desc := audioComponentDescription{Type: kAudioUnitType_Output, SubType: kAudioUnitSubType_DefaultOutput, Manufacturer: kAudioUnitManufacturer_Apple}
	comp := audioComponentFindNext(0, &desc)
	if comp == 0 {
		return nil, fmt.Errorf("audioout: no default output component")
	}
	d := &Device{Rate: rate, callback: cb}
	if st := audioComponentInstanceNew(comp, &d.unit); st != 0 {
		return nil, fmt.Errorf("audioout: AudioComponentInstanceNew: %d", st)
	}
	format := audioStreamBasicDescription{
		SampleRate: float64(rate), FormatID: kAudioFormatLinearPCM,
		FormatFlags:    kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked,
		BytesPerPacket: 8, FramesPerPacket: 1, BytesPerFrame: 8, ChannelsPerFrame: 2, BitsPerChannel: 32,
	}
	if st := audioUnitSetProperty(d.unit, kAudioUnitProperty_StreamFormat, kAudioUnitScope_Input, 0, unsafe.Pointer(&format), uint32(unsafe.Sizeof(format))); st != 0 {
		d.Close()
		return nil, fmt.Errorf("audioout: set stream format: %d", st)
	}
	devicesMu.Lock()
	d.id = nextID
	nextID++
	devices[d.id] = d
	devicesMu.Unlock()
	cbs := auRenderCallbackStruct{Proc: renderCallback, RefCon: d.id}
	if st := audioUnitSetProperty(d.unit, kAudioUnitProperty_SetRenderCallback, kAudioUnitScope_Input, 0, unsafe.Pointer(&cbs), uint32(unsafe.Sizeof(cbs))); st != 0 {
		d.Close()
		return nil, fmt.Errorf("audioout: set render callback: %d", st)
	}
	if st := audioUnitInitialize(d.unit); st != 0 {
		d.Close()
		return nil, fmt.Errorf("audioout: AudioUnitInitialize: %d", st)
	}
	if st := audioOutputUnitStart(d.unit); st != 0 {
		d.Close()
		return nil, fmt.Errorf("audioout: AudioOutputUnitStart: %d", st)
	}
	return d, nil
}

// Close stops and releases the unit.
func (d *Device) Close() {
	d.closeOnce.Do(d.close)
}

func (d *Device) close() {
	if d.queue != 0 {
		d.queueMu.Lock()
		d.queueStopped = true
		d.queueMu.Unlock()
		audioQueueStop(d.queue, 1)
		audioQueueDispose(d.queue, 1)
	}
	if d.unit != 0 {
		audioOutputUnitStop(d.unit)
		audioUnitUninitialize(d.unit)
		audioComponentInstanceDispose(d.unit)
		d.unit = 0
	}
	devicesMu.Lock()
	delete(devices, d.id)
	devicesMu.Unlock()
}

// onRender runs on Core Audio's real-time thread. It fills every buffer in
// the list from the device's callback and writes silence if the device is gone.
func onRender(id uintptr, _ *uint32, _ unsafe.Pointer, _ uint32, frames uint32, data *audioBufferList) int32 {
	devicesMu.Lock()
	d := devices[id]
	devicesMu.Unlock()
	if data == nil {
		return 0
	}
	buffers := unsafe.Slice(&data.Buffers[0], int(data.NumberBuffers))
	for i := range buffers {
		b := &buffers[i]
		if b.Data == nil {
			continue
		}
		out := unsafe.Slice((*float32)(b.Data), int(b.DataByteSize)/4)
		if d == nil {
			clear(out)
			continue
		}
		d.callback(out[:min(len(out), int(frames)*2)])
	}
	return 0
}
