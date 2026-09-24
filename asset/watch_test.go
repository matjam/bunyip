package asset

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// TestWatcherChangedDoesNotWaitForPoll polls a thousand files through a
// slow stat, so each poll takes tens of milliseconds, and checks Changed,
// which a game calls every frame, never waits for a poll to finish.
func TestWatcherChangedDoesNotWaitForPoll(t *testing.T) {
	files := map[string]string{}
	var names []string
	for i := range 1000 {
		name := fmt.Sprintf("art/d%02d/f%04d.png", i%20, i)
		files[name] = "x"
		names = append(names, name)
	}
	fsys, err := Open(tree(t, files))
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	slow := func(name string) (os.FileInfo, error) {
		time.Sleep(20 * time.Microsecond)
		return os.Stat(name)
	}
	w := newWatcher(fsys, time.Hour, slow)
	defer w.Close()
	w.Add(names...)

	var polls []time.Duration
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Go(func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			start := time.Now()
			w.poll()
			polls = append(polls, time.Since(start))
		}
	})
	var worst time.Duration
	for end := time.Now().Add(400 * time.Millisecond); time.Now().Before(end); {
		start := time.Now()
		w.Changed()
		worst = max(worst, time.Since(start))
		time.Sleep(200 * time.Microsecond)
	}
	close(done)
	wg.Wait()
	if len(polls) == 0 || slices.Max(polls) < 20*time.Millisecond {
		t.Fatalf("polls took %v; the test needs polls slower than 20ms to mean anything", polls)
	}
	if worst > 5*time.Millisecond {
		t.Errorf("Changed took up to %v while polls of %v ran, want under 5ms", worst, slices.Max(polls))
	}
}

// TestWatcherOverlay checks a watched name follows the source it
// resolves to: a copy that appears in an overlaying directory, in a
// subdirectory that did not exist before, is reported, and so is the
// fallback to the base copy when the overlay's copy is removed, and a
// watched name that did not exist anywhere when it was added.
func TestWatcherOverlay(t *testing.T) {
	base := tree(t, map[string]string{"art/a.png": "base"})
	mod := t.TempDir()
	fsys, err := Open(mod, base)
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	w := newWatcher(fsys, time.Hour, os.Stat)
	defer w.Close()
	w.Add("art/a.png", "late.png")
	expect := func(want ...string) {
		t.Helper()
		w.poll()
		got := w.Changed()
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("changed %v, want %v", got, want)
		}
	}
	expect()

	over := filepath.Join(mod, "art", "a.png")
	if err := os.MkdirAll(filepath.Dir(over), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, over, "mod", time.Hour)
	expect("art/a.png")
	write(t, over, "mod again", 2*time.Hour)
	expect("art/a.png")

	write(t, filepath.Join(base, "late.png"), "late", 0)
	expect("late.png")

	if err := os.Remove(over); err != nil {
		t.Fatal(err)
	}
	expect("art/a.png")
	write(t, filepath.Join(base, "art", "a.png"), "base again", 3*time.Hour)
	expect("art/a.png")
	expect()
}
