package platform

import (
	"testing"
	"unsafe"
)

func TestNativeCursorAndDisplayLayouts(t *testing.T) {
	var bitmap bitmapInfoHeader
	if unsafe.Sizeof(bitmap) != 40 || unsafe.Offsetof(bitmap.BitCount) != 14 || unsafe.Offsetof(bitmap.SizeImage) != 20 {
		t.Fatal("BITMAPINFOHEADER layout mismatch")
	}
	var display displayDevice
	if unsafe.Sizeof(display) != 840 || unsafe.Offsetof(display.Name) != 4 || unsafe.Offsetof(display.Flags) != 324 {
		t.Fatal("DISPLAY_DEVICEW layout mismatch")
	}
	var info iconInfo
	if unsafe.Sizeof(info) != 32 || unsafe.Offsetof(info.Mask) != 16 || unsafe.Offsetof(info.Color) != 24 {
		t.Fatal("ICONINFO layout mismatch")
	}
}
