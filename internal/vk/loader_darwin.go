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
