package platform

import (
	"errors"
	"testing"
)

func TestRawMouseRegistrationFollowsFocusAndSurvivesWindows(t *testing.T) {
	var r rawMouseRegistration
	calls := 0
	register := func(d rawInputDevice) error {
		calls++
		if d.UsagePage != 1 || d.Usage != 2 || d.Flags != 0 || d.Target != 0 {
			t.Fatalf("mouse registration redirects or captures background input: %+v", d)
		}
		return nil
	}
	for range 3 {
		if err := r.ensure(register); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("creating another window re-registered the mouse %d times", calls)
	}
	var failed rawMouseRegistration
	err := errors.New("registration failed")
	if !errors.Is(failed.ensure(func(rawInputDevice) error { return err }), err) || failed.registered {
		t.Fatal("failed registration was accepted")
	}
	if e := failed.ensure(register); e != nil {
		t.Fatal(e)
	}
}

func TestHostRawMouseRegistrationCompatibility(t *testing.T) {
	for _, tc := range []struct {
		devices             []rawInputDevice
		present, compatible bool
	}{
		{nil, false, true},
		{[]rawInputDevice{{UsagePage: 1, Usage: 6, Target: 99}}, false, true},
		{[]rawInputDevice{{UsagePage: 1, Usage: 2}}, true, true},
		{[]rawInputDevice{{UsagePage: 1, Usage: 2, Target: 99}}, true, false},
		{[]rawInputDevice{{UsagePage: 1, Usage: 2, Flags: 0x100}}, true, false},
	} {
		p, c := foregroundMouse(tc.devices)
		if p != tc.present || c != tc.compatible {
			t.Fatalf("registration=%v present=%v compatible=%v", tc.devices, p, c)
		}
	}
	borrowed := rawMouseRegistration{registered: true}
	if err := borrowed.ensure(func(rawInputDevice) error { t.Fatal("replaced host registration"); return nil }); err != nil || borrowed.owned {
		t.Fatal("borrowed registration became owned")
	}
}

func TestRawMouseReleasePreservesBorrowedOrReplacedRegistration(t *testing.T) {
	foreground := []rawInputDevice{{UsagePage: 1, Usage: 2}}
	for _, tc := range []struct {
		name    string
		owned   bool
		current []rawInputDevice
		remove  bool
	}{
		{"owned", true, foreground, true}, {"borrowed", false, foreground, false},
		{"host replacement", true, []rawInputDevice{{UsagePage: 1, Usage: 2, Target: 99}}, false}, {"already removed", true, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := rawMouseRegistration{registered: true, owned: tc.owned}
			removed := false
			r.release(tc.current, func() { removed = true })
			if removed != tc.remove || r.registered || r.owned {
				t.Fatalf("removed=%v registration=%+v", removed, r)
			}
			calls := 0
			if err := r.ensure(func(rawInputDevice) error { calls++; return nil }); err != nil || calls != 1 {
				t.Fatal("reopened application failed to register")
			}
		})
	}
}
