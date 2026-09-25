// Package save stores a game's files in the platform's own data
// directory: settings, save slots and anything else worth keeping
// between runs, written as JSON through a synced temporary file and
// renamed into place to avoid exposing partial JSON.
//
// To open a store, call Open with the application name. It picks
// Application Support on macOS, AppData on Windows and XDG data on
// Linux. OpenAt takes any directory, for tests. A Store's Write and Read
// take any value encoding/json handles, and Load reads a value with
// defaults for the fields a file does not have, which is how settings
// survive new versions. List names the files present for a load menu,
// Delete removes one, and Exists checks before overwriting. Files are
// written to a temporary name and renamed into place, so a reader sees
// the old file or the new one where the filesystem supports atomic
// replacement. The containing directory is not synced, so power-loss
// durability is not guaranteed.
//
// Write returns once the file is synced to the drive, which takes
// milliseconds. To autosave from the game loop without missing a frame,
// call WriteAsync, which encodes the value at once and writes it on a
// background goroutine, and call Flush before the game exits. For whole
// ECS worlds, ecs.World.Save produces the bytes and this package stores
// them.
package save

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Dir returns the per-user data directory for an app: Application
// Support on macOS, XDG data on Linux, AppData on Windows. The
// BUNYIP_DATA_DIR environment variable overrides the base directory;
// app is appended to that base. The app argument is a trusted
// application directory name, not unvalidated user input.
func Dir(app string) (string, error) {
	if dir := os.Getenv("BUNYIP_DATA_DIR"); dir != "" {
		return filepath.Join(dir, app), nil
	}
	var base string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	case "windows":
		base = os.Getenv("APPDATA")
		if base == "" {
			return "", errors.New("save: APPDATA is not set")
		}
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(base, app), nil
}

// Store reads and writes named JSON documents in one directory. Names
// omit the .json extension and must be nonempty leaf names without
// slashes; "." and ".." are rejected. A Store is safe for concurrent
// use. Writes to one name, from Write and WriteAsync, land in the order
// they were called; writes to different names run independently.
type Store struct {
	dir string

	mu    sync.Mutex
	slots map[string]*slotQueue // names with writes queued or in progress
}

// slotQueue holds one name's writes, which a background goroutine runs
// in order while the queue is not empty.
type slotQueue struct {
	jobs    []*writeJob // waiting, oldest first
	current *writeJob   // being written
}

type writeJob struct {
	data     []byte
	result   chan error    // for the caller, buffered
	finished chan struct{} // closed once the write is done, for Flush
}

// Open creates the app's data directory if needed and returns a store
// for it.
func Open(app string) (*Store, error) {
	dir, err := Dir(app)
	if err != nil {
		return nil, err
	}
	return OpenAt(dir)
}

// OpenAt opens a store on an explicit directory.
func OpenAt(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Path returns the directory the store writes into.
func (s *Store) Path() string { return s.dir }

func (s *Store) file(name string) string {
	return filepath.Join(s.dir, name+".json")
}

// checkName rejects names that would reach outside the store: path
// separators and dot components. Names are file names, not paths.
func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("save: %q is not a valid save name", name)
	}
	return nil
}

// Write stores v as name.json through a synced temporary file, replacing
// the previous file with os.Rename. Atomic replacement depends on the
// host filesystem. Failures before rename leave the old file intact;
// this method does not sync the containing directory.
//
// Write returns once the data is on disk, and the sync waits for the
// storage device: several milliseconds for a small file on macOS, where
// a sync is a full flush of the drive's cache. To save from the game
// loop without missing a frame, call WriteAsync instead. Write waits
// for earlier WriteAsync calls for the same name to finish first.
func (s *Store) Write(name string, v any) error {
	return <-s.WriteAsync(name, v)
}

// WriteAsync stores v as name.json as Write does, but returns before the
// file is written. To autosave from the game loop, call it and check the
// returned channel on a later frame. v is encoded before WriteAsync
// returns, so the caller may change it straight away; the write, sync
// and rename run on a background goroutine. The channel receives one
// value when the write has finished: nil once the new file is in place,
// or the error. An invalid name or a value that cannot be encoded is
// reported on the channel at once, and nothing is written.
//
// Writes to one name land in the order they were called, and Read,
// Load, Exists and Delete for a name wait for its pending writes. A
// process that exits with writes pending loses them; call Flush before
// exiting.
func (s *Store) WriteAsync(name string, v any) <-chan error {
	result := make(chan error, 1)
	if err := checkName(name); err != nil {
		result <- err
		return result
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		result <- fmt.Errorf("save: encode %s: %w", name, err)
		return result
	}
	job := &writeJob{data: data, result: result, finished: make(chan struct{})}
	s.mu.Lock()
	if s.slots == nil {
		s.slots = map[string]*slotQueue{}
	}
	q := s.slots[name]
	if q == nil {
		q = &slotQueue{}
		s.slots[name] = q
		go s.run(name, q)
	}
	q.jobs = append(q.jobs, job)
	s.mu.Unlock()
	return result
}

// run writes a name's queued jobs in order and retires the queue once
// it is empty. A finished job leaves the queue, and an empty queue leaves
// the store, under the lock before the job reports its result, so a
// caller that has seen the result, or a Flush that has returned, finds
// nothing pending for it.
func (s *Store) run(name string, q *slotQueue) {
	s.mu.Lock()
	for len(q.jobs) > 0 {
		job := q.jobs[0]
		q.jobs[0] = nil
		q.jobs = q.jobs[1:]
		q.current = job
		s.mu.Unlock()
		err := s.writeFile(name, job.data)
		s.mu.Lock()
		q.current = nil
		if len(q.jobs) == 0 {
			delete(s.slots, name)
		}
		s.mu.Unlock()
		job.result <- err
		close(job.finished)
		s.mu.Lock()
	}
	s.mu.Unlock()
}

// Flush waits until every write that WriteAsync or Write started before
// the call has finished. To make sure autosaves reach the disk, call it
// before the game exits. Errors go to each write's own channel.
func (s *Store) Flush() {
	for _, f := range s.pending("", true) {
		<-f
	}
}

// wait blocks until the writes pending for one name have finished.
func (s *Store) wait(name string) {
	for _, f := range s.pending(name, false) {
		<-f
	}
}

// pending lists the finish signals of the writes queued or in progress,
// for one name or for all of them.
func (s *Store) pending(name string, all bool) []chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []chan struct{}
	for n, q := range s.slots {
		if !all && n != name {
			continue
		}
		if q.current != nil {
			out = append(out, q.current.finished)
		}
		for _, j := range q.jobs {
			out = append(out, j.finished)
		}
	}
	return out
}

// writeFile writes encoded data to name.json through a synced temporary
// file and a rename.
func (s *Store) writeFile(name string, data []byte) error {
	tmp, err := os.CreateTemp(s.dir, name+".*.tmp")
	if err != nil {
		return fmt.Errorf("save: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("save: write %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("save: sync %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("save: close %s: %w", name, err)
	}
	if err := os.Rename(tmpName, s.file(name)); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("save: replace %s: %w", name, err)
	}
	return nil
}

// Read decodes name.json into v, after any pending writes to name have
// finished. A missing file returns an error that satisfies
// errors.Is(err, os.ErrNotExist).
func (s *Store) Read(name string, v any) error {
	if err := checkName(name); err != nil {
		return err
	}
	s.wait(name)
	data, err := os.ReadFile(s.file(name))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("save: decode %s: %w", name, err)
	}
	return nil
}

// Exists reports whether name.json is present, after any pending writes
// to name have finished.
func (s *Store) Exists(name string) bool {
	if checkName(name) != nil {
		return false
	}
	s.wait(name)
	_, err := os.Stat(s.file(name))
	return err == nil
}

// Delete removes name.json, after any pending writes to name have
// finished; a missing file is not an error.
func (s *Store) Delete(name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	s.wait(name)
	if err := os.Remove(s.file(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("save: %w", err)
	}
	return nil
}

// List returns the names of the documents in the store, sorted.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".json") && !e.IsDir() {
			names = append(names, strings.TrimSuffix(n, ".json"))
		}
	}
	sort.Strings(names)
	return names, nil
}

// Load reads name.json over a copy of defaults, so fields the file lacks
// keep their default values and a missing file yields the defaults
// without error. Use it for settings.
// Defaults are copied through JSON even when the file is missing, so
// maps and slices are independent. Invalid JSON defaults return an error.
func (s *Store) Load[T any](name string, defaults T) (T, error) {
	// A deep copy: decoding into a value that shares the defaults'
	// slices and maps would write into them.
	var v T
	data, err := json.Marshal(defaults)
	if err != nil {
		return v, fmt.Errorf("save: encode defaults for %s: %w", name, err)
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("save: decode defaults for %s: %w", name, err)
	}
	err = s.Read(name, &v)
	if errors.Is(err, os.ErrNotExist) {
		return v, nil
	}
	return v, err
}
