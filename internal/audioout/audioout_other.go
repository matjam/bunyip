//go:build !darwin && !windows && !linux

package audioout

import "errors"

// ErrUnsupported is returned where no audio backend exists yet.
var ErrUnsupported = errors.New("audioout: no audio backend for this operating system yet")

type Device struct{ Rate int }

func Open(rate int, cb Callback) (*Device, error) { return nil, ErrUnsupported }
func (d *Device) Close()                          {}

// CaptureDevice is an input stream, which this operating system has no
// backend for.
type CaptureDevice struct {
	Rate     int
	Channels int
}

func OpenCapture(rate, channels int, cb CaptureCallback) (*CaptureDevice, error) {
	return nil, ErrUnsupported
}
func (d *CaptureDevice) Close() {}
