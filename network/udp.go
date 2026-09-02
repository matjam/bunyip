package network

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
)

// Addr identifies a UDP peer.
type Addr = net.UDPAddr

// Peer sends and receives datagrams. Each message carries a sequence
// number so a stale packet that arrives after a newer one from the same
// sender is dropped; nothing is retransmitted, which is what real-time
// state updates want.
type Peer struct {
	conn     *net.UDPConn
	reg      *Registry
	events   chan Event
	mu       sync.Mutex
	seq      uint32
	lastSeen map[string]uint32
	closed   chan struct{}
	once     sync.Once
	activity func()
}

// SetOnActivity runs fn on the receiving goroutine whenever a message is
// queued; point it at Context.Wake in a turn-based game.
func (p *Peer) SetOnActivity(fn func()) {
	p.mu.Lock()
	p.activity = fn
	p.mu.Unlock()
}

// MaxDatagram is the largest message a Peer sends, chosen to fit a
// typical path MTU without fragmentation.
const MaxDatagram = 1200

// ListenUDP binds a peer to addr (":0" for any free port).
func ListenUDP(addr string, reg *Registry) (*Peer, error) {
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	conn, err := net.ListenUDP("udp", ua)
	if err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	p := &Peer{conn: conn, reg: reg, events: make(chan Event, 4096), lastSeen: map[string]uint32{}, closed: make(chan struct{})}
	go p.readLoop()
	return p, nil
}

// Addr is the local address, useful after ":0".
func (p *Peer) Addr() *Addr { return p.conn.LocalAddr().(*net.UDPAddr) }

// Resolve parses "host:port" into an Addr for Send.
func Resolve(addr string) (*Addr, error) {
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	return ua, nil
}

// Send fires one message at to; it may be lost.
func (p *Peer) Send(to *Addr, msg any) error {
	data, err := p.reg.encode(msg)
	if err != nil {
		return err
	}
	if len(data)+4 > MaxDatagram {
		return fmt.Errorf("network: datagram of %d bytes exceeds MaxDatagram", len(data)+4)
	}
	p.mu.Lock()
	p.seq++
	seq := p.seq
	p.mu.Unlock()
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf, seq)
	copy(buf[4:], data)
	_, err = p.conn.WriteToUDP(buf, to)
	return err
}

func (p *Peer) readLoop() {
	buf := make([]byte, 65536)
	for {
		n, from, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-p.closed:
			default:
				p.events <- Event{Kind: Disconnected, Err: err}
			}
			return
		}
		if n < 6 {
			continue
		}
		seq := binary.BigEndian.Uint32(buf[:4])
		key := from.String()
		p.mu.Lock()
		last, seen := p.lastSeen[key]
		stale := seen && int32(seq-last) <= 0
		if !stale {
			p.lastSeen[key] = seq
		}
		activity := p.activity
		p.mu.Unlock()
		if stale {
			continue
		}
		msg, err := p.reg.decode(buf[4:n])
		if err != nil {
			continue // garbage from the internet is not an event
		}
		select {
		case p.events <- Event{Kind: Message, From: from, Msg: msg}:
		default: // the game is not draining; drop rather than block
		}
		if activity != nil {
			activity()
		}
	}
}

// Poll returns the messages received since the last call without blocking.
func (p *Peer) Poll() []Event { return drain(p.events) }

// Close stops receiving.
func (p *Peer) Close() error {
	var err error
	p.once.Do(func() {
		close(p.closed)
		err = p.conn.Close()
	})
	return err
}
