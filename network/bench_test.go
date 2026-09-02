package network

import (
	"testing"
	"time"
)

func BenchmarkRegistryEncode(b *testing.B) {
	r := registry()
	m := move{3, 4}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.encode(m); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRegistryEncodeBinary(b *testing.B) {
	r := registry()
	m := pos{1.5, 2.5}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.encode(m); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRegistryDecode(b *testing.B) {
	r := registry()
	data, err := r.encode(move{3, 4})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.decode(data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUDPRoundTrip sends one unreliable message over loopback and
// waits for it to arrive, so the measurement covers encode, transmit,
// receive, decode and the event queue.
func BenchmarkUDPRoundTrip(b *testing.B) {
	reg := registry()
	a, err := ListenUDP("127.0.0.1:0", reg)
	if err != nil {
		b.Fatal(err)
	}
	defer a.Close()
	c, err := ListenUDP("127.0.0.1:0", reg)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	to := c.Addr()
	// Let the link come up, and drop the Connected events.
	if err := a.Connect(to); err != nil {
		b.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(c.Poll()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	a.Poll()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if err := a.Send(to, move{i, i}); err != nil {
			b.Fatal(err)
		}
		got := false
		for end := time.Now().Add(time.Second); !got && time.Now().Before(end); {
			for _, ev := range c.Poll() {
				if ev.Kind == Message {
					got = true
				}
			}
		}
		if !got {
			b.Fatal("message never arrived")
		}
	}
	b.StopTimer()
	a.Poll()
	c.Poll()
}

// BenchmarkInterestEnd runs a frame of interest management with a
// steady set: nothing enters or leaves, which is the common case.
func BenchmarkInterestEnd(b *testing.B) {
	const n = 500
	in := Interest[int]{Radius: 100}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		in.Begin(0, 0)
		for i := range n {
			in.Visit(i, float32(i%50), float32(i/50))
		}
		in.End()
	}
}

// BenchmarkInterestEndChurn moves a tenth of the set in and out each
// frame, so both returned slices have entries.
func BenchmarkInterestEndChurn(b *testing.B) {
	const n = 500
	in := Interest[int]{Radius: 100}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		in.Begin(0, 0)
		for j := range n {
			x := float32(j % 50)
			if j%10 == i%10 {
				x = 1000 // out of range this frame
			}
			in.Visit(j, x, float32(j/50))
		}
		in.End()
	}
}
