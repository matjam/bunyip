package platform

import (
	"syscall"
	"time"
	"unsafe"

	"github.com/matjam/bunyip/input"
)

// XInput controller support. The library is optional: without it there
// are simply no gamepads.
var (
	xinput          = syscall.NewLazyDLL("xinput1_4.dll")
	procXInputState = xinput.NewProc("XInputGetState")
)

type xinputGamepad struct {
	Buttons      uint16
	LeftTrigger  uint8
	RightTrigger uint8
	ThumbLX      int16
	ThumbLY      int16
	ThumbRX      int16
	ThumbRY      int16
}

type xinputState struct {
	PacketNumber uint32
	Gamepad      xinputGamepad
}

const (
	xiDpadUp         = 0x0001
	xiDpadDown       = 0x0002
	xiDpadLeft       = 0x0004
	xiDpadRight      = 0x0008
	xiStart          = 0x0010
	xiBack           = 0x0020
	xiLeftThumb      = 0x0040
	xiRightThumb     = 0x0080
	xiLeftShoulder   = 0x0100
	xiRightShoulder  = 0x0200
	xiA              = 0x1000
	xiB              = 0x2000
	xiX              = 0x4000
	xiY              = 0x8000
	errDeviceNotConn = 1167
)

var xiButtons = [...]struct {
	mask uint16
	btn  input.GamepadButton
}{
	{xiA, input.ButtonA}, {xiB, input.ButtonB}, {xiX, input.ButtonX}, {xiY, input.ButtonY},
	{xiLeftShoulder, input.ButtonLeftShoulder}, {xiRightShoulder, input.ButtonRightShoulder},
	{xiLeftThumb, input.ButtonLeftStick}, {xiRightThumb, input.ButtonRightStick},
	{xiStart, input.ButtonMenu}, {xiBack, input.ButtonOptions},
	{xiDpadUp, input.ButtonDpadUp}, {xiDpadDown, input.ButtonDpadDown}, {xiDpadLeft, input.ButtonDpadLeft}, {xiDpadRight, input.ButtonDpadRight},
}

// xiStates is the array Gamepads fills and returns, reused every poll.
var xiStates [input.MaxGamepads]GamepadState

// xiRetry is when each empty slot may be polled again. XInputGetState on
// a slot with no controller is slow, so an empty one is only asked once
// a second; plugging a controller in is noticed within that second.
var xiRetry [input.MaxGamepads]time.Time

// xiRetryInterval is how long an empty slot waits before being polled again.
const xiRetryInterval = time.Second

// xiAddr is XInputGetState resolved once, so each poll is a direct call
// rather than a LazyProc.Call, which allocates its variadic arguments.
var xiAddr uintptr

// Gamepads reads every XInput controller. The slice it returns is reused
// by the next call, so read it before polling again.
func (a *App) Gamepads() []GamepadState {
	if xiAddr == 0 {
		if procXInputState.Find() != nil {
			return nil
		}
		xiAddr = procXInputState.Addr()
	}
	now := time.Now()
	for i := range input.MaxGamepads {
		if now.Before(xiRetry[i]) {
			xiStates[i] = GamepadState{}
			continue
		}
		var st xinputState
		r, _, _ := syscall.SyscallN(xiAddr, uintptr(i), uintptr(unsafe.Pointer(&st)))
		if r != 0 {
			xiRetry[i] = now.Add(xiRetryInterval)
			xiStates[i] = GamepadState{}
			continue
		}
		g := GamepadState{Connected: true, Name: "XInput controller"}
		for _, b := range xiButtons {
			g.Buttons[b.btn] = st.Gamepad.Buttons&b.mask != 0
		}
		g.Axes[input.AxisLeftX] = stick(st.Gamepad.ThumbLX)
		g.Axes[input.AxisLeftY] = stick(st.Gamepad.ThumbLY)
		g.Axes[input.AxisRightX] = stick(st.Gamepad.ThumbRX)
		g.Axes[input.AxisRightY] = stick(st.Gamepad.ThumbRY)
		g.Axes[input.AxisLeftTrigger] = float32(st.Gamepad.LeftTrigger) / 255
		g.Axes[input.AxisRightTrigger] = float32(st.Gamepad.RightTrigger) / 255
		xiStates[i] = g
	}
	return xiStates[:]
}

func stick(v int16) float32 {
	const dead = 7849 // XINPUT_GAMEPAD_LEFT_THUMB_DEADZONE
	f := float32(v)
	if f > -dead && f < dead {
		return 0
	}
	return max(-1, min(1, f/32767))
}
