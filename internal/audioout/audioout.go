// Package audioout enumerates and opens native audio endpoints and
// pulls interleaved float32 stereo frames from a callback on the device's
// own thread. It also opens the default input, pushing interleaved
// float32 samples to a callback the same way. Each operating system has
// its own implementation; the callback contracts are the same
// everywhere.
package audioout

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// DeviceInfo identifies a native endpoint; its ID can be passed back to open.
type DeviceInfo struct {
	ID      string
	Name    string
	Default bool
}

var ErrUnsupported = errors.New("audioout: backend unavailable")
var ErrUnavailable = errors.New("audioout: device unavailable")

// Open starts the system-default output.
func Open(rate int, cb Callback) (*Device, error) { return OpenDevice("", rate, cb) }

// OpenDevice starts a named output; empty selects system-default routing.
func OpenDevice(id string, rate int, cb Callback) (*Device, error) {
	if strings.ContainsRune(id, 0) || rate <= 0 || uint64(rate) > math.MaxUint32/8 || cb == nil {
		return nil, fmt.Errorf("%w: invalid output options", ErrUnavailable)
	}
	d, err := openDevice(id, rate, cb)
	return d, deviceError(err)
}

// OpenCapture starts the system-default input.
func OpenCapture(rate, channels int, cb CaptureCallback) (*CaptureDevice, error) {
	return OpenCaptureDevice("", rate, channels, cb)
}

// OpenCaptureDevice starts a named input; empty selects the system default.
func OpenCaptureDevice(id string, rate, channels int, cb CaptureCallback) (*CaptureDevice, error) {
	if strings.ContainsRune(id, 0) || rate <= 0 || channels < 1 || channels > 32 || uint64(rate) > math.MaxUint32/4/uint64(channels) || cb == nil {
		return nil, fmt.Errorf("%w: invalid input options", ErrUnavailable)
	}
	d, err := openCaptureDevice(id, rate, channels, cb)
	return d, deviceError(err)
}

func deviceError(err error) error {
	if err == nil || errors.Is(err, ErrUnsupported) || errors.Is(err, ErrUnavailable) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// Callback fills out with len(out)/2 stereo frames.
type Callback func(out []float32)

// CaptureCallback receives interleaved float32 samples the input device
// has recorded, at the rate and channel count the stream was opened
// with. The slice belongs to the device and is only valid for the call,
// so a callback that keeps the samples copies them.
type CaptureCallback func(in []float32)
