package asset

import (
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestStandardFS(t *testing.T) {
	packed := tree(t, map[string]string{"sub/packed": "pack", "duplicate": "pack"})
	archive := filepath.Join(t.TempDir(), "assets.pak")
	if err := Pack(packed, archive); err != nil {
		t.Fatal(err)
	}
	loose := tree(t, map[string]string{"sub/loose": "loose", "duplicate": "loose"})
	embedded := fstest.MapFS{"sub/embedded": {Data: []byte("embedded")}}
	f, err := OpenFS(Dir(loose), PackFile(archive), FSSource(embedded))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := fstest.TestFS(f, "sub/packed", "sub/loose", "sub/embedded", "duplicate"); err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(f, "duplicate")
	if err != nil || string(data) != "loose" {
		t.Fatalf("precedence = %q, %v", data, err)
	}
	sub, err := fs.Sub(f, "sub")
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(sub, "packed", "loose", "embedded"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "/sub", "sub/", "sub/../duplicate", "./duplicate"} {
		if _, err := f.ReadFile(name); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("%q: %v", name, err)
		}
	}
	if _, err := f.ReadFile("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err := f.ReadFile("sub"); err == nil {
		t.Fatal("read directory succeeded")
	}
}

func TestOverlayMasks(t *testing.T) {
	f, err := OpenFS(FSSource(fstest.MapFS{
		"file":    {Data: []byte("top")},
		"dir/top": {Data: []byte("top")},
	}), FSSource(fstest.MapFS{
		"file/hidden": {Data: []byte("hidden")},
		"dir":         {Data: []byte("hidden")},
	}), FSSource(fstest.MapFS{"dir/bottom": {Data: []byte("bottom")}}))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := fstest.TestFS(f, "file", "dir/top", "dir/bottom"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Open("file/hidden"); err == nil {
		t.Fatal("file exposed lower directory")
	}
	if f.Exists("file/hidden") || f.Path("file/hidden") != "" {
		t.Fatal("hidden file exposed by lookup helpers")
	}
	if got := f.List(""); !reflect.DeepEqual(got, []string{"dir/bottom", "dir/top", "file"}) {
		t.Fatal(got)
	}
	entries, err := fs.ReadDir(f, "dir")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !reflect.DeepEqual(names, []string{"bottom", "top"}) {
		t.Fatal(names)
	}
}

type permissionFS struct{}

func (permissionFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}

func TestOverlayPropagatesErrors(t *testing.T) {
	f, err := OpenFS(FSSource(permissionFS{}), FSSource(fstest.MapFS{"secret": {Data: []byte("fallback")}}))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.ReadFile("secret"); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("permission: %v", err)
	}
	if _, err := f.Read("secret"); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("legacy permission: %v", err)
	}
}

func TestEmptyFSAndDirectoryHandle(t *testing.T) {
	f, err := OpenFS()
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(f); err != nil {
		t.Fatal(err)
	}
	d, err := f.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := d.(fs.ReadDirFile).ReadDir(1); len(entries) != 0 || err != io.EOF {
		t.Fatalf("empty: %v %v", entries, err)
	}
	d.Close()
	if _, err := d.Stat(); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("closed stat: %v", err)
	}
	if _, err := d.(fs.ReadDirFile).ReadDir(-1); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("closed readdir: %v", err)
	}
}

type statErrorFile struct{ fs.File }

func (statErrorFile) Stat() (fs.FileInfo, error) { return nil, fs.ErrPermission }

type statErrorFS struct{ fstest.MapFS }

func (s statErrorFS) Open(name string) (fs.File, error) {
	f, err := s.MapFS.Open(name)
	if err != nil {
		return nil, err
	}
	return statErrorFile{f}, nil
}

func TestOpenErrorDescribesRequestedPath(t *testing.T) {
	for _, tc := range []struct {
		source fs.FS
		name   string
		want   error
	}{
		{fstest.MapFS{}, "missing/file", fs.ErrNotExist},
		{statErrorFS{fstest.MapFS{"file": {Data: []byte("x")}}}, "file", fs.ErrPermission},
	} {
		f, err := OpenFS(FSSource(tc.source))
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Open(tc.name)
		pe, ok := err.(*fs.PathError)
		if !ok || pe.Op != "open" || pe.Path != tc.name || !errors.Is(err, tc.want) {
			t.Fatalf("Open(%q): %v", tc.name, err)
		}
		f.Close()
	}
}
