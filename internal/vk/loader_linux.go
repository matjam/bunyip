package vk

import "github.com/ebitengine/purego"

func platformLibraries() []string {
	return []string{"libvulkan.so.1", "libvulkan.so"}
}

func openLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

func lookupSymbol(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}

// PrepareLayers is a no-op: the loader finds layer libraries itself here.
func PrepareLayers() {}
