package asset

// Load benchmarks: one Watcher poll over 1000 loose files, opening a pack
// of 10000 entries, and reading one file from it.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func loadTree(b *testing.B, n int) (string, []string) {
	dir := b.TempDir()
	names := make([]string, n)
	for i := range n {
		name := fmt.Sprintf("art/d%02d/f%04d.png", i%20, i)
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			b.Fatal(err)
		}
		names[i] = name
	}
	return dir, names
}

// BenchmarkWatcherPoll is one poll of 1000 watched loose files, the work
// the watcher's goroutine does every interval (twice a second by
// default).
func BenchmarkWatcherPoll(b *testing.B) {
	dir, names := loadTree(b, 1000)
	for _, layered := range []bool{false, true} {
		srcs := []string{dir}
		if layered {
			srcs = []string{b.TempDir(), dir} // a mod directory over the base
		}
		fsys, err := Open(srcs...)
		if err != nil {
			b.Fatal(err)
		}
		w := NewWatcher(fsys, time.Hour)
		w.Add(names...)
		b.Run(fmt.Sprintf("sources%d", len(srcs)), func(b *testing.B) {
			for b.Loop() {
				w.poll()
			}
		})
		w.Close()
		fsys.Close()
	}
	// The floor: one os.Stat per file, what a poll needs at minimum.
	b.Run("baseline_stat", func(b *testing.B) {
		for b.Loop() {
			for _, n := range names {
				os.Stat(filepath.Join(dir, n))
			}
		}
	})
}

// BenchmarkWatcherChanged is Changed, the call Reloader.Reload makes every
// frame, while the watcher polls 1000 files in the background; the worst
// call is reported as max-ms, the stall a frame sees when it lands on a
// poll.
func BenchmarkWatcherChanged(b *testing.B) {
	dir, names := loadTree(b, 1000)
	fsys, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer fsys.Close()
	w := NewWatcher(fsys, 100*time.Millisecond)
	defer w.Close()
	w.Add(names...)
	var worst time.Duration
	for b.Loop() {
		start := time.Now()
		w.Changed()
		worst = max(worst, time.Since(start))
		time.Sleep(time.Millisecond)
	}
	b.ReportMetric(float64(worst.Microseconds())/1000, "max-ms")
}

func loadPack(b *testing.B, n int) (string, []string) {
	dir, names := loadTree(b, n)
	out := filepath.Join(b.TempDir(), "game.pack")
	if err := Pack(dir, out); err != nil {
		b.Fatal(err)
	}
	return out, names
}

// BenchmarkPack opens a pack of 10000 entries (the zip central directory
// and the name index) and reads one small file from it.
func BenchmarkPack(b *testing.B) {
	pack, names := loadPack(b, 10000)
	b.Run("open", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			fsys, err := Open(pack)
			if err != nil {
				b.Fatal(err)
			}
			fsys.Close()
		}
	})
	fsys, err := Open(pack)
	if err != nil {
		b.Fatal(err)
	}
	defer fsys.Close()
	b.Run("read", func(b *testing.B) {
		b.ReportAllocs()
		i := 0
		for b.Loop() {
			if _, err := fsys.Read(names[i%len(names)]); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
