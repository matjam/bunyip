package platform

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Host policy cannot be exercised on every build machine. Check the native
// startup boundaries without loading AppKit or creating a desktop window.
func TestEmbeddingPreservesHostApplicationPolicy(t *testing.T) {
	for _, tc := range []struct {
		file      string
		forbidden []string
	}{{"app_darwin.go", []string{"setActivationPolicy", "finishLaunching", "activateIgnoringOtherApps"}}, {"win32_windows.go", []string{"procSetProcessDpiAwarenessCt.Call"}}} {
		data, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, tc.file, data, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "NewApp" {
				continue
			}
			body := string(data[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset])
			for _, s := range tc.forbidden {
				if strings.Contains(body, s) {
					t.Errorf("%s NewApp overrides borrowed host policy with %s", tc.file, s)
				}
			}
		}
	}
}
