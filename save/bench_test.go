package save

// Benchmarks for the cost of an atomic save, which a game may call from
// its loop, split into encoding and the synced write.

import (
	"encoding/json"
	"testing"
)

type benchState struct {
	Name      string
	Level     int
	Inventory []benchItem
	Flags     map[string]bool
}

type benchItem struct {
	ID    int
	Name  string
	Count int
	X, Y  float64
}

func benchSave(items int) benchState {
	s := benchState{Name: "hero", Level: 12, Flags: map[string]bool{}}
	for i := range items {
		s.Inventory = append(s.Inventory, benchItem{ID: i, Name: "item", Count: i % 9, X: float64(i), Y: float64(i) / 3})
		if i%10 == 0 {
			s.Flags["flag"+string(rune('a'+i%26))+string(rune('a'+i/26%26))] = true
		}
	}
	return s
}

// BenchmarkWrite writes a small (100 items, about 10 KB) and a large
// (5000 items, about 500 KB) save through Store.Write.
func BenchmarkWrite(b *testing.B) {
	for _, n := range []int{100, 5000} {
		st := benchSave(n)
		data, _ := json.MarshalIndent(st, "", "  ")
		b.Run(map[int]string{100: "small", 5000: "large"}[n], func(b *testing.B) {
			s, err := OpenAt(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := s.Write("slot1", st); err != nil {
					b.Fatal(err)
				}
			}
		})
		// The caller's share of an asynchronous save: encoding and
		// queueing. The write itself is waited for outside the timer.
		b.Run(map[int]string{100: "small", 5000: "large"}[n]+"_async", func(b *testing.B) {
			s, err := OpenAt(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				done := s.WriteAsync("slot1", st)
				b.StopTimer()
				if err := <-done; err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
		b.Run(map[int]string{100: "small", 5000: "large"}[n]+"_encode_only", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := json.MarshalIndent(st, "", "  "); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
