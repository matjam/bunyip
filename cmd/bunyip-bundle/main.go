// Command bunyip-bundle packages a built game for distribution: a .app
// bundle on macOS with the Vulkan loader and MoltenVK inside, or a plain
// folder with the executable and assets elsewhere.
// Missing macOS Vulkan libraries produce warnings rather than a build
// error; inspect the output before distributing it as a self-contained app.
//
//	bunyip-bundle -name "My Game" -id com.example.mygame -exe ./mygame -assets ./assets -o dist
//
// -name and -exe are required. -target selects the output layout and
// defaults to the host OS; it does not cross-compile the executable.
// -o defaults to dist. Optional -assets copies a directory or pack file.
// For macOS, -icon takes an .icns file, -version defaults to 1.0, and
// -vulkan names the directory containing libvulkan.1.dylib and
// libMoltenVK.dylib (otherwise the usual Homebrew paths are tried).
// Without -id the bundle identifier is com.example. followed by the
// lowercased display name with spaces removed. The tool does not sign,
// notarize or build an installer for the result.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	name := flag.String("name", "", "display name of the game (required)")
	id := flag.String("id", "", "bundle identifier, like com.example.game (macOS)")
	exe := flag.String("exe", "", "path to the built executable (required)")
	assets := flag.String("assets", "", "asset directory or pack file to copy alongside")
	icon := flag.String("icon", "", "icon file (.icns on macOS)")
	out := flag.String("o", "dist", "output directory")
	vulkan := flag.String("vulkan", "", "directory holding libvulkan.1.dylib and libMoltenVK.dylib (macOS; default: Homebrew's lib)")
	target := flag.String("target", runtime.GOOS, "darwin, linux or windows: how to lay out the output")
	version := flag.String("version", "1.0", "version string")
	flag.Parse()
	if *name == "" || *exe == "" {
		fmt.Fprintln(os.Stderr, "usage: bunyip-bundle -name NAME -exe PATH [-id ID] [-assets DIR] [-icon FILE] [-o DIR]")
		os.Exit(2)
	}
	var err error
	switch *target {
	case "darwin":
		if *id == "" {
			*id = "com.example." + strings.ToLower(strings.ReplaceAll(*name, " ", ""))
		}
		err = bundleMac(*name, *id, *exe, *assets, *icon, *out, *vulkan, *version)
	default:
		err = bundleFolder(*name, *exe, *assets, *out)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bunyip-bundle:", err)
		os.Exit(1)
	}
}

func bundleMac(name, id, exe, assets, icon, out, vulkan, version string) error {
	app := filepath.Join(out, name+".app")
	contents := filepath.Join(app, "Contents")
	for _, d := range []string{"MacOS", "Resources", "Frameworks"} {
		if err := os.MkdirAll(filepath.Join(contents, d), 0o755); err != nil {
			return err
		}
	}
	exeName := filepath.Base(exe)
	if err := copyFile(exe, filepath.Join(contents, "MacOS", exeName), 0o755); err != nil {
		return err
	}
	if assets != "" {
		if err := copyTree(assets, filepath.Join(contents, "Resources", filepath.Base(assets))); err != nil {
			return err
		}
	}
	iconLine := ""
	if icon != "" {
		if err := copyFile(icon, filepath.Join(contents, "Resources", "icon.icns"), 0o644); err != nil {
			return err
		}
		iconLine = "\t<key>CFBundleIconFile</key>\n\t<string>icon</string>\n"
	}
	if vulkan == "" {
		vulkan = "/opt/homebrew/lib"
		if _, err := os.Stat(filepath.Join(vulkan, "libvulkan.1.dylib")); err != nil {
			vulkan = "/usr/local/lib"
		}
	}
	for _, lib := range []string{"libvulkan.1.dylib", "libMoltenVK.dylib"} {
		src := filepath.Join(vulkan, lib)
		if _, err := os.Stat(src); err != nil {
			fmt.Fprintf(os.Stderr, "bunyip-bundle: %s not found in %s; the app will need Vulkan installed\n", lib, vulkan)
			continue
		}
		if err := copyFile(src, filepath.Join(contents, "Frameworks", lib), 0o755); err != nil {
			return err
		}
	}
	// The Vulkan loader looks inside the bundle for driver manifests at
	// Contents/Resources/vulkan/icd.d, so the app carries its own MoltenVK.
	icdDir := filepath.Join(contents, "Resources", "vulkan", "icd.d")
	if err := os.MkdirAll(icdDir, 0o755); err != nil {
		return err
	}
	icd := `{"file_format_version":"1.0.0","ICD":{"library_path":"../../../Frameworks/libMoltenVK.dylib","api_version":"1.2.0","is_portability_driver":true}}`
	if err := os.WriteFile(filepath.Join(icdDir, "MoltenVK_icd.json"), []byte(icd), 0o644); err != nil {
		return err
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>%s</string>
	<key>CFBundleDisplayName</key>
	<string>%s</string>
	<key>CFBundleIdentifier</key>
	<string>%s</string>
	<key>CFBundleExecutable</key>
	<string>%s</string>
	<key>CFBundleVersion</key>
	<string>%s</string>
	<key>CFBundleShortVersionString</key>
	<string>%s</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>LSMinimumSystemVersion</key>
	<string>12.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.games</string>
%s</dict>
</plist>
`, name, name, id, exeName, version, version, iconLine)
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", app)
	return nil
}

func bundleFolder(name, exe, assets, out string) error {
	dir := filepath.Join(out, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := copyFile(exe, filepath.Join(dir, filepath.Base(exe)), 0o755); err != nil {
		return err
	}
	if assets != "" {
		if err := copyTree(assets, filepath.Join(dir, filepath.Base(assets))); err != nil {
			return err
		}
	}
	fmt.Println("wrote", dir)
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	o, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer o.Close()
	_, err = io.Copy(o, in)
	return err
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst, 0o644)
	}
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		return copyFile(p, filepath.Join(dst, rel), 0o644)
	})
}
