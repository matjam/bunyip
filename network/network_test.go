package network

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

type hello struct{ Name string }
type move struct{ X, Y int }

// pos uses a custom binary encoding.
type pos struct{ X, Y float32 }

func (p pos) MarshalBinary() ([]byte, error) {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, uint32(p.X*1000))
	binary.LittleEndian.PutUint32(b[4:], uint32(p.Y*1000))
	return b, nil
}

func (p *pos) UnmarshalBinary(b []byte) error {
	if len(b) != 8 {
		return errors.New("bad pos")
	}
	p.X = float32(binary.LittleEndian.Uint32(b)) / 1000
	p.Y = float32(binary.LittleEndian.Uint32(b[4:])) / 1000
	return nil
}

func registry() *Registry { return NewRegistry().Register(hello{}, move{}, pos{}) }

// wait polls until fn reports done or the deadline passes.
func wait(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for", what)
}

func TestRegistry(t *testing.T) {
	r := registry()
	data, err := r.encode(&move{3, 4})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := r.decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := msg.(*move); !ok || m.X != 3 || m.Y != 4 {
		t.Fatalf("decoded %#v", msg)
	}
	data, _ = r.encode(pos{1.5, 2.25})
	if len(data) != 10 {
		t.Fatalf("binary marshaler not used: %d bytes", len(data))
	}
	if p, _ := r.decode(data); p.(*pos).Y != 2.25 {
		t.Fatalf("binary round trip %#v", p)
	}
	if _, err := r.encode(struct{}{}); !errors.Is(err, ErrUnknownMessage) {
		t.Fatalf("unregistered type: %v", err)
	}
}

func TestTCP(t *testing.T) {
	reg := registry()
	srv, err := Listen("127.0.0.1:0", reg)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	woke := make(chan struct{}, 64)
	srv.SetOnActivity(func() {
		select {
		case woke <- struct{}{}:
		default:
		}
	})
	cl, err := Dial(srv.Addr(), reg, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var serverEvents, clientEvents []Event
	wait(t, "connect", func() bool {
		serverEvents = append(serverEvents, srv.Poll()...)
		clientEvents = append(clientEvents, cl.Poll()...)
		return len(serverEvents) >= 1 && len(clientEvents) >= 1
	})
	if serverEvents[0].Kind != Connected || clientEvents[0].Kind != Connected {
		t.Fatalf("first events %v %v", serverEvents[0].Kind, clientEvents[0].Kind)
	}
	sc := serverEvents[0].Conn
	if sc.ID != 1 {
		t.Fatalf("server conn id %d", sc.ID)
	}
	<-woke
	// Client to server, several messages, in order.
	for i := range 50 {
		if err := cl.Send(move{i, i * 2}); err != nil {
			t.Fatal(err)
		}
	}
	cl.Send(&hello{"alice"})
	var got []Event
	wait(t, "messages", func() bool {
		got = append(got, srv.Poll()...)
		return len(got) >= 51
	})
	for i := range 50 {
		m, ok := got[i].Msg.(*move)
		if !ok || m.X != i || got[i].Conn != sc {
			t.Fatalf("message %d: %#v", i, got[i].Msg)
		}
	}
	if h, ok := got[50].Msg.(*hello); !ok || h.Name != "alice" {
		t.Fatalf("hello %#v", got[50].Msg)
	}
	// Server to client, via broadcast, with a large payload.
	big := hello{Name: string(make([]byte, 200000))}
	srv.Broadcast(big)
	wait(t, "broadcast", func() bool {
		for _, ev := range cl.Poll() {
			if h, ok := ev.Msg.(*hello); ok && len(h.Name) == 200000 {
				return true
			}
		}
		return false
	})
	// A clean close shows up as Disconnected with no error.
	cl.Close()
	wait(t, "disconnect", func() bool {
		for _, ev := range srv.Poll() {
			if ev.Kind == Disconnected {
				if ev.Err != nil {
					t.Fatalf("disconnect error %v", ev.Err)
				}
				return true
			}
		}
		return false
	})
	wait(t, "conn removal", func() bool { return len(srv.Conns()) == 0 })
}

func TestUDP(t *testing.T) {
	reg := registry()
	a, err := ListenUDP("127.0.0.1:0", reg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := ListenUDP("127.0.0.1:0", reg)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := a.Send(b.Addr(), pos{1, 2}); err != nil {
		t.Fatal(err)
	}
	var got []Event
	wait(t, "datagram", func() bool {
		got = append(got, b.Poll()...)
		return len(got) >= 1
	})
	if p, ok := got[0].Msg.(*pos); !ok || p.X != 1 || got[0].From.Port != a.Addr().Port {
		t.Fatalf("got %#v from %v", got[0].Msg, got[0].From)
	}
	// Reply the other way.
	b.Send(got[0].From, move{7, 8})
	wait(t, "reply", func() bool {
		for _, ev := range a.Poll() {
			if m, ok := ev.Msg.(*move); ok && m.X == 7 {
				return true
			}
		}
		return false
	})
	if err := a.Send(b.Addr(), hello{Name: string(make([]byte, 2000))}); err == nil {
		t.Fatal("oversized datagram accepted")
	}
}
