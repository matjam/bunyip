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
// durability is not guaranteed. For whole
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
// slashes; "." and ".." are rejected. Concurrent writes use separate
// temporary files; the last successful rename to a name wins.
type Store struct {
	dir string
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
func (s *Store) Write(name string, v any) error {
	if err := checkName(name); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("save: encode %s: %w", name, err)
	}
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

// Read decodes name.json into v. A missing file returns an error that
// satisfies errors.Is(err, os.ErrNotExist).
func (s *Store) Read(name string, v any) error {
	if err := checkName(name); err != nil {
		return err
	}
	data, err := os.ReadFile(s.file(name))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("save: decode %s: %w", name, err)
	}
	return nil
}

// Exists reports whether name.json is present.
func (s *Store) Exists(name string) bool {
	if checkName(name) != nil {
		return false
	}
	_, err := os.Stat(s.file(name))
	return err == nil
}

// Delete removes name.json; a missing file is not an error.
func (s *Store) Delete(name string) error {
	if err := checkName(name); err != nil {
		return err
	}
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
// Defaults are copied through JSON before applying a present file;
// provide JSON-round-trippable defaults for independent maps and slices.
// A missing file returns defaults directly, which can share their data.
func Load[T any](s *Store, name string, defaults T) (T, error) {
	// A deep copy: decoding into a value that shares the defaults'
	// slices and maps would write into them.
	var v T
	if data, err := json.Marshal(defaults); err == nil {
		if err := json.Unmarshal(data, &v); err != nil {
			v = defaults
		}
	} else {
		v = defaults
	}
	err := s.Read(name, &v)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	return v, err
}
