package asset

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tree(t *testing.T, files map[string]string) string {
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPackAndPrecedence(t *testing.T) {
	packed := tree(t, map[string]string{"a.txt": "packed a", "sub/b.txt": "packed b", "img.png": "png"})
	out := filepath.Join(t.TempDir(), "assets.pak")
	if err := Pack(packed, out); err != nil {
		t.Fatal(err)
	}
	loose := tree(t, map[string]string{"a.txt": "loose a", "c.txt": "loose c"})
	fs, err := Open(loose, out)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	for name, want := range map[string]string{"a.txt": "loose a", "sub/b.txt": "packed b", "c.txt": "loose c", "img.png": "png", "./sub/../sub/b.txt": "packed b"} {
		data, err := fs.Read(name)
		if err != nil || string(data) != want {
			t.Fatalf("%s: %q %v", name, data, err)
		}
	}
	if _, err := fs.Read("missing.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if !fs.Exists("img.png") || fs.Exists("nope") {
		t.Fatal("Exists wrong")
	}
	if got := strings.Join(fs.List(""), ","); got != "a.txt,c.txt,img.png,sub/b.txt" {
		t.Fatalf("list %s", got)
	}
	if got := strings.Join(fs.List("sub"), ","); got != "sub/b.txt" {
		t.Fatalf("list sub %s", got)
	}
	if fs.Path("a.txt") == "" || fs.Path("sub/b.txt") != "" {
		t.Fatal("Path should name loose files only")
	}
}

func TestLoader(t *testing.T) {
	dir := tree(t, map[string]string{"one": "1", "two": "22", "three": "333"})
	fs, _ := Open(dir)
	l := NewLoader(fs, 2)
	defer l.Close()
	decode := func(data []byte) (int, error) { return len(data), nil }
	handles := []*Handle[int]{Load(l, "one", decode), Load(l, "two", decode), Load(l, "three", decode)}
	bad := Load(l, "missing", decode)
	l.Wait()
	for i, h := range handles {
		v, err := h.Get()
		if err != nil || v != i+1 || !h.Ready() {
			t.Fatalf("handle %d: %d %v", i, v, err)
		}
	}
	if _, err := bad.Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad handle error %v", err)
	}
	if done, total := l.Progress(); done != 4 || total != 4 {
		t.Fatalf("progress %d/%d", done, total)
	}
}

func TestWatcher(t *testing.T) {
	dir := tree(t, map[string]string{"shader.glsl": "v1"})
	fs, _ := Open(dir)
	w := NewWatcher(fs, 20*time.Millisecond)
	defer w.Close()
	w.Add("shader.glsl", "packed-or-missing")
	time.Sleep(50 * time.Millisecond)
	if c := w.Changed(); len(c) != 0 {
		t.Fatalf("spurious change %v", c)
	}
	// Ensure the modification time moves even on coarse filesystems.
	p := filepath.Join(dir, "shader.glsl")
	os.WriteFile(p, []byte("v2"), 0o644)
	os.Chtimes(p, time.Now(), time.Now().Add(2*time.Second))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c := w.Changed(); len(c) == 1 && c[0] == "shader.glsl" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("change not noticed")
}
