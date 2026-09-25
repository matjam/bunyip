package gfx

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestEngineShadersComplete checks that engineShaders names every SPIR-V
// program package shaders exports, so a changed or added engine shader
// changes the pipeline cache file's key.
func TestEngineShadersComplete(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("shaders/*.go")
	if err != nil {
		t.Fatal(err)
	}
	var exported []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				arr, ok := vs.Type.(*ast.ArrayType)
				if !ok || arr.Len != nil {
					continue
				}
				if id, ok := arr.Elt.(*ast.Ident); !ok || id.Name != "byte" {
					continue
				}
				for _, n := range vs.Names {
					if n.IsExported() {
						exported = append(exported, n.Name)
					}
				}
			}
		}
	}
	f, err := parser.ParseFile(fset, "pipes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "engineShaders" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "shaders" {
					listed = append(listed, sel.Sel.Name)
				}
			}
			return true
		})
	}
	if len(exported) == 0 {
		t.Fatal("found no SPIR-V programs in package shaders")
	}
	for _, name := range exported {
		if !slices.Contains(listed, name) {
			t.Errorf("engineShaders does not list shaders.%s", name)
		}
	}
	if len(engineShaders()) != len(listed) {
		t.Errorf("engineShaders returns %d programs, lists %d", len(engineShaders()), len(listed))
	}
}
