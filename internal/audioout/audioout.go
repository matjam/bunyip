// Package audioout opens the operating system's default audio output and
// pulls interleaved float32 stereo frames from a callback on the device's
// own thread. It also opens the default input, pushing interleaved
// float32 samples to a callback the same way. Each operating system has
// its own implementation; the callback contracts are the same
// everywhere.
package audioout

// Callback fills out with len(out)/2 stereo frames.
type Callback func(out []float32)

// CaptureCallback receives interleaved float32 samples the input device
// has recorded, at the rate and channel count the stream was opened
// with. The slice belongs to the device and is only valid for the call,
// so a callback that keeps the samples copies them.
type CaptureCallback func(in []float32)
