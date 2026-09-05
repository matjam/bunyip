package save_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/matjam/bunyip/save"
)

type reviewSettings struct {
	Keys []string
	Tags map[string]int
}

// Load fills in from the defaults without writing into them.
func TestLoadLeavesDefaultsAlone(t *testing.T) {
	st, err := save.OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("cfg", reviewSettings{Keys: []string{"x"}, Tags: map[string]int{"b": 2}}); err != nil {
		t.Fatal(err)
	}
	defaults := reviewSettings{Keys: []string{"a", "b"}, Tags: map[string]int{"a": 1}}
	got, err := st.Load("cfg", defaults)
	if err != nil {
		t.Fatal(err)
	}
	if got.Keys[0] != "x" {
		t.Errorf("loaded %+v", got)
	}
	if defaults.Keys[0] != "a" || len(defaults.Tags) != 1 {
		t.Errorf("Load changed the caller's defaults: %+v", defaults)
	}
}

// Names are file names, not paths: nothing outside the store is touched.
func TestNamesStayInsideStore(t *testing.T) {
	root := t.TempDir()
	st, err := save.OpenAt(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("../outside"); err == nil {
		t.Error("Delete accepted a name with a path separator")
	}
	if _, err := os.Stat(outside); errors.Is(err, os.ErrNotExist) {
		t.Fatal("Delete removed a file outside the store")
	}
	if err := st.Write("../outside", 1); err == nil {
		t.Error("Write accepted a name with a path separator")
	}
	if st.Exists("../outside") {
		t.Error("Exists looked outside the store")
	}
}
