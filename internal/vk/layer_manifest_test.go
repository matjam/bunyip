//go:build darwin || linux

package vk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func manifestFixture(t *testing.T) (manifest, libDir, cache string) {
	t.Helper()
	root := t.TempDir()
	libDir, cache = filepath.Join(root, "lib"), filepath.Join(root, "cache")
	for _, dir := range []string{libDir, cache} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest = filepath.Join(root, "VkLayer_khronos_validation.json")
	data := `{"file_format_version":"1.2.0","layer":{"name":"VK_LAYER_KHRONOS_validation","library_path":"libVkLayer_khronos_validation.dylib"}}`
	if err := os.WriteFile(manifest, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "libVkLayer_khronos_validation.dylib"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return manifest, libDir, cache
}

// A loader can open the old file just before another process publishes a
// replacement. Its descriptor must keep a complete, unchanged snapshot.
// This deterministically detects truncate-and-rewrite publication without
// relying on the scheduler to catch the interval before WriteFile writes.
func TestRewriteManifestPreservesOpenReader(t *testing.T) {
	manifest, libDir, cache := manifestFixture(t)
	target := filepath.Join(cache, filepath.Base(manifest))
	old := []byte(`{"layer":{"library_path":"/previous/lib.dylib"}}`)
	if err := os.WriteFile(target, old, 0o644); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if !rewriteManifest(manifest, libDir, cache) {
		t.Fatal("rewrite failed")
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, old) {
		t.Fatalf("publication changed an already-open manifest: got %s, want %s", got, old)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var rewritten struct {
		Layer struct {
			LibraryPath string `json:"library_path"`
		} `json:"layer"`
	}
	if err := json.Unmarshal(data, &rewritten); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(libDir, "libVkLayer_khronos_validation.dylib"); rewritten.Layer.LibraryPath != want {
		t.Errorf("library path = %q, want %q", rewritten.Layer.LibraryPath, want)
	}
}

func TestRewriteManifestConcurrentReaders(t *testing.T) {
	manifest, libDir, cache := manifestFixture(t)
	if !rewriteManifest(manifest, libDir, cache) {
		t.Fatal("initial rewrite failed")
	}
	var writers sync.WaitGroup
	errors := make(chan error, 5)
	start := make(chan struct{})
	for range 4 {
		writers.Go(func() {
			<-start
			for range 100 {
				if !rewriteManifest(manifest, libDir, cache) {
					errors <- fmt.Errorf("concurrent rewrite failed")
					return
				}
			}
		})
	}
	var readers sync.WaitGroup
	done := make(chan struct{})
	readers.Go(func() {
		<-start
		for {
			entries, err := os.ReadDir(cache)
			if err != nil {
				errors <- err
				return
			}
			for _, entry := range entries {
				// The loader discovers JSON manifests by extension. It must
				// never discover temporary files while they are being written.
				if !strings.HasSuffix(entry.Name(), ".json") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(cache, entry.Name()))
				if err != nil {
					errors <- err
					return
				}
				if !json.Valid(data) {
					errors <- fmt.Errorf("reader observed invalid JSON (%d bytes)", len(data))
					return
				}
			}
			select {
			case <-done:
				return
			default:
			}
		}
	})
	close(start)
	writers.Wait()
	close(done)
	readers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("temporary manifests remain: %v", entries)
	}
}

func TestRewriteManifestPublicationFailureCleansTemp(t *testing.T) {
	manifest, libDir, cache := manifestFixture(t)
	// A directory at the final path makes rename fail after writing the
	// complete temporary file, without depending on user permissions.
	if err := os.Mkdir(filepath.Join(cache, filepath.Base(manifest)), 0o755); err != nil {
		t.Fatal(err)
	}
	if rewriteManifest(manifest, libDir, cache) {
		t.Fatal("publication unexpectedly succeeded")
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("failed publication left temporary files: %v", entries)
	}
}
