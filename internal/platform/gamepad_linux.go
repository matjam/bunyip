package platform

import (
	"encoding/binary"
	"syscall"
	"time"
	"unsafe"

	"github.com/matjam/bunyip/input"
)

// Gamepads come from the kernel joystick interface, /dev/input/js0 to
// js3, read without blocking each poll. Kernel button/axis maps identify
// supported controls independently of each driver's index ordering.
// The devices are read through raw descriptors rather than os.File: an
// os.File opened non-blocking joins the runtime's poller, and a Read on
// it parks the goroutine until the device has data, which would stop the
// game loop whenever an idle controller is plugged in.

type joystick struct {
	fd                     int
	state                  GamepadState
	axisMap                [64]uint8
	buttonMap              [512]uint16
	axisCount, buttonCount uint8
}

var joysticks [input.MaxGamepads]*joystick

const (
	jsEventButton = 0x01
	jsEventAxis   = 0x02
	jsEventInit   = 0x80
	jsiocgname    = 0x80006a13 // JSIOCGNAME(len) with len folded in below
)

// jsPaths are the device paths, formatted once rather than per poll.
var jsPaths = [input.MaxGamepads]string{"/dev/input/js0", "/dev/input/js1", "/dev/input/js2", "/dev/input/js3"}

// jsStates is the array Gamepads fills and returns, reused every poll.
var jsStates [input.MaxGamepads]GamepadState

// jsRetry is when each empty slot may be opened again. Opening a device
// that is not there costs a system call and fails, so a slot with no
// controller is only tried once a second; plugging one in is noticed
// within that second, which is faster than a player can look up.
var jsRetry [input.MaxGamepads]time.Time

// jsRetryInterval is how long an empty slot waits before being tried again.
const jsRetryInterval = time.Second

// Gamepads reads every joystick device that can be opened. The slice it
// returns is reused by the next call, so read it before polling again.
func (a *App) Gamepads() []GamepadState {
	now := time.Now()
	for i := range joysticks {
		js := joysticks[i]
		if js == nil {
			if now.Before(jsRetry[i]) {
				jsStates[i] = GamepadState{}
				continue
			}
			fd, err := syscall.Open(jsPaths[i], syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
			if err != nil {
				jsRetry[i] = now.Add(jsRetryInterval)
				jsStates[i] = GamepadState{}
				continue
			}
			js = &joystick{fd: fd, state: GamepadState{Connected: true, Name: jsName(fd)}}
			js.readInfo(i)
			joysticks[i] = js
		}
		if !js.read() {
			syscall.Close(js.fd)
			joysticks[i] = nil
			jsRetry[i] = now.Add(jsRetryInterval)
			jsStates[i] = GamepadState{}
			continue
		}
		jsStates[i] = js.state
	}
	return jsStates[:]
}

func jsName(fd int) string {
	var buf [128]byte
	req := uintptr(jsiocgname | (uintptr(len(buf)) << 16))
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
		return "joystick"
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n])
}

// read drains queued events into the state and reports whether the
// device is still there.
func (js *joystick) read() bool {
	var buf [8 * 64]byte
	for {
		n, err := syscall.Read(js.fd, buf[:])
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EINTR {
				return true // nothing queued; the device is still there
			}
			return false
		}
		if n <= 0 {
			return false // end of file: the device went away
		}
		for i := 0; i+8 <= n; i += 8 {
			value := int16(binary.LittleEndian.Uint16(buf[i+4:]))
			typ := buf[i+6] &^ jsEventInit
			number := int(buf[i+7])
			js.event(typ, number, value)
		}
	}
}
