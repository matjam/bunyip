package rng

import (
	"math"
	"testing"
)

func TestDeterministic(t *testing.T) {
	a, b := New(42), New(42)
	for range 100 {
		if a.Uint32() != b.Uint32() {
			t.Fatal("same seed diverged")
		}
	}
	// Known PCG32 output for seed 42 on the reference stream 54.
	r := NewStream(42, 54)
	want := []uint32{0xa15c02b7, 0x7b47f409, 0xba1d3330, 0x83d2f293, 0xbfa4784b, 0xcbed606e}
	for i, w := range want {
		if got := r.Uint32(); got != w {
			t.Fatalf("output %d = %#x, want %#x", i, got, w)
		}
	}
}

func TestBoundsAndDistribution(t *testing.T) {
	r := New(7)
	counts := make([]int, 6)
	for range 60000 {
		v := r.Intn(6)
		if v < 0 || v >= 6 {
			t.Fatal("Intn out of range")
		}
		counts[v]++
	}
	for _, c := range counts {
		if c < 9500 || c > 10500 {
			t.Fatalf("uneven distribution %v", counts)
		}
	}
	for range 1000 {
		if f := r.Float(); f < 0 || f >= 1 {
			t.Fatal("Float out of range")
		}
		if v := r.Range(-3, 3); v < -3 || v > 3 {
			t.Fatal("Range out of range")
		}
		if v := r.Roll(2, 6); v < 2 || v > 12 {
			t.Fatal("Roll out of range")
		}
	}
	var sum float64
	const n = 20000
	for range n {
		sum += float64(r.Normal(10, 2))
	}
	if mean := sum / n; math.Abs(mean-10) > 0.1 {
		t.Fatalf("Normal mean %.3f", mean)
	}
}

func TestForkStateAndHelpers(t *testing.T) {
	r := New(1)
	child := r.Fork()
	s, inc := r.State()
	next := r.Uint32()
	r.Restore(s, inc)
	if r.Uint32() != next {
		t.Fatal("Restore did not replay")
	}
	if child.Uint32() == next {
		t.Fatal("fork shares the parent's sequence")
	}
	items := []int{0, 1, 2, 3, 4, 5, 6, 7}
	r.Shuffle(items)
	seen := map[int]bool{}
	for _, v := range items {
		seen[v] = true
	}
	if len(seen) != 8 {
		t.Fatal("shuffle lost elements")
	}
	if p := r.Pick(items); p < 0 || p > 7 {
		t.Fatal("Pick out of range")
	}
	hits := make([]int, 3)
	for range 3000 {
		hits[WeightedIndex(r, []float32{1, 0, 3})]++
	}
	if hits[1] != 0 || hits[2] < hits[0]*2 {
		t.Fatalf("weighted picks %v", hits)
	}
	if WeightedIndex(r, []float32{0, 0}) != -1 {
		t.Fatal("expected -1 with no weight")
	}
}
