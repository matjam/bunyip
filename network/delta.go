package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
)

// ErrDeltaType is returned for a snapshot type a delta cannot describe.
var ErrDeltaType = errors.New("network: unsupported delta type")

// ErrDeltaData is returned for delta bytes that do not fit the type.
var ErrDeltaData = errors.New("network: bad delta data")

// EncodeDelta encodes the exported fields of current that differ from
// baseline, both structs (or pointers to structs) of the same type, as
// a change mask followed by only the changed values. An unchanged
// snapshot encodes to the mask alone, one byte per eight fields.
//
// Supported field types are bool, the sized and unsized integers,
// float32, float64, string, arrays of those, and nested structs, which
// are encoded as deltas of their own. Unexported fields are skipped.
// Anything else (slices, maps, pointers, interfaces) is ErrDeltaType.
func EncodeDelta(baseline, current any) ([]byte, error) {
	b, c := reflect.Indirect(reflect.ValueOf(baseline)), reflect.Indirect(reflect.ValueOf(current))
	if !b.IsValid() || !c.IsValid() || b.Type() != c.Type() {
		return nil, fmt.Errorf("%w: baseline and current must be the same struct type", ErrDeltaType)
	}
	dt, err := deltaTypeOf(b.Type())
	if err != nil {
		return nil, err
	}
	return dt.encode(nil, b, c), nil
}

// DecodeDelta applies delta bytes from EncodeDelta to a copy of
// baseline and stores the result in into, a pointer to the same struct
// type. baseline must be the same value the encoder used.
func DecodeDelta(baseline any, data []byte, into any) error {
	b := reflect.Indirect(reflect.ValueOf(baseline))
	dst := reflect.ValueOf(into)
	if !b.IsValid() || dst.Kind() != reflect.Pointer || dst.IsNil() || dst.Elem().Type() != b.Type() {
		return fmt.Errorf("%w: into must be a pointer to the baseline's type", ErrDeltaType)
	}
	dt, err := deltaTypeOf(b.Type())
	if err != nil {
		return err
	}
	dst = dst.Elem()
	dst.Set(b)
	r := &deltaReader{data: data}
	dt.decode(r, dst)
	if r.err != nil {
		return r.err
	}
	if len(r.data) != 0 {
		return fmt.Errorf("%w: %d bytes left over", ErrDeltaData, len(r.data))
	}
	return nil
}

type deltaField struct {
	index  int
	nested *deltaType // for struct fields
}

type deltaType struct {
	fields  []deltaField
	maskLen int
}

var deltaTypes sync.Map // reflect.Type -> *deltaType

func deltaTypeOf(t reflect.Type) (*deltaType, error) {
	if dt, ok := deltaTypes.Load(t); ok {
		return dt.(*deltaType), nil
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: %s is not a struct", ErrDeltaType, t)
	}
	dt := &deltaType{}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		df := deltaField{index: i}
		switch f.Type.Kind() {
		case reflect.Struct:
			nested, err := deltaTypeOf(f.Type)
			if err != nil {
				return nil, fmt.Errorf("%w (in field %s.%s)", err, t, f.Name)
			}
			df.nested = nested
		case reflect.Array:
			if !scalarKind(f.Type.Elem().Kind()) {
				return nil, fmt.Errorf("%w: field %s.%s is an array of %s", ErrDeltaType, t, f.Name, f.Type.Elem())
			}
		default:
			if !scalarKind(f.Type.Kind()) {
				return nil, fmt.Errorf("%w: field %s.%s has type %s", ErrDeltaType, t, f.Name, f.Type)
			}
		}
		dt.fields = append(dt.fields, df)
	}
	dt.maskLen = (len(dt.fields) + 7) / 8
	deltaTypes.Store(t, dt)
	return dt, nil
}

func scalarKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.String:
		return true
	}
	return false
}

// equal reports whether two values of this struct type agree on their
// exported fields.
func (dt *deltaType) equal(a, b reflect.Value) bool {
	for _, f := range dt.fields {
		af, bf := a.Field(f.index), b.Field(f.index)
		if f.nested != nil {
			if !f.nested.equal(af, bf) {
				return false
			}
		} else if !af.Equal(bf) {
			return false
		}
	}
	return true
}

func (dt *deltaType) encode(buf []byte, base, cur reflect.Value) []byte {
	maskAt := len(buf)
	buf = append(buf, make([]byte, dt.maskLen)...)
	for i, f := range dt.fields {
		bf, cf := base.Field(f.index), cur.Field(f.index)
		if f.nested != nil {
			if f.nested.equal(bf, cf) {
				continue
			}
			buf[maskAt+i/8] |= 1 << (i % 8)
			buf = f.nested.encode(buf, bf, cf)
			continue
		}
		if bf.Equal(cf) {
			continue
		}
		buf[maskAt+i/8] |= 1 << (i % 8)
		buf = encodeValue(buf, cf)
	}
	return buf
}

func encodeValue(buf []byte, v reflect.Value) []byte {
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			return append(buf, 1)
		}
		return append(buf, 0)
	case reflect.Int8, reflect.Uint8:
		return append(buf, byte(bits(v)))
	case reflect.Int16, reflect.Uint16:
		return binary.LittleEndian.AppendUint16(buf, uint16(bits(v)))
	case reflect.Int32, reflect.Uint32, reflect.Float32:
		return binary.LittleEndian.AppendUint32(buf, uint32(bits(v)))
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Float64:
		return binary.LittleEndian.AppendUint64(buf, bits(v))
	case reflect.String:
		s := v.String()
		buf = binary.AppendUvarint(buf, uint64(len(s)))
		return append(buf, s...)
	case reflect.Array:
		for i := range v.Len() {
			buf = encodeValue(buf, v.Index(i))
		}
		return buf
	}
	panic("network: unreachable delta kind " + v.Kind().String())
}

// bits is a value's representation as up to 64 bits.
func bits(v reflect.Value) uint64 {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return uint64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Float32:
		return uint64(math.Float32bits(float32(v.Float())))
	case reflect.Float64:
		return math.Float64bits(v.Float())
	}
	panic("network: unreachable delta kind " + v.Kind().String())
}

type deltaReader struct {
	data []byte
	err  error
}

// take returns the next n bytes, or nil after recording an error.
func (r *deltaReader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if len(r.data) < n {
		r.err = fmt.Errorf("%w: truncated", ErrDeltaData)
		return nil
	}
	out := r.data[:n]
	r.data = r.data[n:]
	return out
}

func (dt *deltaType) decode(r *deltaReader, dst reflect.Value) {
	mask := r.take(dt.maskLen)
	if mask == nil {
		return
	}
	for i, f := range dt.fields {
		if mask[i/8]&(1<<(i%8)) == 0 {
			continue
		}
		if f.nested != nil {
			f.nested.decode(r, dst.Field(f.index))
		} else {
			decodeValue(r, dst.Field(f.index))
		}
		if r.err != nil {
			return
		}
	}
}

func decodeValue(r *deltaReader, v reflect.Value) {
	var raw uint64
	switch v.Kind() {
	case reflect.Bool:
		if b := r.take(1); b != nil {
			v.SetBool(b[0] != 0)
		}
		return
	case reflect.String:
		n, k := binary.Uvarint(r.data)
		if k <= 0 || n > uint64(len(r.data)-k) {
			r.err = fmt.Errorf("%w: bad string length", ErrDeltaData)
			return
		}
		r.data = r.data[k:]
		v.SetString(string(r.take(int(n))))
		return
	case reflect.Array:
		for i := range v.Len() {
			decodeValue(r, v.Index(i))
			if r.err != nil {
				return
			}
		}
		return
	case reflect.Int8, reflect.Uint8:
		if b := r.take(1); b != nil {
			raw = uint64(b[0])
		}
	case reflect.Int16, reflect.Uint16:
		if b := r.take(2); b != nil {
			raw = uint64(binary.LittleEndian.Uint16(b))
		}
	case reflect.Int32, reflect.Uint32, reflect.Float32:
		if b := r.take(4); b != nil {
			raw = uint64(binary.LittleEndian.Uint32(b))
		}
	default:
		if b := r.take(8); b != nil {
			raw = binary.LittleEndian.Uint64(b)
		}
	}
	if r.err != nil {
		return
	}
	switch v.Kind() {
	case reflect.Int8:
		v.SetInt(int64(int8(raw)))
	case reflect.Int16:
		v.SetInt(int64(int16(raw)))
	case reflect.Int32:
		v.SetInt(int64(int32(raw)))
	case reflect.Int, reflect.Int64:
		v.SetInt(int64(raw))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(raw)
	case reflect.Float32:
		v.SetFloat(float64(math.Float32frombits(uint32(raw))))
	case reflect.Float64:
		v.SetFloat(math.Float64frombits(raw))
	}
}

// SnapshotBuffer is the server side of delta-compressed snapshots. Each
// client is sent its snapshot encoded against the last one it
// acknowledged, and the buffer remembers what was sent so the
// acknowledgement can be matched. K identifies a client (a Conn ID, an
// address string) and S is the snapshot struct, which must satisfy
// EncodeDelta. The zero value is ready to use; it is not safe for
// concurrent use.
type SnapshotBuffer[K comparable, S any] struct {
	// Keep is how many sent snapshots to remember per client, so a late
	// acknowledgement still finds its baseline; zero means 32.
	Keep    int
	clients map[K]*snapshotClient[S]
}

type snapshotClient[S any] struct {
	sent  []snapshotEntry[S]
	acked uint32
}

type snapshotEntry[S any] struct {
	seq  uint32
	snap S
}

// Encode encodes a client's snapshot against the newest one it has
// acknowledged, or the zero S when it has acknowledged none the buffer
// still holds, and remembers it under seq (which must be nonzero and
// increase). It returns the baseline's sequence, 0 for the zero S, and
// the delta; send both with seq to the client for SnapshotReceiver.
func (b *SnapshotBuffer[K, S]) Encode(client K, seq uint32, snap S) (base uint32, data []byte, err error) {
	if seq == 0 {
		return 0, nil, fmt.Errorf("%w: snapshot sequence 0 is reserved", ErrDeltaData)
	}
	if b.clients == nil {
		b.clients = map[K]*snapshotClient[S]{}
	}
	c := b.clients[client]
	if c == nil {
		c = &snapshotClient[S]{}
		b.clients[client] = c
	}
	var baseline S
	for _, e := range c.sent {
		if e.seq == c.acked && c.acked != 0 {
			baseline, base = e.snap, e.seq
			break
		}
	}
	data, err = EncodeDelta(&baseline, &snap)
	if err != nil {
		return 0, nil, err
	}
	c.sent = append(c.sent, snapshotEntry[S]{seq, snap})
	keep := b.Keep
	if keep <= 0 {
		keep = 32
	}
	if len(c.sent) > keep {
		c.sent = c.sent[len(c.sent)-keep:]
	}
	return base, data, nil
}

// Ack records that a client received the snapshot with sequence seq;
// later snapshots for it are encoded against that one. An unknown seq
// is ignored.
func (b *SnapshotBuffer[K, S]) Ack(client K, seq uint32) {
	c := b.clients[client]
	if c == nil {
		return
	}
	for i, e := range c.sent {
		if e.seq == seq {
			c.acked = seq
			c.sent = c.sent[i:] // nothing older will be a baseline again
			return
		}
	}
}

// Forget drops a client that has gone.
func (b *SnapshotBuffer[K, S]) Forget(client K) { delete(b.clients, client) }

// SnapshotReceiver is the client side of SnapshotBuffer: it decodes
// each snapshot against the earlier one the server named and keeps the
// result so it can be a baseline in turn. The zero value is ready to
// use.
type SnapshotReceiver[S any] struct {
	// Keep is how many decoded snapshots to remember; zero means 32.
	Keep int
	got  []snapshotEntry[S]
}

// Decode applies a delta to the snapshot with sequence base (0 for the
// zero S), remembers the result under seq, and returns it. Acknowledge
// seq to the server afterwards. A baseline no longer held is
// ErrDeltaData; the fix is to acknowledge nothing until a snapshot
// against a baseline the receiver has arrives.
func (r *SnapshotReceiver[S]) Decode(base, seq uint32, data []byte) (S, error) {
	var baseline S
	if base != 0 {
		found := false
		for _, e := range r.got {
			if e.seq == base {
				baseline, found = e.snap, true
				break
			}
		}
		if !found {
			return baseline, fmt.Errorf("%w: baseline snapshot %d not held", ErrDeltaData, base)
		}
	}
	var snap S
	if err := DecodeDelta(&baseline, data, &snap); err != nil {
		return snap, err
	}
	r.got = append(r.got, snapshotEntry[S]{seq, snap})
	keep := r.Keep
	if keep <= 0 {
		keep = 32
	}
	if len(r.got) > keep {
		r.got = r.got[len(r.got)-keep:]
	}
	return snap, nil
}
