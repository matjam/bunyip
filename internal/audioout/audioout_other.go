//go:build !darwin

package audioout

import "errors"

// ErrUnsupported is returned where no audio backend exists yet.
var ErrUnsupported = errors.New("audioout: no audio backend for this operating system yet")

type Device struct{ Rate int }

func Open(rate int, cb Callback) (*Device, error) { return nil, ErrUnsupported }
func (d *Device) Close()                          {}
