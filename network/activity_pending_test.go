package network

import (
	"net"
	"testing"
	"testing/synctest"
)

func TestActivityRegistrationWakesPending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		cl := newClient(a, registry())
		defer cl.Close()
		defer b.Close()
		calls := 0
		cl.SetOnActivity(func() { calls++; cl.Poll(); cl.SetOnActivity(nil) })
		if calls != 1 {
			t.Errorf("client pending callbacks=%d, want 1", calls)
		}
		p := &Peer{events: make(chan Event, 1)}
		p.events <- Event{Kind: Connected}
		calls = 0
		p.SetOnActivity(func() { calls++; p.Poll(); p.SetOnActivity(nil) })
		if calls != 1 {
			t.Errorf("UDP pending callbacks=%d, want 1", calls)
		}
	})
}

func TestUDPActivityReplacementRemovalAndClose(t *testing.T) {
	p, err := ListenUDP("127.0.0.1:0", registry())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.events <- Event{Kind: Connected}
	first, second := 0, 0
	p.SetOnActivity(func() { first++ })
	p.SetOnActivity(func() { second++ })
	p.SetOnActivity(nil)
	if first != 1 || second != 1 {
		t.Fatalf("pending replacements=%d,%d", first, second)
	}
	p.Poll()
	p.SetOnActivity(func() { second++ })
	if second != 1 {
		t.Fatal("empty queue invoked callback")
	}
	p.events <- Event{Kind: Disconnected}
	p.SetOnActivity(func() { p.Poll(); p.SetOnActivity(nil); p.Close() })
}
