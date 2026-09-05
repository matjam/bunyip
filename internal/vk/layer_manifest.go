//go:build darwin || linux

package vk

import (
	"os"
	"path/filepath"
	"regexp"
)

// The macOS manifest rewrite is kept separate from dyld loading so its
// file publication can also be tested on Linux without a Vulkan driver.
var reLibraryPath = regexp.MustCompile(`"library_path"\s*:\s*"([^"/]+)"`)

// rewriteManifest copies manifest into dir with library_path made absolute
// against libDir, when the library exists there. The final name is only
// replaced after the complete file is closed, so concurrent loaders see
// either the previous manifest or the new one, never a truncated write.
func rewriteManifest(manifest, libDir, dir string) bool {
	data, err := os.ReadFile(manifest)
	if err != nil {
		return false
	}
	m := reLibraryPath.FindSubmatch(data)
	if m == nil {
		return false
	}
	lib := filepath.Join(libDir, string(m[1]))
	if _, err := os.Stat(lib); err != nil {
		return false
	}
	out := reLibraryPath.ReplaceAll(data, []byte(`"library_path": "`+lib+`"`))
	// Keep the temporary file beside its destination for atomic rename.
	// Its name must not end in .json: the Vulkan loader scans this directory
	// concurrently and must not discover an unfinished temporary manifest.
	tmp, err := os.CreateTemp(dir, ".vulkan-layer-*")
	if err != nil {
		return false
	}
	// Best-effort cleanup after a failed write or rename. After successful
	// publication the temporary name no longer exists.
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return false
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return false
	}
	if err := tmp.Close(); err != nil {
		return false
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, filepath.Base(manifest))) == nil
}
