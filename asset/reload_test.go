package asset

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// write puts contents at a path and dates it later than the file that
// was there, so the watcher notices however coarse the file system's
// timestamps are.
func write(t *testing.T, path string, contents string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

// waitReload polls the reloader until it reports a name or the deadline
// passes, so the test does not depend on the poll landing in one call.
func waitReload(t *testing.T, r *Reloader) ([]string, error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		names, err := r.Reload()
		if len(names) > 0 || err != nil {
			return names, err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil, nil
}

// TestReloaderWatch changes a watched file and checks that the reloader
// hands the new bytes to the target and names the asset.
func TestReloaderWatch(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "tuning.txt"), "speed 1", -2*time.Second)
	fs, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	r := NewReloader(nil, fs, 5*time.Millisecond)
	defer r.Close()

	var got string
	r.Watch("tuning.txt", func(data []byte) error {
		got = string(data)
		return nil
	})
	if names, err := r.Reload(); len(names) != 0 || err != nil {
		t.Fatalf("an unchanged file reloaded: %v %v", names, err)
	}

	write(t, filepath.Join(dir, "tuning.txt"), "speed 2", 0)
	names, err := waitReload(t, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "tuning.txt" {
		t.Fatalf("reloaded %v, want [tuning.txt]", names)
	}
	if got != "speed 2" {
		t.Errorf("target saw %q, want %q", got, "speed 2")
	}
}

// TestReloaderTargetError checks that a target that fails keeps its
// error and its name out of the reloaded list, and that the other
// targets on the same file still run.
func TestReloaderTargetError(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), "one", -2*time.Second)
	fs, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	r := NewReloader(nil, fs, 5*time.Millisecond)
	defer r.Close()

	boom := errors.New("bad file")
	second := false
	r.Watch("a.txt", func([]byte) error { return boom })
	r.Watch("a.txt", func([]byte) error { second = true; return nil })

	write(t, filepath.Join(dir, "a.txt"), "two", 0)
	var names []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		names, err = r.Reload()
		if err != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("error %v, want %v", err, boom)
	}
	if len(names) != 0 {
		t.Errorf("named %v as reloaded although a target failed", names)
	}
	if !second {
		t.Error("the second target did not run after the first failed")
	}
}

// TestReloaderIgnoresPacked checks that a name resolving into a pack is
// never watched, since packed bytes cannot change under a running game.
func TestReloaderIgnoresPacked(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(src, "level.txt"), "one", 0)
	packPath := filepath.Join(dir, "assets.pak")
	if err := Pack(src, packPath); err != nil {
		t.Fatal(err)
	}
	fs, err := Open(packPath)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	r := NewReloader(nil, fs, 5*time.Millisecond)
	defer r.Close()

	r.Watch("level.txt", func([]byte) error {
		t.Error("a packed file was reloaded")
		return nil
	})
	r.mu.Lock()
	n := len(r.targets)
	r.mu.Unlock()
	if n != 0 {
		t.Errorf("%d packed names watched, want none", n)
	}
	time.Sleep(30 * time.Millisecond)
	if names, err := r.Reload(); len(names) != 0 || err != nil {
		t.Fatalf("a packed file reloaded: %v %v", names, err)
	}
}
