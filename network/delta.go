package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"reflect"
	"strconv"
	"sync"
	"unsafe"
)

// ErrDeltaType is returned for a snapshot type a delta cannot describe.
var ErrDeltaType = errors.New("network: unsupported delta type")

// ErrDeltaData is returned for delta bytes that do not fit the type.
var ErrDeltaData = errors.New("network: bad delta data")

// EncodeDelta encodes the exported fields of current that differ from
// baseline, both structs (or pointers to structs) of the same type, as
// a change mask followed by only the changed values. An unchanged
// snapshot encodes to the mask alone, one byte per eight fields. To
// encode into a buffer the caller reuses, call AppendDelta.
//
// Supported field types are bool, the sized and unsized integers,
// float32, float64, string, arrays of those, and nested structs, which
// are encoded as deltas of their own. An array is encoded as a change
// mask of its own, one bit per element, followed by only the changed
// elements, so a large array with a few changed entries stays small.
// Values are compared by their bits, so a float that changes from 0 to
// -0 is sent and an unchanged NaN is not. Unexported fields are
// skipped. Anything else (slices, maps, pointers, interfaces) is
// ErrDeltaType.
//
// The encoding may change between Bunyip versions before 1.0; both ends
// must run the same version.
func EncodeDelta(baseline, current any) ([]byte, error) {
	return AppendDelta(nil, baseline, current)
}

// AppendDelta appends the delta EncodeDelta would return to dst and
// returns the extended slice. To encode every tick without allocating,
// pass pointers to baseline and current and a buffer kept from the last
// call, truncated to zero length. Values passed by value are copied
// first, which allocates.
func AppendDelta(dst []byte, baseline, current any) ([]byte, error) {
	b, c := reflect.Indirect(reflect.ValueOf(baseline)), reflect.Indirect(reflect.ValueOf(current))
	if !b.IsValid() || !c.IsValid() || b.Type() != c.Type() {
		return dst, fmt.Errorf("%w: baseline and current must be the same struct type", ErrDeltaType)
	}
	dt, err := deltaTypeOf(b.Type())
	if err != nil {
		return dst, err
	}
	return dt.encode(dst, addrOf(b), addrOf(c)), nil
}

// addrOf returns the address of a struct value, copying it to the heap
// when it is not addressable.
func addrOf(v reflect.Value) unsafe.Pointer {
	if v.CanAddr() {
		return v.Addr().UnsafePointer()
	}
	p := reflect.New(v.Type())
	p.Elem().Set(v)
	return p.UnsafePointer()
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
	dst.Elem().Set(b)
	return dt.apply(dst.UnsafePointer(), data)
}

// apply decodes data onto the struct at p, which already holds the
// baseline.
func (dt *deltaType) apply(p unsafe.Pointer, data []byte) error {
	r := &deltaReader{data: data}
	dt.decode(r, p)
	if r.err != nil {
		return r.err
	}
	if len(r.data) != 0 {
		return fmt.Errorf("%w: %d bytes left over", ErrDeltaData, len(r.data))
	}
	return nil
}

// deltaField is one exported field of a struct type: a scalar, an array
// of scalars or a nested struct, found at off bytes into the struct.
type deltaField struct {
	off    uintptr
	kind   reflect.Kind // the scalar kind, or the element kind of an array
	array  bool
	n      int     // array length
	size   uintptr // bytes per scalar or array element
	nested *deltaType
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
		df := deltaField{off: f.Offset, kind: f.Type.Kind(), size: f.Type.Size()}
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
			df.array, df.n = true, f.Type.Len()
			df.kind, df.size = f.Type.Elem().Kind(), f.Type.Elem().Size()
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

// sameScalar reports whether the scalars of a kind at a and b have the
// same bits, or for strings the same bytes.
func sameScalar(kind reflect.Kind, size uintptr, a, b unsafe.Pointer) bool {
	if kind == reflect.String {
		return *(*string)(a) == *(*string)(b)
	}
	switch size {
	case 1:
		return *(*uint8)(a) == *(*uint8)(b)
	case 2:
		return *(*uint16)(a) == *(*uint16)(b)
	case 4:
		return *(*uint32)(a) == *(*uint32)(b)
	}
	return *(*uint64)(a) == *(*uint64)(b)
}

// equal reports whether two values of this struct type agree on their
// exported fields.
func (dt *deltaType) equal(a, b unsafe.Pointer) bool {
	for i := range dt.fields {
		f := &dt.fields[i]
		af, bf := unsafe.Add(a, f.off), unsafe.Add(b, f.off)
		switch {
		case f.nested != nil:
			if !f.nested.equal(af, bf) {
				return false
			}
		case f.array:
			for e := range f.n {
				o := uintptr(e) * f.size
				if !sameScalar(f.kind, f.size, unsafe.Add(af, o), unsafe.Add(bf, o)) {
					return false
				}
			}
		default:
			if !sameScalar(f.kind, f.size, af, bf) {
				return false
			}
		}
	}
	return true
}

// encode appends the delta from the struct at base to the one at cur.
func (dt *deltaType) encode(buf []byte, base, cur unsafe.Pointer) []byte {
	maskAt := len(buf)
	buf = appendZeros(buf, dt.maskLen)
	for i := range dt.fields {
		f := &dt.fields[i]
		bf, cf := unsafe.Add(base, f.off), unsafe.Add(cur, f.off)
		switch {
		case f.nested != nil:
			if f.nested.equal(bf, cf) {
				continue
			}
			buf = f.nested.encode(buf, bf, cf)
		case f.array:
			var changed bool
			if buf, changed = f.encodeArray(buf, bf, cf); !changed {
				continue
			}
		default:
			if sameScalar(f.kind, f.size, bf, cf) {
				continue
			}
			buf = appendScalar(buf, f.kind, cf)
		}
		buf[maskAt+i/8] |= 1 << (i % 8)
	}
	return buf
}

// encodeArray appends an element change mask and the changed elements,
// or leaves buf as it was and reports false when nothing changed.
func (f *deltaField) encodeArray(buf []byte, base, cur unsafe.Pointer) ([]byte, bool) {
	start := len(buf)
	buf = appendZeros(buf, (f.n+7)/8)
	changed := false
	if rawKind(f.kind) {
		// The wire form is the element's memory, so compare and copy
		// whole words.
		switch f.size {
		case 1:
			buf, changed = diffRaw(buf, start, base, cur, f.n, func(b []byte, v uint8) []byte { return append(b, v) })
		case 2:
			buf, changed = diffRaw(buf, start, base, cur, f.n, binary.LittleEndian.AppendUint16)
		case 4:
			buf, changed = diffRaw(buf, start, base, cur, f.n, binary.LittleEndian.AppendUint32)
		default:
			buf, changed = diffRaw(buf, start, base, cur, f.n, binary.LittleEndian.AppendUint64)
		}
		if !changed {
			return buf[:start], false
		}
		return buf, true
	}
	for e := range f.n {
		o := uintptr(e) * f.size
		c := unsafe.Add(cur, o)
		if sameScalar(f.kind, f.size, unsafe.Add(base, o), c) {
			continue
		}
		buf[start+e/8] |= 1 << (e % 8)
		buf = appendScalar(buf, f.kind, c)
		changed = true
	}
	if !changed {
		return buf[:start], false
	}
	return buf, true
}

// rawKind reports whether a kind's wire form is exactly its memory:
// every fixed-size kind except int and uint on a 32-bit platform, which
// are widened to eight bytes. A bool's memory is 0 or 1, as on the wire.
func rawKind(k reflect.Kind) bool {
	switch k {
	case reflect.String:
		return false
	case reflect.Int, reflect.Uint:
		return strconv.IntSize == 64
	}
	return true
}

// diffRaw sets a mask bit and appends the element for every element of
// two arrays of n words that differs, and reports whether any did.
func diffRaw[T uint8 | uint16 | uint32 | uint64](buf []byte, maskAt int, base, cur unsafe.Pointer, n int, put func([]byte, T) []byte) ([]byte, bool) {
	bs, cs := unsafe.Slice((*T)(base), n), unsafe.Slice((*T)(cur), n)
	changed := false
	for e, c := range cs {
		if bs[e] != c {
			buf[maskAt+e/8] |= 1 << (e % 8)
			buf = put(buf, c)
			changed = true
		}
	}
	return buf, changed
}

func appendZeros(buf []byte, n int) []byte {
	for range n {
		buf = append(buf, 0)
	}
	return buf
}

// appendScalar appends the scalar of a kind at p: one byte for bool and
// the 8-bit kinds, two, four or eight bytes little-endian for the wider
// numbers (eight for int and uint on every platform), and a length and
// the bytes for a string.
func appendScalar(buf []byte, kind reflect.Kind, p unsafe.Pointer) []byte {
	switch kind {
	case reflect.Bool:
		if *(*bool)(p) {
			return append(buf, 1)
		}
		return append(buf, 0)
	case reflect.Int8, reflect.Uint8:
		return append(buf, *(*uint8)(p))
	case reflect.Int16, reflect.Uint16:
		return binary.LittleEndian.AppendUint16(buf, *(*uint16)(p))
	case reflect.Int32, reflect.Uint32, reflect.Float32:
		return binary.LittleEndian.AppendUint32(buf, *(*uint32)(p))
	case reflect.Int64, reflect.Uint64, reflect.Float64:
		return binary.LittleEndian.AppendUint64(buf, *(*uint64)(p))
	case reflect.Int:
		return binary.LittleEndian.AppendUint64(buf, uint64(int64(*(*int)(p))))
	case reflect.Uint:
		return binary.LittleEndian.AppendUint64(buf, uint64(*(*uint)(p)))
	case reflect.String:
		s := *(*string)(p)
		buf = binary.AppendUvarint(buf, uint64(len(s)))
		return append(buf, s...)
	}
	panic("network: unreachable delta kind " + kind.String())
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

func (dt *deltaType) decode(r *deltaReader, dst unsafe.Pointer) {
	mask := r.take(dt.maskLen)
	if mask == nil {
		return
	}
	for i := range dt.fields {
		if mask[i/8]&(1<<(i%8)) == 0 {
			continue
		}
		f := &dt.fields[i]
		p := unsafe.Add(dst, f.off)
		switch {
		case f.nested != nil:
			f.nested.decode(r, p)
		case f.array:
			f.decodeArray(r, p)
		default:
			decodeScalar(r, f.kind, p)
		}
		if r.err != nil {
			return
		}
	}
}

// decodeArray reads an element change mask and the changed elements.
func (f *deltaField) decodeArray(r *deltaReader, dst unsafe.Pointer) {
	mask := r.take((f.n + 7) / 8)
	if mask == nil {
		return
	}
	for i, m := range mask {
		for m != 0 {
			e := i*8 + bits.TrailingZeros8(m)
			m &= m - 1
			if e >= f.n {
				r.err = fmt.Errorf("%w: array element %d of %d", ErrDeltaData, e, f.n)
				return
			}
			decodeScalar(r, f.kind, unsafe.Add(dst, uintptr(e)*f.size))
			if r.err != nil {
				return
			}
		}
	}
}

// decodeScalar reads a scalar of a kind as appendScalar wrote it and
// stores it at p.
func decodeScalar(r *deltaReader, kind reflect.Kind, p unsafe.Pointer) {
	switch kind {
	case reflect.Bool:
		if b := r.take(1); b != nil {
			*(*bool)(p) = b[0] != 0
		}
	case reflect.Int8, reflect.Uint8:
		if b := r.take(1); b != nil {
			*(*uint8)(p) = b[0]
		}
	case reflect.Int16, reflect.Uint16:
		if b := r.take(2); b != nil {
			*(*uint16)(p) = binary.LittleEndian.Uint16(b)
		}
	case reflect.Int32, reflect.Uint32, reflect.Float32:
		if b := r.take(4); b != nil {
			*(*uint32)(p) = binary.LittleEndian.Uint32(b)
		}
	case reflect.Int64, reflect.Uint64, reflect.Float64:
		if b := r.take(8); b != nil {
			*(*uint64)(p) = binary.LittleEndian.Uint64(b)
		}
	case reflect.Int:
		if b := r.take(8); b != nil {
			*(*int)(p) = int(int64(binary.LittleEndian.Uint64(b)))
		}
	case reflect.Uint:
		if b := r.take(8); b != nil {
			*(*uint)(p) = uint(binary.LittleEndian.Uint64(b))
		}
	case reflect.String:
		n, k := binary.Uvarint(r.data)
		if r.err != nil {
			return
		}
		if k <= 0 || n > uint64(len(r.data)-k) {
			r.err = fmt.Errorf("%w: bad string length", ErrDeltaData)
			return
		}
		r.data = r.data[k:]
		*(*string)(p) = string(r.take(int(n)))
	}
}

// snapshotRing holds the newest snapshots sent to or received by one
// end, oldest first, in storage that is reused once it has grown.
type snapshotRing[S any] struct {
	buf     []snapshotEntry[S]
	head, n int
}

type snapshotEntry[S any] struct {
	seq  uint32
	snap S
}

// fit sizes the ring for keep entries plus the one being added, keeping
// the newest entries it already holds.
func (r *snapshotRing[S]) fit(keep int) {
	if len(r.buf) == keep+1 {
		return
	}
	buf := make([]snapshotEntry[S], keep+1)
	n := min(r.n, keep)
	for i := range n {
		buf[i] = *r.at(r.n - n + i)
	}
	r.buf, r.head, r.n = buf, 0, n
}

// at returns the i-th oldest entry.
func (r *snapshotRing[S]) at(i int) *snapshotEntry[S] {
	return &r.buf[(r.head+i)%len(r.buf)]
}

// find returns the position of the entry with a sequence, or -1.
func (r *snapshotRing[S]) find(seq uint32) int {
	for i := range r.n {
		if r.at(i).seq == seq {
			return i
		}
	}
	return -1
}

// next returns the free slot after the newest entry. fit must have left
// room for it.
func (r *snapshotRing[S]) next() *snapshotEntry[S] { return r.at(r.n) }

// commit keeps the entry written to next, then drops the oldest entries
// beyond keep.
func (r *snapshotRing[S]) commit(keep int) {
	r.n++
	if r.n > keep {
		r.drop(r.n - keep)
	}
}

// drop forgets the k oldest entries.
func (r *snapshotRing[S]) drop(k int) {
	var zero S
	for i := range k {
		r.at(i).snap = zero // release anything the snapshot refers to
	}
	r.head = (r.head + k) % len(r.buf)
	r.n -= k
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
	zero    *S     // the baseline before any acknowledgement
	scratch []byte // Encode's buffer, copied out for the caller
}

type snapshotClient[S any] struct {
	sent  snapshotRing[S]
	acked uint32
}

// Encode encodes a client's snapshot against the newest one it has
// acknowledged, or the zero S when it has acknowledged none the buffer
// still holds, and remembers it under seq (which must be nonzero and
// increase). It returns the baseline's sequence, 0 for the zero S, and
// the delta; send both with seq to the client for SnapshotReceiver. The
// delta is a fresh slice; to reuse one buffer for every client and
// tick, call AppendEncode.
func (b *SnapshotBuffer[K, S]) Encode(client K, seq uint32, snap S) (base uint32, data []byte, err error) {
	base, b.scratch, err = b.AppendEncode(b.scratch[:0], client, seq, snap)
	if err != nil {
		return 0, nil, err
	}
	return base, append([]byte(nil), b.scratch...), nil
}

// AppendEncode encodes as Encode does and appends the delta to dst,
// returning the extended slice. To encode without allocating once the
// buffer has grown, pass the slice the last call returned, truncated to
// zero length, and send it before the next call.
func (b *SnapshotBuffer[K, S]) AppendEncode(dst []byte, client K, seq uint32, snap S) (base uint32, data []byte, err error) {
	if seq == 0 {
		return 0, dst, fmt.Errorf("%w: snapshot sequence 0 is reserved", ErrDeltaData)
	}
	dt, err := deltaTypeOf(reflect.TypeFor[S]())
	if err != nil {
		return 0, dst, err
	}
	if b.clients == nil {
		b.clients = map[K]*snapshotClient[S]{}
	}
	if b.zero == nil {
		b.zero = new(S)
	}
	c := b.clients[client]
	if c == nil {
		c = &snapshotClient[S]{}
		b.clients[client] = c
	}
	keep := keepOrDefault(b.Keep)
	c.sent.fit(keep)
	baseline := b.zero
	if c.acked != 0 {
		if i := c.sent.find(c.acked); i >= 0 {
			e := c.sent.at(i)
			baseline, base = &e.snap, e.seq
		}
	}
	slot := c.sent.next()
	slot.seq, slot.snap = seq, snap
	data = dt.encode(dst, unsafe.Pointer(baseline), unsafe.Pointer(&slot.snap))
	c.sent.commit(keep)
	return base, data, nil
}

func keepOrDefault(keep int) int {
	if keep <= 0 {
		return 32
	}
	return keep
}

// Ack records that a client received the snapshot with sequence seq;
// later snapshots for it are encoded against that one. An unknown seq
// is ignored.
func (b *SnapshotBuffer[K, S]) Ack(client K, seq uint32) {
	c := b.clients[client]
	if c == nil {
		return
	}
	if i := c.sent.find(seq); i >= 0 {
		c.acked = seq
		c.sent.drop(i) // nothing older will be a baseline again
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
	got  snapshotRing[S]
}

// Decode applies a delta to the snapshot with sequence base (0 for the
// zero S), remembers the result under seq, and returns it. Acknowledge
// seq to the server afterwards. A baseline no longer held is
// ErrDeltaData; the fix is to acknowledge nothing until a snapshot
// against a baseline the receiver has arrives.
func (r *SnapshotReceiver[S]) Decode(base, seq uint32, data []byte) (S, error) {
	var zero S
	dt, err := deltaTypeOf(reflect.TypeFor[S]())
	if err != nil {
		return zero, err
	}
	keep := keepOrDefault(r.Keep)
	r.got.fit(keep)
	slot := r.got.next()
	if base == 0 {
		slot.snap = zero
	} else {
		i := r.got.find(base)
		if i < 0 {
			return zero, fmt.Errorf("%w: baseline snapshot %d not held", ErrDeltaData, base)
		}
		slot.snap = r.got.at(i).snap
	}
	slot.seq = seq
	if err := dt.apply(unsafe.Pointer(&slot.snap), data); err != nil {
		snap := slot.snap
		slot.snap = zero
		return snap, err
	}
	r.got.commit(keep)
	return slot.snap, nil
}
