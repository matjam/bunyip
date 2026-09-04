package platform

import (
	"testing"
	"unsafe"
)

// generic reinterprets a key event as the header the poll loop sees.
func generic(ev *xcbInputEvent) *xcbGenericEvent {
	return (*xcbGenericEvent)(unsafe.Pointer(ev))
}

func TestRepeatPress(t *testing.T) {
	release := &xcbInputEvent{ResponseType: xcbKeyRelease, Detail: 38, Time: 1000, Event: 7}
	cases := []struct {
		name string
		next *xcbInputEvent
		want bool
	}{
		{"the press X sends for a repeat", &xcbInputEvent{ResponseType: xcbKeyPress, Detail: 38, Time: 1000, Event: 7}, true},
		{"a press sent by the server", &xcbInputEvent{ResponseType: xcbKeyPress | 0x80, Detail: 38, Time: 1000, Event: 7}, true},
		{"a press of another key", &xcbInputEvent{ResponseType: xcbKeyPress, Detail: 39, Time: 1000, Event: 7}, false},
		{"a press at another time", &xcbInputEvent{ResponseType: xcbKeyPress, Detail: 38, Time: 1017, Event: 7}, false},
		{"a press on another window", &xcbInputEvent{ResponseType: xcbKeyPress, Detail: 38, Time: 1000, Event: 8}, false},
		{"another release", &xcbInputEvent{ResponseType: xcbKeyRelease, Detail: 38, Time: 1000, Event: 7}, false},
		{"a button press", &xcbInputEvent{ResponseType: xcbButtonPress, Detail: 38, Time: 1000, Event: 7}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := repeatPress(release, generic(c.next)); got != c.want {
				t.Errorf("repeatPress = %v, want %v", got, c.want)
			}
		})
	}
	if repeatPress(release, nil) {
		t.Error("repeatPress with nothing queued behind the release = true, want false")
	}
	if repeatPress(nil, generic(&xcbInputEvent{ResponseType: xcbKeyPress})) {
		t.Error("repeatPress with no release = true, want false")
	}
}
