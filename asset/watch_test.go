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
	// Waiting for a poll would take the whole poll; a quarter of the
	// slowest one, and never less than 5ms, leaves room for a busy
	// machine to deschedule the call.
	bound := max(5*time.Millisecond, slices.Max(polls)/4)
	if worst > bound {
		t.Errorf("Changed took up to %v while polls of up to %v ran, want under %v", worst, slices.Max(polls), bound)
	}
}

// TestWatcherNamesAChangeOnce changes a file several times between two
// calls to Changed, with a poll after each change, as an editor that
// truncates, writes and then sets the time does. Changed names the file
// once, and names it again after the next change.
func TestWatcherNamesAChangeOnce(t *testing.T) {
	dir := tree(t, map[string]string{"a.txt": "one", "b.txt": "one"})
	fsys, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fsys.Close()
	w := newWatcher(fsys, time.Hour, os.Stat)
	defer w.Close()
	w.Add("a.txt", "b.txt")
	w.poll()
	if got := w.Changed(); len(got) != 0 {
		t.Fatalf("changed %v before any write", got)
	}
	write(t, filepath.Join(dir, "a.txt"), "two", time.Hour)
	w.poll()
	write(t, filepath.Join(dir, "b.txt"), "two", time.Hour)
	w.poll()
	write(t, filepath.Join(dir, "a.txt"), "three", 2*time.Hour)
	w.poll()
	if got, want := w.Changed(), []string{"a.txt", "b.txt"}; !slices.Equal(got, want) {
		t.Fatalf("changed %v, want %v", got, want)
	}
	write(t, filepath.Join(dir, "a.txt"), "four", 3*time.Hour)
	w.poll()
	if got, want := w.Changed(), []string{"a.txt"}; !slices.Equal(got, want) {
		t.Fatalf("after the next write, changed %v, want %v", got, want)
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
