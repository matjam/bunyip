package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"sync"
	"time"
)

// Addr identifies a UDP peer.
type Addr = net.UDPAddr

// Peer sends and receives datagrams. Send fires a message that may be
// lost or arrive out of order (a stale packet that arrives after a
// newer one from the same sender is dropped), which is what real-time
// state updates want. SendReliable retransmits a message until the
// other side acknowledges it and delivers it in order with the other
// reliable messages from that sender, for anything that must arrive.
// Both kinds share one packet header, so every packet acknowledges what
// has been received so far.
//
// A Peer also tracks who it is talking to. The first packet from an
// address is a Connected event, silence for the timeout is a
// Disconnected one (see SetTimeout), a peer that closes says goodbye
// and one that restarts shows up as Disconnected then Connected again.
// Peers lists the addresses in between; keepalives go out on idle
// links so a quiet game stays connected.
type Peer struct {
	conn     *net.UDPConn
	reg      *Registry
	events   chan Event
	mu       sync.Mutex // guards the fields below
	links    map[linkKey]*link
	timeout  time.Duration
	dropRate float64
	activity func()
	txBuf    []byte     // the packet being written, reused between sends
	emitMu   sync.Mutex // keeps the read and tick goroutines' events in order
	evs      []Event    // the events one receive or tick produced, reused
	closed   chan struct{}
	once     sync.Once
}

// linkKey names a remote address without the string net.UDPAddr.String
// would allocate for every packet. IPv4 addresses are held in their
// v4-in-v6 form so that the two spellings of one address agree.
type linkKey struct {
	ip   [16]byte
	port uint16
	zone string
}

func keyOf(a *Addr) linkKey {
	var k linkKey
	if ip4 := a.IP.To4(); ip4 != nil {
		k.ip[10], k.ip[11] = 0xff, 0xff
		copy(k.ip[12:], ip4)
	} else {
		copy(k.ip[:], a.IP)
	}
	k.port = uint16(a.Port)
	k.zone = a.Zone
	return k
}

// keyOfPort is keyOf for the address form the read loop receives. As16
// gives an IPv4 address its v4-in-v6 form, which is what keyOf stores.
func keyOfPort(ap netip.AddrPort) linkKey {
	return linkKey{ip: ap.Addr().As16(), port: ap.Port(), zone: ap.Addr().Zone()}
}

// Stats describes one UDP link.
type Stats struct {
	RTT       time.Duration // smoothed round trip; zero before the first acknowledgement
	Loss      float32       // fraction of recent packets that went unacknowledged, 0 to 1
	Pending   int           // reliable messages sent but not yet acknowledged
	Connected bool          // whether a packet has arrived from the address
}

// ErrTimeout reports a UDP peer that went silent.
var ErrTimeout = errors.New("network: peer timed out")

// ErrReset reports a UDP peer that restarted; reliable messages it had
// not acknowledged are lost.
var ErrReset = errors.New("network: peer restarted")

// Packet header: flags, the sender's session, the packet sequence, then
// the newest sequence received from the other side and a bitfield of
// the 32 before it. A reliable packet adds its message number.
const (
	flagNeedAck  = 1 << iota // the receiver should acknowledge this packet
	flagReliable             // the payload starts with a reliable message number
	flagBye                  // the sender is going away
)

const (
	headerSize     = 17
	ringSize       = 512 // packets remembered per link for acknowledgement
	ackEvery       = 8   // packets received before an acknowledgement goes out at once
	tickInterval   = 10 * time.Millisecond
	minRTO         = 50 * time.Millisecond
	maxRTO         = time.Second
	defaultTimeout = 5 * time.Second
)

// link is the state kept for one remote address.
type link struct {
	addr      *Addr
	session   uint32 // ours, so the other side can tell a restart from a pause
	remote    uint32 // the other side's session, 0 until its first packet
	connected bool
	created   time.Time
	lastRecv  time.Time
	lastSent  time.Time
	outSeq    uint32
	ring      [ringSize]sentPacket // packets awaiting acknowledgement, by seq
	inLatest  uint32               // newest packet sequence received
	inBits    uint32               // which of the 32 before inLatest arrived
	needAck   bool
	unacked   int // packets received since we last sent anything
	nextRid   uint32
	pending   map[uint32]*pendingMsg // reliable messages not yet acknowledged
	expectRid uint32                 // next reliable message to deliver
	held      map[uint32][]byte      // reliable messages waiting for a gap to fill
	srtt      time.Duration
	loss      float32
}

type sentPacket struct {
	seq  uint32
	rid  uint32 // reliable message carried, 0 for none
	at   time.Time
	live bool
}

type pendingMsg struct {
	data  []byte
	sends int
	due   time.Time
}

func newLink(addr *Addr, now time.Time) *link {
	a := *addr
	l := &link{addr: &a}
	for l.session == 0 {
		l.session = rand.Uint32()
	}
	l.reset(now)
	return l
}

// reset forgets everything but the address and our session.
func (l *link) reset(now time.Time) {
	*l = link{addr: l.addr, session: l.session, created: now, nextRid: 1, expectRid: 1,
		pending: map[uint32]*pendingMsg{}, held: map[uint32][]byte{}}
}

// rto is how long to wait for an acknowledgement before resending.
func (l *link) rto() time.Duration {
	return min(max(2*l.srtt, minRTO), maxRTO)
}

func (l *link) sampleLoss(lost float32) { l.loss += (lost - l.loss) * 0.05 }

// noteReceived records a packet sequence and reports whether it was
// already seen.
func (l *link) noteReceived(seq uint32) (dup bool) {
	if l.inLatest == 0 {
		l.inLatest = seq
		return false
	}
	d := int32(seq - l.inLatest)
	switch {
	case d > 0:
		if d >= 32 {
			l.inBits = 0
		} else {
			l.inBits = l.inBits<<d | 1<<(d-1)
		}
		l.inLatest = seq
	case d == 0:
		return true
	default:
		back := uint32(-d)
		if back > 32 {
			return false // too old to remember; reliable numbering catches repeats
		}
		bit := uint32(1) << (back - 1)
		if l.inBits&bit != 0 {
			return true
		}
		l.inBits |= bit
	}
	return false
}

// processAcks applies the other side's acknowledgement to the packets
// we remember, measuring the round trip and counting losses.
func (l *link) processAcks(ack, bits uint32, now time.Time) {
	if ack == 0 {
		return
	}
	l.ackOne(ack, now)
	for i := range uint32(32) {
		if bits&(1<<i) != 0 {
			l.ackOne(ack-1-i, now)
		}
	}
	for i := range l.ring {
		e := &l.ring[i]
		if e.live && int32(ack-e.seq) > 32 {
			e.live = false // fell out of the window unacknowledged
			l.sampleLoss(1)
		}
	}
}

func (l *link) ackOne(seq uint32, now time.Time) {
	if seq == 0 {
		return
	}
	e := &l.ring[seq%ringSize]
	if !e.live || e.seq != seq {
		return
	}
	e.live = false
	rtt := now.Sub(e.at)
	if l.srtt == 0 {
		l.srtt = rtt
	} else {
		l.srtt += (rtt - l.srtt) / 8
	}
	l.sampleLoss(0)
	if e.rid != 0 {
		delete(l.pending, e.rid)
	}
}

// receiveReliable takes one reliable message and returns those now
// deliverable in order, if any.
func (l *link) receiveReliable(rid uint32, data []byte) [][]byte {
	if int32(rid-l.expectRid) < 0 {
		return nil // delivered already
	}
	if rid != l.expectRid {
		if _, ok := l.held[rid]; !ok {
			l.held[rid] = bytes.Clone(data)
		}
		return nil
	}
	out := [][]byte{data}
	l.expectRid++
	for {
		d, ok := l.held[l.expectRid]
		if !ok {
			return out
		}
		delete(l.held, l.expectRid)
		out = append(out, d)
		l.expectRid++
	}
}

// SetOnActivity runs fn on a network goroutine whenever an event is
// queued; point it at Context.Wake in a turn-based game.
func (p *Peer) SetOnActivity(fn func()) {
	p.mu.Lock()
	p.activity = fn
	p.mu.Unlock()
}

// SetTimeout sets how long an address may be silent before it is
// reported Disconnected; keepalives go out at a quarter of it on idle
// links. Zero restores the default of five seconds.
func (p *Peer) SetTimeout(d time.Duration) {
	if d <= 0 {
		d = defaultTimeout
	}
	p.mu.Lock()
	p.timeout = d
	p.mu.Unlock()
}

// SetLoss drops a fraction (0 to 1) of outgoing packets at random, for
// testing a game against a bad link. Zero, the default, sends everything.
func (p *Peer) SetLoss(rate float64) {
	p.mu.Lock()
	p.dropRate = rate
	p.mu.Unlock()
}

// MaxDatagram is the largest packet a Peer sends, chosen to fit a
// typical path MTU without fragmentation. The packet header takes 17
// bytes of it, plus 4 for a reliable message.
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
	p := &Peer{conn: conn, reg: reg, events: make(chan Event, 4096), links: map[linkKey]*link{},
		timeout: defaultTimeout, closed: make(chan struct{})}
	go p.readLoop()
	go p.tick()
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

// link finds or creates the state for an address. Call with mu held.
func (p *Peer) link(to *Addr, now time.Time) *link {
	key := keyOf(to)
	l := p.links[key]
	if l == nil {
		l = newLink(to, now)
		p.links[key] = l
	}
	return l
}

// Connect says hello to an address so both sides see Connected before
// any message is sent; Send does the same on first use.
func (p *Peer) Connect(to *Addr) error {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.transmit(p.link(to, now), flagNeedAck, 0, nil, now)
}

// Disconnect says goodbye to an address and forgets it; the other side
// sees Disconnected with no error. Unacknowledged reliable messages are
// dropped.
func (p *Peer) Disconnect(to *Addr) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := keyOf(to)
	if l := p.links[key]; l != nil {
		p.transmit(l, flagBye, 0, nil, time.Now())
		delete(p.links, key)
	}
}

// Send fires one message at to; it may be lost.
func (p *Peer) Send(to *Addr, msg any) error { return p.send(to, msg, false) }

// SendReliable sends one message that is resent until acknowledged and
// delivered in order with the sender's other reliable messages. It
// returns once the message is queued; Stats reports what is pending.
func (p *Peer) SendReliable(to *Addr, msg any) error { return p.send(to, msg, true) }

func (p *Peer) send(to *Addr, msg any, reliable bool) error {
	if reliable {
		// A reliable message is kept until it is acknowledged, so it
		// needs a buffer of its own.
		data, err := p.reg.encode(msg)
		if err != nil {
			return err
		}
		if size := headerSize + 4 + len(data); size > MaxDatagram {
			return fmt.Errorf("network: datagram of %d bytes exceeds MaxDatagram", size)
		}
		now := time.Now()
		p.mu.Lock()
		defer p.mu.Unlock()
		l := p.link(to, now)
		rid := l.nextRid
		l.nextRid++
		l.pending[rid] = &pendingMsg{data: data}
		return p.transmit(l, flagNeedAck|flagReliable, rid, data, now)
	}
	// An unreliable packet goes out before send returns and nothing
	// keeps its payload, so it encodes into a borrowed buffer that
	// transmit copies out of.
	bp := sendBufs.Get().(*[]byte)
	defer sendBufs.Put(bp)
	data, err := p.reg.appendEncoded((*bp)[:0], msg)
	*bp = data
	if err != nil {
		return err
	}
	if size := headerSize + len(data); size > MaxDatagram {
		return fmt.Errorf("network: datagram of %d bytes exceeds MaxDatagram", size)
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.transmit(p.link(to, now), flagNeedAck, 0, data, now)
}

// sendBufs lends payload buffers to unreliable sends.
var sendBufs = sync.Pool{New: func() any {
	b := make([]byte, 0, 256)
	return &b
}}

// transmit writes one packet to a link. Call with mu held.
func (p *Peer) transmit(l *link, flags uint8, rid uint32, payload []byte, now time.Time) error {
	l.outSeq++
	seq := l.outSeq
	n := headerSize
	if flags&flagReliable != 0 {
		n += 4
	}
	// WriteToUDP hands the bytes to the kernel before it returns, so
	// one buffer serves every packet this peer sends.
	total := n + len(payload)
	if cap(p.txBuf) < total {
		p.txBuf = make([]byte, total)
	}
	buf := p.txBuf[:total]
	buf[0] = flags
	binary.BigEndian.PutUint32(buf[1:], l.session)
	binary.BigEndian.PutUint32(buf[5:], seq)
	binary.BigEndian.PutUint32(buf[9:], l.inLatest)
	binary.BigEndian.PutUint32(buf[13:], l.inBits)
	if flags&flagReliable != 0 {
		binary.BigEndian.PutUint32(buf[17:], rid)
		if pm := l.pending[rid]; pm != nil {
			pm.sends++
			pm.due = now.Add(min(l.rto()<<min(pm.sends-1, 4), maxRTO))
		}
	}
	copy(buf[n:], payload)
	if flags&flagNeedAck != 0 {
		e := &l.ring[seq%ringSize]
		if e.live {
			l.sampleLoss(1) // never acknowledged before its slot came round
		}
		*e = sentPacket{seq: seq, rid: rid, at: now, live: true}
	}
	l.lastSent = now
	l.needAck = false
	l.unacked = 0
	if p.dropRate > 0 && rand.Float64() < p.dropRate {
		return nil
	}
	_, err := p.conn.WriteToUDP(buf, l.addr)
	return err
}

func (p *Peer) readLoop() {
	buf := make([]byte, 65536)
	for {
		// ReadFromUDPAddrPort reports the sender as a value, where
		// ReadFromUDP allocates a *net.UDPAddr for every packet.
		n, from, err := p.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			select {
			case <-p.closed:
			default:
				p.emit(Event{Kind: Disconnected, Err: err}, true)
			}
			return
		}
		if n < headerSize {
			continue
		}
		p.receive(buf[:n], from, time.Now())
	}
}

// receive handles one packet: link bookkeeping, acknowledgements, then
// the payload. The sender is a value, so a packet from a known address
// allocates nothing to identify it.
func (p *Peer) receive(pkt []byte, from netip.AddrPort, now time.Time) {
	flags := pkt[0]
	session := binary.BigEndian.Uint32(pkt[1:])
	seq := binary.BigEndian.Uint32(pkt[5:])
	ack := binary.BigEndian.Uint32(pkt[9:])
	bits := binary.BigEndian.Uint32(pkt[13:])
	payload := pkt[headerSize:]
	key := keyOfPort(from)

	p.emitMu.Lock()
	defer p.emitMu.Unlock()
	p.mu.Lock()
	evs := p.evs[:0]
	l := p.links[key]
	if l == nil {
		if flags&flagBye != 0 {
			p.mu.Unlock()
			return
		}
		l = newLink(net.UDPAddrFromAddrPort(from), now)
		p.links[key] = l
	}
	if l.remote != 0 && l.remote != session {
		if flags&flagBye != 0 {
			p.mu.Unlock()
			return // a goodbye from a previous run
		}
		if l.connected {
			evs = append(evs, Event{Kind: Disconnected, From: l.addr, Err: ErrReset})
		}
		l.reset(now)
	}
	l.remote = session
	l.lastRecv = now
	if !l.connected {
		l.connected = true
		evs = append(evs, Event{Kind: Connected, From: l.addr})
	}
	if flags&flagBye != 0 {
		delete(p.links, key)
		evs = append(evs, Event{Kind: Disconnected, From: l.addr})
	} else {
		dup := l.noteReceived(seq)
		l.processAcks(ack, bits, now)
		if flags&flagNeedAck != 0 {
			l.needAck = true
			l.unacked++
		}
		switch {
		case dup || len(payload) == 0:
		case flags&flagReliable != 0:
			if len(payload) >= 4 {
				rid := binary.BigEndian.Uint32(payload)
				for _, data := range l.receiveReliable(rid, payload[4:]) {
					if msg, err := p.reg.decode(data); err == nil {
						evs = append(evs, Event{Kind: Message, From: l.addr, Msg: msg})
					}
				}
			}
		case seq == l.inLatest: // the newest packet; older unreliable data is stale
			if msg, err := p.reg.decode(payload); err == nil {
				evs = append(evs, Event{Kind: Message, From: l.addr, Msg: msg})
			}
		}
		// A burst must not outrun the 32-packet acknowledgement window:
		// answer at once every few packets, and let tick cover the tail.
		if l.unacked >= ackEvery {
			p.transmit(l, 0, 0, nil, now)
		}
	}
	activity := p.activity
	p.evs = evs // keep the grown slice for the next packet
	p.mu.Unlock()
	for _, ev := range evs {
		p.emit(ev, ev.Kind != Message || flags&flagReliable != 0)
	}
	if len(evs) > 0 && activity != nil {
		activity()
	}
}

// emit queues an event. Reliable messages and connection events wait
// for room (a game that is not polling gets backpressure); unreliable
// messages are dropped when the queue is full.
func (p *Peer) emit(ev Event, block bool) {
	if !block {
		select {
		case p.events <- ev:
		default:
		}
		return
	}
	select {
	case p.events <- ev:
	case <-p.closed:
	}
}

// tick resends, acknowledges, keeps links alive and times them out.
func (p *Peer) tick() {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-p.closed:
			return
		case now := <-t.C:
			p.maintain(now)
		}
	}
}

func (p *Peer) maintain(now time.Time) {
	p.emitMu.Lock()
	defer p.emitMu.Unlock()
	p.mu.Lock()
	evs := p.evs[:0]
	for key, l := range p.links {
		since := l.lastRecv
		if since.IsZero() {
			since = l.created
		}
		if now.Sub(since) > p.timeout {
			delete(p.links, key)
			if l.connected {
				// A peer that never answered was never connected, so it
				// does not disconnect either.
				evs = append(evs, Event{Kind: Disconnected, From: l.addr, Err: ErrTimeout})
			}
			continue
		}
		for rid, pm := range l.pending {
			if !now.Before(pm.due) {
				p.transmit(l, flagNeedAck|flagReliable, rid, pm.data, now)
			}
		}
		switch {
		case l.needAck:
			p.transmit(l, 0, 0, nil, now)
		case now.Sub(l.lastSent) >= p.timeout/4:
			p.transmit(l, flagNeedAck, 0, nil, now)
		}
	}
	activity := p.activity
	p.evs = evs // keep the grown slice for the next tick
	p.mu.Unlock()
	for _, ev := range evs {
		p.emit(ev, true)
	}
	if len(evs) > 0 && activity != nil {
		activity()
	}
}

// Poll returns the events received since the last call without
// blocking: Connected and Disconnected with From set, and Message.
func (p *Peer) Poll() []Event { return drain(p.events) }

// Peers lists the addresses that have sent something and not gone away.
func (p *Peer) Peers() []*Addr {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*Addr, 0, len(p.links))
	for _, l := range p.links {
		if l.connected {
			out = append(out, l.addr)
		}
	}
	return out
}

// Stats reports on the link to an address, false when there is none.
func (p *Peer) Stats(addr *Addr) (Stats, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	l := p.links[keyOf(addr)]
	if l == nil {
		return Stats{}, false
	}
	return Stats{RTT: l.srtt, Loss: l.loss, Pending: len(l.pending), Connected: l.connected}, true
}

// Close says goodbye to every peer and stops receiving.
func (p *Peer) Close() error {
	var err error
	p.once.Do(func() {
		close(p.closed)
		p.mu.Lock()
		now := time.Now()
		for _, l := range p.links {
			p.transmit(l, flagBye, 0, nil, now)
		}
		p.links = map[linkKey]*link{}
		p.mu.Unlock()
		err = p.conn.Close()
	})
	return err
}
