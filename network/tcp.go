package network

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
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
	handshook  atomic.Bool // a TLS handshake has completed; sends skip it
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
	f, err := c.reg.frame(msg)
	if err != nil {
		return err
	}
	defer f.release()
	return c.sendFrame(sendBudget{deadline: time.Now().Add(DefaultSendTimeout)}, f.b)
}

// sendBudget bounds one send: a context, or for Send and Broadcast a
// bare deadline, which spares them building a context per message.
type sendBudget struct {
	ctx      context.Context // nil for a bare deadline
	deadline time.Time
}

// err reports a budget that is already spent.
func (b sendBudget) err() error {
	if b.ctx != nil {
		return sendContextErr(b.ctx)
	}
	if !b.deadline.IsZero() && !time.Now().Before(b.deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

// frame is an encoded message with its length header, in a buffer
// borrowed from a pool.
type frame struct {
	b  []byte
	bp *[]byte
}

// frameBufs lends frame buffers to sends. A buffer that grew past 64 KB
// for one large message is not kept.
var frameBufs = sync.Pool{New: func() any {
	b := make([]byte, 0, 512)
	return &b
}}

// frame encodes msg as [length uint32][type uint16][payload], ready to
// write with one call.
func (r *Registry) frame(msg any) (frame, error) {
	bp := frameBufs.Get().(*[]byte)
	b, err := r.appendEncoded(append((*bp)[:0], 0, 0, 0, 0), msg)
	f := frame{b: b, bp: bp}
	if err != nil {
		f.release()
		return frame{}, err
	}
	if n := len(b) - 4; n > MaxMessage {
		f.release()
		return frame{}, fmt.Errorf("network: message of %d bytes exceeds MaxMessage", n)
	}
	binary.BigEndian.PutUint32(b, uint32(len(b)-4))
	return f, nil
}

// release returns the frame's buffer to the pool.
func (f frame) release() {
	if f.bp == nil || cap(f.b) > 64<<10 {
		return
	}
	*f.bp = f.b[:0]
	frameBufs.Put(f.bp)
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
	f, err := c.reg.frame(msg)
	if err != nil {
		return err
	}
	defer f.release()
	deadline, _ := ctx.Deadline()
	return c.sendFrame(sendBudget{ctx: ctx, deadline: deadline}, f.b)
}

// sendFrame writes one encoded frame within a budget: it waits for the
// connection's writer, completes a pending TLS handshake, and writes the
// frame with one call where the transport accepts it whole.
func (c *Conn) sendFrame(b sendBudget, data []byte) (err error) {
	ctx := b.ctx
	c.sendInit.Do(func() { c.sendGate = make(chan struct{}, 1) })
	select {
	case c.sendGate <- struct{}{}:
	default:
		// Another sender holds the writer; wait within the budget.
		var expired <-chan time.Time
		var done <-chan struct{}
		if ctx != nil {
			done = ctx.Done()
		} else {
			t := time.NewTimer(time.Until(b.deadline))
			defer t.Stop()
			expired = t.C
		}
		select {
		case <-done:
			return ctx.Err()
		case <-expired:
			return context.DeadlineExceeded
		case <-c.closed:
			return net.ErrClosed
		case c.sendGate <- struct{}{}:
		}
	}
	defer func() { <-c.sendGate }()
	if err := b.err(); err != nil {
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
	// Once the handshake is done the check is skipped, so a send with a bare
	// deadline builds no context.
	if tlsConn, ok := c.c.(interface{ HandshakeContext(context.Context) error }); ok && !c.handshook.Load() {
		hctx := ctx
		if hctx == nil {
			var cancel context.CancelFunc
			hctx, cancel = context.WithDeadline(context.Background(), b.deadline)
			defer cancel()
		}
		if err := tlsConn.HandshakeContext(hctx); err != nil {
			c.Close()
			return fmt.Errorf("network: TLS handshake: %w", err)
		}
		c.handshook.Store(true)
	}
	if err := c.c.SetWriteDeadline(b.deadline); err != nil {
		c.Close()
		return fmt.Errorf("network: set write deadline: %w", err)
	}
	// A bare deadline is enforced by the write deadline alone. A context
	// that can be canceled also expires the write deadline when it ends.
	var stopCancel func() bool
	var canceled chan struct{}
	if ctx != nil && ctx.Done() != nil {
		canceled = make(chan struct{})
		stopCancel = context.AfterFunc(ctx, func() {
			defer close(canceled)
			if c.c.SetWriteDeadline(time.Now()) != nil {
				c.Close()
			}
		})
	}
	defer c.endSend(stopCancel, canceled, &err)
	started, err := writeFrame(b, c.c, data)
	if err != nil {
		if started {
			c.Close()
		}
		var cause error
		if ctx != nil {
			cause = ctx.Err()
		}
		var timeout net.Error
		if cause == nil && !b.deadline.IsZero() && !time.Now().Before(b.deadline) && errors.As(err, &timeout) && timeout.Timeout() {
			cause = context.DeadlineExceeded
		}
		return errors.Join(err, cause)
	}
	return nil
}

// endSend finishes a send that set a write deadline: it joins the
// cancellation callback if one is running, then clears the deadline.
func (c *Conn) endSend(stopCancel func() bool, canceled chan struct{}, err *error) {
	// Join an already running callback before clearing its deadline. Otherwise
	// it could expire the next sender's write after this sender releases the gate.
	if stopCancel != nil && !stopCancel() {
		<-canceled
	}
	if clearErr := c.c.SetWriteDeadline(time.Time{}); clearErr != nil && *err == nil {
		c.Close()
		*err = fmt.Errorf("network: clear write deadline: %w", clearErr)
	}
}

// writeFrame writes p in as few calls as the transport allows, checking
// the budget before each one.
func writeFrame(b sendBudget, w io.Writer, p []byte) (started bool, err error) {
	for len(p) > 0 {
		if err := b.err(); err != nil {
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
// It reads through a buffer, so a burst of small frames costs one read
// from the transport, and decodes each frame from storage it reuses: a
// decoded message is a fresh value, and a binary message's
// UnmarshalBinary copies what it keeps, as encoding.BinaryUnmarshaler
// requires.
func (c *Conn) readLoop() {
	const keepBuf = 64 << 10 // frames up to this size reuse one buffer
	r := bufio.NewReaderSize(c.c, 16<<10)
	var hdr [4]byte
	var reuse []byte
	var err error
	for {
		if _, err = io.ReadFull(r, hdr[:]); err != nil {
			break
		}
		n := binary.BigEndian.Uint32(hdr[:])
		if n > MaxMessage {
			err = fmt.Errorf("network: frame of %d bytes exceeds MaxMessage", n)
			break
		}
		var buf []byte
		if n <= keepBuf {
			if uint32(cap(reuse)) < n {
				reuse = make([]byte, max(n, 1024))
			}
			buf = reuse[:n]
		} else {
			buf = make([]byte, n)
		}
		if _, err = io.ReadFull(r, buf); err != nil {
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

// Broadcast sends to every connection except those in except. The message
// is encoded once, and the sends run sequentially with one shared
// DefaultSendTimeout budget. The returned map
// contains failed peers only; nil means every selected peer accepted its frame.
func (s *Server) Broadcast(msg any, except ...*Conn) map[*Conn]error {
	return s.broadcast(sendBudget{deadline: time.Now().Add(DefaultSendTimeout)}, msg, except)
}

// BroadcastContext sends sequentially with one shared context budget. It returns
// failed peers only, including peers not reached before cancellation. Nil means
// every selected peer accepted its frame. The message is encoded once, before
// the first send, and a blocking custom marshaler cannot be interrupted; an
// encoding error is reported for every selected peer. The connection snapshot
// and iteration order are unspecified; sends may block the caller.
func (s *Server) BroadcastContext(ctx context.Context, msg any, except ...*Conn) map[*Conn]error {
	deadline, _ := ctx.Deadline()
	return s.broadcast(sendBudget{ctx: ctx, deadline: deadline}, msg, except)
}

func (s *Server) broadcast(b sendBudget, msg any, except []*Conn) map[*Conn]error {
	var failures map[*Conn]error
	fail := func(c *Conn, err error) {
		if failures == nil {
			failures = make(map[*Conn]error)
		}
		failures[c] = err
	}
	// Every connection of a server shares its registry, so the frame is
	// encoded once; it is encoded again only for a connection that has
	// another registry.
	var f frame
	var encErr error
	var encReg *Registry
	defer func() { f.release() }()
	for _, c := range s.Conns() {
		skip := false
		for _, e := range except {
			skip = skip || e == c
		}
		if skip {
			continue
		}
		if err := b.err(); err != nil {
			fail(c, err)
			continue
		}
		if encReg != c.reg {
			f.release()
			f, encErr = c.reg.frame(msg)
			encReg = c.reg
		}
		if encErr != nil {
			fail(c, encErr)
			continue
		}
		if err := c.sendFrame(b, f.b); err != nil {
			fail(c, err)
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
