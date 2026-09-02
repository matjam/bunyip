package network

import (
	"errors"
	"testing"
	"time"
)

// messages keeps only the Message events.
func messages(evs []Event) []Event {
	var out []Event
	for _, ev := range evs {
		if ev.Kind == Message {
			out = append(out, ev)
		}
	}
	return out
}

func udpPair(t *testing.T) (a, b *Peer) {
	t.Helper()
	reg := registry()
	a, err := ListenUDP("127.0.0.1:0", reg)
	if err != nil {
		t.Fatal(err)
	}
	b, err = ListenUDP("127.0.0.1:0", reg)
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// waitFor drains a peer until fn accepts an event.
func waitFor(t *testing.T, p *Peer, what string, fn func(Event) bool) Event {
	t.Helper()
	var found Event
	wait(t, what, func() bool {
		for _, ev := range p.Poll() {
			if fn(ev) {
				found = ev
				return true
			}
		}
		return false
	})
	return found
}

func TestUDPReliableLossy(t *testing.T) {
	a, b := udpPair(t)
	a.SetLoss(0.3)
	b.SetLoss(0.3)
	const n = 200
	for i := range n {
		if err := a.SendReliable(b.Addr(), move{i, 0}); err != nil {
			t.Fatal(err)
		}
		if i%10 == 0 {
			a.Send(b.Addr(), pos{float32(i), 0}) // unreliable traffic shares the link
		}
	}
	var got []int
	var unreliable int
	wait(t, "reliable messages", func() bool {
		for _, ev := range messages(b.Poll()) {
			switch m := ev.Msg.(type) {
			case *move:
				got = append(got, m.X)
			case *pos:
				unreliable++
			}
		}
		return len(got) >= n
	})
	// Give any duplicate a chance to show up.
	time.Sleep(50 * time.Millisecond)
	for _, ev := range messages(b.Poll()) {
		if m, ok := ev.Msg.(*move); ok {
			got = append(got, m.X)
		}
	}
	if len(got) != n {
		t.Fatalf("delivered %d reliable messages, want %d", len(got), n)
	}
	for i, x := range got {
		if x != i {
			t.Fatalf("message %d carried %d: out of order", i, x)
		}
	}
	if unreliable == 0 {
		t.Error("no unreliable message arrived")
	}
	wait(t, "acknowledgements", func() bool {
		s, _ := a.Stats(b.Addr())
		return s.Pending == 0
	})
	s, ok := a.Stats(b.Addr())
	if !ok || s.RTT <= 0 || s.Loss <= 0 || !s.Connected {
		t.Errorf("stats %+v", s)
	}
}

func TestUDPReliableOrderAcrossSenders(t *testing.T) {
	a, b := udpPair(t)
	c, err := ListenUDP("127.0.0.1:0", registry())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	b.SetLoss(0.2)
	for i := range 20 {
		a.SendReliable(b.Addr(), move{i, 1})
		c.SendReliable(b.Addr(), move{i, 2})
	}
	next := map[int]int{}
	wait(t, "both streams", func() bool {
		for _, ev := range messages(b.Poll()) {
			m := ev.Msg.(*move)
			if m.X != next[m.Y] {
				t.Fatalf("sender %d: got %d, want %d", m.Y, m.X, next[m.Y])
			}
			next[m.Y]++
		}
		return next[1] == 20 && next[2] == 20
	})
}

func TestUDPConnectionLifetime(t *testing.T) {
	a, b := udpPair(t)
	a.SetTimeout(300 * time.Millisecond)
	b.SetTimeout(300 * time.Millisecond)
	if err := a.Connect(b.Addr()); err != nil {
		t.Fatal(err)
	}
	ev := waitFor(t, b, "b sees a", func(ev Event) bool { return ev.Kind == Connected })
	if ev.From == nil || ev.From.Port != a.Addr().Port {
		t.Fatalf("b connected from %v", ev.From)
	}
	waitFor(t, a, "a sees b", func(ev Event) bool { return ev.Kind == Connected && ev.From.Port == b.Addr().Port })
	if ps := b.Peers(); len(ps) != 1 || ps[0].Port != a.Addr().Port {
		t.Fatalf("b's peers %v", ps)
	}
	// Idle well past the timeout: keepalives hold the link up.
	time.Sleep(700 * time.Millisecond)
	for _, ev := range append(a.Poll(), b.Poll()...) {
		if ev.Kind == Disconnected {
			t.Fatalf("disconnected while idle: %v", ev.Err)
		}
	}
	if len(a.Peers()) != 1 || len(b.Peers()) != 1 {
		t.Fatal("idle link dropped")
	}
	// b goes silent: a times it out.
	b.SetLoss(1)
	ev = waitFor(t, a, "timeout", func(ev Event) bool { return ev.Kind == Disconnected })
	if !errors.Is(ev.Err, ErrTimeout) || ev.From.Port != b.Addr().Port {
		t.Fatalf("disconnect %v from %v", ev.Err, ev.From)
	}
	if len(a.Peers()) != 0 {
		t.Fatal("timed-out peer still listed")
	}
}

func TestUDPGoodbye(t *testing.T) {
	a, b := udpPair(t)
	a.Send(b.Addr(), move{1, 1})
	waitFor(t, a, "connect", func(ev Event) bool { return ev.Kind == Connected })
	b.Close()
	ev := waitFor(t, a, "goodbye", func(ev Event) bool { return ev.Kind == Disconnected })
	if ev.Err != nil || ev.From.Port != b.Addr().Port {
		t.Fatalf("goodbye %v from %v", ev.Err, ev.From)
	}
	// Disconnect from this side tells the other.
	c, err := ListenUDP("127.0.0.1:0", registry())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	a.Send(c.Addr(), move{2, 2})
	waitFor(t, c, "c sees a", func(ev Event) bool { return ev.Kind == Connected })
	a.Disconnect(c.Addr())
	ev = waitFor(t, c, "disconnect", func(ev Event) bool { return ev.Kind == Disconnected })
	if ev.Err != nil {
		t.Fatalf("disconnect err %v", ev.Err)
	}
	if _, ok := a.Stats(c.Addr()); ok {
		t.Fatal("disconnected link still has stats")
	}
}

func TestUDPPeerRestart(t *testing.T) {
	a, b := udpPair(t)
	a.Send(b.Addr(), move{1, 1})
	waitFor(t, a, "connect", func(ev Event) bool { return ev.Kind == Connected })
	// b vanishes without a goodbye and comes back on the same port.
	b.SetLoss(1)
	addr := b.Addr().String()
	b.Close()
	b2, err := ListenUDP(addr, registry())
	if err != nil {
		t.Skip("could not rebind", err)
	}
	defer b2.Close()
	b2.SendReliable(a.Addr(), move{2, 2})
	var kinds []EventKind
	var resetErr error
	wait(t, "reset", func() bool {
		for _, ev := range a.Poll() {
			kinds = append(kinds, ev.Kind)
			if ev.Kind == Disconnected {
				resetErr = ev.Err
			}
		}
		return len(kinds) >= 3
	})
	want := []EventKind{Disconnected, Connected, Message}
	for i, k := range want {
		if kinds[i] != k {
			t.Fatalf("events %v, want %v", kinds, want)
		}
	}
	if !errors.Is(resetErr, ErrReset) {
		t.Fatalf("reset error %v", resetErr)
	}
}

func TestUDPAckWindow(t *testing.T) {
	var l link
	l.reset(time.Now())
	for _, seq := range []uint32{1, 2, 4, 3, 40, 39} {
		if l.noteReceived(seq) {
			t.Fatalf("%d reported as a duplicate", seq)
		}
	}
	if !l.noteReceived(40) || !l.noteReceived(39) {
		t.Fatal("duplicate not caught")
	}
	if l.noteReceived(4) {
		t.Fatal("a packet older than the window is not a known duplicate")
	}
	if l.inLatest != 40 || l.inBits != 1 {
		t.Fatalf("latest %d bits %b", l.inLatest, l.inBits)
	}
	l.noteReceived(41)
	l.noteReceived(43)
	if l.inLatest != 43 || l.inBits != 0b1110 { // 42 missing; 41, 40 and 39 present
		t.Fatalf("latest %d bits %b", l.inLatest, l.inBits)
	}
}
