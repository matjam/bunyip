package network

import (
	"testing"
	"time"
)

// A peer that never answered was never connected, so its timeout is not
// reported as a disconnection.
func TestSilentPeerDoesNotDisconnect(t *testing.T) {
	p, err := ListenUDP("127.0.0.1:0", NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.SetTimeout(200 * time.Millisecond)
	to, err := Resolve("127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Connect(to); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, ev := range p.Poll() {
			if ev.Kind == Disconnected {
				t.Fatal("Disconnected reported for an address that never connected")
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
