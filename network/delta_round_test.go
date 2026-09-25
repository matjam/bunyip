package network

import (
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

type deltaInner struct {
	On    bool
	Code  int8
	Flags [11]bool
	Deep  deltaDeep
	Tags  [3]string
}

type deltaDeep struct {
	Level uint16
	Hist  [9]int64
}

// deltaWide has every supported kind, arrays of each, nested structs,
// an empty array, unexported fields and more than eight fields, so the
// masks take several bytes.
type deltaWide struct {
	B      bool
	I      int
	I8     int8
	I16    int16
	I32    int32
	I64    int64
	U      uint
	U8     uint8
	U16    uint16
	U32    uint32
	U64    uint64
	F32    float32
	F64    float64
	S      string
	hidden [4]int
	AB     [17]bool
	AI     [5]int
	AI8    [8]int8
	AI16   [9]int16
	AI32   [3]int32
	AI64   [2]int64
	AU     [4]uint
	AU8    [33]uint8
	AU16   [2]uint16
	AU32   [7]uint32
	AU64   [1]uint64
	AF32   [200]float32
	AF64   [6]float64
	AS     [4]string
	Empty  [0]int32
	In     deltaInner
	Tail   uint8
	secret string
}

// randomScalar sets v, a scalar of a supported kind, to a random value,
// including the float values with unusual bits.
func randomScalar(rng *rand.Rand, v reflect.Value) {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(rng.IntN(2) == 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(rng.Uint64()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(rng.Uint64())
	case reflect.Float32, reflect.Float64:
		specials := []float64{0, math.Copysign(0, -1), math.NaN(), math.Inf(1), math.Inf(-1), 1e-310}
		if rng.IntN(3) == 0 {
			v.SetFloat(specials[rng.IntN(len(specials))])
		} else {
			v.SetFloat(rng.NormFloat64() * 1000)
		}
	case reflect.String:
		b := make([]byte, rng.IntN(20))
		for i := range b {
			b[i] = byte(rng.IntN(256))
		}
		v.SetString(string(b))
	}
}

// mutate changes each exported scalar and array element of the struct v
// with probability p.
func mutate(rng *rand.Rand, v reflect.Value, p float64) {
	for i := range v.NumField() {
		if !v.Type().Field(i).IsExported() {
			continue
		}
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Struct:
			mutate(rng, f, p)
		case reflect.Array:
			for e := range f.Len() {
				if rng.Float64() < p {
					randomScalar(rng, f.Index(e))
				}
			}
		default:
			if rng.Float64() < p {
				randomScalar(rng, f)
			}
		}
	}
}

// sameBits compares two values field by field, floats by their bits.
func sameBits(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Struct:
		for i := range a.NumField() {
			if !a.Type().Field(i).IsExported() {
				continue // callers check unexported fields themselves
			}
			if !sameBits(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Array:
		for i := range a.Len() {
			if !sameBits(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Float32:
		return math.Float32bits(float32(a.Float())) == math.Float32bits(float32(b.Float()))
	case reflect.Float64:
		return math.Float64bits(a.Float()) == math.Float64bits(b.Float())
	}
	return a.Equal(b)
}

func TestDeltaRoundTripRandom(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	for trial := range 2000 {
		var base, cur deltaWide
		mutate(rng, reflect.ValueOf(&base).Elem(), 0.5)
		base.hidden[1], base.secret = 5, "base"
		cur = base
		cur.hidden[1], cur.secret = 9, "cur"
		// Some trials change nothing, some a few values, some most.
		p := []float64{0, 0.01, 0.1, 0.9}[trial%4]
		mutate(rng, reflect.ValueOf(&cur).Elem(), p)
		var data []byte
		var err error
		switch trial % 3 {
		case 0:
			data, err = EncodeDelta(base, cur)
		case 1:
			data, err = EncodeDelta(&base, &cur)
		default:
			data, err = AppendDelta([]byte{0xAA, 0xBB}, &base, &cur)
			if err == nil {
				if data[0] != 0xAA || data[1] != 0xBB {
					t.Fatalf("trial %d: AppendDelta changed the prefix", trial)
				}
				data = data[2:]
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		var out deltaWide
		out.secret = "untouched"
		if err := DecodeDelta(&base, data, &out); err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		if !sameBits(reflect.ValueOf(out), reflect.ValueOf(cur)) {
			t.Fatalf("trial %d (p %v): decoded snapshot differs from the one encoded", trial, p)
		}
		// Unexported fields come from the baseline.
		if out.hidden != base.hidden || out.secret != base.secret {
			t.Fatalf("trial %d: unexported fields %v %q, want the baseline's", trial, out.hidden, out.secret)
		}
		if p == 0 && len(data) != (len(deltaTypeFor[deltaWide](t).fields)+7)/8 {
			t.Fatalf("trial %d: an unchanged snapshot is %d bytes, want the mask alone", trial, len(data))
		}
		// Every truncation is refused, and so is a trailing byte.
		for n := range len(data) {
			if err := DecodeDelta(&base, data[:n], &out); !errors.Is(err, ErrDeltaData) {
				t.Fatalf("trial %d: %d of %d bytes decoded with %v", trial, n, len(data), err)
			}
		}
		if err := DecodeDelta(&base, append(data[:len(data):len(data)], 0), &out); !errors.Is(err, ErrDeltaData) {
			t.Fatalf("trial %d: a trailing byte decoded with %v", trial, err)
		}
	}
}

func deltaTypeFor[S any](t *testing.T) *deltaType {
	t.Helper()
	dt, err := deltaTypeOf(reflect.TypeFor[S]())
	if err != nil {
		t.Fatal(err)
	}
	return dt
}

// TestDeltaArrayElements checks that an array sends only the elements
// that changed, and that signed zero and NaN are compared by their bits.
func TestDeltaArrayElements(t *testing.T) {
	type s struct {
		A [20]float32
		B float64
	}
	var base s
	base.B = math.NaN()
	cur := base
	cur.A[3], cur.A[19] = 1, float32(math.Copysign(0, -1))
	data, err := EncodeDelta(&base, &cur)
	if err != nil {
		t.Fatal(err)
	}
	// Field mask, A's three-byte element mask and two values; the NaN
	// has the same bits and is not sent.
	if len(data) != 1+3+2*4 {
		t.Fatalf("delta is %d bytes: %x", len(data), data)
	}
	var out s
	if err := DecodeDelta(&base, data, &out); err != nil {
		t.Fatal(err)
	}
	if !sameBits(reflect.ValueOf(out), reflect.ValueOf(cur)) {
		t.Fatalf("decoded %+v, want %+v", out, cur)
	}
	// An element bit past the end of the array is refused.
	bad := []byte{1, 0, 0, 0x10}
	if err := DecodeDelta(&base, bad, &out); !errors.Is(err, ErrDeltaData) {
		t.Errorf("element 20 of 20 decoded with %v", err)
	}
}

// TestSnapshotStreamRandom sends a stream through SnapshotBuffer and
// SnapshotReceiver with lost packets, late and missing acknowledgements
// and changes to Keep, and checks every received snapshot exactly.
func TestSnapshotStreamRandom(t *testing.T) {
	rng := rand.New(rand.NewPCG(21, 22))
	var server SnapshotBuffer[string, deltaWide]
	clients := map[string]*SnapshotReceiver[deltaWide]{"a": {}, "b": {Keep: 4}, "c": {Keep: 64}}
	var cur deltaWide
	type pending struct {
		client string
		seq    uint32
	}
	var acks []pending
	var buf []byte
	for seq := uint32(1); seq <= 600; seq++ {
		mutate(rng, reflect.ValueOf(&cur).Elem(), 0.05)
		if seq%150 == 0 {
			server.Keep = []int{0, 3, 40}[seq/150%3]
		}
		for _, name := range []string{"a", "b", "c"} {
			var base uint32
			var data []byte
			var err error
			if seq%2 == 0 {
				base, data, err = server.Encode(name, seq, cur)
			} else {
				base, buf, err = server.AppendEncode(buf[:0], name, seq, cur)
				data = buf
			}
			if err != nil {
				t.Fatal(err)
			}
			if rng.IntN(5) == 0 {
				continue // lost on the way
			}
			got, err := clients[name].Decode(base, seq, data)
			if err != nil {
				// The receiver no longer holds the baseline; it waits
				// for a snapshot it can decode, acknowledging nothing.
				if !errors.Is(err, ErrDeltaData) {
					t.Fatal(err)
				}
				continue
			}
			if !sameBits(reflect.ValueOf(got), reflect.ValueOf(cur)) {
				t.Fatalf("seq %d client %s: decoded snapshot differs", seq, name)
			}
			if rng.IntN(4) != 0 {
				acks = append(acks, pending{name, seq})
			}
		}
		// Acknowledgements arrive late and out of order.
		rng.Shuffle(len(acks), func(i, j int) { acks[i], acks[j] = acks[j], acks[i] })
		for len(acks) > 0 && rng.IntN(3) != 0 {
			a := acks[len(acks)-1]
			acks = acks[:len(acks)-1]
			server.Ack(a.client, a.seq)
		}
	}
}

// snapshotScene is the 200-entity scene the network benchmarks use.
func snapshotScene() (*benchSnap, func(seq uint32)) {
	s := &benchSnap{}
	for i := range s.ID {
		s.ID[i] = uint32(i)
		s.HP[i] = 100
	}
	return s, func(seq uint32) {
		s.Tick = seq
		for e := int(seq) % 10; e < len(s.X); e += 10 {
			s.X[e] += 0.1
			s.Y[e] -= 0.05
			s.Angle[e] = float32(math.Mod(float64(s.Angle[e])+0.01, 6.28))
		}
	}
}

// TestSnapshotFitsDatagram checks the scene from the benchmarks, a tenth
// of 200 entities moving each tick and acknowledgements two ticks behind:
// each delta must fit one datagram with room for the message around it,
// and encoding must allocate at most the returned slice.
func TestSnapshotFitsDatagram(t *testing.T) {
	var buf SnapshotBuffer[int, benchSnap]
	var rx SnapshotReceiver[benchSnap]
	s, step := snapshotScene()
	var out []byte
	for seq := uint32(1); seq <= 100; seq++ {
		step(seq)
		base, data, err := buf.Encode(0, seq, *s)
		if err != nil {
			t.Fatal(err)
		}
		got, err := rx.Decode(base, seq, data)
		if err != nil || got != *s {
			t.Fatalf("seq %d: decode %v", seq, err)
		}
		// A message carrying the delta also has the packet header, the
		// message type and the two sequence numbers.
		if seq > 3 && headerSize+2+8+len(data) > MaxDatagram {
			t.Fatalf("seq %d: delta of %d bytes does not fit a datagram", seq, len(data))
		}
		if seq > 2 {
			buf.Ack(0, seq-2)
		}
		if seq > 50 {
			snap := *s
			if n := testing.AllocsPerRun(1, func() { buf.Encode(1, seq, snap) }); n > 1 {
				t.Errorf("Encode allocates %v times, want at most 1", n)
			}
			if n := testing.AllocsPerRun(1, func() { _, out, _ = buf.AppendEncode(out[:0], 2, seq, snap) }); n > 0 && seq > 60 {
				t.Errorf("AppendEncode allocates %v times, want 0", n)
			}
		}
	}
}
