package network

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

func TestTCPClientActivityRegistration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		cl := newClient(a, registry())
		defer cl.Close()
		defer b.Close()
		cl.Poll()
		writer := &Conn{c: b, reg: cl.reg}
		var first, second int
		for _, tc := range []struct {
			name          string
			fn            func()
			first, second int
		}{
			{name: "initial nil"},
			{name: "register", fn: func() { first++ }, first: 1},
			{name: "replace", fn: func() { second++ }, first: 1, second: 1},
			{name: "clear", first: 1, second: 1},
			{name: "register again", fn: func() { second++ }, first: 1, second: 2},
		} {
			cl.SetOnActivity(tc.fn)
			if err := writer.Send(move{X: 7}); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			if first != tc.first || second != tc.second {
				t.Errorf("%s: callbacks = (%d, %d), want (%d, %d)", tc.name, first, second, tc.first, tc.second)
			}
			events := cl.Poll()
			if len(events) != 1 || events[0].Kind != Message {
				t.Fatalf("%s: events = %#v", tc.name, events)
			}
		}
		cl.SetOnActivity(nil)
	})
}

func TestTCPClientActivityConcurrentRegistration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		cl := newClient(a, registry())
		defer cl.Close()
		defer b.Close()
		cl.Poll()
		writer := &Conn{c: b, reg: cl.reg}
		var calls atomic.Int64
		callbacks := []func(){func() { calls.Add(1) }, nil, func() { calls.Add(1) }}
		var setters sync.WaitGroup
		for worker := range 4 {
			setters.Go(func() {
				for i := range 4096 {
					cl.SetOnActivity(callbacks[(worker+i)%len(callbacks)])
				}
			})
		}
		const messages = 512
		for i := range messages {
			if err := writer.Send(move{X: i}); err != nil {
				t.Fatal(err)
			}
		}
		setters.Wait()
		synctest.Wait()
		events := cl.Poll()
		if len(events) != messages {
			t.Fatalf("received %d events, want %d", len(events), messages)
		}
		for i, ev := range events {
			if msg, ok := ev.Msg.(*move); ev.Kind != Message || !ok || msg.X != i {
				t.Fatalf("event %d = %#v", i, ev)
			}
		}
		before := calls.Load()
		cl.SetOnActivity(callbacks[0])
		if err := writer.Send(move{}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if got := calls.Load(); got != before+1 {
			t.Errorf("final registration: calls = %d, want %d", got, before+1)
		}
		cl.SetOnActivity(nil)
	})
}

func TestTCPClientActivityReplacementDuringCallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		cl := newClient(a, registry())
		defer cl.Close()
		defer b.Close()
		cl.Poll() // the following callback should wait for the message
		writer := &Conn{c: b, reg: cl.reg}
		entered, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		defer unblock()
		var first, second int
		cl.SetOnActivity(func() {
			close(entered)
			<-release
			first++
		})
		if err := writer.Send(move{}); err != nil {
			t.Fatal(err)
		}
		<-entered
		registered := make(chan struct{})
		go func() {
			cl.SetOnActivity(func() { second++ })
			close(registered)
		}()
		synctest.Wait()
		select {
		case <-registered:
		default:
			t.Fatal("registration blocked on an executing callback")
		}
		unblock()
		synctest.Wait()
		if first != 1 || second != 1 {
			t.Fatalf("in-flight and pending callbacks = (%d, %d), want (1, 1)", first, second)
		}
		if err := writer.Send(move{}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if first != 1 || second != 2 {
			t.Errorf("replaced callback = (%d, %d), want (1, 2)", first, second)
		}
		cl.SetOnActivity(nil)
	})
}

func TestTCPClientActivityReentry(t *testing.T) {
	for _, name := range []string{"replace", "close"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, b := net.Pipe()
				cl := newClient(a, registry())
				defer cl.Close()
				defer b.Close()
				cl.Poll()
				writer := &Conn{c: b, reg: cl.reg}
				var first, second int
				done := make(chan struct{})
				cl.SetOnActivity(func() {
					first++
					if name == "close" {
						cl.SetOnActivity(nil)
						cl.Close()
					} else {
						cl.Poll()
						cl.SetOnActivity(func() { second++ })
					}
					close(done)
				})
				if err := writer.Send(move{}); err != nil {
					t.Fatal(err)
				}
				awaitTCPDone(t, done)
				if name == "replace" {
					if err := writer.Send(move{}); err != nil {
						t.Fatal(err)
					}
				}
				synctest.Wait()
				if first != 1 || (name == "replace" && second != 1) {
					t.Errorf("callback counts = (%d, %d)", first, second)
				}
				if name == "close" {
					events := cl.Poll()
					if len(events) != 2 || events[0].Kind != Message || events[1].Kind != Disconnected {
						t.Errorf("callback Close events = %#v", events)
					}
				}
				cl.SetOnActivity(nil)
			})
		})
	}
}

func TestTCPServerActivityAppliesToExistingConnections(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		accepted := make(chan net.Conn)
		ln := &tcpCloseListener{closed: make(chan struct{})}
		ln.accept = func() (net.Conn, error) {
			select {
			case c := <-accepted:
				return c, nil
			case <-ln.closed:
				return nil, net.ErrClosed
			}
		}
		s := newServer(ln, registry())
		defer s.Close()
		var first, second int
		callbacks := []func(){func() { first++ }, func() { second++ }, nil}
		var writers []*Conn
		for _, fn := range callbacks {
			s.SetOnActivity(fn)
			a, b := net.Pipe()
			defer b.Close()
			accepted <- a
			synctest.Wait()
			events := s.Poll()
			if len(events) != 1 || events[0].Kind != Connected {
				t.Fatalf("accept events = %#v", events)
			}
			writers = append(writers, &Conn{c: b, reg: s.reg})
		}
		if first != 1 || second != 1 {
			t.Fatalf("connect callbacks = (%d, %d), want (1, 1)", first, second)
		}
		for _, writer := range writers {
			if err := writer.Send(move{}); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if first != 1 || second != 1 {
			t.Errorf("removed callbacks = (%d, %d), want (1, 1)", first, second)
		}
		s.SetOnActivity(func() { first++; s.Poll(); s.SetOnActivity(nil) })
		if first != 2 {
			t.Fatalf("pending callback = %d, want 2", first)
		}
		s.SetOnActivity(func() { second++ })
		for _, writer := range writers {
			if err := writer.Send(move{}); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if second != 4 {
			t.Fatalf("existing callbacks = %d, want 4", second)
		}
		s.SetOnActivity(nil)
	})
}
