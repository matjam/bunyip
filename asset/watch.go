package asset

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Watcher polls loose files for changes so a running game can reload a
// texture or shader the moment it is saved. Packed files never change,
// so names that resolve into a pack are ignored.
//
// Add resolves each name to its file on disk once. A poll then stats
// that file, plus each directory a copy of the name could appear in
// ahead of it: the name's directory in every loose source up to the one
// that holds it, or in every loose source when none does. A name is
// resolved again only when one of those directories changes, so a copy
// added to or removed from an overlaying source is still noticed. The
// poll runs without the lock Changed takes, so a game calling Changed
// every frame never waits for one.
type Watcher struct {
	fs       *FS
	interval time.Duration
	stat     func(string) (os.FileInfo, error)

	mu      sync.Mutex // guards files, list, dirty, changed and noted
	files   map[string]*watchEntry
	list    []*watchEntry // in the order added; only ever appended to
	dirty   bool          // entries added since the last poll
	changed []string
	noted   map[string]bool // the names in changed

	// pollMu serialises polls. The fields below belong to the poll
	// holding it, and so do the fields of every entry once it is in
	// list.
	pollMu  sync.Mutex
	dirList []string            // every directory some entry watches
	dirNow  map[string]dirStamp // each directory's stamp in this poll
	scratch []*watchEntry       // entries noticed changing in this poll
	stop    chan struct{}
	once    sync.Once
}

// watchEntry is one watched name and where it resolved.
type watchEntry struct {
	name   string
	path   string    // the loose file on disk, "" when the name is packed, embedded or missing
	seen   time.Time // the file's modification time when last looked at
	dirs   []string  // directories a copy of the name could appear in first
	stamps []dirStamp
}

// dirStamp is what a directory looked like: whether it existed and when
// it last changed. Adding or removing a file changes a directory's
// modification time.
type dirStamp struct {
	ok  bool
	mod time.Time
}

func (a dirStamp) same(b dirStamp) bool { return a.ok == b.ok && a.mod.Equal(b.mod) }

// NewWatcher polls every interval (zero means half a second).
func NewWatcher(fs *FS, interval time.Duration) *Watcher {
	return newWatcher(fs, interval, os.Stat)
}

// newWatcher is NewWatcher with the stat a poll uses, which tests make
// slow.
func newWatcher(fs *FS, interval time.Duration, stat func(string) (os.FileInfo, error)) *Watcher {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	w := &Watcher{fs: fs, interval: interval, stat: stat, files: map[string]*watchEntry{},
		noted: map[string]bool{}, dirNow: map[string]dirStamp{}, stop: make(chan struct{})}
	go w.run()
	return w
}

// Add starts watching names; loading an asset and adding it here is the
// usual pair.
func (w *Watcher) Add(names ...string) {
	w.mu.Lock()
	var fresh []string
	for _, n := range names {
		if _, ok := w.files[n]; !ok {
			fresh = append(fresh, n)
		}
	}
	w.mu.Unlock()
	if len(fresh) == 0 {
		return
	}
	// Resolving stats the file system, so it happens outside the lock.
	entries := make([]*watchEntry, len(fresh))
	for i, n := range fresh {
		e := &watchEntry{name: n}
		w.resolve(e, nil)
		e.seen = w.mtime(e.path)
		entries[i] = e
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, e := range entries {
		if _, ok := w.files[e.name]; ok {
			continue // added twice in names, or by another Add meanwhile
		}
		w.files[e.name] = e
		w.list = append(w.list, e)
		w.dirty = true
	}
}

// resolve finds where the entry's name lives now and which directories
// could hold a copy ahead of it, stamping each. Stamps already taken in
// this poll are reused from known.
func (w *Watcher) resolve(e *watchEntry, known map[string]dirStamp) {
	path, index, _ := w.fs.locateIndex(clean(e.name))
	e.path = path
	e.dirs, e.stamps = e.dirs[:0], e.stamps[:0]
	for i, s := range w.fs.sources {
		if i > index {
			break
		}
		d, ok := s.(dirSource)
		if !ok {
			continue
		}
		dir := filepath.Dir(d.join(clean(e.name)))
		st, ok := known[dir]
		if !ok {
			st = w.dirStamp(dir)
		}
		e.dirs = append(e.dirs, dir)
		e.stamps = append(e.stamps, st)
	}
}

func (w *Watcher) dirStamp(dir string) dirStamp {
	info, err := w.stat(dir)
	if err != nil || !info.IsDir() {
		return dirStamp{}
	}
	return dirStamp{ok: true, mod: info.ModTime()}
}

func (w *Watcher) mtime(path string) time.Time {
	if path == "" {
		return time.Time{}
	}
	info, err := w.stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// Changed returns the names modified since the last call, in the order
// they were first noticed, and clears them. A name appears once however
// many times its file changed, because a save can change a file more
// than once (truncate, write, set the time) and one reload of the
// latest contents covers them all. Call it once per frame.
func (w *Watcher) Changed() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.changed
	w.changed = nil
	clear(w.noted)
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

// poll looks at every watched file once. Only taking the list and
// publishing what changed hold the lock Changed needs.
func (w *Watcher) poll() {
	w.pollMu.Lock()
	defer w.pollMu.Unlock()
	w.mu.Lock()
	entries, dirty := w.list, w.dirty
	w.dirty = false
	w.mu.Unlock()

	if dirty || w.dirList == nil {
		w.collectDirs(entries)
	}
	clear(w.dirNow)
	for _, d := range w.dirList {
		w.dirNow[d] = w.dirStamp(d)
	}
	moved := false
	w.scratch = w.scratch[:0]
	for _, e := range entries {
		for k, d := range e.dirs {
			if !e.stamps[k].same(w.dirNow[d]) {
				// A copy may have appeared ahead of the file, or the file
				// may have gone: look the name up again.
				w.resolve(e, w.dirNow)
				moved = true
				break
			}
		}
		now := w.mtime(e.path)
		if !now.IsZero() && !now.Equal(e.seen) {
			e.seen = now
			w.scratch = append(w.scratch, e)
		}
	}
	if moved {
		w.collectDirs(entries)
	}
	if len(w.scratch) == 0 {
		return
	}
	w.mu.Lock()
	for _, e := range w.scratch {
		if !w.noted[e.name] {
			w.noted[e.name] = true
			w.changed = append(w.changed, e.name)
		}
	}
	w.mu.Unlock()
}

// collectDirs lists every directory some entry watches, once each.
func (w *Watcher) collectDirs(entries []*watchEntry) {
	seen := make(map[string]bool, len(w.dirList))
	w.dirList = w.dirList[:0]
	for _, e := range entries {
		for _, d := range e.dirs {
			if !seen[d] {
				seen[d] = true
				w.dirList = append(w.dirList, d)
			}
		}
	}
	if w.dirList == nil {
		w.dirList = []string{}
	}
}

// Close stops polling.
func (w *Watcher) Close() { w.once.Do(func() { close(w.stop) }) }
