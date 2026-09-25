package save

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type counter struct {
	N    int
	Tags []string
}

// TestWriteAsyncOrder queues many writes to one slot at once and checks
// that they land in call order: a read waits for them and sees the last,
// and every write reports success.
func TestWriteAsyncOrder(t *testing.T) {
	s, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for round := range 5 {
		var results []<-chan error
		for i := range 40 {
			results = append(results, s.WriteAsync("slot", counter{N: round*100 + i}))
		}
		var got counter
		if err := s.Read("slot", &got); err != nil {
			t.Fatal(err)
		}
		if want := round*100 + 39; got.N != want {
			t.Fatalf("round %d: read %d after the queued writes, want %d", round, got.N, want)
		}
		for i, r := range results {
			if err := <-r; err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}
	}
	// A synchronous write queues behind pending asynchronous ones.
	for i := range 20 {
		s.WriteAsync("slot", counter{N: i})
	}
	if err := s.Write("slot", counter{N: 1000}); err != nil {
		t.Fatal(err)
	}
	var got counter
	if err := s.Read("slot", &got); err != nil || got.N != 1000 {
		t.Fatalf("after Write: %+v %v", got, err)
	}
	assertNoTemp(t, s)
}

// TestWriteAsyncCapturesValue checks that the value is encoded before
// WriteAsync returns, so the game may change it at once.
func TestWriteAsyncCapturesValue(t *testing.T) {
	s, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v := counter{N: 1, Tags: []string{"a"}}
	done := s.WriteAsync("slot", &v)
	v.N, v.Tags[0] = 2, "b"
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var got counter
	if err := s.Read("slot", &got); err != nil || got.N != 1 || got.Tags[0] != "a" {
		t.Fatalf("read %+v %v, want the value at the call", got, err)
	}
}

func TestWriteAsyncErrors(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-s.WriteAsync("../x", counter{}); err == nil {
		t.Error("invalid name accepted")
	}
	if err := <-s.WriteAsync("slot", make(chan int)); err == nil {
		t.Error("unencodable value accepted")
	}
	if s.Exists("slot") {
		t.Error("a failed encode wrote a file")
	}
	if err := <-s.WriteAsync("slot", counter{N: 7}); err != nil {
		t.Fatal(err)
	}
	// A write that fails on disk reports its error and leaves the previous
	// file in place.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	if f, err := os.CreateTemp(dir, "probe"); err == nil {
		f.Close()
		os.Remove(f.Name())
		t.Skip("the directory stays writable (running as root?)")
	}
	failed := s.WriteAsync("slot", counter{N: 8})
	if err := <-failed; err == nil {
		t.Fatal("write into a read-only directory succeeded")
	}
	var got counter
	if err := s.Read("slot", &got); err != nil || got.N != 7 {
		t.Fatalf("after a failed write: %+v %v, want the previous file", got, err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Write("slot", counter{N: 9}); err != nil {
		t.Fatal("the slot did not recover:", err)
	}
	assertNoTemp(t, s)
}

// TestFlushWaitsForEveryWrite writes to many slots from many goroutines,
// then checks Flush leaves nothing pending and every file holds its
// last value.
func TestFlushWaitsForEveryWrite(t *testing.T) {
	s, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 25 {
				s.WriteAsync(fmt.Sprintf("slot%d", g), counter{N: i})
			}
		})
	}
	wg.Wait()
	s.Flush()
	if n := len(s.pending("", true)); n != 0 {
		t.Fatalf("%d writes pending after Flush", n)
	}
	for g := range 8 {
		var got counter
		if err := s.Read(fmt.Sprintf("slot%d", g), &got); err != nil || got.N != 24 {
			t.Fatalf("slot%d: %+v %v", g, got, err)
		}
	}
	// Delete waits for the pending write it would otherwise race.
	s.WriteAsync("gone", counter{N: 1})
	if err := s.Delete("gone"); err != nil {
		t.Fatal(err)
	}
	s.Flush()
	if s.Exists("gone") {
		t.Error("a pending write landed after Delete")
	}
	assertNoTemp(t, s)
}

// assertNoTemp checks that no temporary file is left behind.
func assertNoTemp(t *testing.T, s *Store) {
	t.Helper()
	s.Flush()
	left, err := filepath.Glob(filepath.Join(s.Path(), "*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}
	if _, err := os.Stat(s.Path()); errors.Is(err, os.ErrNotExist) {
		t.Error("store directory vanished")
	}
}
