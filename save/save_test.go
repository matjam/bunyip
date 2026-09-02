package save

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type settings struct {
	Version    int
	Volume     float32
	Fullscreen bool
	Name       string
}

func TestStore(t *testing.T) {
	s, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Write("slot1", settings{Version: 1, Volume: 0.5, Name: "hero"}); err != nil {
		t.Fatal(err)
	}
	var got settings
	if err := s.Read("slot1", &got); err != nil || got.Name != "hero" || got.Volume != 0.5 {
		t.Fatalf("read %+v %v", got, err)
	}
	if !s.Exists("slot1") || s.Exists("slot2") {
		t.Fatal("Exists wrong")
	}
	if err := s.Read("slot2", &got); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error %v", err)
	}
	s.Write("slot2", settings{})
	names, _ := s.List()
	if strings.Join(names, ",") != "slot1,slot2" {
		t.Fatalf("list %v", names)
	}
	if err := s.Delete("slot2"); err != nil || s.Exists("slot2") {
		t.Fatal("delete failed")
	}
	if err := s.Delete("slot2"); err != nil {
		t.Fatal("deleting a missing file should be fine")
	}
	// No temp files linger after writes.
	entries, _ := os.ReadDir(s.Path())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatal("temp file left behind")
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	s, _ := OpenAt(t.TempDir())
	defaults := settings{Version: 2, Volume: 0.8, Fullscreen: true, Name: "player"}
	got, err := Load(s, "settings", defaults)
	if err != nil || got != defaults {
		t.Fatalf("missing file should yield defaults: %+v %v", got, err)
	}
	// A partial file keeps defaults for the fields it lacks.
	os.WriteFile(filepath.Join(s.Path(), "settings.json"), []byte(`{"Volume": 0.2}`), 0o644)
	got, err = Load(s, "settings", defaults)
	if err != nil || got.Volume != 0.2 || !got.Fullscreen || got.Name != "player" {
		t.Fatalf("partial load %+v %v", got, err)
	}
	os.WriteFile(filepath.Join(s.Path(), "settings.json"), []byte(`{broken`), 0o644)
	if _, err = Load(s, "settings", defaults); err == nil {
		t.Fatal("corrupt file should error")
	}
}

func TestDir(t *testing.T) {
	t.Setenv("BUNYIP_DATA_DIR", "/tmp/x")
	if d, _ := Dir("game"); d != filepath.Join("/tmp/x", "game") {
		t.Fatalf("override dir %s", d)
	}
	t.Setenv("BUNYIP_DATA_DIR", "")
	d, err := Dir("game")
	if err != nil || !strings.HasSuffix(d, "game") {
		t.Fatalf("dir %s %v", d, err)
	}
}
