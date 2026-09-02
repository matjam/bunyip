// Package network moves typed messages between game instances over TCP
// and UDP. TCP is ordered and reliable, for turn-based games, lobbies
// and chat, and is encrypted with ListenTLS and DialTLS. UDP is faster.
// Send carries real-time state, and SendReliable delivers a message
// reliably and in order when it must arrive. A Registry names the
// message types both ends agree on. Messages are plain Go structs
// encoded as JSON unless they implement encoding.BinaryMarshaler.
//
// Every connection delivers Events through a channel. Drain it once per
// frame with Poll, so game code needs no locking. A turn-based game can
// set OnActivity to Context.Wake to wake up when a message arrives. UDP
// peers get Connected and Disconnected events too, from a hello and
// keepalive exchange with a timeout.
//
// For real-time state the helpers cover the usual techniques.
// Interpolator, Predictor, History and Clock handle smoothing,
// prediction, lag compensation and server time. EncodeDelta with
// SnapshotBuffer and SnapshotReceiver sends only what changed since the
// snapshot a client acknowledged. Interest chooses which entities a
// viewer needs at all.
package network

import (
	"encoding"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// Registry maps message types to the small numbers sent on the wire.
// Register the same types in the same order on both ends.
type Registry struct {
	byType map[reflect.Type]uint16
	byID   []reflect.Type
}

// NewRegistry makes an empty registry.
func NewRegistry() *Registry {
	return &Registry{byType: map[reflect.Type]uint16{}}
}

// Register adds a message type, given as a zero value: r.Register(Move{}).
// Types are numbered in registration order.
func (r *Registry) Register(msgs ...any) *Registry {
	for _, m := range msgs {
		t := reflect.TypeOf(m)
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if _, dup := r.byType[t]; dup {
			continue
		}
		r.byType[t] = uint16(len(r.byID))
		r.byID = append(r.byID, t)
	}
	return r
}

// ErrUnknownMessage is returned for types outside the registry.
var ErrUnknownMessage = errors.New("network: message type not registered")

// MaxMessage caps a single message's encoded size.
const MaxMessage = 16 << 20

// encode produces [type uint16][payload] for a message value or pointer.
func (r *Registry) encode(msg any) ([]byte, error) {
	if _, ok := msg.(encoding.BinaryMarshaler); ok {
		// A binary message knows its own size, so appendEncoded sizes
		// the buffer exactly.
		return r.appendEncoded(nil, msg)
	}
	// A JSON message does not, so start with room for a small one and
	// let the prefix and the payload land in one allocation.
	return r.appendEncoded(make([]byte, 0, 64), msg)
}

// appendEncoded appends [type uint16][payload] to dst and returns the
// grown slice, so a caller with a buffer of its own encodes without
// allocating. dst is left alone when the message cannot be encoded.
func (r *Registry) appendEncoded(dst []byte, msg any) ([]byte, error) {
	t := reflect.TypeOf(msg)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	id, ok := r.byType[t]
	if !ok {
		return dst, fmt.Errorf("%w: %s", ErrUnknownMessage, t)
	}
	if bm, ok := msg.(encoding.BinaryMarshaler); ok {
		payload, err := bm.MarshalBinary()
		if err != nil {
			return dst, fmt.Errorf("network: encode %s: %w", t, err)
		}
		if need := 2 + len(payload); cap(dst)-len(dst) < need {
			grown := make([]byte, len(dst), len(dst)+need)
			copy(grown, dst)
			dst = grown
		}
		dst = binary.BigEndian.AppendUint16(dst, id)
		return append(dst, payload...), nil
	}
	out := binary.BigEndian.AppendUint16(dst, id)
	// json.Marshal would allocate a payload for us to copy out of. An
	// Encoder writing into the buffer skips that copy; it adds a
	// newline the encoding does not need, which comes off again.
	w := jsonAppenders.Get().(*jsonAppender)
	w.buf = out
	err := w.enc.Encode(msg)
	out, w.buf = w.buf, nil
	jsonAppenders.Put(w)
	if err != nil {
		return dst, fmt.Errorf("network: encode %s: %w", t, err)
	}
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}

// jsonAppender lets a json.Encoder write straight into a caller's
// buffer. The encoders are pooled because one is bound to its writer
// for life.
type jsonAppender struct {
	buf []byte
	enc *json.Encoder
}

func (w *jsonAppender) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

var jsonAppenders = sync.Pool{New: func() any {
	w := &jsonAppender{}
	w.enc = json.NewEncoder(w)
	return w
}}

// decode turns a wire buffer back into a pointer to a fresh message.
func (r *Registry) decode(data []byte) (any, error) {
	if len(data) < 2 {
		return nil, errors.New("network: short message")
	}
	id := binary.BigEndian.Uint16(data)
	if int(id) >= len(r.byID) {
		return nil, fmt.Errorf("%w: id %d", ErrUnknownMessage, id)
	}
	p := reflect.New(r.byID[id])
	msg := p.Interface()
	if bu, ok := msg.(encoding.BinaryUnmarshaler); ok {
		if err := bu.UnmarshalBinary(data[2:]); err != nil {
			return nil, fmt.Errorf("network: decode %s: %w", r.byID[id], err)
		}
		return msg, nil
	}
	if err := json.Unmarshal(data[2:], msg); err != nil {
		return nil, fmt.Errorf("network: decode %s: %w", r.byID[id], err)
	}
	return msg, nil
}

// EventKind says what an Event reports.
type EventKind uint8

const (
	Connected    EventKind = iota + 1 // a peer joined (server), the dial completed (client) or a UDP address sent its first packet
	Disconnected                      // the peer went away; Err says why, nil for a clean close or goodbye
	Message                           // Msg holds a pointer to a decoded message
)

// Event is something that happened on a connection.
type Event struct {
	Kind EventKind
	Conn *Conn // TCP connection, when applicable
	From *Addr // UDP address, when applicable
	Msg  any   // *T for a registered T
	Err  error
}
