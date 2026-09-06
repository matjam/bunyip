package platform

import (
	"github.com/matjam/bunyip/input"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

func (js *joystick) readInfo(slot int) {
	ioctl := func(request uintptr, value unsafe.Pointer) bool {
		_, _, err := syscall.Syscall(syscall.SYS_IOCTL, uintptr(js.fd), request, uintptr(value))
		return err == 0
	}
	if !ioctl(0x80016a11, unsafe.Pointer(&js.axisCount)) || !ioctl(0x80406a32, unsafe.Pointer(&js.axisMap[0])) {
		js.axisCount = 0
	}
	if !ioctl(0x80016a12, unsafe.Pointer(&js.buttonCount)) || !ioctl(0x84006a34, unsafe.Pointer(&js.buttonMap[0])) {
		js.buttonCount = 0
	}
	info := js.mappedInfo()
	base := "/sys/class/input/js" + strconv.Itoa(slot) + "/device/id/"
	readID := func(name string) uint16 {
		b, err := os.ReadFile(base + name)
		if err != nil {
			return 0
		}
		v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 16, 16)
		if err != nil {
			return 0
		}
		return uint16(v)
	}
	info.VendorID, info.ProductID = readID("vendor"), readID("product")
	js.state.Info = info
}

func jsButton(code uint16) (input.GamepadButton, bool) {
	switch code {
	case 0x130:
		return input.ButtonA, true
	case 0x131:
		return input.ButtonB, true
	case 0x133:
		return input.ButtonX, true
	case 0x134:
		return input.ButtonY, true
	case 0x136:
		return input.ButtonLeftShoulder, true
	case 0x137:
		return input.ButtonRightShoulder, true
	case 0x13a:
		return input.ButtonOptions, true
	case 0x13b:
		return input.ButtonMenu, true
	case 0x13c:
		return input.ButtonHome, true
	case 0x13d:
		return input.ButtonLeftStick, true
	case 0x13e:
		return input.ButtonRightStick, true
	case 0x220:
		return input.ButtonDpadUp, true
	case 0x221:
		return input.ButtonDpadDown, true
	case 0x222:
		return input.ButtonDpadLeft, true
	case 0x223:
		return input.ButtonDpadRight, true
	}
	return 0, false
}

func jsAxis(code uint8) (input.GamepadAxis, bool) {
	switch code {
	case 0:
		return input.AxisLeftX, true
	case 1:
		return input.AxisLeftY, true
	case 2:
		return input.AxisLeftTrigger, true
	case 3:
		return input.AxisRightX, true
	case 4:
		return input.AxisRightY, true
	case 5:
		return input.AxisRightTrigger, true
	}
	return 0, false
}

func (js *joystick) mappedInfo() input.GamepadInfo {
	info := input.GamepadInfo{Name: js.state.Name, Backend: "linux-joystick"}
	for _, code := range js.buttonMap[:js.buttonCount] {
		if b, ok := jsButton(code); ok {
			info.Buttons[b] = true
		}
	}
	for _, code := range js.axisMap[:min(int(js.axisCount), len(js.axisMap))] {
		if a, ok := jsAxis(code); ok {
			info.Axes[a] = true
		}
		if code == 16 {
			info.Buttons[input.ButtonDpadLeft], info.Buttons[input.ButtonDpadRight] = true, true
		}
		if code == 17 {
			info.Buttons[input.ButtonDpadUp], info.Buttons[input.ButtonDpadDown] = true, true
		}
	}
	return info
}

func (js *joystick) event(typ uint8, number int, value int16) {
	if typ == jsEventButton {
		if number < int(js.buttonCount) {
			if b, ok := jsButton(js.buttonMap[number]); ok {
				js.state.Buttons[b] = value != 0
			}
		}
		return
	}
	if typ != jsEventAxis || number >= min(int(js.axisCount), len(js.axisMap)) {
		return
	}
	code := js.axisMap[number]
	if a, ok := jsAxis(code); ok {
		v := max(-1, float32(value)/32767)
		if a == input.AxisLeftY || a == input.AxisRightY {
			v = -v
		}
		if a == input.AxisLeftTrigger || a == input.AxisRightTrigger {
			v = (v + 1) / 2
		}
		js.state.Axes[a] = v
	}
	if code == 16 {
		js.state.Buttons[input.ButtonDpadLeft], js.state.Buttons[input.ButtonDpadRight] = value < 0, value > 0
	}
	if code == 17 {
		js.state.Buttons[input.ButtonDpadUp], js.state.Buttons[input.ButtonDpadDown] = value < 0, value > 0
	}
}
