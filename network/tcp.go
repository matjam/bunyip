package network

import (
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
	sendMu     sync.Mutex
	closed     chan struct{}
	once       sync.Once
	ID         int // server-assigned, 1 upward; 0 on the client side
	Data       any // for the game: player state, name, anything
	closeErr   error
}

// Addr returns the peer's address.
func (c *Conn) Addr() string { return c.c.RemoteAddr().String() }

// Send encodes and writes one message; it is safe from any goroutine.
func (c *Conn) Send(msg any) error {
	data, err := c.reg.encode(msg)
	if err != nil {
		return err
	}
	if len(data) > MaxMessage {
		return fmt.Errorf("network: message of %d bytes exceeds MaxMessage", len(data))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if _, err := c.c.Write(hdr[:]); err != nil {
		return err
	}
	_, err = c.c.Write(data)
	return err
}

// Close ends the connection; the other side sees a Disconnected event.
// Locally queued events remain available to Poll. Pending events, including
// Disconnected, may be discarded if the local event queue is full.
func (c *Conn) Close() error {
	var err error
	c.once.Do(func() {
		close(c.closed)
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

// SetOnActivity runs fn on a network goroutine whenever an event is
// queued for a connection accepted from now on; point it at
// Context.Wake in a turn-based game.
func (s *Server) SetOnActivity(fn func()) {
	s.mu.Lock()
	s.activity = fn
	s.mu.Unlock()
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

// Broadcast sends a message to every connection, skipping any in except.
func (s *Server) Broadcast(msg any, except ...*Conn) {
	for _, c := range s.Conns() {
		skip := false
		for _, e := range except {
			skip = skip || e == c
		}
		if !skip {
			c.Send(msg)
		}
	}
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

// SetOnActivity runs fn on a network goroutine whenever an event is
// queued; point it at Context.Wake in a turn-based game.
// It is safe to call concurrently, including from fn. A callback already
// captured by the reader may still run after SetOnActivity returns.
// A nil fn disables future callbacks.
func (cl *Client) SetOnActivity(fn func()) {
	cl.Conn.activityMu.Lock()
	cl.Conn.activity = fn
	cl.Conn.activityMu.Unlock()
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
