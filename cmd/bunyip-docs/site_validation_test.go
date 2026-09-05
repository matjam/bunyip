package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var htmlLinkRE = regexp.MustCompile(`(?:href|src)="([^"]+)"`)
var htmlIDRE = regexp.MustCompile(`\bid="([^"]+)"`)

// Check every link against the emitted files, including fragments from
// GoDoc and symbols.json. This also catches relative-path regressions in
// nested packages and references to APIs that no longer exist.
func checkSiteLinks(t *testing.T, site *Site, dir string) {
	t.Helper()
	anchors := map[string]map[string]bool{}
	for page, data := range site.pages {
		if !strings.HasSuffix(page, ".html") {
			continue
		}
		anchors[page] = map[string]bool{}
		for _, m := range htmlIDRE.FindAllStringSubmatch(string(data), -1) {
			id := html.UnescapeString(m[1])
			if anchors[page][id] {
				t.Errorf("%s: duplicate anchor %s", page, id)
			}
			anchors[page][id] = true
		}
	}
	check := func(page, raw string) {
		u, err := url.Parse(html.UnescapeString(raw))
		if err != nil {
			t.Errorf("%s: invalid link %q: %v", page, raw, err)
			return
		}
		if u.IsAbs() || u.Host != "" {
			return
		}
		from := &url.URL{Path: "/" + page}
		resolved := from.ResolveReference(u)
		target := strings.TrimPrefix(resolved.Path, "/")
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(target))); err != nil {
			t.Errorf("%s: link %s has no file %s", page, raw, target)
			return
		}
		if resolved.Fragment != "" && strings.HasSuffix(target, ".html") && !anchors[target][resolved.Fragment] {
			t.Errorf("%s: link %s has no anchor in %s", page, raw, target)
		}
	}
	for page, data := range site.pages {
		if !strings.HasSuffix(page, ".html") {
			continue
		}
		for _, m := range htmlLinkRE.FindAllStringSubmatch(string(data), -1) {
			check(page, m[1])
		}
	}
	for _, sym := range site.symbols {
		check("index.html", sym.URL)
		page, fragment, ok := strings.Cut(sym.URL, "#")
		if !ok {
			t.Errorf("symbol %s has no fragment", sym.Name)
			continue
		}
		mdPage := strings.TrimSuffix(page, ".html") + ".md"
		if !strings.Contains(string(site.pages[mdPage]), `id="`+html.EscapeString(fragment)+`"`) {
			t.Errorf("%s: symbol %s missing Markdown anchor", mdPage, sym.Name)
		}
	}
}

// Derive expected symbols from source independently of go/doc's grouping,
// so omissions of associated functions, values or entire packages fail.
func checkPublicAPICoverage(t *testing.T, site *Site) {
	t.Helper()
	indexed := map[string]bool{}
	for _, sym := range site.symbols {
		if indexed[sym.URL] {
			t.Errorf("duplicate search entry %s", sym.URL)
		}
		indexed[sym.URL] = true
	}
	err := filepath.WalkDir(moduleRoot, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if file != moduleRoot {
				switch entry.Name() {
				case "internal", "testdata", "third_party", "site", "bin", "docs", "examples":
					return filepath.SkipDir
				}
				if strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(moduleRoot, filepath.Dir(file))
		if err != nil {
			return err
		}
		if rel == "." {
			rel = ""
		}
		page := pkgURL(filepath.ToSlash(rel))
		check := func(name string) {
			if !indexed[page+"#"+name] {
				t.Errorf("%s: public API %s missing from search", file, name)
			}
		}
		for _, decl := range f.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if !decl.Name.IsExported() {
					continue
				}
				name := decl.Name.Name
				if decl.Recv != nil {
					recv := embeddedName(decl.Recv.List[0].Type)
					if recv == nil || !recv.IsExported() {
						continue
					}
					name = recv.Name + "." + name
				}
				check(name)
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if spec.Name.IsExported() {
							check(spec.Name.Name)
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if name.IsExported() {
								check(name.Name)
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
