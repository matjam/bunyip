package audioout

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ALSA output and input through libasound's "default" device, which
// PulseAudio and PipeWire both provide, so one path serves every Linux
// desktop.

var errUnsupported = ErrUnsupported

const (
	sndPCMStreamPlayback     = 0
	sndPCMStreamCapture      = 1
	sndPCMFormatFloatLE      = 14
	sndPCMAccessRWInterleave = 3
)

type alsa struct {
	hints     func(card int32, iface *byte, hints *unsafe.Pointer) int32
	getHint   func(hint unsafe.Pointer, key *byte) *byte
	freeHints func(hints unsafe.Pointer) int32
	free      func(unsafe.Pointer)
	open      func(pcm *unsafe.Pointer, name *byte, stream int32, mode int32) int32
	setParams func(pcm unsafe.Pointer, format, access int32, channels uint32, rate uint32, softResample int32, latencyUS uint32) int32
	writei    func(pcm unsafe.Pointer, buf unsafe.Pointer, frames uintptr) int
	readi     func(pcm unsafe.Pointer, buf unsafe.Pointer, frames uintptr) int
	prepare   func(pcm unsafe.Pointer) int32
	resume    func(pcm unsafe.Pointer) int32
	drop      func(pcm unsafe.Pointer) int32
	close     func(pcm unsafe.Pointer) int32
	strerror  func(err int32) *byte
}

// loadALSA binds the handful of libasound calls both directions need.
var alsaOnce sync.Once
var alsaLib *alsa
var alsaErr error

func loadALSA() (*alsa, error) {
	alsaOnce.Do(func() { alsaLib, alsaErr = bindALSA() })
	return alsaLib, alsaErr
}

func bindALSA() (*alsa, error) {
	h, err := purego.Dlopen("libasound.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUnsupported, err)
	}
	lib := &alsa{}
	for name, fptr := range map[string]any{
		"snd_pcm_open": &lib.open, "snd_pcm_set_params": &lib.setParams, "snd_pcm_writei": &lib.writei,
		"snd_pcm_readi": &lib.readi, "snd_pcm_prepare": &lib.prepare, "snd_pcm_resume": &lib.resume, "snd_pcm_close": &lib.close,
		"snd_pcm_drop":         &lib.drop,
		"snd_strerror":         &lib.strerror,
		"snd_device_name_hint": &lib.hints, "snd_device_name_get_hint": &lib.getHint,
		"snd_device_name_free_hint": &lib.freeHints, "free": &lib.free,
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
func (lib *alsa) openPCM(id string, stream int32, rate, channels int, latencyUS uint32) (unsafe.Pointer, error) {
	if id == "" {
		id = "default"
	}
	name := append([]byte(id), 0)
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
	closeOnce sync.Once
	control   sync.Mutex
	Rate      int
	lib       *alsa
	pcm       unsafe.Pointer
	stop      chan struct{}
	done      chan struct{}
}

// Open starts stereo float32 output at rate, calling cb for frames from
// its own goroutine.
func openDevice(id string, rate int, cb Callback) (*Device, error) {
	lib, err := loadALSA()
	if err != nil {
		return nil, err
	}
	const latencyUS = 40000
	pcm, err := lib.openPCM(id, sndPCMStreamPlayback, rate, 2, latencyUS)
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
			select {
			case <-d.stop:
				return
			default:
			}
			n := d.lib.writei(d.pcm, unsafe.Pointer(&buf[written*2]), uintptr(frames-written))
			select {
			case <-d.stop:
				return
			default:
			}
			if n < 0 {
				if !d.lib.recoverPCM(d.pcm, n, d.stop, &d.control) {
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
	d.closeOnce.Do(func() {
		if d.pcm == nil {
			return
		}
		close(d.stop)
		// Stop native I/O before joining a worker that may be blocked in it.
		d.control.Lock()
		d.lib.drop(d.pcm)
		d.control.Unlock()
		<-d.done
		d.lib.close(d.pcm)
	})
}

// CaptureDevice is an open ALSA capture stream.
type CaptureDevice struct {
	closeOnce sync.Once
	control   sync.Mutex
	Rate      int
	Channels  int
	lib       *alsa
	pcm       unsafe.Pointer
	stop      chan struct{}
	done      chan struct{}
}

// OpenCapture starts recording interleaved float32 at rate from the
// default device, calling cb with what arrives from its own goroutine.
func openCaptureDevice(id string, rate, channels int, cb CaptureCallback) (*CaptureDevice, error) {
	lib, err := loadALSA()
	if err != nil {
		return nil, err
	}
	// A shorter latency than the output's, because a game reads captured
	// audio once a frame and a long buffer would lag behind the player.
	const latencyUS = 20000
	pcm, err := lib.openPCM(id, sndPCMStreamCapture, rate, channels, latencyUS)
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
		select {
		case <-d.stop:
			return
		default:
		}
		if n < 0 {
			if !d.lib.recoverPCM(d.pcm, n, d.stop, &d.control) {
				return
			}
			continue
		}
		if n > 0 {
			cb(buf[:n*d.Channels])
		}
	}
}

// Close stops native recording and joins the capture callback worker.
func (d *CaptureDevice) Close() {
	d.closeOnce.Do(func() {
		if d.pcm == nil {
			return
		}
		close(d.stop)
		d.control.Lock()
		d.lib.drop(d.pcm)
		d.control.Unlock()
		<-d.done
		d.lib.close(d.pcm)
	})
}

// recoverPCM follows ALSA's recover policy without its uninterruptible suspend
// retry loop. Serialize native state changes with drop so shutdown cannot be
// followed by another prepare/resume. Native read/write and retry waits do not
// hold control.
func (lib *alsa) recoverPCM(pcm unsafe.Pointer, code int, stop <-chan struct{}, control *sync.Mutex) bool {
	for {
		control.Lock()
		select {
		case <-stop:
			control.Unlock()
			return false
		default:
		}
		var rc int32
		retry := false
		switch code {
		case -int(syscall.EINTR):
		case -int(syscall.EPIPE):
			rc = lib.prepare(pcm)
		case -int(syscall.ESTRPIPE):
			rc = lib.resume(pcm)
			retry = rc == -int32(syscall.EAGAIN)
			if rc < 0 && !retry {
				rc = lib.prepare(pcm)
			}
		default:
			rc = -1
		}
		control.Unlock()
		if !retry {
			return rc >= 0
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-stop:
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

// OutputDevices enumerates configured ALSA playback PCMs, including virtual routing endpoints.
func OutputDevices() ([]DeviceInfo, error) { return alsaDevices("Output") }

// InputDevices enumerates configured ALSA capture PCMs.
func InputDevices() ([]DeviceInfo, error) { return alsaDevices("Input") }

func alsaDevices(direction string) ([]DeviceInfo, error) {
	lib, err := loadALSA()
	if err != nil {
		return nil, err
	}
	return lib.devices(direction)
}

func (lib *alsa) devices(direction string) ([]DeviceInfo, error) {
	var hints unsafe.Pointer
	iface := []byte("pcm\x00")
	if rc := lib.hints(-1, &iface[0], &hints); rc < 0 {
		return nil, fmt.Errorf("%w: ALSA hints: %s", ErrUnavailable, cstr(lib.strerror(rc)))
	}
	if hints == nil {
		return nil, nil
	}
	defer lib.freeHints(hints)
	get := func(hint unsafe.Pointer, key string) string {
		b := append([]byte(key), 0)
		p := lib.getHint(hint, &b[0])
		if p == nil {
			return ""
		}
		defer lib.free(unsafe.Pointer(p))
		return cstr(p)
	}
	var out []DeviceInfo
	seen := map[string]bool{}
	for i := 0; ; i++ {
		hint := *(*unsafe.Pointer)(unsafe.Add(hints, uintptr(i)*unsafe.Sizeof(hints)))
		if hint == nil {
			break
		}
		io := get(hint, "IOID")
		if io != "" && io != direction {
			continue
		}
		id := get(hint, "NAME")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.Join(strings.Fields(get(hint, "DESC")), " ")
		if name == "" {
			name = id
		}
		out = append(out, DeviceInfo{ID: id, Name: name, Default: id == "default"})
	}
	return out, nil
}
