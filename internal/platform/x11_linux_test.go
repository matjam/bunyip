package platform

import (
	"testing"
	"time"
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

func TestDecodeSelection(t *testing.T) {
	cases := []struct {
		name   string
		data   []byte
		latin1 bool
		want   string
	}{
		{"UTF8_STRING is taken as it is", []byte("caffè ☕"), false, "caffè ☕"},
		{"STRING holding UTF-8, as most toolkits write", []byte("caffè"), true, "caffè"},
		{"STRING holding real Latin-1", []byte{'c', 'a', 'f', 'f', 0xe8}, true, "caffè"},
		{"empty", nil, true, ""},
		{"invalid bytes in a UTF8_STRING are left alone", []byte{0xff, 0xfe}, false, "\xff\xfe"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decodeSelection(c.data, c.latin1); got != c.want {
				t.Errorf("decodeSelection = %q, want %q", got, c.want)
			}
		})
	}
}

func TestIncrMaskCountsPerWindow(t *testing.T) {
	m := incrMask{}
	if !m.add(7) {
		t.Error("the first transfer to a window did not ask for the mask")
	}
	if m.add(7) {
		t.Error("the second transfer to a window asked for the mask again")
	}
	if !m.add(9) {
		t.Error("the first transfer to another window did not ask for the mask")
	}
	if m.remove(7) {
		t.Error("ending one of two transfers cleared the mask, which stalls the other")
	}
	if !m.remove(7) {
		t.Error("ending the last transfer did not clear the mask")
	}
	if _, ok := m[7]; ok {
		t.Error("the window is still counted after its last transfer ended")
	}
	if !m.remove(9) {
		t.Error("ending the other window's only transfer did not clear its mask")
	}
	// Removing what was never added must not report a stale window as live.
	if !m.remove(11) {
		t.Error("removing an unknown window did not report the mask as clear")
	}
}

func TestPutIncrReplacesTheSameProperty(t *testing.T) {
	var ts []incrSend
	ts = putIncr(ts, incrSend{requestor: 1, property: 2, data: []byte("first")})
	ts = putIncr(ts, incrSend{requestor: 1, property: 3, data: []byte("other property")})
	ts = putIncr(ts, incrSend{requestor: 4, property: 2, data: []byte("other window")})
	if len(ts) != 3 {
		t.Fatalf("transfers = %d, want 3", len(ts))
	}
	ts = putIncr(ts, incrSend{requestor: 1, property: 2, data: []byte("second")})
	if len(ts) != 3 {
		t.Fatalf("a second request for one property added a transfer: %d, want 3", len(ts))
	}
	if got := string(ts[0].data); got != "second" {
		t.Errorf("data = %q, want the later request's %q", got, "second")
	}
}

func TestDropStaleIncr(t *testing.T) {
	now := time.Now()
	ts := []incrSend{
		{requestor: 1, touched: now.Add(-time.Second)},
		{requestor: 2, touched: now.Add(-time.Minute)},
		{requestor: 3, touched: now},
	}
	kept, stale := dropStaleIncr(ts, now, 5*time.Second)
	if len(kept) != 2 || kept[0].requestor != 1 || kept[1].requestor != 3 {
		t.Errorf("kept = %v, want the transfers touched within the timeout", kept)
	}
	if len(stale) != 1 || stale[0].requestor != 2 {
		t.Errorf("stale = %v, want the transfer idle for a minute", stale)
	}
	if kept, stale := dropStaleIncr(nil, now, time.Second); kept != nil || stale != nil {
		t.Error("an empty list produced transfers out of nowhere")
	}
}
