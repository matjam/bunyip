package main

import (
	"go/doc/comment"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureSite(t *testing.T) *Site {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		"api.go": `// Package bunyip links [Thing], [First], [Thing.Field] and [fmt.Sprint].
package bunyip
import "fmt"
const Top = 1
var Global = 1
type Number int
const (First Number = iota; Second)
var Default Number
type Thing struct { Field string }
func NewThing() *Thing { return nil }
func (*Thing) Run() {}
type Runner interface { Run() }
`,
		"nested/child/api.go":   "// Package child links [github.com/matjam/bunyip.Thing].\npackage child\n",
		"docs/guides/start.md":  "---\ntitle: Start\n---\n[Thing](../pkg/bunyip.html#Thing)\n![Shot](shot.png)\n",
		"examples/demo/main.go": "package main\nfunc main() {}\n",
		"docs/examples/demo.md": "---\ntitle: Demo\n---\n[Start](../guides/start.html)\n",
	} {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := build(root, filepath.Join(root, "docs/guides"), filepath.Join(root, "docs/examples"))
	if err != nil {
		t.Fatal(err)
	}
	s.Base = "https://example.test/docs/"
	if err := s.write(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSymbolCoverageAndAnchors(t *testing.T) {
	s := fixtureSite(t)
	want := map[string]string{"Top": "const", "Global": "var", "Number": "type", "First": "const", "Second": "const", "Default": "var", "Thing": "type", "NewThing": "func", "Thing.Run": "method", "Thing.Field": "field", "Runner": "type", "Runner.Run": "method"}
	seen := map[string]bool{}
	for _, sym := range s.symbols {
		if seen[sym.URL] {
			t.Errorf("duplicate symbol %s", sym.URL)
		}
		seen[sym.URL] = true
		if sym.Pkg != "bunyip" {
			continue
		}
		if want[sym.Name] != sym.Kind {
			t.Errorf("%s kind = %s, want %s", sym.Name, sym.Kind, want[sym.Name])
		}
		delete(want, sym.Name)
		for _, page := range []string{"pkg/bunyip.html", "pkg/bunyip.md"} {
			if !strings.Contains(string(s.pages[page]), `id="`+sym.Name+`"`) {
				t.Errorf("%s missing anchor %s", page, sym.Name)
			}
		}
	}
	for name := range want {
		t.Errorf("missing symbol %s", name)
	}
	if got := s.Packages[0].Types[0].Src; !strings.HasPrefix(got, repo+"/blob/main/api.go#L") {
		t.Errorf("source outside root: %s", got)
	}
	for page, link := range map[string]string{"pkg/bunyip.html": `href="bunyip.html#Thing"`, "pkg/nested/child.html": `href="../bunyip.html#Thing"`} {
		if !strings.Contains(string(s.pages[page]), link) {
			t.Errorf("%s missing %s", page, link)
		}
	}
	full := string(s.pages["llms-full.txt"])
	for _, link := range []string{"https://example.test/docs/pkg/bunyip.md#Thing", "https://example.test/docs/guides/shot.png", "https://example.test/docs/guides/start.md"} {
		if !strings.Contains(full, link) {
			t.Errorf("aggregate missing %s", link)
		}
	}
}

func TestDocLinkURL(t *testing.T) {
	r := renderer{rel: "nested/child"}
	for _, tc := range []struct {
		name string
		link comment.DocLink
		want string
	}{
		{"local", comment.DocLink{Name: "Thing"}, "child.html#Thing"},
		{"root", comment.DocLink{ImportPath: module, Name: "Thing"}, "../bunyip.html#Thing"},
		{"external", comment.DocLink{ImportPath: "fmt", Name: "Sprint"}, "https://pkg.go.dev/fmt#Sprint"},
		{"external method", comment.DocLink{ImportPath: "strings", Recv: "Builder", Name: "Write"}, "https://pkg.go.dev/strings#Builder.Write"},
		{"prefix boundary", comment.DocLink{ImportPath: module + "x", Name: "Thing"}, "https://pkg.go.dev/" + module + "x#Thing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.docLinkURL(&tc.link, ".html"); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestMarkdownLinksPreserveExternalAndCode(t *testing.T) {
	input := "[Local](../pkg/gfx.html#Graphics) [External](https://example.test/api.html#A)\n" +
		"`[code](code.html)`\n\n```md\n[fenced](fenced.html)\n```\n\n    [indented](indented.html)\n\n[Ref][r]\n[r]: nested.html#A\n"
	want := strings.ReplaceAll(strings.ReplaceAll(input, "../pkg/gfx.html#Graphics", "../pkg/gfx.md#Graphics"), "[r]: nested.html#A", "[r]: nested.md#A")
	if got := markdownLinks(input); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestLoadPackagesReportsTraversalError(t *testing.T) {
	if err := (&Site{}).loadPackages(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing root must fail")
	}
}

func TestCopyImagesReportsReadError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyImages(file, t.TempDir()); err == nil {
		t.Fatal("a non-directory image source must fail")
	}
	if err := copyImages(filepath.Join(t.TempDir(), "missing"), t.TempDir()); err != nil {
		t.Fatalf("missing optional images directory: %v", err)
	}
}

func TestMarkdownLinkDestinations(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`[A](a.html)[B](b.html)`, `[A](a.md)[B](b.md)`},
		{`[A](<a.html#Case> "Title")`, `[A](<a.md#Case> "Title")`},
		{`[A](a(b).html?query=1#Case)`, `[A](a(b).md?query=1#Case)`},
		{`[A](//example.test/a.html)`, `[A](//example.test/a.html)`},
		{"~~~md\n[code](a.html)\n~~~\n", "~~~md\n[code](a.html)\n~~~\n"},
		{"``[code](a.html)``", "``[code](a.html)``"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := markdownLinks(tc.input); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPackageGrouping(t *testing.T) {
	s := &Site{Packages: []*Package{{Rel: "gfx/ktx2"}, {Rel: "gfx/shaders"}, {Rel: "grid/autotile"}}}
	s.group()
	want := map[string]string{"gfx/ktx2": "Graphics", "gfx/shaders": "Graphics", "grid/autotile": "Services"}
	for _, group := range s.Groups {
		for _, p := range group.Packages {
			if group.Title != want[p.Rel] {
				t.Errorf("%s in %s, want %s", p.Rel, group.Title, want[p.Rel])
			}
			delete(want, p.Rel)
		}
	}
	if len(want) != 0 {
		t.Errorf("ungrouped: %v", want)
	}
}
