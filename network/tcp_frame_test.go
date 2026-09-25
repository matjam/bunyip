package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// countedBlob is a binary message that counts how often it is encoded
// and keeps a copy of what it decodes, as encoding.BinaryUnmarshaler
// requires.
type countedBlob struct {
	B []byte
}

var blobEncodes atomic.Int64

func (m countedBlob) MarshalBinary() ([]byte, error) {
	blobEncodes.Add(1)
	return append([]byte(nil), m.B...), nil
}

func (m *countedBlob) UnmarshalBinary(b []byte) error {
	m.B = append([]byte(nil), b...)
	return nil
}

// TestSendWritesOneFrame checks that the header and payload of a
// message reach the transport in a single write.
func TestSendWritesOneFrame(t *testing.T) {
	var writes [][]byte
	nc := &sendTestTransport{write: func(p []byte) (int, error) {
		writes = append(writes, append([]byte(nil), p...))
		return len(p), nil
	}}
	c := sendTestConn(nc)
	if err := c.Send(move{X: 1, Y: 2}); err != nil {
		t.Fatal(err)
	}
	if err := c.SendContext(context.Background(), move{X: 3}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.SendContext(ctx, move{X: 4}); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 3 {
		t.Fatalf("three messages took %d writes", len(writes))
	}
	for i, w := range writes {
		if len(w) < 4 || int(binary.BigEndian.Uint32(w)) != len(w)-4 {
			t.Fatalf("write %d is not one whole frame: %x", i, w)
		}
		if _, err := c.reg.decode(w[4:]); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
}

// TestBroadcastEncodesOnce checks that a broadcast encodes its message
// once for every peer and sends each the same frame.
func TestBroadcastEncodesOnce(t *testing.T) {
	reg := NewRegistry().Register(move{}, countedBlob{})
	var wires [3]bytes.Buffer
	s := &Server{conns: map[int]*Conn{}}
	for i := range wires {
		w := &wires[i]
		s.conns[i] = &Conn{c: &sendTestTransport{write: w.Write}, reg: reg, closed: make(chan struct{})}
	}
	blobEncodes.Store(0)
	msg := countedBlob{B: bytes.Repeat([]byte("abc"), 100)}
	if failed := s.Broadcast(msg); failed != nil {
		t.Fatal(failed)
	}
	if n := blobEncodes.Load(); n != 1 {
		t.Errorf("broadcast to 3 peers encoded %d times", n)
	}
	for i := range wires {
		if !bytes.Equal(wires[i].Bytes(), wires[0].Bytes()) || wires[i].Len() != 4+2+300 {
			t.Fatalf("peer %d got %x", i, wires[i].Bytes())
		}
	}
	// An unencodable message fails every selected peer.
	type unknown struct{}
	failed := s.Broadcast(unknown{}, s.conns[1])
	if len(failed) != 2 || failed[s.conns[1]] != nil {
		t.Fatal(failed)
	}
	for _, err := range failed {
		if !errors.Is(err, ErrUnknownMessage) {
			t.Fatal(err)
		}
	}
}

// TestSendDefaultTimeout checks the bare deadline Send uses: a stalled
// peer's write and a sender waiting behind it both end after
// DefaultSendTimeout with context.DeadlineExceeded, and only the stalled
// write closes the connection.
func TestSendDefaultTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := net.Pipe()
		defer b.Close()
		nc := &notifyWriteConn{Conn: a, entered: make(chan struct{})}
		c := sendTestConn(nc)
		start := time.Now()
		first := make(chan error, 1)
		go func() { first <- c.Send(move{X: 1}) }()
		<-nc.entered
		time.Sleep(DefaultSendTimeout / 2)
		second := make(chan error, 1)
		go func() { second <- c.Send(move{X: 2}) }()
		if err := <-first; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("stalled write:", err)
		}
		if elapsed := time.Since(start); elapsed != DefaultSendTimeout {
			t.Errorf("stalled write ended after %v", elapsed)
		}
		if err := <-second; err == nil {
			t.Fatal("waiting send succeeded on a closed connection")
		}
		select {
		case <-c.closed:
		default:
			t.Fatal("interrupted write left the connection open")
		}
	})
	synctest.Test(t, func(t *testing.T) {
		// A waiter whose budget runs out before the writer is free gives up
		// without touching the connection.
		a, b := net.Pipe()
		defer b.Close()
		defer a.Close()
		c := sendTestConn(a)
		c.sendInit.Do(func() { c.sendGate = make(chan struct{}, 1) })
		c.sendGate <- struct{}{} // another sender holds the writer
		start := time.Now()
		if err := c.Send(move{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed != DefaultSendTimeout {
			t.Errorf("waiting send gave up after %v", elapsed)
		}
		select {
		case <-c.closed:
			t.Fatal("a waiter's timeout closed the connection")
		default:
		}
	})
}

// TestReadLoopReusesBuffer sends frames of many sizes, some larger than
// the reused buffer, and checks every message arrives intact after
// later frames have been read into the same storage.
func TestReadLoopReusesBuffer(t *testing.T) {
	reg := NewRegistry().Register(move{}, countedBlob{})
	srv, err := Listen("127.0.0.1:0", reg)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cl, err := Dial(srv.Addr(), reg, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	sizes := []int{0, 1, 5, 1000, 70 << 10, 3, 20 << 10, 200 << 10, 7}
	var sent []any
	for i, n := range sizes {
		b := make([]byte, n)
		for j := range b {
			b[j] = byte(i*31 + j)
		}
		sent = append(sent, countedBlob{B: b}, move{X: i, Y: n})
	}
	for _, m := range sent {
		if err := cl.Send(m); err != nil {
			t.Fatal(err)
		}
	}
	var got []any
	deadline := time.Now().Add(5 * time.Second)
	for len(got) < len(sent) && time.Now().Before(deadline) {
		for _, ev := range srv.Poll() {
			if ev.Kind == Message {
				got = append(got, ev.Msg)
			}
		}
		time.Sleep(time.Millisecond)
	}
	if len(got) != len(sent) {
		t.Fatalf("received %d of %d messages", len(got), len(sent))
	}
	for i := range sent {
		switch want := sent[i].(type) {
		case countedBlob:
			if g := got[i].(*countedBlob); !bytes.Equal(g.B, want.B) {
				t.Fatalf("message %d: blob of %d bytes differs", i, len(want.B))
			}
		case move:
			if g := got[i].(*move); *g != want {
				t.Fatalf("message %d: %+v, want %+v", i, *g, want)
			}
		}
	}
}
