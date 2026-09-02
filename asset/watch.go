package asset

import (
	"os"
	"sync"
	"time"
)

// Watcher polls loose files for changes so a running game can reload a
// texture or shader the moment it is saved. Packed files never change,
// so names that resolve into a pack are ignored.
type Watcher struct {
	fs       *FS
	interval time.Duration
	mu       sync.Mutex
	files    map[string]time.Time
	changed  []string
	stop     chan struct{}
	once     sync.Once
}

// NewWatcher polls every interval (zero means half a second).
func NewWatcher(fs *FS, interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	w := &Watcher{fs: fs, interval: interval, files: map[string]time.Time{}, stop: make(chan struct{})}
	go w.run()
	return w
}

// Add starts watching names; loading an asset and adding it here is the
// usual pair.
func (w *Watcher) Add(names ...string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, n := range names {
		if _, ok := w.files[n]; ok {
			continue
		}
		w.files[n] = w.mtime(n)
	}
}

func (w *Watcher) mtime(name string) time.Time {
	p := w.fs.Path(name)
	if p == "" {
		return time.Time{}
	}
	info, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// Changed returns the names modified since the last call, in the order
// they were noticed, and clears them. Call it once per frame.
func (w *Watcher) Changed() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.changed
	w.changed = nil
	return out
}

func (w *Watcher) run() {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-t.C:
			w.poll()
		}
	}
}

func (w *Watcher) poll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for name, seen := range w.files {
		now := w.mtime(name)
		if !now.IsZero() && now != seen {
			w.files[name] = now
			w.changed = append(w.changed, name)
		}
	}
}

// Close stops polling.
func (w *Watcher) Close() { w.once.Do(func() { close(w.stop) }) }
