package asset

import (
	"errors"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"
)

var (
	_ fs.FS         = (*FS)(nil)
	_ fs.ReadFileFS = (*FS)(nil)
)

// Open opens a file or merged directory using io/fs paths. Earlier
// sources win when names conflict; directories merge their children.
// A file hides all lower-priority entries beneath its name. Only missing
// entries fall through to later sources; other errors are returned.
// The returned handle belongs to the caller and must be closed before FS.
func (f *FS) Open(name string) (file fs.File, err error) {
	defer func() {
		if err != nil {
			if pe, ok := err.(*fs.PathError); !ok || pe.Op != "open" || pe.Path != name {
				err = &fs.PathError{Op: "open", Path: name, Err: err}
			}
		}
	}()
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	// Check ancestors so a higher-priority file cannot expose children
	// from a lower-priority directory of the same name.
	sources := f.sources
	for i := strings.IndexByte(name, '/'); i >= 0; {
		ancestor, err := openSources(sources, name[:i])
		if err != nil {
			return nil, err
		}
		info, err := ancestor.Stat()
		ancestor.Close()
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
		}
		sources = ancestor.(*overlayDir).sources
		j := strings.IndexByte(name[i+1:], '/')
		if j < 0 {
			break
		}
		i += j + 1
	}
	return openSources(sources, name)
}

func openSources(sources []source, name string) (fs.File, error) {
	var dir *overlayDir
	for _, s := range sources {
		file, err := s.Open(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
		if !info.IsDir() {
			if dir == nil {
				return file, nil
			}
			file.Close()
			continue
		}
		file.Close()
		if dir == nil {
			dir = &overlayDir{name: name, info: info}
		}
		dir.sources = append(dir.sources, s)
	}
	if dir != nil {
		return dir, nil
	}
	if name == "." {
		return &overlayDir{name: name, info: rootInfo{}}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadFile reads a file using io/fs paths and errors. The result belongs
// to the caller. Directories cannot be read as files.
func (f *FS) ReadFile(name string) ([]byte, error) {
	file, err := f.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

type overlayDir struct {
	sources        []source
	name           string
	info           fs.FileInfo
	entries        []fs.DirEntry
	loaded, closed bool
	pos            int
}

func (d *overlayDir) err(op string, err error) error {
	return &fs.PathError{Op: op, Path: d.name, Err: err}
}

func (d *overlayDir) Stat() (fs.FileInfo, error) {
	if d.closed {
		return nil, d.err("stat", fs.ErrClosed)
	}
	return d.info, nil
}

func (d *overlayDir) Close() error { d.closed = true; return nil }

func (d *overlayDir) Read([]byte) (int, error) {
	if d.closed {
		return 0, d.err("read", fs.ErrClosed)
	}
	return 0, d.err("read", fs.ErrInvalid)
}

func (d *overlayDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.closed {
		return nil, d.err("readdir", fs.ErrClosed)
	}
	if !d.loaded {
		seen := map[string]fs.DirEntry{}
		for _, s := range d.sources {
			entries, err := fs.ReadDir(s, d.name)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if _, ok := seen[entry.Name()]; !ok {
					seen[entry.Name()] = entry
				}
			}
		}
		d.entries = make([]fs.DirEntry, 0, len(seen))
		for _, entry := range seen {
			d.entries = append(d.entries, entry)
		}
		sort.Slice(d.entries, func(i, j int) bool { return d.entries[i].Name() < d.entries[j].Name() })
		d.loaded = true
	}
	if n > 0 && d.pos == len(d.entries) {
		return nil, io.EOF
	}
	end := len(d.entries)
	if n > 0 {
		end = min(end, d.pos+n)
	}
	entries := append([]fs.DirEntry{}, d.entries[d.pos:end]...)
	d.pos = end
	return entries, nil
}

type rootInfo struct{}

func (rootInfo) Name() string       { return "." }
func (rootInfo) Size() int64        { return 0 }
func (rootInfo) Mode() fs.FileMode  { return fs.ModeDir | 0555 }
func (rootInfo) ModTime() time.Time { return time.Time{} }
func (rootInfo) IsDir() bool        { return true }
func (rootInfo) Sys() any           { return nil }
