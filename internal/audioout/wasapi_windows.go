package audioout

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// WASAPI output in shared mode: the device's mix format is requested as
// float32 stereo at the mixer's rate through the audio engine's own
// resampler (AUTOCONVERTPCM), and a goroutine feeds the render client
// each time the event handle signals.

var (
	ole32                 = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx    = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance  = ole32.NewProc("CoCreateInstance")
	kernel                = syscall.NewLazyDLL("kernel32.dll")
	procCreateEventW      = kernel.NewProc("CreateEventW")
	procWaitForSingleObj  = kernel.NewProc("WaitForSingleObject")
	procCloseHandle       = kernel.NewProc("CloseHandle")
	procSetEvent          = kernel.NewProc("SetEvent")
	avrt                  = syscall.NewLazyDLL("avrt.dll")
	procAvSetMmThreadChar = avrt.NewProc("AvSetMmThreadCharacteristicsW")
	procAvRevertMmThread  = avrt.NewProc("AvRevertMmThreadCharacteristics")
	errUnsupported        = errors.New("audioout: WASAPI unavailable")

	// proAudio is the scheduler class an audio thread asks for, so the
	// output and capture loops are not descheduled mid-buffer.
	proAudio = syscall.StringToUTF16Ptr("Pro Audio")
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidMMDeviceEnumerator = guid{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = guid{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = guid{0x1CB9AD4C, 0xDBFA, 0x4C32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioRenderClient   = guid{0xF294ACFC, 0x3146, 0x4483, [8]byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2}}
	ksDataFormatIEEEFloat   = guid{0x00000003, 0x0000, 0x0010, [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
)

const (
	clsctxAll                = 0x17
	eRender                  = 0
	eConsole                 = 0
	audclntShareModeShared   = 0
	audclntStreamEventCB     = 0x00040000
	audclntStreamAutoConvert = 0x80000000
	audclntStreamSRCDefault  = 0x08000000
	waveFormatExtensibleTag  = 0xFFFE
	coinitMultithreaded      = 0
	infinite                 = 0xFFFFFFFF
)

// COM objects are pointers to pointers to vtables; methods are called by
// index.
type comObject struct{ vtbl *[64]uintptr }

func (o *comObject) call(index int, args ...uintptr) uintptr {
	all := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	r, _, _ := syscall.SyscallN(o.vtbl[index], all...)
	return r
}

func (o *comObject) release() { o.call(2) }

type waveFormatExtensible struct {
	FormatTag          uint16
	Channels           uint16
	SamplesPerSec      uint32
	AvgBytesPerSec     uint32
	BlockAlign         uint16
	BitsPerSample      uint16
	Size               uint16
	ValidBitsPerSample uint16
	ChannelMask        uint32
	SubFormat          guid
}

// Device is an open WASAPI output stream.
type Device struct {
	Rate    int
	client  *comObject
	render  *comObject
	event   uintptr
	stop    chan struct{}
	done    chan struct{}
	cb      Callback
	frames  uint32
	scratch []float32
}

// Open starts stereo float32 output at rate, calling cb for frames.
func Open(rate int, cb Callback) (*Device, error) {
	if procCoCreateInstance.Find() != nil {
		return nil, errUnsupported
	}
	procCoInitializeEx.Call(0, coinitMultithreaded)
	var enum *comObject
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum))); int32(hr) < 0 {
		return nil, fmt.Errorf("audioout: CoCreateInstance(MMDeviceEnumerator): %#x", hr)
	}
	defer enum.release()
	var dev *comObject
	if hr := enum.call(4, eRender, eConsole, uintptr(unsafe.Pointer(&dev))); int32(hr) < 0 { // GetDefaultAudioEndpoint
		return nil, fmt.Errorf("audioout: GetDefaultAudioEndpoint: %#x", hr)
	}
	defer dev.release()
	var client *comObject
	if hr := dev.call(3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client))); int32(hr) < 0 { // Activate
		return nil, fmt.Errorf("audioout: IMMDevice.Activate: %#x", hr)
	}
	format := waveFormatExtensible{
		FormatTag: waveFormatExtensibleTag, Channels: 2, SamplesPerSec: uint32(rate), AvgBytesPerSec: uint32(rate) * 8,
		BlockAlign: 8, BitsPerSample: 32, Size: 22, ValidBitsPerSample: 32, ChannelMask: 3, SubFormat: ksDataFormatIEEEFloat,
	}
	const bufferDuration = 200000 // 20 ms in 100 ns units
	flags := uintptr(audclntStreamEventCB | audclntStreamAutoConvert | audclntStreamSRCDefault)
	if hr := client.call(3, audclntShareModeShared, flags, bufferDuration, 0, uintptr(unsafe.Pointer(&format)), 0); int32(hr) < 0 { // Initialize
		client.release()
		return nil, fmt.Errorf("audioout: IAudioClient.Initialize: %#x", hr)
	}
	var frames uint32
	client.call(4, uintptr(unsafe.Pointer(&frames))) // GetBufferSize
	event, _, _ := procCreateEventW.Call(0, 0, 0, 0)
	if event == 0 {
		client.release()
		return nil, errors.New("audioout: CreateEvent failed")
	}
	if hr := client.call(13, event); int32(hr) < 0 { // SetEventHandle
		client.release()
		return nil, fmt.Errorf("audioout: SetEventHandle: %#x", hr)
	}
	var render *comObject
	if hr := client.call(14, uintptr(unsafe.Pointer(&iidIAudioRenderClient)), uintptr(unsafe.Pointer(&render))); int32(hr) < 0 { // GetService
		client.release()
		return nil, fmt.Errorf("audioout: GetService(IAudioRenderClient): %#x", hr)
	}
	d := &Device{Rate: rate, client: client, render: render, event: event, stop: make(chan struct{}), done: make(chan struct{}), cb: cb, frames: frames}
	d.fill()                                  // prime the buffer before starting
	if hr := client.call(10); int32(hr) < 0 { // Start
		// Not Close: that waits for the loop goroutine, which has not
		// been started, so it would wait forever.
		render.release()
		client.release()
		procCloseHandle.Call(event)
		return nil, fmt.Errorf("audioout: IAudioClient.Start: %#x", hr)
	}
	go d.loop()
	return d, nil
}

// fill writes as many frames as the buffer has room for.
func (d *Device) fill() {
	var padding uint32
	d.client.call(6, uintptr(unsafe.Pointer(&padding))) // GetCurrentPadding
	avail := d.frames - padding
	if avail == 0 {
		return
	}
	var data *float32
	if hr := d.render.call(3, uintptr(avail), uintptr(unsafe.Pointer(&data))); int32(hr) < 0 || data == nil { // GetBuffer
		return
	}
	out := unsafe.Slice(data, int(avail)*2)
	d.cb(out)
	d.render.call(4, uintptr(avail), 0) // ReleaseBuffer
}

func (d *Device) loop() {
	runtime.LockOSThread()
	defer close(d.done)
	var taskIndex uint32
	task, _, _ := procAvSetMmThreadChar.Call(uintptr(unsafe.Pointer(proAudio)), uintptr(unsafe.Pointer(&taskIndex)))
	if task != 0 {
		defer procAvRevertMmThread.Call(task)
	}
	for {
		procWaitForSingleObj.Call(d.event, infinite)
		select {
		case <-d.stop:
			return
		default:
		}
		d.fill()
	}
}

// Close stops the stream.
func (d *Device) Close() {
	if d.client == nil {
		return
	}
	close(d.stop)
	procSetEvent.Call(d.event)
	<-d.done
	d.client.call(11) // Stop
	if d.render != nil {
		d.render.release()
	}
	d.client.release()
	procCloseHandle.Call(d.event)
	d.client, d.render = nil, nil
}
