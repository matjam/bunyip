package audioout

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// WASAPI capture in shared mode, the mirror of the output side: the
// device's mix format is requested as float32 at the given rate and
// channel count through the audio engine's own resampler
// (AUTOCONVERTPCM), and a goroutine drains the capture client each time
// the event handle signals. This compiles and is written against the
// WASAPI headers, but it has not yet been run on Windows.

var iidIAudioCaptureClient = guid{0xC8ADBD64, 0xE71E, 0x48A0, [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}

const (
	eCapture              = 1
	audclntBufferSilent   = 0x2
	captureBufferDuration = 200000 // 20 ms in 100 ns units
)

// CaptureDevice is an open WASAPI capture stream.
type CaptureDevice struct {
	closeOnce sync.Once
	Rate      int
	Channels  int
	client    *comObject
	capture   *comObject
	event     uintptr
	stop      chan struct{}
	done      chan struct{}
	cb        CaptureCallback
	silence   []float32
}

// OpenCapture starts recording interleaved float32 at rate from the
// default input, calling cb from its own goroutine.
func openCaptureDevice(id string, rate, channels int, cb CaptureCallback) (*CaptureDevice, error) {
	type result struct {
		dev *CaptureDevice
		err error
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
		d, err := openCaptureWASAPI(id, rate, channels, cb)
		ready <- result{d, err}
		if err == nil {
			d.loop()
		}
	}()
	r := <-ready
	return r.dev, r.err
}

func openCaptureWASAPI(id string, rate, channels int, cb CaptureCallback) (*CaptureDevice, error) {
	enum, err := newEnumerator()
	if err != nil {
		return nil, err
	}
	defer enum.release()
	dev, err := endpoint(enum, id, eCapture)
	if err != nil {
		return nil, err
	}
	defer dev.release()
	var client *comObject
	if hr := dev.call(3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client))); int32(hr) < 0 { // Activate
		return nil, fmt.Errorf("audioout: IMMDevice.Activate: %#x", hr)
	}
	bytesPerFrame := uint16(4 * channels)
	format := waveFormatExtensible{
		FormatTag: waveFormatExtensibleTag, Channels: uint16(channels), SamplesPerSec: uint32(rate),
		AvgBytesPerSec: uint32(rate) * uint32(bytesPerFrame), BlockAlign: bytesPerFrame, BitsPerSample: 32,
		Size: 22, ValidBitsPerSample: 32, ChannelMask: uint32(1<<channels - 1), SubFormat: ksDataFormatIEEEFloat,
	}
	flags := uintptr(audclntStreamEventCB | audclntStreamAutoConvert | audclntStreamSRCDefault)
	if hr := client.call(3, audclntShareModeShared, flags, captureBufferDuration, 0, uintptr(unsafe.Pointer(&format)), 0); int32(hr) < 0 { // Initialize
		client.release()
		return nil, fmt.Errorf("audioout: IAudioClient.Initialize(capture): %#x", hr)
	}
	event, _, _ := procCreateEventW.Call(0, 0, 0, 0)
	if event == 0 {
		client.release()
		return nil, errors.New("audioout: CreateEvent failed")
	}
	if hr := client.call(13, event); int32(hr) < 0 { // SetEventHandle
		client.release()
		procCloseHandle.Call(event)
		return nil, fmt.Errorf("audioout: SetEventHandle: %#x", hr)
	}
	var capture *comObject
	if hr := client.call(14, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&capture))); int32(hr) < 0 { // GetService
		client.release()
		procCloseHandle.Call(event)
		return nil, fmt.Errorf("audioout: GetService(IAudioCaptureClient): %#x", hr)
	}
	d := &CaptureDevice{Rate: rate, Channels: channels, client: client, capture: capture, event: event,
		stop: make(chan struct{}), done: make(chan struct{}), cb: cb}
	if hr := client.call(10); int32(hr) < 0 { // Start
		// Not Close: that waits for the loop goroutine, which has not
		// been started, so it would wait forever.
		capture.release()
		client.release()
		procCloseHandle.Call(event)
		return nil, fmt.Errorf("audioout: IAudioClient.Start(capture): %#x", hr)
	}
	return d, nil
}

// drain hands over every packet the device has ready.
func (d *CaptureDevice) drain() {
	for {
		var packet uint32
		if hr := d.capture.call(5, uintptr(unsafe.Pointer(&packet))); int32(hr) < 0 || packet == 0 { // GetNextPacketSize
			return
		}
		var (
			data   *float32
			frames uint32
			flags  uint32
		)
		if hr := d.capture.call(3, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)),
			uintptr(unsafe.Pointer(&flags)), 0, 0); int32(hr) < 0 { // GetBuffer
			return
		}
		samples := int(frames) * d.Channels
		switch {
		case frames == 0:
		case flags&audclntBufferSilent != 0 || data == nil:
			// The device says this packet is silence and may not have
			// written it, so it is passed on as zeroes.
			if len(d.silence) < samples {
				d.silence = make([]float32, samples)
			}
			clear(d.silence[:samples])
			d.cb(d.silence[:samples])
		default:
			d.cb(unsafe.Slice(data, samples))
		}
		d.capture.call(4, uintptr(frames)) // ReleaseBuffer
	}
}

func (d *CaptureDevice) loop() {
	defer close(d.done)
	defer func() {
		d.client.call(11)
		d.capture.release()
		d.client.release()
		procCloseHandle.Call(d.event)
	}()
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
		d.drain()
	}
}

// Close stops recording and releases the device.
func (d *CaptureDevice) Close() {
	d.closeOnce.Do(func() { close(d.stop); procSetEvent.Call(d.event); <-d.done })
}
