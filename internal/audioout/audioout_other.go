//go:build !darwin && !windows && !linux

package audioout

type Device struct{ Rate int }

func openDevice(id string, rate int, cb Callback) (*Device, error) { return nil, ErrUnsupported }
func (d *Device) Close()                                           {}

// CaptureDevice is an input stream, which this operating system has no
// backend for.
type CaptureDevice struct {
	Rate     int
	Channels int
}

func openCaptureDevice(id string, rate, channels int, cb CaptureCallback) (*CaptureDevice, error) {
	return nil, ErrUnsupported
}
func (d *CaptureDevice) Close() {}

func OutputDevices() ([]DeviceInfo, error) { return nil, ErrUnsupported }
func InputDevices() ([]DeviceInfo, error)  { return nil, ErrUnsupported }
