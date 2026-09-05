// The tests here keep the example walkthroughs in docs/examples in step
// with the programs in examples. A walkthrough quotes its example
// verbatim, so a change to an example that is not carried into its
// walkthrough is a failure here rather than a wrong page on the site.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	exampleSrc = "../../examples"      // the example programs
	exampleDoc = "../../docs/examples" // one Markdown walkthrough per example
	guideDoc   = "../../docs/guides"
	moduleRoot = "../.."
)

// allowMissing reports whether a walkthrough that has not been written
// yet is tolerated. It exists for the window in which the walkthroughs
// are being written on separate branches, and must not be set in
// continuous integration.
func allowMissing() bool { return os.Getenv("BUNYIP_DOCS_ALLOW_MISSING") == "1" }

// exampleNames lists every directory under examples that holds a
// main.go, which is every program the site documents.
func exampleNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(exampleSrc)
	if err != nil {
		t.Fatalf("read %s: %v", exampleSrc, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(exampleSrc, e.Name(), "main.go")); err == nil {
			names = append(names, e.Name())
		}
	}
	return names
}

// TestEveryExampleHasAWalkthrough checks that no example is left
// undocumented. Every directory under examples with a main.go needs
// docs/examples/<dir>.md, including the ones the examples test skips
// running: an example that cannot run on a test machine still has
// source to explain.
func TestEveryExampleHasAWalkthrough(t *testing.T) {
	if allowMissing() {
		t.Skip("BUNYIP_DOCS_ALLOW_MISSING=1: missing walkthroughs tolerated while they are being written; never set this in CI")
	}
	for _, name := range exampleNames(t) {
		if _, err := os.Stat(filepath.Join(exampleDoc, name+".md")); err != nil {
			t.Errorf("examples/%s has no walkthrough: write docs/examples/%s.md", name, name)
		}
	}
}

// TestWalkthroughsMatchTheirExamples checks every walkthrough against
// the program it documents: the front matter, the example it names, that
// each Go excerpt is a verbatim run of lines from the file it quotes,
// and that no top-level declaration of the program is left unquoted.
func TestWalkthroughsMatchTheirExamples(t *testing.T) {
	entries, err := os.ReadDir(exampleDoc)
	if err != nil {
		t.Fatalf("read %s: %v", exampleDoc, err)
	}
	found := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		found = true
		t.Run(strings.TrimSuffix(e.Name(), ".md"), func(t *testing.T) {
			checkWalkthrough(t, e.Name())
		})
	}
	if !found {
		t.Errorf("no walkthroughs in %s", exampleDoc)
	}
}

func checkWalkthrough(t *testing.T, file string) {
	t.Helper()
	doc := "docs/examples/" + file
	raw, err := os.ReadFile(filepath.Join(exampleDoc, file))
	if err != nil {
		t.Fatalf("%s: %v", doc, err)
	}
	meta, body := frontMatter(string(raw))
	for _, key := range []string{"title", "example", "summary"} {
		if meta[key] == "" {
			t.Errorf("%s: front matter has no %s key", doc, key)
		}
	}
	name := meta["example"]
	if want := strings.TrimSuffix(file, ".md"); name != want {
		t.Errorf("%s: front matter says example: %q, but the file is named for %q", doc, name, want)
		return
	}
	dir := filepath.Join(exampleSrc, name)
	if _, err := os.Stat(filepath.Join(dir, "main.go")); err != nil {
		t.Errorf("%s: names example %q, which has no examples/%s/main.go", doc, name, name)
		return
	}
	// The line offset of the body inside the file, so a message about an
	// excerpt names the line the reader sees in an editor.
	offset := strings.Count(string(raw), "\n") - strings.Count(body, "\n")

	sources := map[string][]string{}     // file name to its lines
	covered := map[string]map[int]bool{} // file name to the lines quoted
	load := func(fileName string) ([]string, bool) {
		if lines, ok := sources[fileName]; ok {
			return lines, true
		}
		data, err := os.ReadFile(filepath.Join(dir, fileName))
		if err != nil {
			return nil, false
		}
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		sources[fileName] = lines
		covered[fileName] = map[int]bool{}
		return lines, true
	}

	for _, ex := range goExcerpts(body) {
		at := offset + ex.line
		lines, ok := load(ex.file)
		if !ok {
			t.Errorf("%s: excerpt at line %d quotes %s, which examples/%s does not have; first line: %q",
				doc, at, ex.file, name, first(ex.lines))
			continue
		}
		matches := findRuns(lines, ex.lines)
		if len(matches) == 0 {
			t.Errorf("%s: excerpt at line %d is not in examples/%s/%s; first line: %q\n"+
				"\tthe example changed under the walkthrough: copy the lines back out of the source, keeping tabs and blank lines",
				doc, at, name, ex.file, first(ex.lines))
			continue
		}
		for _, start := range matches {
			for i := range ex.lines {
				covered[ex.file][start+i] = true
			}
		}
	}

	// Every top-level declaration of every Go file in the example has to
	// appear in some excerpt, so a reader of the walkthrough sees the
	// whole program.
	names, err := goFiles(dir)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	for _, fileName := range names {
		if _, ok := sources[fileName]; !ok {
			load(fileName)
		}
		for _, d := range declarations(t, filepath.Join(dir, fileName)) {
			if !covered[fileName][d.line] {
				t.Errorf("%s: examples/%s/%s line %d, %s, is in no excerpt; quote it in a section of the walkthrough",
					doc, name, fileName, d.line, d.what)
			}
		}
	}
}

// excerpt is one fenced Go block of a walkthrough.
type excerpt struct {
	file  string   // the example file it quotes
	line  int      // the line of the opening fence within the body
	lines []string // the quoted lines
}

// filePrefix opens the comment that points an excerpt at a file other
// than main.go: <!-- file: dungeon.go -->
const filePrefix = "<!-- file:"

// goExcerpts collects the fenced go blocks of a Markdown body, in order.
// A block is quoted from main.go unless the line before its fence is a
// file comment.
func goExcerpts(body string) []excerpt {
	lines := strings.Split(body, "\n")
	var out []excerpt
	for i := 0; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " ") != "```go" {
			continue
		}
		from := "main.go"
		if i > 0 {
			if prev := strings.TrimSpace(lines[i-1]); strings.HasPrefix(prev, filePrefix) && strings.HasSuffix(prev, "-->") {
				from = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(prev, filePrefix), "-->"))
			}
		}
		var quoted []string
		j := i + 1
		for ; j < len(lines) && strings.TrimRight(lines[j], " ") != "```"; j++ {
			quoted = append(quoted, lines[j])
		}
		out = append(out, excerpt{file: from, line: i + 1, lines: quoted})
		i = j
	}
	return out
}

// findRuns returns every index in lines where want appears as a
// contiguous run.
func findRuns(lines, want []string) []int {
	var out []int
	if len(want) == 0 || len(want) > len(lines) {
		return nil
	}
	for i := 0; i+len(want) <= len(lines); i++ {
		match := true
		for j, w := range want {
			if lines[i+j] != w {
				match = false
				break
			}
		}
		if match {
			out = append(out, i+1) // one-based, to match go/token line numbers
		}
	}
	return out
}

func first(lines []string) string {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

// decl is one top-level declaration and the line it starts on.
type decl struct {
	what string
	line int
}

// declarations lists the file-scope types, functions, variables and
// constants of one Go file. Imports are left out; they are part of the
// file rather than of the program a reader follows.
func declarations(t *testing.T, path string) []decl {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	line := func(p token.Pos) int { return fset.Position(p).Line }
	var out []decl
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			what := "func " + d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				what = "method " + types(d.Recv.List[0].Type) + "." + d.Name.Name
			}
			out = append(out, decl{what: what, line: line(d.Pos())})
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			for _, spec := range d.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					out = append(out, decl{what: "type " + spec.Name.Name, line: line(spec.Pos())})
				case *ast.ValueSpec:
					var names []string
					for _, n := range spec.Names {
						names = append(names, n.Name)
					}
					out = append(out, decl{what: d.Tok.String() + " " + strings.Join(names, ", "), line: line(spec.Pos())})
				}
			}
		}
	}
	return out
}

// types names a receiver type for a failure message.
func types(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return "*" + types(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return types(e.X)
	case *ast.IndexListExpr:
		return types(e.X)
	}
	return "?"
}

// TestSiteBuilds renders the whole site into a temporary directory, so a
// walkthrough that breaks the templates fails here rather than on the
// published site.
func TestSiteBuilds(t *testing.T) {
	site, err := build(moduleRoot, guideDoc, exampleDoc)
	if err != nil {
		t.Fatal(err)
	}
	site.Base = siteURL
	if err := site.write(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, p := range site.Programs {
		if p.Missing && !allowMissing() {
			t.Errorf("examples/%s has no walkthrough", p.Name)
		}
		if _, ok := site.pages[p.MarkdownURL()]; !ok {
			t.Errorf("%s: no Markdown page rendered", p.Name)
		}
	}
	if _, ok := site.pages["examples/index.md"]; !ok {
		t.Error("no example index rendered")
	}
	full := string(site.pages["llms-full.txt"])
	for _, p := range site.Programs {
		if p.Missing {
			continue
		}
		if !strings.Contains(full, "# "+p.Title+"\n") {
			t.Errorf("%s: the walkthrough is not in llms-full.txt", p.Name)
		}
	}
	index := string(site.pages["llms.txt"])
	if !strings.Contains(index, "## Example programs") {
		t.Error("llms.txt does not list the example programs")
	}
}
