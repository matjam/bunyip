package audioout

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ALSA output and input through libasound's "default" device, which
// PulseAudio and PipeWire both provide, so one path serves every Linux
// desktop.

var errUnsupported = errors.New("audioout: libasound unavailable")

const (
	sndPCMStreamPlayback     = 0
	sndPCMStreamCapture      = 1
	sndPCMFormatFloatLE      = 14
	sndPCMAccessRWInterleave = 3
)

type alsa struct {
	open      func(pcm *unsafe.Pointer, name *byte, stream int32, mode int32) int32
	setParams func(pcm unsafe.Pointer, format, access int32, channels uint32, rate uint32, softResample int32, latencyUS uint32) int32
	writei    func(pcm unsafe.Pointer, buf unsafe.Pointer, frames uintptr) int
	readi     func(pcm unsafe.Pointer, buf unsafe.Pointer, frames uintptr) int
	recover   func(pcm unsafe.Pointer, err int32, silent int32) int32
	close     func(pcm unsafe.Pointer) int32
	strerror  func(err int32) *byte
}

// loadALSA binds the handful of libasound calls both directions need.
func loadALSA() (*alsa, error) {
	h, err := purego.Dlopen("libasound.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUnsupported, err)
	}
	lib := &alsa{}
	for name, fptr := range map[string]any{
		"snd_pcm_open": &lib.open, "snd_pcm_set_params": &lib.setParams, "snd_pcm_writei": &lib.writei,
		"snd_pcm_readi": &lib.readi, "snd_pcm_recover": &lib.recover, "snd_pcm_close": &lib.close,
		"snd_strerror": &lib.strerror,
	} {
		sym, err := purego.Dlsym(h, name)
		if err != nil {
			return nil, fmt.Errorf("audioout: %s: %w", name, err)
		}
		purego.RegisterFunc(fptr, sym)
	}
	return lib, nil
}

// openPCM opens the default device for one direction and sets it to
// interleaved float32 at rate.
func (lib *alsa) openPCM(stream int32, rate, channels int, latencyUS uint32) (unsafe.Pointer, error) {
	name := []byte("default\x00")
	var pcm unsafe.Pointer
	if rc := lib.open(&pcm, &name[0], stream, 0); rc < 0 {
		return nil, fmt.Errorf("audioout: snd_pcm_open: %s", cstr(lib.strerror(rc)))
	}
	if rc := lib.setParams(pcm, sndPCMFormatFloatLE, sndPCMAccessRWInterleave, uint32(channels), uint32(rate), 1, latencyUS); rc < 0 {
		lib.close(pcm)
		return nil, fmt.Errorf("audioout: snd_pcm_set_params: %s", cstr(lib.strerror(rc)))
	}
	return pcm, nil
}

// Device is an open ALSA playback stream.
type Device struct {
	Rate int
	lib  *alsa
	pcm  unsafe.Pointer
	stop chan struct{}
	done chan struct{}
}

// Open starts stereo float32 output at rate, calling cb for frames from
// its own goroutine.
func Open(rate int, cb Callback) (*Device, error) {
	lib, err := loadALSA()
	if err != nil {
		return nil, err
	}
	const latencyUS = 40000
	pcm, err := lib.openPCM(sndPCMStreamPlayback, rate, 2, latencyUS)
	if err != nil {
		return nil, err
	}
	d := &Device{Rate: rate, lib: lib, pcm: pcm, stop: make(chan struct{}), done: make(chan struct{})}
	go d.loop(cb)
	return d, nil
}

func cstr(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

func (d *Device) loop(cb Callback) {
	defer close(d.done)
	const frames = 512
	buf := make([]float32, frames*2)
	for {
		select {
		case <-d.stop:
			return
		default:
		}
		cb(buf)
		written := 0
		for written < frames {
			n := d.lib.writei(d.pcm, unsafe.Pointer(&buf[written*2]), uintptr(frames-written))
			if n < 0 {
				if d.lib.recover(d.pcm, int32(n), 1) < 0 {
					return
				}
				continue
			}
			written += n
		}
	}
}

// Close stops the stream.
func (d *Device) Close() {
	if d.pcm == nil {
		return
	}
	close(d.stop)
	<-d.done
	d.lib.close(d.pcm)
	d.pcm = nil
}

// CaptureDevice is an open ALSA capture stream.
type CaptureDevice struct {
	Rate     int
	Channels int
	lib      *alsa
	pcm      unsafe.Pointer
	stop     chan struct{}
	done     chan struct{}
}

// OpenCapture starts recording interleaved float32 at rate from the
// default device, calling cb with what arrives from its own goroutine.
func OpenCapture(rate, channels int, cb CaptureCallback) (*CaptureDevice, error) {
	lib, err := loadALSA()
	if err != nil {
		return nil, err
	}
	// A shorter latency than the output's, because a game reads captured
	// audio once a frame and a long buffer would lag behind the player.
	const latencyUS = 20000
	pcm, err := lib.openPCM(sndPCMStreamCapture, rate, channels, latencyUS)
	if err != nil {
		return nil, err
	}
	d := &CaptureDevice{Rate: rate, Channels: channels, lib: lib, pcm: pcm,
		stop: make(chan struct{}), done: make(chan struct{})}
	go d.loop(cb)
	return d, nil
}

// loop reads the device until Close. snd_pcm_readi blocks until the
// frames are there, which is why it has a goroutine of its own; an
// overrun is recovered from and reading goes on.
func (d *CaptureDevice) loop(cb CaptureCallback) {
	defer close(d.done)
	const frames = 256
	buf := make([]float32, frames*d.Channels)
	for {
		select {
		case <-d.stop:
			return
		default:
		}
		n := d.lib.readi(d.pcm, unsafe.Pointer(&buf[0]), frames)
		if n < 0 {
			if d.lib.recover(d.pcm, int32(n), 1) < 0 {
				return
			}
			continue
		}
		if n > 0 {
			cb(buf[:n*d.Channels])
		}
	}
}

// Close stops recording. The device may take one read's worth of frames
// to notice, because snd_pcm_readi blocks.
func (d *CaptureDevice) Close() {
	if d.pcm == nil {
		return
	}
	close(d.stop)
	<-d.done
	d.lib.close(d.pcm)
	d.pcm = nil
}
