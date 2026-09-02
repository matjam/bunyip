// Package save keeps a game's files where the operating system expects
// them: settings, save slots and anything else worth keeping between
// runs, written as JSON and replaced atomically so a crash mid-write
// never corrupts a save.
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
// BUNYIP_DATA_DIR environment variable overrides it.
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

// Store reads and writes named JSON documents in one directory.
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

// Write stores v as name.json, replacing any previous file atomically.
func (s *Store) Write(name string, v any) error {
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
	_, err := os.Stat(s.file(name))
	return err == nil
}

// Delete removes name.json; a missing file is not an error.
func (s *Store) Delete(name string) error {
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
func Load[T any](s *Store, name string, defaults T) (T, error) {
	v := defaults
	err := s.Read(name, &v)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	return v, err
}
