package vk

import "testing"

func TestMissingBindingClearsPreviousFunctionAndFastAddress(t *testing.T) {
	fn := func() {}
	address := uintptr(42)
	fastAddrs[&fn] = &address
	defer delete(fastAddrs, &fn)
	bind(&fn, 0)
	if fn != nil || address != 0 {
		t.Fatalf("missing command retained previous binding: function=%v address=%d", fn != nil, address)
	}
}

func TestDeviceCommandsBindOnlyOncePerInstance(t *testing.T) {
	previous := VkGetInstanceProcAddr
	defer func() { ClearInstance(); VkGetInstanceProcAddr = previous }()
	ClearInstance()
	calls := 0
	VkGetInstanceProcAddr = func(VkInstance, string) PFN_vkVoidFunction { calls++; return 0 }
	if err := LoadDevice(11); err != nil {
		t.Fatal(err)
	}
	first := calls
	if first == 0 {
		t.Fatal("first device did not bind commands")
	}
	if err := LoadDevice(11); err != nil {
		t.Fatal(err)
	}
	if calls != first {
		t.Fatal("second device rewrote shared command table")
	}
	ClearInstance()
	if err := LoadDevice(11); err != nil {
		t.Fatal(err)
	}
	if calls != first*2 {
		t.Fatal("new instance lifetime did not bind commands again")
	}
}
