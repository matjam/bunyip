package platform

import "testing"

func TestX11WakeTargetMovesToSurvivingWindow(t *testing.T) {
	a := &App{windows: map[uint32]*Window{9: {}, 3: {}}}
	a.wakeWin.Store(9)
	delete(a.windows, 9)
	a.refreshWakeWindow(9)
	if a.wakeWin.Load() != 3 {
		t.Fatal("closing wake target did not select survivor")
	}
	a.refreshWakeWindow(77)
	if a.wakeWin.Load() != 3 {
		t.Fatal("closing another window replaced wake target")
	}
	delete(a.windows, 3)
	a.refreshWakeWindow(3)
	if a.wakeWin.Load() != 0 {
		t.Fatal("wake retained destroyed last window")
	}
}
