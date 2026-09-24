package network

// Benchmarks for the per-message cost on the UDP receive path without
// the socket, a 64-client UDP server tick over loopback, TCP send and
// broadcast, delta-encoded 200-entity snapshots through SnapshotBuffer,
// and interest management for 64 viewers.

import (
	"encoding/binary"
	"math"
	"net"
	"net/netip"
	"testing"
	"time"
)

// benchBlob is a binary message of a fixed size, standing in for a
// snapshot payload that fits one datagram.
type benchBlob struct{ B [1000]byte }

func (a benchBlob) MarshalBinary() ([]byte, error) { return append([]byte(nil), a.B[:]...), nil }
func (a *benchBlob) UnmarshalBinary(b []byte) error {
	copy(a.B[:], b)
	return nil
}

// benchInput is a small JSON message, a client's input for a tick.
type benchInput struct {
	Tick    uint32
	MoveX   float32
	MoveY   float32
	Buttons uint16
}

func benchRegistry() *Registry {
	return NewRegistry().Register(hello{}, move{}, pos{}, benchBlob{}, benchInput{})
}

// benchPacket builds an unreliable packet carrying msg, with the given
// sequence number, as a peer would receive it.
func benchPacket(reg *Registry, seq uint32, msg any) []byte {
	payload, err := reg.encode(msg)
	if err != nil {
		panic(err)
	}
	pkt := make([]byte, headerSize+len(payload))
	pkt[0] = flagNeedAck
	binary.BigEndian.PutUint32(pkt[1:], 7)
	binary.BigEndian.PutUint32(pkt[5:], seq)
	copy(pkt[headerSize:], payload)
	return pkt
}

// BenchmarkUDPReceive runs Peer.receive on one packet from a known
// address: header, acknowledgements (the ring scan), decode and the
// event queue, without the socket.
func BenchmarkUDPReceive(b *testing.B) {
	for _, tc := range []struct {
		name string
		msg  any
	}{
		{"json_input", benchInput{Tick: 1, MoveX: 0.5, MoveY: -1, Buttons: 3}},
		{"binary_pos", pos{1, 2}},
		{"binary_1000B", benchBlob{}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			reg := benchRegistry()
			conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				b.Fatal(err)
			}
			defer conn.Close()
			p := &Peer{conn: conn, reg: reg, events: make(chan Event, 1<<16), links: map[linkKey]*link{},
				timeout: defaultTimeout, closed: make(chan struct{})}
			from := netip.MustParseAddrPort("127.0.0.1:9")
			pkts := make([][]byte, 1024)
			for i := range pkts {
				pkts[i] = benchPacket(reg, uint32(i+1), tc.msg)
			}
			now := time.Now()
			seq := uint32(0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				seq++
				pkt := pkts[i%len(pkts)]
				binary.BigEndian.PutUint32(pkt[5:], seq)
				// Acknowledge the recent packets so the ring has live
				// entries, as a busy link does.
				binary.BigEndian.PutUint32(pkt[9:], seq)
				p.receive(pkt, from, now)
				if len(p.events) > 1<<15 {
					b.StopTimer()
					drain(p.events)
					b.StartTimer()
				}
			}
		})
	}
}

// BenchmarkUDPServerTick64 is one server tick over loopback: the server
// sends a 1000-byte binary snapshot to each of 64 client peers, each
// client sends one JSON input back, and the tick ends when every message
// has been polled out. Divide by 128 for the cost per message.
func BenchmarkUDPServerTick64(b *testing.B) {
	const clients = 64
	reg := benchRegistry()
	srv, err := ListenUDP("127.0.0.1:0", reg)
	if err != nil {
		b.Fatal(err)
	}
	defer srv.Close()
	cs := make([]*Peer, clients)
	addrs := make([]*Addr, clients)
	for i := range cs {
		cs[i], err = ListenUDP("127.0.0.1:0", reg)
		if err != nil {
			b.Fatal(err)
		}
		defer cs[i].Close()
		addrs[i] = cs[i].Addr()
		if err := cs[i].Connect(srv.Addr()); err != nil {
			b.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	connected := 0
	for connected < clients && time.Now().Before(deadline) {
		for _, ev := range srv.Poll() {
			if ev.Kind == Connected {
				connected++
			}
		}
		time.Sleep(time.Millisecond)
	}
	for _, c := range cs {
		c.Poll()
	}
	snap := benchBlob{}
	in := benchInput{MoveX: 1}
	lost := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		for _, a := range addrs {
			if err := srv.Send(a, snap); err != nil {
				b.Fatal(err)
			}
		}
		for _, c := range cs {
			in.Tick = uint32(i)
			if err := c.Send(srv.Addr(), in); err != nil {
				b.Fatal(err)
			}
		}
		got := 0
		end := time.Now().Add(200 * time.Millisecond)
		for got < 2*clients && time.Now().Before(end) {
			for _, ev := range srv.Poll() {
				if ev.Kind == Message {
					got++
				}
			}
			for _, c := range cs {
				for _, ev := range c.Poll() {
					if ev.Kind == Message {
						got++
					}
				}
			}
		}
		lost += 2*clients - got
	}
	b.ReportMetric(float64(lost)/float64(b.N), "lost/tick")
}

// BenchmarkTCPSend sends one small JSON message on a TCP connection while
// the other end drains, and reports the sender's cost.
func BenchmarkTCPSend(b *testing.B) {
	reg := benchRegistry()
	srv, err := Listen("127.0.0.1:0", reg)
	if err != nil {
		b.Fatal(err)
	}
	defer srv.Close()
	cl, err := Dial(srv.Addr(), reg, time.Second)
	if err != nil {
		b.Fatal(err)
	}
	defer cl.Close()
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				srv.Poll()
				time.Sleep(100 * time.Microsecond)
			}
		}
	}()
	defer close(stop)
	msg := benchInput{MoveX: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := cl.Send(msg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTCPBroadcast64 broadcasts one 1000-byte binary message to 64
// connections.
func BenchmarkTCPBroadcast64(b *testing.B) {
	const clients = 64
	reg := benchRegistry()
	srv, err := Listen("127.0.0.1:0", reg)
	if err != nil {
		b.Fatal(err)
	}
	defer srv.Close()
	cls := make([]*Client, clients)
	for i := range cls {
		cls[i], err = Dial(srv.Addr(), reg, time.Second)
		if err != nil {
			b.Fatal(err)
		}
		defer cls[i].Close()
	}
	for len(srv.Conns()) < clients {
		time.Sleep(time.Millisecond)
	}
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				for _, c := range cls {
					c.Poll()
				}
				time.Sleep(200 * time.Microsecond)
			}
		}
	}()
	defer close(stop)
	msg := benchBlob{}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if fails := srv.Broadcast(msg); fails != nil {
			b.Fatal(fails)
		}
	}
}

// benchSnap is a 200-entity snapshot in the struct-of-arrays form
// EncodeDelta accepts.
type benchSnap struct {
	Tick  uint32
	ID    [200]uint32
	X, Y  [200]float32
	Angle [200]float32
	HP    [200]uint8
}

// BenchmarkSnapshotEncode200 encodes one client's snapshot per op through
// SnapshotBuffer with 64 clients, a tenth of the entities moving each
// tick, and acknowledgements two ticks behind.
func BenchmarkSnapshotEncode200(b *testing.B) {
	const clients = 64
	var buf SnapshotBuffer[int, benchSnap]
	var s benchSnap
	for i := range s.ID {
		s.ID[i] = uint32(i)
		s.HP[i] = 100
	}
	seq := uint32(0)
	size := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		c := i % clients
		if c == 0 {
			seq++
			s.Tick = seq
			for e := int(seq) % 10; e < len(s.X); e += 10 {
				s.X[e] += 0.1
				s.Y[e] -= 0.05
				s.Angle[e] = float32(math.Mod(float64(s.Angle[e])+0.01, 6.28))
			}
		}
		_, data, err := buf.Encode(c, seq, s)
		if err != nil {
			b.Fatal(err)
		}
		size = len(data)
		if seq > 2 {
			buf.Ack(c, seq-2)
		}
	}
	b.ReportMetric(float64(size), "delta-bytes")
}

// BenchmarkSnapshotDecode200 is the client side of the same stream.
func BenchmarkSnapshotDecode200(b *testing.B) {
	var buf SnapshotBuffer[int, benchSnap]
	var rx SnapshotReceiver[benchSnap]
	var s benchSnap
	type pkt struct {
		base, seq uint32
		data      []byte
	}
	pkts := make([]pkt, 0, 256)
	for seq := uint32(1); seq <= 256; seq++ {
		s.Tick = seq
		for e := int(seq) % 10; e < len(s.X); e += 10 {
			s.X[e] += 0.1
		}
		base, data, err := buf.Encode(0, seq, s)
		if err != nil {
			b.Fatal(err)
		}
		pkts = append(pkts, pkt{base, seq, data})
		buf.Ack(0, seq)
	}
	b.ReportAllocs()
	b.ResetTimer()
	i := 0
	for b.Loop() {
		p := pkts[i%len(pkts)]
		if i%len(pkts) == 0 {
			rx = SnapshotReceiver[benchSnap]{}
		}
		i++
		if _, err := rx.Decode(p.base, p.seq, p.data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkInterest64x200 runs a server frame of interest for 64 viewers
// over 200 entities.
func BenchmarkInterest64x200(b *testing.B) {
	const viewers, entities = 64, 200
	ins := make([]Interest[int], viewers)
	for i := range ins {
		ins[i].Radius = 50
	}
	b.ReportAllocs()
	b.ResetTimer()
	for f := 0; b.Loop(); f++ {
		for v := range ins {
			in := &ins[v]
			in.Begin(float32(v*3), float32(v%8*10))
			for e := range entities {
				in.Visit(e, float32((e*7+f)%200), float32(e%50))
			}
			in.End()
		}
	}
}
