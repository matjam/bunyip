package vk

import (
	"os"
	"path/filepath"

	"github.com/ebitengine/purego"
)

// platformLibraries prefers the Khronos loader (which finds MoltenVK through
// its ICD manifest and also enables validation layers) and falls back to
// MoltenVK itself, which exports vkGetInstanceProcAddr directly.
func platformLibraries() []string {
	names := []string{"libvulkan.1.dylib", "libvulkan.dylib", "libMoltenVK.dylib"}
	dirs := []string{"/opt/homebrew/lib", "/usr/local/lib"}
	if exe, err := os.Executable(); err == nil {
		// Inside an .app bundle, Contents/MacOS/<exe> sits beside Contents/Frameworks.
		dirs = append([]string{filepath.Join(filepath.Dir(exe), "..", "Frameworks"), filepath.Dir(exe)}, dirs...)
	}
	if sdk := os.Getenv("VULKAN_SDK"); sdk != "" {
		dirs = append([]string{filepath.Join(sdk, "lib")}, dirs...)
	}
	out := names[:len(names):len(names)] // bare names first: dyld's own search
	for _, dir := range dirs {
		for _, n := range names {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}

func openLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

func lookupSymbol(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}

// PrepareLayers makes Homebrew's layer manifests loadable. They name their
// library by bare file name, which dyld cannot resolve from a process that
// has no DYLD_LIBRARY_PATH, so each manifest is rewritten with an absolute
// library_path into a cache directory that VK_ADD_LAYER_PATH points at.
// Errors are ignored: layers are a development aid, never a requirement.
func PrepareLayers() {
	cache, err := os.UserCacheDir()
	if err != nil {
		return
	}
	dir := filepath.Join(cache, "bunyip", "vulkan-layers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	var wrote bool
	for _, manifestDir := range []string{"/opt/homebrew/share/vulkan/explicit_layer.d", "/usr/local/share/vulkan/explicit_layer.d"} {
		manifests, _ := filepath.Glob(filepath.Join(manifestDir, "*.json"))
		for _, m := range manifests {
			if rewriteManifest(m, filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(manifestDir))), "lib"), dir) {
				wrote = true
			}
		}
	}
	if wrote {
		os.Setenv("VK_ADD_LAYER_PATH", dir)
	}
}
