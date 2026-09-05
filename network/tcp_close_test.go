package network

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestTCPConnCloseFullQueue(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message bool
		remote  bool
	}{
		{name: "blocked read"},
		{name: "blocked message", message: true},
		{name: "blocked disconnect", remote: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, b := net.Pipe()
				c := &Conn{c: a, reg: registry(), events: make(chan Event, 1), closed: make(chan struct{})}
				c.events <- Event{Kind: Connected}
				done := make(chan struct{})
				go func() { c.readLoop(); close(done) }()
				defer func() {
					c.Close()
					b.Close()
					finishTCPTest(done, c.events)
				}()
				if tc.message {
					writer := &Conn{c: b, reg: c.reg}
					if err := writer.Send(move{X: 1}); err != nil {
						t.Fatal(err)
					}
				}
				if tc.remote {
					b.Close()
				}
				synctest.Wait()
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				awaitTCPDone(t, done)
				if len(c.events) != 1 {
					t.Errorf("Close changed the queued events: %d", len(c.events))
				}
			})
		})
	}
}

func TestTCPRemoteDisconnectPreservesOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		c := &Conn{c: a, reg: registry(), events: make(chan Event, 1), closed: make(chan struct{})}
		done := make(chan struct{})
		go func() { c.readLoop(); close(done) }()
		defer func() {
			c.Close()
			b.Close()
			finishTCPTest(done, c.events)
		}()
		writer := &Conn{c: b, reg: c.reg}
		if err := writer.Send(move{X: 7}); err != nil {
			t.Fatal(err)
		}
		b.Close()
		synctest.Wait()
		select {
		case <-done:
			t.Error("remote disconnect discarded its event while the queue was full")
		default:
		}
		ev := <-c.events
		if m, ok := ev.Msg.(*move); ev.Kind != Message || !ok || m.X != 7 || ev.Conn != c {
			t.Errorf("message event = %#v", ev)
		}
		awaitTCPDone(t, done)
		ev = <-c.events
		if ev.Kind != Disconnected || ev.Err != nil || ev.Conn != c {
			t.Errorf("disconnect event = %#v", ev)
		}
	})
}

func TestTCPConnCloseQueuesDisconnectWhenSpaceAvailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		c := &Conn{c: a, events: make(chan Event, 1), closed: make(chan struct{})}
		done := make(chan struct{})
		go func() { c.readLoop(); close(done) }()
		defer func() {
			c.Close()
			b.Close()
			finishTCPTest(done, c.events)
		}()
		c.Close()
		awaitTCPDone(t, done)
		events := drain(c.events)
		if len(events) != 1 || events[0].Kind != Disconnected || events[0].Err != nil || events[0].Conn != c {
			t.Fatalf("local disconnect events = %#v", events)
		}
	})
}

// A controlled listener makes Accept/Close interleavings independent of the OS.
type tcpCloseListener struct {
	net.Listener
	accept func() (net.Conn, error)
	closed chan struct{}
	once   sync.Once
}

func (l *tcpCloseListener) Accept() (net.Conn, error) { return l.accept() }
func (l *tcpCloseListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func TestTCPServerCloseFullQueue(t *testing.T) {
	for _, name := range []string{"connected", "reader", "accept error", "accepted after close"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, b := net.Pipe()
				ln := &tcpCloseListener{closed: make(chan struct{})}
				release := make(chan struct{})
				calls := 0
				ln.accept = func() (net.Conn, error) {
					calls++
					if calls == 1 {
						if name == "accept error" {
							return nil, io.ErrUnexpectedEOF
						}
						if name == "accepted after close" {
							<-release
						}
						return a, nil
					}
					<-ln.closed
					return nil, net.ErrClosed
				}
				s := &Server{ln: ln, reg: registry(), events: make(chan Event, 1), conns: map[int]*Conn{}, closed: make(chan struct{})}
				if name != "reader" {
					s.events <- Event{Kind: Message}
				}
				done := make(chan struct{})
				go func() { s.accept(); close(done) }()
				defer func() {
					s.Close()
					a.Close()
					b.Close()
					finishTCPTest(done, s.events)
					// Also let readers created during failure cleanup finish.
					synctest.Wait()
					drain(s.events)
					synctest.Wait()
				}()
				synctest.Wait()
				if name == "reader" {
					ev := <-s.events
					if ev.Kind != Connected {
						t.Fatalf("first event = %#v", ev)
					}
					s.events <- Event{Kind: Message}
					writer := &Conn{c: b, reg: s.reg}
					if err := writer.Send(move{X: 1}); err != nil {
						t.Fatal(err)
					}
					synctest.Wait()
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				close(release)
				awaitTCPDone(t, done)
				synctest.Wait()
				if got := len(s.Conns()); got != 0 {
					t.Errorf("closed server retains %d connections", got)
				}
				if name != "accept error" {
					b.SetReadDeadline(time.Now().Add(time.Second))
					if _, err := b.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
						t.Errorf("accepted socket was not closed: %v", err)
					}
				}
			})
		})
	}
}

func TestTCPAcceptErrorDelivered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := &tcpCloseListener{closed: make(chan struct{}), accept: func() (net.Conn, error) {
			return nil, io.ErrUnexpectedEOF
		}}
		s := newServer(ln, registry())
		defer s.Close()
		synctest.Wait()
		events := s.Poll()
		if len(events) != 1 || events[0].Kind != Disconnected || !errors.Is(events[0].Err, io.ErrUnexpectedEOF) {
			t.Fatalf("accept error events = %#v", events)
		}
	})
}

func awaitTCPDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("network goroutine remained blocked after Close or event delivery")
	}
}

// Drain on failure so the pre-fix regression run also leaves no goroutines.
func finishTCPTest(done <-chan struct{}, events <-chan Event) {
	for {
		select {
		case <-done:
			return
		case <-events:
		}
	}
}
