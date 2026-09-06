package platform

import (
	"github.com/matjam/bunyip/input"
	"testing"
	"unsafe"
)

func TestXInputCapabilitiesDescribeMappedControls(t *testing.T) {
	c := xinputCapabilities{Gamepad: xinputGamepad{Buttons: xiA | xiStart, ThumbLX: -1, RightTrigger: 255}}
	i := xinputInfo(c)
	if !i.HasButton(input.ButtonA) || !i.HasButton(input.ButtonMenu) || i.HasButton(input.ButtonB) || i.HasButton(input.ButtonHome) || !i.HasAxis(input.AxisLeftX) || i.HasAxis(input.AxisLeftY) || !i.HasAxis(input.AxisRightTrigger) || i.VendorID != 0 || i.ProductID != 0 {
		t.Fatalf("capabilities=%+v", i)
	}
	if unsafe.Sizeof(c) != 20 || unsafe.Offsetof(c.Gamepad) != 4 || unsafe.Offsetof(c.Vibration) != 16 {
		t.Fatal("XInput capabilities ABI mismatch")
	}
}
