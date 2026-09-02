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
	v := reflect.ValueOf(msg)
	t := v.Type()
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	id, ok := r.byType[t]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownMessage, t)
	}
	var payload []byte
	var err error
	if bm, ok := msg.(encoding.BinaryMarshaler); ok {
		payload, err = bm.MarshalBinary()
	} else {
		payload, err = json.Marshal(msg)
	}
	if err != nil {
		return nil, fmt.Errorf("network: encode %s: %w", t, err)
	}
	out := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(out, id)
	copy(out[2:], payload)
	return out, nil
}

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
