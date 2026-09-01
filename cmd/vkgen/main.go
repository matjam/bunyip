// Command vkgen generates the Vulkan binding in internal/vk from the Khronos
// registry. It covers the core versions up to -version and the extensions in
// -ext, closed over every type those definitions mention, and emits a test
// that pins each struct's C layout.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const defaultExtensions = "VK_KHR_surface,VK_KHR_swapchain,VK_KHR_get_surface_capabilities2," +
	"VK_EXT_metal_surface,VK_KHR_win32_surface,VK_KHR_wayland_surface,VK_KHR_xcb_surface,VK_KHR_xlib_surface," +
	"VK_KHR_portability_enumeration,VK_KHR_portability_subset,VK_EXT_debug_utils,VK_EXT_headless_surface," +
	"VK_KHR_dynamic_rendering,VK_KHR_synchronization2"

func main() {
	registryPath := flag.String("registry", "../../third_party/vulkan/vk.xml", "path to vk.xml")
	out := flag.String("out", ".", "output directory")
	pkg := flag.String("pkg", "vk", "package name")
	version := flag.String("version", "1.3", "highest core version to include")
	ext := flag.String("ext", defaultExtensions, "comma-separated extensions to include")
	flag.Parse()
	if err := run(*registryPath, *out, *pkg, *version, strings.Split(*ext, ",")); err != nil {
		fmt.Fprintln(os.Stderr, "vkgen:", err)
		os.Exit(1)
	}
}

func run(registryPath, out, pkg, version string, exts []string) error {
	reg, err := loadRegistry(registryPath)
	if err != nil {
		return err
	}
	m, err := buildModel(reg)
	if err != nil {
		return err
	}
	sel, err := m.selectAPI(reg, version, exts)
	if err != nil {
		return err
	}
	if err := m.checkConsts(); err != nil {
		return err
	}
	files := []func() (*file, error){
		func() (*file, error) { return m.emitBaseTypes(sel, pkg) },
		func() (*file, error) { return m.emitConstants(pkg), nil },
		func() (*file, error) { return m.emitEnums(sel, pkg), nil },
		func() (*file, error) { return m.emitStructs(sel, pkg) },
		func() (*file, error) { return m.emitUnions(sel, pkg) },
		func() (*file, error) { return m.emitCommands(sel, pkg) },
		func() (*file, error) { return m.emitLoader(sel, pkg), nil },
		func() (*file, error) { return m.emitLayoutTest(sel, pkg) },
	}
	for _, gen := range files {
		f, err := gen()
		if err != nil {
			return err
		}
		if err := f.write(out); err != nil {
			return err
		}
	}
	fmt.Printf("vkgen: %d types, %d commands, header %s\n", len(sel.Types), len(sel.Cmds), m.HeaderVersion)
	return nil
}
