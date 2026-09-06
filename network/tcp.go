package network

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Conn is one TCP peer. Messages sent on it arrive in order.
type Conn struct {
	c          net.Conn
	reg        *Registry
	events     chan Event
	activityMu sync.Mutex
	activity   func()
	sendInit   sync.Once
	sendGate   chan struct{}
	closed     chan struct{}
	once       sync.Once
	ID         int // server-assigned, 1 upward; 0 on the client side
	Data       any // for the game: player state, name, anything
	closeErr   error
}

// Addr returns the peer's address.
func (c *Conn) Addr() string { return c.c.RemoteAddr().String() }

// DefaultSendTimeout bounds writer acquisition and network I/O for Send and
// the entire sequential Broadcast. Encoding runs synchronously; a custom
// marshaler that blocks cannot be interrupted by this timeout.
const DefaultSendTimeout = 5 * time.Second

// Send encodes and writes one message with DefaultSendTimeout. It is safe from
// any goroutine. Use SendContext to choose a shorter or longer budget.
func (c *Conn) Send(msg any) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultSendTimeout)
	defer cancel()
	return c.SendContext(ctx, msg)
}

// SendContext encodes and writes one message. Cancellation covers waiting for
// another sender and transport writes, including TLS; encoding is synchronous
// and cannot interrupt a custom marshaler. A context without a deadline may
// wait indefinitely. Concurrent sends are serialized in acquisition order.
//
// Cancellation before transport I/O leaves the connection usable. An interrupted
// TLS handshake or transport write failure closes it, since a partial frame or TLS write error
// can prevent further messages from being decoded. A successful write means the
// transport accepted the frame, not that the peer processed it. Cancellation
// racing after a complete frame does not turn that successful send into an error.
func (c *Conn) SendContext(ctx context.Context, msg any) (err error) {
	if err := sendContextErr(ctx); err != nil {
		return err
	}
	data, err := c.reg.encode(msg)
	if err != nil {
		return err
	}
	if len(data) > MaxMessage {
		return fmt.Errorf("network: message of %d bytes exceeds MaxMessage", len(data))
	}
	c.sendInit.Do(func() { c.sendGate = make(chan struct{}, 1) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return net.ErrClosed
	case c.sendGate <- struct{}{}:
	}
	defer func() { <-c.sendGate }()
	if err := sendContextErr(ctx); err != nil {
		return err
	}
	select {
	case <-c.closed:
		return net.ErrClosed
	default:
	}
	// TLS's implicit Write handshake may already be waiting in the reader.
	// A write deadline cannot interrupt its read or mutex wait; HandshakeContext
	// cancels that work by closing the underlying transport when necessary.
	if tlsConn, ok := c.c.(interface{ HandshakeContext(context.Context) error }); ok {
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			c.Close()
			return fmt.Errorf("network: TLS handshake: %w", err)
		}
	}
	deadline, _ := ctx.Deadline()
	if err := c.c.SetWriteDeadline(deadline); err != nil {
		c.Close()
		return fmt.Errorf("network: set write deadline: %w", err)
	}
	canceled := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() {
		defer close(canceled)
		if c.c.SetWriteDeadline(time.Now()) != nil {
			c.Close()
		}
	})
	defer func() {
		// Join an already running callback before clearing its deadline. Otherwise
		// it could expire the next sender's write after this sender releases the gate.
		if !stopCancel() {
			<-canceled
		}
		if clearErr := c.c.SetWriteDeadline(time.Time{}); clearErr != nil && err == nil {
			c.Close()
			err = fmt.Errorf("network: clear write deadline: %w", clearErr)
		}
	}()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	started, err := writeFramePart(ctx, c.c, hdr[:])
	if err == nil {
		_, err = writeFramePart(ctx, c.c, data)
	}
	if err != nil {
		if started {
			c.Close()
		}
		cause := ctx.Err()
		var timeout net.Error
		if cause == nil && !deadline.IsZero() && !time.Now().Before(deadline) && errors.As(err, &timeout) && timeout.Timeout() {
			cause = context.DeadlineExceeded
		}
		return errors.Join(err, cause)
	}
	return nil
}

func writeFramePart(ctx context.Context, w io.Writer, p []byte) (started bool, err error) {
	for len(p) > 0 {
		if err := sendContextErr(ctx); err != nil {
			return started, err
		}
		started = true
		n, err := w.Write(p)
		if n < 0 || n > len(p) {
			return started, fmt.Errorf("network: invalid write count %d for %d bytes", n, len(p))
		}
		p = p[n:]
		if err != nil {
			return started, err
		}
		if n == 0 {
			return started, io.ErrShortWrite
		}
	}
	return started, nil
}

// A socket deadline may fire before the context's timer goroutine publishes
// cancellation. Do not start another frame or broadcast peer in that interval.
func sendContextErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

// Close ends the connection; the other side sees a Disconnected event.
// Locally queued events remain available to Poll. Pending events, including
// Disconnected, may be discarded if the local event queue is full.
func (c *Conn) Close() error {
	var err error
	c.once.Do(func() {
		if c.closed != nil {
			close(c.closed)
		}
		err = c.c.Close()
	})
	return err
}

// readLoop turns frames into Message events until the connection ends.
func (c *Conn) readLoop() {
	var hdr [4]byte
	var err error
	for {
		if _, err = io.ReadFull(c.c, hdr[:]); err != nil {
			break
		}
		n := binary.BigEndian.Uint32(hdr[:])
		if n > MaxMessage {
			err = fmt.Errorf("network: frame of %d bytes exceeds MaxMessage", n)
			break
		}
		buf := make([]byte, n)
		if _, err = io.ReadFull(c.c, buf); err != nil {
			break
		}
		msg, derr := c.reg.decode(buf)
		if derr != nil {
			err = derr
			break
		}
		if !c.emit(Event{Kind: Message, Conn: c, Msg: msg}) {
			break
		}
	}
	select {
	case <-c.closed:
		err = nil // we closed it
	default:
		if errors.Is(err, io.EOF) {
			err = nil // the peer closed cleanly
		}
	}
	c.closeErr = err
	c.c.Close()
	c.emit(Event{Kind: Disconnected, Conn: c, Err: err})
}

func (c *Conn) emit(ev Event) bool {
	// Preserve events when there is room, even during local shutdown.
	// A full queue must never keep a closed connection's reader alive.
	select {
	case c.events <- ev:
	default:
		select {
		case c.events <- ev:
		case <-c.closed:
			return false
		}
	}
	c.activityMu.Lock()
	activity := c.activity
	c.activityMu.Unlock()
	// The callback may register another callback or close the connection.
	if activity != nil {
		activity()
	}
	return true
}

// Server accepts TCP connections.
type Server struct {
	ln       net.Listener
	reg      *Registry
	events   chan Event
	conns    map[int]*Conn
	mu       sync.Mutex
	nextID   int
	activity func()
	closed   chan struct{}
	once     sync.Once
}

// SetOnActivity sets the wake callback for existing and future connections.
// If events are pending, it calls fn before returning; later calls run on
// network goroutines. Callbacks run outside locks and may replace the hook
// or close the server. Keep them short; drain pending events before
// registering again from a callback to avoid recursion. Nil disables
// future captures; an already captured callback may still run.
func (s *Server) SetOnActivity(fn func()) {
	s.mu.Lock()
	s.activity = fn
	for _, c := range s.conns {
		c.activityMu.Lock()
		c.activity = fn
		c.activityMu.Unlock()
	}
	s.mu.Unlock()
	if fn != nil && len(s.events) > 0 {
		fn()
	}
}

// Listen starts a server on addr (":7777" for every interface).
func Listen(addr string, reg *Registry) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	return newServer(ln, reg), nil
}

func newServer(ln net.Listener, reg *Registry) *Server {
	s := &Server{ln: ln, reg: reg, events: make(chan Event, 1024), conns: map[int]*Conn{}, closed: make(chan struct{})}
	go s.accept()
	return s
}

// Addr is the address the server is listening on, useful after ":0".
func (s *Server) Addr() string { return s.ln.Addr().String() }

func (s *Server) accept() {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
			default:
				select {
				case s.events <- Event{Kind: Disconnected, Err: fmt.Errorf("network: accept: %w", err)}:
					s.mu.Lock()
					activity := s.activity
					s.mu.Unlock()
					if activity != nil {
						activity()
					}
				case <-s.closed:
				}
			}
			return
		}
		s.mu.Lock()
		// Accept may return a socket concurrently with listener shutdown.
		// Registration and Close's shutdown marker share the same lock.
		select {
		case <-s.closed:
			s.mu.Unlock()
			nc.Close()
			return
		default:
		}
		s.nextID++
		c := &Conn{c: nc, reg: s.reg, events: s.events, activity: s.activity, closed: make(chan struct{}), ID: s.nextID}
		s.conns[c.ID] = c
		s.mu.Unlock()
		if !c.emit(Event{Kind: Connected, Conn: c}) {
			c.Close()
			s.mu.Lock()
			delete(s.conns, c.ID)
			s.mu.Unlock()
			continue
		}
		go func() {
			c.readLoop()
			s.mu.Lock()
			delete(s.conns, c.ID)
			s.mu.Unlock()
		}()
	}
}

// Poll returns the events queued since the last call. Call it once per
// frame; it never blocks.
func (s *Server) Poll() []Event { return drain(s.events) }

// Conns lists the live connections.
func (s *Server) Conns() []*Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Conn, 0, len(s.conns))
	for _, c := range s.conns {
		out = append(out, c)
	}
	return out
}

// Broadcast sends to every connection except those in except. Sends run
// sequentially with one shared DefaultSendTimeout budget. The returned map
// contains failed peers only; nil means every selected peer accepted its frame.
func (s *Server) Broadcast(msg any, except ...*Conn) map[*Conn]error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultSendTimeout)
	defer cancel()
	return s.BroadcastContext(ctx, msg, except...)
}

// BroadcastContext sends sequentially with one shared context budget. It returns
// failed peers only, including peers not reached before cancellation. Nil means
// every selected peer accepted its frame. Encoding runs synchronously for each
// peer and a blocking custom marshaler cannot be interrupted. The connection
// snapshot and iteration order are unspecified; sends may block the caller.
func (s *Server) BroadcastContext(ctx context.Context, msg any, except ...*Conn) map[*Conn]error {
	var failures map[*Conn]error
	for _, c := range s.Conns() {
		skip := false
		for _, e := range except {
			skip = skip || e == c
		}
		if !skip {
			if err := c.SendContext(ctx, msg); err != nil {
				if failures == nil {
					failures = make(map[*Conn]error)
				}
				failures[c] = err
			}
		}
	}
	return failures
}

// Close stops accepting and closes every connection.
// Pending events may be discarded if the local event queue is full,
// as with Conn.Close. Already queued events remain available to Poll.
func (s *Server) Close() error {
	var err error
	s.once.Do(func() {
		s.mu.Lock()
		close(s.closed)
		s.mu.Unlock()
		err = s.ln.Close()
		for _, c := range s.Conns() {
			c.Close()
		}
	})
	return err
}

// Client is the dialling side of a TCP connection.
type Client struct {
	*Conn
	events chan Event
}

// Dial connects to a server. The client's first event is Connected.
// A nonpositive timeout defaults to ten seconds.
func Dial(addr string, reg *Registry, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	nc, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("network: %w", err)
	}
	return newClient(nc, reg), nil
}

func newClient(nc net.Conn, reg *Registry) *Client {
	events := make(chan Event, 1024)
	c := &Conn{c: nc, reg: reg, events: events, closed: make(chan struct{})}
	cl := &Client{Conn: c, events: events}
	c.emit(Event{Kind: Connected, Conn: c})
	go c.readLoop()
	return cl
}

// SetOnActivity sets the wake callback; point it at Context.Wake in a
// turn-based game. Pending events call fn before this returns; later
// events call it on a network goroutine. Callbacks run outside locks.
// It is safe to call concurrently, including from fn. A callback already
// captured by the reader may still run after SetOnActivity returns.
// A nil fn disables future callbacks.
// Keep fn short and drain pending events before registering again from
// a callback to avoid recursion.
func (cl *Client) SetOnActivity(fn func()) {
	cl.Conn.activityMu.Lock()
	cl.Conn.activity = fn
	cl.Conn.activityMu.Unlock()
	if fn != nil && len(cl.events) > 0 {
		fn()
	}
}

// Poll returns the events queued since the last call without blocking.
func (cl *Client) Poll() []Event { return drain(cl.events) }

func drain(ch chan Event) []Event {
	var out []Event
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		default:
			return out
		}
	}
}
