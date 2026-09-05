// Package asset finds and loads a game's files. Sources are loose
// directories while developing, pack files when shipping, files
// embedded in the binary with go:embed, or any mix, with earlier
// sources taking precedence so a modder's or developer's copy overrides
// the packed or embedded one. Open takes directory and pack paths;
// OpenFS also takes an io/fs.FS. FS implements io/fs.FS with merged
// directories; loaders also accept embed.FS, os.DirFS and other fs.FS
// values directly. The one-call loaders (Image, Texture,
// Atlas, Aseprite, Font, Sound, Music, Model, Tracker, Scene, Prefab)
// read and decode an asset into an engine object. A
// Loader decodes assets on worker goroutines for loading screens, a
// Watcher reports loose files that change on disk, and a Reloader swaps
// a changed file's texture or shader into the objects a game already
// holds, so hot reload needs no bookkeeping in the game.
// GPU loaders and Reloader methods run on the rendering goroutine.
// Closing FS releases pack handles, not loaded resources. Graphics owns
// uploaded GPU resources; Destroy releases them early. The caller closes
// loaded music and other resources with independent lifetimes.
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

// FS resolves names against its sources in order and implements fs.FS
// and fs.ReadFileFS. Open and ReadFile use standard io/fs paths. Read,
// Exists, Path and List retain their cleaned-name convenience syntax.
type FS struct {
	sources []source
}

// source is one place an FS looks. Missing names return
// fs.ErrNotExist (possibly wrapped) so the FS moves on to the next one.
type source interface {
	fs.FS
	// stat reports whether the source holds name and, for loose files,
	// its on-disk path.
	stat(name string) (diskPath string, ok bool)
	close()
}

// Source is a place an FS looks for files. Build one with Dir, PackFile
// or FSSource and pass it to OpenFS.
type Source struct {
	dir  string
	pack string
	fsys fs.FS
}

// Dir is a directory of loose files.
func Dir(path string) Source { return Source{dir: path} }

// PackFile is a pack written by Pack or bunyip-pack.
func PackFile(path string) Source { return Source{pack: path} }

// FSSource is any io/fs.FS, typically an embed.FS so a game ships its
// assets inside the binary. Names resolve relative to the FS root, so
// pass fs.Sub(embedded, "assets") when the embed directive names a
// directory. Path returns "" for its files and a Watcher ignores them.
func FSSource(fsys fs.FS) Source { return Source{fsys: fsys} }

// Open takes directories and pack files, searched in the order given.
// A missing source is an error. Check optional source paths with os.Stat before opening;
// FS.Exists checks asset names after an FS is open. OpenFS accepts
// embedded file systems as well.
func Open(sources ...string) (*FS, error) {
	srcs := make([]Source, 0, len(sources))
	for _, src := range sources {
		info, err := os.Stat(src)
		if err != nil {
			return nil, fmt.Errorf("asset: %w", err)
		}
		if info.IsDir() {
			srcs = append(srcs, Dir(src))
		} else {
			srcs = append(srcs, PackFile(src))
		}
	}
	return OpenFS(srcs...)
}

// OpenFS takes sources of any kind, searched in the order given. A
// missing directory or pack is an error.
func OpenFS(sources ...Source) (*FS, error) {
	f := &FS{}
	for _, src := range sources {
		s, err := src.open()
		if err != nil {
			f.Close()
			return nil, err
		}
		f.sources = append(f.sources, s)
	}
	return f, nil
}

func (s Source) open() (source, error) {
	switch {
	case s.fsys != nil:
		return fsSource{s.fsys}, nil
	case s.pack != "":
		return openPack(s.pack)
	case s.dir != "":
		info, err := os.Stat(s.dir)
		if err != nil {
			return nil, fmt.Errorf("asset: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("asset: %s: not a directory", s.dir)
		}
		return dirSource(s.dir), nil
	}
	return nil, errors.New("asset: empty source")
}

// dirSource is a directory of loose files.
type dirSource string

func (d dirSource) Open(name string) (fs.File, error) { return os.DirFS(string(d)).Open(name) }

func (d dirSource) join(name string) string {
	return filepath.Join(string(d), filepath.FromSlash(name))
}

func (d dirSource) stat(name string) (string, bool) {
	p := d.join(name)
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p, true
	}
	return "", false
}

func (d dirSource) close() {}

// pack is an open pack file.
type pack struct {
	path  string
	zr    *zip.Reader
	f     *os.File
	files map[string]*zip.File
}

func (p *pack) Open(name string) (fs.File, error) { return p.zr.Open(name) }

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

func (p *pack) stat(name string) (string, bool) {
	_, ok := p.files[name]
	return "", ok
}

func (p *pack) close() { p.f.Close() }

// fsSource wraps an io/fs.FS.
type fsSource struct{ fsys fs.FS }

func (s fsSource) Open(name string) (fs.File, error) { return s.fsys.Open(name) }

func (s fsSource) ReadDir(name string) ([]fs.DirEntry, error) { return fs.ReadDir(s.fsys, name) }

func (s fsSource) stat(name string) (string, bool) {
	info, err := fs.Stat(s.fsys, name)
	return "", err == nil && !info.IsDir()
}

func (s fsSource) close() {}

// Close releases pack files.
func (f *FS) Close() {
	for _, s := range f.sources {
		s.close()
	}
}

// ErrNotFound is returned for names no source holds.
var ErrNotFound = fs.ErrNotExist

// Read returns the named file's contents. Names use forward slashes
// relative to the source roots, like "sprites/hero.png". Unlike ReadFile,
// this convenience method cleans dot components and backslashes first.
func (f *FS) Read(name string) ([]byte, error) {
	return f.ReadFile(clean(name))
}

// Exists reports whether some source holds the name.
func (f *FS) Exists(name string) bool {
	_, err := f.locate(clean(name))
	return err == nil
}

// Path returns the on-disk path of a loose file, or "" when the name
// resolves to a pack or fs.FS entry or nothing. Watchers use it.
func (f *FS) Path(name string) string {
	p, err := f.locate(clean(name))
	if err != nil {
		return ""
	}
	return p
}

func (f *FS) locate(name string) (string, error) {
	file, err := f.Open(name)
	if err != nil {
		return "", err
	}
	info, err := file.Stat()
	file.Close()
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", ErrNotFound
	}
	for _, s := range f.sources {
		if p, ok := s.stat(name); ok {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// List returns visible file names under prefix, sorted and without
// duplicates. Unreadable directories are omitted; use fs.WalkDir when
// errors need to be reported.
func (f *FS) List(prefix string) []string {
	prefix = clean(prefix)
	if prefix == "" {
		prefix = "."
	}
	var names []string
	fs.WalkDir(f, prefix, func(name string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			names = append(names, name)
		}
		return nil
	})
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
	case ".png", ".jpg", ".jpeg", ".ogg", ".mp3", ".zip", ".pak", ".glb", ".webp", ".mp4", ".ktx2":
		return true
	}
	return false
}
