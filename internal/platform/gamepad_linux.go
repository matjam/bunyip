package platform

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/matjam/bunyip/input"
)

// Gamepads come from the kernel joystick interface, /dev/input/js0 to
// js3, read without blocking each poll. The button and axis numbering
// follows the xpad driver's Xbox layout, which most controllers report.

type joystick struct {
	f     *os.File
	state GamepadState
}

var joysticks [input.MaxGamepads]*joystick

const (
	jsEventButton = 0x01
	jsEventAxis   = 0x02
	jsEventInit   = 0x80
	jsiocgname    = 0x80006a13 // JSIOCGNAME(len) with len folded in below
)

var jsButtons = [...]input.GamepadButton{
	0: input.ButtonA, 1: input.ButtonB, 2: input.ButtonX, 3: input.ButtonY, 4: input.ButtonLeftShoulder, 5: input.ButtonRightShoulder,
	6: input.ButtonOptions, 7: input.ButtonMenu, 8: input.ButtonHome, 9: input.ButtonLeftStick, 10: input.ButtonRightStick,
}

// Gamepads reads every joystick device that can be opened.
func (a *App) Gamepads() []GamepadState {
	out := make([]GamepadState, input.MaxGamepads)
	for i := range joysticks {
		js := joysticks[i]
		if js == nil {
			f, err := os.OpenFile(fmt.Sprintf("/dev/input/js%d", i), os.O_RDONLY|syscall.O_NONBLOCK, 0)
			if err != nil {
				continue
			}
			js = &joystick{f: f, state: GamepadState{Connected: true, Name: jsName(f)}}
			joysticks[i] = js
		}
		if !js.read() {
			js.f.Close()
			joysticks[i] = nil
			continue
		}
		out[i] = js.state
	}
	return out
}

func jsName(f *os.File) string {
	var buf [128]byte
	req := uintptr(jsiocgname | (uintptr(len(buf)) << 16))
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
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
		n, err := js.f.Read(buf[:])
		if err != nil {
			if pe, ok := err.(*os.PathError); ok && pe.Err == syscall.EAGAIN {
				return true
			}
			return false
		}
		for i := 0; i+8 <= n; i += 8 {
			value := int16(binary.LittleEndian.Uint16(buf[i+4:]))
			typ := buf[i+6] &^ jsEventInit
			number := int(buf[i+7])
			switch typ {
			case jsEventButton:
				if number < len(jsButtons) {
					js.state.Buttons[jsButtons[number]] = value != 0
				}
			case jsEventAxis:
				v := float32(value) / 32767
				switch number {
				case 0:
					js.state.Axes[input.AxisLeftX] = v
				case 1:
					js.state.Axes[input.AxisLeftY] = -v
				case 2:
					js.state.Axes[input.AxisLeftTrigger] = (v + 1) / 2
				case 3:
					js.state.Axes[input.AxisRightX] = v
				case 4:
					js.state.Axes[input.AxisRightY] = -v
				case 5:
					js.state.Axes[input.AxisRightTrigger] = (v + 1) / 2
				case 6:
					js.state.Buttons[input.ButtonDpadLeft] = value < 0
					js.state.Buttons[input.ButtonDpadRight] = value > 0
				case 7:
					js.state.Buttons[input.ButtonDpadUp] = value < 0
					js.state.Buttons[input.ButtonDpadDown] = value > 0
				}
			}
		}
	}
}
