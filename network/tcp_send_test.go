package network

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type sendTestTransport struct {
	net.Conn
	write     func([]byte) (int, error)
	deadline  func(time.Time) error
	mu        sync.Mutex
	deadlines []time.Time
	closes    int
}

func (c *sendTestTransport) Write(p []byte) (int, error) { return c.write(p) }
func (c *sendTestTransport) SetWriteDeadline(d time.Time) error {
	c.mu.Lock()
	c.deadlines = append(c.deadlines, d)
	c.mu.Unlock()
	if c.deadline != nil {
		return c.deadline(d)
	}
	return nil
}
func (c *sendTestTransport) Close() error {
	c.mu.Lock()
	c.closes++
	c.mu.Unlock()
	return nil
}

func sendTestConn(nc net.Conn) *Conn {
	return &Conn{c: nc, reg: registry(), closed: make(chan struct{})}
}

func sendResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("send did not finish")
		return nil
	}
}

func TestSendCompletesShortWrites(t *testing.T) {
	var wire bytes.Buffer
	nc := &sendTestTransport{write: func(p []byte) (int, error) {
		return wire.Write(p[:min(2, len(p))])
	}}
	c := sendTestConn(nc)
	if err := c.Send(move{X: 31, Y: 7}); err != nil {
		t.Fatal(err)
	}
	b := wire.Bytes()
	if len(b) < 4 || int(binary.BigEndian.Uint32(b)) != len(b)-4 {
		t.Fatalf("bad frame: %x", b)
	}
	m, err := c.reg.decode(b[4:])
	if err != nil || *m.(*move) != (move{31, 7}) {
		t.Fatal(m, err)
	}
	if len(nc.deadlines) != 2 || nc.deadlines[0].IsZero() || !nc.deadlines[1].IsZero() {
		t.Fatal("default deadline not installed and cleared", nc.deadlines)
	}
	if remaining := time.Until(nc.deadlines[0]); remaining <= 0 || remaining > DefaultSendTimeout {
		t.Fatal(remaining)
	}
}

func TestSendWriteFailuresCloseConnection(t *testing.T) {
	broken := errors.New("broken transport")
	for _, tc := range []struct {
		name        string
		failCall, n int
		err         error
	}{
		{"zero", 1, 0, nil}, {"partial_header", 1, 2, broken},
		{"partial_payload", 2, 1, broken}, {"negative", 1, -1, nil},
		{"oversized", 1, 100, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			nc := &sendTestTransport{write: func(p []byte) (int, error) {
				calls++
				if calls == tc.failCall {
					return tc.n, tc.err
				}
				return len(p), nil
			}}
			c := sendTestConn(nc)
			err := c.Send(move{})
			if err == nil || tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatal(err)
			}
			if tc.name == "zero" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal(err)
			}
			if nc.closes != 1 {
				t.Fatal("failed write left connection open", nc.closes)
			}
			if err := c.Send(move{}); !errors.Is(err, net.ErrClosed) {
				t.Fatal(err)
			}
			if calls != tc.failCall {
				t.Fatal("sent data after corrupting frame", calls)
			}
		})
	}
}

type notifyWriteConn struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *notifyWriteConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Write(p)
}

func TestSendContextInterruptsStalledPeer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, deadline := range []bool{false, true} {
			a, b := net.Pipe()
			nc := &notifyWriteConn{Conn: a, entered: make(chan struct{})}
			c := sendTestConn(nc)
			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
				want = context.DeadlineExceeded
			}
			result := make(chan error, 1)
			go func() { result <- c.SendContext(ctx, move{}) }()
			<-nc.entered
			if !deadline {
				cancel()
			}
			if err := sendResult(t, result); !errors.Is(err, want) {
				t.Fatal(err)
			}
			cancel()
			select {
			case <-c.closed:
			default:
				t.Fatal("interrupted active write remained open")
			}
			b.Close()
		}
	})
}

func TestSendContextBoundsPendingTLSHandshake(t *testing.T) {
	serverConfig, _, err := SelfSignedConfig()
	if err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		transport := &notifyReadConn{Conn: a, entered: make(chan struct{})}
		nc := tls.Server(transport, serverConfig)
		handshake := &notifyHandshakeConn{Conn: nc, entered: make(chan struct{})}
		c := sendTestConn(handshake)
		defer c.Close()
		// The normal reader can already own the TLS handshake lock when Send starts.
		readDone := make(chan error, 1)
		go func() { var p [1]byte; _, err := nc.Read(p[:]); readDone <- err }()
		<-transport.entered
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- c.SendContext(ctx, move{}) }()
		<-handshake.entered
		cancel()
		if err := sendResult(t, result); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := sendResult(t, readDone); err == nil {
			t.Fatal("TLS reader unexpectedly completed")
		}
	})
}

type notifyHandshakeConn struct {
	*tls.Conn
	entered chan struct{}
}

func (c *notifyHandshakeConn) HandshakeContext(ctx context.Context) error {
	close(c.entered)
	return c.Conn.HandshakeContext(ctx)
}

type notifyReadConn struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *notifyReadConn) Read(p []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Read(p)
}

func TestSendCanceledBeforeFirstWritePreservesConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := 0
	var once sync.Once
	nc := &sendTestTransport{
		deadline: func(time.Time) error { once.Do(cancel); return nil },
		write:    func(p []byte) (int, error) { writes++; return len(p), nil },
	}
	c := sendTestConn(nc)
	if err := c.SendContext(ctx, move{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if writes != 0 || nc.closes != 0 {
		t.Fatal(writes, nc.closes)
	}
	if err := c.Send(move{}); err != nil {
		t.Fatal(err)
	}
}

func TestSendContextCanceledWaiterDoesNotAffectActiveSend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		nc := &notifyWriteConn{Conn: a, entered: make(chan struct{})}
		c := sendTestConn(nc)
		first := make(chan error, 1)
		go func() { first <- c.SendContext(context.Background(), move{X: 9}) }()
		<-nc.entered
		ctx, cancel := context.WithCancel(context.Background())
		waiter := make(chan error, 1)
		go func() { waiter <- c.SendContext(ctx, move{X: 99}) }()
		synctest.Wait()
		cancel()
		if err := sendResult(t, waiter); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		select {
		case <-c.closed:
			t.Fatal("waiting cancellation closed active connection")
		default:
		}
		readFrame := func() *move {
			t.Helper()
			var header [4]byte
			if _, err := io.ReadFull(b, header[:]); err != nil {
				t.Fatal(err)
			}
			data := make([]byte, binary.BigEndian.Uint32(header[:]))
			if _, err := io.ReadFull(b, data); err != nil {
				t.Fatal(err)
			}
			m, err := c.reg.decode(data)
			if err != nil {
				t.Fatal(err)
			}
			return m.(*move)
		}
		if m := readFrame(); m.X != 9 {
			t.Fatal(m)
		}
		if err := sendResult(t, first); err != nil {
			t.Fatal(err)
		}
		go func() { first <- c.Send(move{X: 10}) }()
		if m := readFrame(); m.X != 10 {
			t.Fatal(m)
		}
		if err := sendResult(t, first); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSendJoinsCancellationBeforeClearingDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	writes := 0
	nc := &sendTestTransport{
		write: func(p []byte) (int, error) {
			writes++
			if writes == 2 {
				cancel()
				<-entered
			}
			return len(p), nil
		},
		deadline: func(d time.Time) error {
			if !d.IsZero() {
				close(entered)
				<-release
			}
			return nil
		},
	}
	c := sendTestConn(nc)
	result := make(chan error, 1)
	go func() { result <- c.SendContext(ctx, move{}) }()
	<-entered
	select {
	case err := <-result:
		t.Fatal("send did not join deadline callback", err)
	default:
	}
	close(release)
	if err := sendResult(t, result); err != nil {
		t.Fatal("complete frame lost to late cancellation", err)
	}
	if err := c.SendContext(context.Background(), move{}); err != nil {
		t.Fatal(err)
	}
	if d := nc.deadlines[len(nc.deadlines)-1]; !d.IsZero() {
		t.Fatal("deadline left behind", d)
	}
}

func TestSendCloseUnblocksWriterAndWaiter(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	nc := &notifyWriteConn{Conn: a, entered: make(chan struct{})}
	c := sendTestConn(nc)
	results := make(chan error, 2)
	go func() { results <- c.SendContext(context.Background(), move{}) }()
	<-nc.entered
	go func() { results <- c.SendContext(context.Background(), move{}) }()
	c.Close()
	for range 2 {
		if err := sendResult(t, results); err == nil {
			t.Fatal("closed send succeeded")
		}
	}
}

func TestBroadcastReturnsFailuresAndSharesBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broken := errors.New("peer failed")
		good := sendTestConn(&sendTestTransport{write: func(p []byte) (int, error) { return len(p), nil }})
		bad := sendTestConn(&sendTestTransport{write: func([]byte) (int, error) { return 0, broken }})
		skip := sendTestConn(&sendTestTransport{write: func([]byte) (int, error) { t.Error("excluded peer used"); return 0, broken }})
		s := &Server{conns: map[int]*Conn{1: good, 2: bad, 3: skip}}
		failed := s.Broadcast(move{}, skip)
		if len(failed) != 1 || !errors.Is(failed[bad], broken) {
			t.Fatal(failed)
		}
		s.conns = map[int]*Conn{1: good}
		if failed := s.Broadcast(move{}); failed != nil {
			t.Fatal(failed)
		}
		var peers []net.Conn
		s.conns = map[int]*Conn{}
		for i := range 3 {
			a, b := net.Pipe()
			peers = append(peers, b)
			c := sendTestConn(a)
			defer c.Close()
			s.conns[i] = c
		}
		defer func() {
			for _, p := range peers {
				p.Close()
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		failed = s.BroadcastContext(ctx, move{})
		if len(failed) != 3 {
			t.Fatal(failed)
		}
		closed := 0
		for c, err := range failed {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			select {
			case <-c.closed:
				closed++
			default:
			}
		}
		if closed != 1 {
			t.Fatal("shared cancellation should reach only one active peer", closed)
		}
	})
}
