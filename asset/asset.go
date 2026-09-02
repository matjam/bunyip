// Package asset finds a game's files: loose directories while
// developing, pack files when shipping, or both with directories taking
// precedence so a modder's or developer's copy overrides the packed one.
// A Loader decodes assets on worker goroutines for loading screens, and
// a Watcher reports loose files that change on disk for hot reload.
package asset

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// FS resolves names against its sources in order.
type FS struct {
	dirs  []string
	packs []*pack
}

type pack struct {
	path  string
	zr    *zip.Reader
	f     *os.File
	files map[string]*zip.File
}

// Open takes directories and pack files, searched in the order given.
// A missing source is an error; use Exists checks first when a source is
// optional.
func Open(sources ...string) (*FS, error) {
	f := &FS{}
	for _, src := range sources {
		info, err := os.Stat(src)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("asset: %w", err)
		}
		if info.IsDir() {
			f.dirs = append(f.dirs, src)
			f.packs = append(f.packs, nil) // keep order across kinds
			continue
		}
		p, err := openPack(src)
		if err != nil {
			f.Close()
			return nil, err
		}
		f.dirs = append(f.dirs, "")
		f.packs = append(f.packs, p)
	}
	return f, nil
}

func openPack(name string) (*pack, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("asset: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("asset: %w", err)
	}
	zr, err := zip.NewReader(file, info.Size())
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("asset: %s: %w", name, err)
	}
	p := &pack{path: name, zr: zr, f: file, files: map[string]*zip.File{}}
	for _, zf := range zr.File {
		if !strings.HasSuffix(zf.Name, "/") {
			p.files[path.Clean(zf.Name)] = zf
		}
	}
	return p, nil
}

// Close releases pack files.
func (f *FS) Close() {
	for _, p := range f.packs {
		if p != nil {
			p.f.Close()
		}
	}
}

// ErrNotFound is returned for names no source holds.
var ErrNotFound = errors.New("asset: not found")

// Read returns the named file's contents. Names use forward slashes
// relative to the source roots, like "sprites/hero.png".
func (f *FS) Read(name string) ([]byte, error) {
	name = clean(name)
	for i := range f.dirs {
		if f.dirs[i] != "" {
			data, err := os.ReadFile(filepath.Join(f.dirs[i], filepath.FromSlash(name)))
			if err == nil {
				return data, nil
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("asset: %s: %w", name, err)
			}
			continue
		}
		if zf, ok := f.packs[i].files[name]; ok {
			rc, err := zf.Open()
			if err != nil {
				return nil, fmt.Errorf("asset: %s: %w", name, err)
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("asset: %s: %w", name, err)
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// Exists reports whether some source holds the name.
func (f *FS) Exists(name string) bool {
	_, err := f.locate(clean(name))
	return err == nil
}

// Path returns the on-disk path of a loose file, or "" when the name
// resolves to a pack entry or nothing. Watchers use it.
func (f *FS) Path(name string) string {
	p, err := f.locate(clean(name))
	if err != nil {
		return ""
	}
	return p
}

func (f *FS) locate(name string) (string, error) {
	for i := range f.dirs {
		if f.dirs[i] != "" {
			p := filepath.Join(f.dirs[i], filepath.FromSlash(name))
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p, nil
			}
			continue
		}
		if _, ok := f.packs[i].files[name]; ok {
			return "", nil
		}
	}
	return "", ErrNotFound
}

// List returns every name under prefix across all sources, sorted and
// without duplicates.
func (f *FS) List(prefix string) []string {
	prefix = clean(prefix)
	if prefix == "." {
		prefix = ""
	}
	seen := map[string]bool{}
	for i := range f.dirs {
		if f.dirs[i] != "" {
			root := filepath.Join(f.dirs[i], filepath.FromSlash(prefix))
			filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(f.dirs[i], p)
				if err == nil {
					seen[filepath.ToSlash(rel)] = true
				}
				return nil
			})
			continue
		}
		for name := range f.packs[i].files {
			if prefix == "" || name == prefix || strings.HasPrefix(name, prefix+"/") {
				seen[name] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func clean(name string) string {
	return path.Clean("/" + strings.ReplaceAll(name, "\\", "/"))[1:]
}

// Pack writes every file under dir into a pack file at out. Files that
// are already compressed (images, audio, video) are stored as they are;
// the rest are deflated.
func Pack(dir, out string) error {
	tmp := out + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("asset: %w", err)
	}
	zw := zip.NewWriter(file)
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		hdr := &zip.FileHeader{Name: filepath.ToSlash(rel), Method: zip.Deflate}
		if stored(rel) {
			hdr.Method = zip.Store
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, bytes.NewReader(data))
		return err
	})
	if err == nil {
		err = zw.Close()
	}
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("asset: pack: %w", err)
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("asset: pack: %w", err)
	}
	return nil
}

func stored(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".ogg", ".mp3", ".zip", ".pak", ".glb", ".webp", ".mp4":
		return true
	}
	return false
}
