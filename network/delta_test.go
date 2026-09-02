package network

import (
	"errors"
	"testing"
)

type vitals struct {
	HP    int16
	Alive bool
}

type snap struct {
	X, Y   float32
	Name   string
	Vitals vitals
	Ammo   [3]uint8
	Score  int
	hidden int
}

func TestDelta(t *testing.T) {
	base := snap{X: 1, Y: 2, Name: "ann", Vitals: vitals{HP: 100, Alive: true}, Ammo: [3]uint8{1, 2, 3}, Score: -5, hidden: 7}
	cur := base
	cur.X = 1.5
	cur.Vitals.HP = 90
	cur.Ammo[2] = 0
	cur.hidden = 9
	data, err := EncodeDelta(base, &cur)
	if err != nil {
		t.Fatal(err)
	}
	// mask + X (4) + nested mask + HP (2) + Ammo (3)
	if len(data) != 1+4+1+2+3 {
		t.Errorf("delta is %d bytes: %x", len(data), data)
	}
	var out snap
	if err := DecodeDelta(&base, data, &out); err != nil {
		t.Fatal(err)
	}
	want := cur
	want.hidden = base.hidden // unexported fields come from the baseline
	if out != want {
		t.Errorf("decoded %+v, want %+v", out, want)
	}

	same, err := EncodeDelta(cur, cur)
	if err != nil {
		t.Fatal(err)
	}
	if len(same) != 1 {
		t.Errorf("unchanged snapshot is %d bytes, want 1", len(same))
	}
	if err := DecodeDelta(&cur, same, &out); err != nil || out != cur {
		t.Errorf("unchanged round trip: %v %+v", err, out)
	}

	if err := DecodeDelta(&base, data[:3], &out); !errors.Is(err, ErrDeltaData) {
		t.Errorf("truncated data: %v", err)
	}
	if _, err := EncodeDelta(base, vitals{}); !errors.Is(err, ErrDeltaType) {
		t.Errorf("mismatched types: %v", err)
	}
	type bad struct{ Items []int }
	if _, err := EncodeDelta(bad{}, bad{}); !errors.Is(err, ErrDeltaType) {
		t.Errorf("slice field: %v", err)
	}
}

func TestSnapshotBuffer(t *testing.T) {
	var server SnapshotBuffer[int, snap]
	var client SnapshotReceiver[snap]
	s1 := snap{X: 10, Name: "ann", Vitals: vitals{HP: 100, Alive: true}}
	base, data, err := server.Encode(7, 1, s1)
	if err != nil || base != 0 {
		t.Fatalf("first encode: base %d err %v", base, err)
	}
	full := len(data)
	got, err := client.Decode(base, 1, data)
	if err != nil || got != s1 {
		t.Fatalf("first decode: %+v %v", got, err)
	}
	// Until the client acknowledges, snapshots stay against the zero value.
	s2 := s1
	s2.X = 11
	if base, data, _ = server.Encode(7, 2, s2); base != 0 || len(data) < full {
		t.Fatalf("unacknowledged: base %d, %d bytes", base, len(data))
	}
	client.Decode(base, 2, data)
	server.Ack(7, 2)
	s3 := s2
	s3.X = 12
	base, data, _ = server.Encode(7, 3, s3)
	if base != 2 || len(data) != 1+4 {
		t.Fatalf("against seq 2: base %d, %d bytes", base, len(data))
	}
	if got, err = client.Decode(base, 3, data); err != nil || got != s3 {
		t.Fatalf("delta decode: %+v %v", got, err)
	}
	// An old acknowledgement still finds its baseline; an unknown one is ignored.
	server.Ack(7, 99)
	if base, _, _ = server.Encode(7, 4, s3); base != 2 {
		t.Errorf("unknown ack changed the baseline to %d", base)
	}
	if _, err := client.Decode(50, 5, data); !errors.Is(err, ErrDeltaData) {
		t.Errorf("missing baseline: %v", err)
	}
	server.Forget(7)
	if base, _, _ = server.Encode(7, 6, s3); base != 0 {
		t.Errorf("forgotten client kept baseline %d", base)
	}
}
