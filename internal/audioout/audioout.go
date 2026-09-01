// Package audioout opens the operating system's default audio output and
// pulls interleaved float32 stereo frames from a callback on the device's
// own thread. Each operating system has its own implementation; the
// callback contract is the same everywhere.
package audioout

// Callback fills out with len(out)/2 stereo frames.
type Callback func(out []float32)
