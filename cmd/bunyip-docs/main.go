// Command bunyip-docs renders the module's documentation as a static
// website: guides written in Markdown, every package's godoc with its
// examples, a symbol search, and links back to the source on GitHub.
//
//	go run ./cmd/bunyip-docs -out site
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/doc"
	"go/doc/comment"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"html"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	mdparser "github.com/yuin/goldmark/parser"
	ghtml "github.com/yuin/goldmark/renderer/html"
)

//go:embed site.css site.js
var assets embed.FS

const (
	module  = "github.com/matjam/bunyip"
	repo    = "https://github.com/matjam/bunyip"
	siteURL = "https://matjam.github.io/bunyip/"
)

// groups orders the package sidebar; packages not listed fall into the
// last matching group by prefix.
var groups = []struct {
	Title string
	Paths []string
}{
	{"Engine", []string{"bunyip", "input"}},
	{"Graphics", []string{"gfx", "anim", "ui", "particle", "tiled", "gltf", "lin"}},
	{"Simulation", []string{"ecs", "phys", "orbit", "orbit/sol"}},
	{"Audio", []string{"audio", "audio/tracker"}},
	{"Services", []string{"asset", "save", "locale", "rng", "timer", "tween", "grid", "network"}},
	{"Tools", []string{"cmd/"}},
	{"Example programs", []string{"examples/"}},
}

// guideGroups orders the guide sections, from a guide's `group` front
// matter key. A guide with no group, or with a group not listed here,
// falls into a trailing "Other" section.
var guideGroups = []string{"Start", "Engine", "Graphics", "Simulation", "Audio"}

func main() {
	out := flag.String("out", "site", "output directory")
	guides := flag.String("guides", "docs/guides", "directory of Markdown guides")
	base := flag.String("base", siteURL, "the URL the site is published at, for the llms.txt index")
	flag.Parse()
	site, err := build(".", *guides)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bunyip-docs:", err)
		os.Exit(1)
	}
	site.Base = strings.TrimSuffix(*base, "/") + "/"
	if err := site.write(*out); err != nil {
		fmt.Fprintln(os.Stderr, "bunyip-docs:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d pages to %s\n", len(site.pages), *out)
}

// Site is everything rendered.
type Site struct {
	Guides      []*Guide
	GuideGroups []GuideGroup
	Packages    []*Package
	Groups      []Group
	Base        string // the published URL, with a trailing slash
	pages       map[string][]byte
	symbols     []symbol
}

// Group is a sidebar section of packages.
type Group struct {
	Title    string
	Packages []*Package
}

// GuideGroup is a sidebar section of guides.
type GuideGroup struct {
	Title  string
	Guides []*Guide
}

// Guide is one Markdown page.
type Guide struct {
	Title, Slug, Summary, Group string
	Order                       int
	Body                        template.HTML
	Markdown                    string // the source, with the front matter replaced by a heading
	Headings                    []heading
}

type heading struct{ ID, Text string }

// Package is one rendered package.
type Package struct {
	Name, ImportPath, Rel, URL, Synopsis string
	IsCommand                            bool
	Doc                                  template.HTML
	DocMD                                string // the package comment as Markdown
	Consts, Vars                         []*Value
	Funcs                                []*Func
	Types                                []*Type
	Examples                             []*Example
	Files                                []string
}

// MarkdownURL is the package's Markdown page, beside the HTML one.
func (p *Package) MarkdownURL() string { return strings.TrimSuffix(p.URL, ".html") + ".md" }

// Value is a const or var block.
type Value struct {
	Names    []string
	Doc      template.HTML
	DocMD    string
	Decl     template.HTML
	DeclText string
	Src      string
}

// Func is a function or method.
type Func struct {
	Name, ID string
	Doc      template.HTML
	DocMD    string
	Decl     template.HTML
	DeclText string
	Src      string
	Examples []*Example
}

// Type is a type with its associated declarations.
type Type struct {
	Name         string
	Doc          template.HTML
	DocMD        string
	Decl         template.HTML
	DeclText     string
	Src          string
	Consts, Vars []*Value
	Funcs        []*Func // constructors
	Methods      []*Func
	Examples     []*Example
}

// Example is a runnable example with its code and output.
type Example struct {
	Name, Suffix string
	Doc          template.HTML
	DocMD        string
	Code         template.HTML
	CodeText     string
	Output       string
}

type symbol struct {
	Name string `json:"name"`
	Pkg  string `json:"pkg"`
	URL  string `json:"url"`
	Kind string `json:"kind"`
}

func build(root, guideDir string) (*Site, error) {
	site := &Site{pages: map[string][]byte{}}
	if err := site.loadPackages(root); err != nil {
		return nil, err
	}
	if err := site.loadGuides(guideDir); err != nil {
		return nil, err
	}
	site.group()
	return site, nil
}

// loadPackages walks the module for Go packages outside internal and
// testdata directories.
func (s *Site) loadPackages(root string) error {
	var dirs []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "internal" || name == "testdata" || name == "third_party" || name == "site" || name == "bin" || name == "docs") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			dir := filepath.Dir(p)
			if len(dirs) == 0 || dirs[len(dirs)-1] != dir {
				dirs = append(dirs, dir)
			}
		}
		return nil
	})
	sort.Strings(dirs)
	seen := map[string]bool{}
	for _, dir := range dirs {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		rel, _ := filepath.Rel(root, dir)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		pkg, err := s.loadPackage(dir, rel)
		if err != nil {
			return err
		}
		if pkg != nil {
			s.Packages = append(s.Packages, pkg)
		}
	}
	return nil
}

func (s *Site) loadPackage(dir, rel string) (*Package, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	var files []*ast.File
	var pkgName string
	var fileNames []string
	for _, p := range pkgs {
		for name, f := range p.Files {
			files = append(files, f)
			fileNames = append(fileNames, filepath.Base(name))
			if !strings.HasSuffix(p.Name, "_test") {
				pkgName = p.Name
			}
		}
	}
	if pkgName == "" {
		return nil, nil
	}
	sort.Slice(files, func(i, j int) bool { return fset.File(files[i].Pos()).Name() < fset.File(files[j].Pos()).Name() })
	importPath := module
	if rel != "" {
		importPath = module + "/" + rel
	}
	dp, err := doc.NewFromFiles(fset, files, importPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	r := &renderer{fset: fset, pkg: dp, rel: rel, site: s}
	p := &Package{Name: pkgName, ImportPath: importPath, Rel: rel, URL: pkgURL(rel), IsCommand: pkgName == "main",
		Synopsis: dp.Synopsis(dp.Doc), Doc: r.doc(dp.Doc), DocMD: r.md(dp.Doc), Files: fileNames}
	sort.Strings(p.Files)
	for _, v := range dp.Consts {
		p.Consts = append(p.Consts, r.value(v))
	}
	for _, v := range dp.Vars {
		p.Vars = append(p.Vars, r.value(v))
	}
	for _, f := range dp.Funcs {
		p.Funcs = append(p.Funcs, r.fn(f, ""))
	}
	for _, t := range dp.Types {
		tt := &Type{Name: t.Name, Doc: r.doc(t.Doc), DocMD: r.md(t.Doc), Decl: r.decl(t.Decl), DeclText: r.declText(t.Decl), Src: r.src(t.Decl), Examples: r.examples(t.Examples)}
		for _, v := range t.Consts {
			tt.Consts = append(tt.Consts, r.value(v))
		}
		for _, v := range t.Vars {
			tt.Vars = append(tt.Vars, r.value(v))
		}
		for _, f := range t.Funcs {
			tt.Funcs = append(tt.Funcs, r.fn(f, ""))
		}
		for _, m := range t.Methods {
			tt.Methods = append(tt.Methods, r.fn(m, t.Name))
		}
		p.Types = append(p.Types, tt)
		s.symbols = append(s.symbols, symbol{Name: t.Name, Pkg: shortName(rel), URL: p.URL + "#" + t.Name, Kind: "type"})
		for _, m := range tt.Methods {
			s.symbols = append(s.symbols, symbol{Name: t.Name + "." + m.Name, Pkg: shortName(rel), URL: p.URL + "#" + m.ID, Kind: "method"})
		}
	}
	for _, f := range p.Funcs {
		s.symbols = append(s.symbols, symbol{Name: f.Name, Pkg: shortName(rel), URL: p.URL + "#" + f.ID, Kind: "func"})
	}
	p.Examples = r.examples(dp.Examples)
	return p, nil
}

func shortName(rel string) string {
	if rel == "" {
		return "bunyip"
	}
	return rel
}

func pkgURL(rel string) string {
	if rel == "" {
		return "pkg/bunyip.html"
	}
	return "pkg/" + rel + ".html"
}

// renderer turns go/doc structures into HTML fragments.
type renderer struct {
	fset *token.FileSet
	pkg  *doc.Package
	rel  string
	site *Site
}

func (r *renderer) doc(text string) template.HTML {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	p := r.pkg.Printer()
	p.HeadingLevel = 3
	p.DocLinkURL = func(link *comment.DocLink) string {
		rel := r.rel
		if link.ImportPath != "" {
			if !strings.HasPrefix(link.ImportPath, module) {
				return "https://pkg.go.dev/" + link.ImportPath
			}
			rel = strings.TrimPrefix(strings.TrimPrefix(link.ImportPath, module), "/")
		}
		url := pkgURL(rel)
		switch {
		case link.Recv != "" && link.Name != "":
			return url + "#" + link.Recv + "." + link.Name
		case link.Name != "":
			return url + "#" + link.Name
		}
		return url
	}
	return template.HTML(p.HTML(r.pkg.Parser().Parse(text)))
}

// md renders a doc comment as Markdown, for the pages language models
// read. Links to other symbols point at the Markdown pages.
func (r *renderer) md(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	p := r.pkg.Printer()
	p.HeadingLevel = 3
	p.DocLinkURL = func(link *comment.DocLink) string {
		rel := r.rel
		if link.ImportPath != "" {
			if !strings.HasPrefix(link.ImportPath, module) {
				return "https://pkg.go.dev/" + link.ImportPath
			}
			rel = strings.TrimPrefix(strings.TrimPrefix(link.ImportPath, module), "/")
		}
		url := strings.TrimSuffix(pkgURL(rel), ".html") + ".md"
		switch {
		case link.Recv != "" && link.Name != "":
			return url + "#" + link.Recv + "." + link.Name
		case link.Name != "":
			return url + "#" + link.Name
		}
		return url
	}
	return strings.TrimSpace(string(p.Markdown(r.pkg.Parser().Parse(text))))
}

func (r *renderer) value(v *doc.Value) *Value {
	return &Value{Names: v.Names, Doc: r.doc(v.Doc), DocMD: r.md(v.Doc), Decl: r.decl(v.Decl), DeclText: r.declText(v.Decl), Src: r.src(v.Decl)}
}

func (r *renderer) fn(f *doc.Func, recv string) *Func {
	decl := *f.Decl
	decl.Body = nil
	decl.Doc = nil
	id := f.Name
	if recv != "" {
		id = recv + "." + f.Name
	}
	return &Func{Name: f.Name, ID: id, Doc: r.doc(f.Doc), DocMD: r.md(f.Doc), Decl: r.decl(&decl), DeclText: r.declText(&decl), Src: r.src(f.Decl), Examples: r.examples(f.Examples)}
}

// decl prints a declaration without its doc comment and highlights it.
func (r *renderer) decl(d ast.Node) template.HTML { return highlight(r.declText(d)) }

// declText prints a declaration without its doc comment. Large
// composite literals in variable initialisers are elided, as the value
// is data rather than API.
func (r *renderer) declText(d ast.Node) string {
	elided := false
	if gd, ok := d.(*ast.GenDecl); ok {
		cp := *gd
		cp.Doc = nil
		if gd.Tok == token.VAR {
			cp.Specs = nil
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					cp.Specs = append(cp.Specs, spec)
					continue
				}
				v := *vs
				v.Values = nil
				for _, val := range vs.Values {
					if lit, ok := val.(*ast.CompositeLit); ok && len(lit.Elts) > 3 {
						l := *lit
						l.Elts = nil
						l.Lbrace, l.Rbrace = token.NoPos, token.NoPos
						val = &l
						elided = true
					}
					v.Values = append(v.Values, val)
				}
				cp.Specs = append(cp.Specs, &v)
			}
		}
		d = &cp
	}
	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	cfg.Fprint(&buf, r.fset, d)
	text := buf.String()
	if elided {
		text = strings.ReplaceAll(text, "{}", "{ /* … */ }")
	}
	return text
}

func (r *renderer) src(d ast.Node) string {
	pos := r.fset.Position(d.Pos())
	rel, _ := filepath.Rel(".", pos.Filename)
	return fmt.Sprintf("%s/blob/main/%s#L%d", repo, filepath.ToSlash(rel), pos.Line)
}

func (r *renderer) examples(list []*doc.Example) []*Example {
	var out []*Example
	for _, ex := range list {
		var buf bytes.Buffer
		cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
		node := ex.Code
		if ex.Play != nil {
			node = ex.Play
		}
		cfg.Fprint(&buf, r.fset, node)
		code := buf.String()
		if _, ok := node.(*ast.BlockStmt); ok {
			code = unwrapBlock(code)
		}
		name := ex.Suffix
		if name == "" {
			name = "Example"
		}
		out = append(out, &Example{Name: name, Suffix: ex.Suffix, Doc: r.doc(ex.Doc), DocMD: r.md(ex.Doc), Code: highlight(code), CodeText: code, Output: strings.TrimSpace(ex.Output)})
	}
	return out
}

// unwrapBlock strips the outer braces and one level of indentation from a
// printed function body.
func unwrapBlock(code string) string {
	lines := strings.Split(strings.TrimSpace(code), "\n")
	if len(lines) >= 2 && strings.TrimSpace(lines[0]) == "{" && strings.TrimSpace(lines[len(lines)-1]) == "}" {
		lines = lines[1 : len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, "\t")
	}
	return strings.Join(lines, "\n")
}

// highlight wraps Go tokens in spans for the stylesheet.
func highlight(src string) template.HTML {
	var s scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	s.Init(file, []byte(src), nil, scanner.ScanComments)
	var b strings.Builder
	last := 0
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		off := file.Offset(pos)
		if off < last || off > len(src) {
			continue
		}
		b.WriteString(html.EscapeString(src[last:off]))
		text := lit
		if text == "" || tok.IsOperator() {
			text = tok.String()
			if tok == token.SEMICOLON && lit == "\n" {
				text = ""
			}
		}
		if off+len(text) > len(src) {
			text = src[off:]
		}
		class := ""
		switch {
		case tok.IsKeyword():
			class = "kw"
		case tok == token.STRING || tok == token.CHAR:
			class = "str"
		case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
			class = "num"
		case tok == token.COMMENT:
			class = "cmt"
		case tok == token.IDENT && isBuiltinType(text):
			class = "typ"
		}
		if class != "" {
			fmt.Fprintf(&b, `<span class="%s">%s</span>`, class, html.EscapeString(text))
		} else {
			b.WriteString(html.EscapeString(text))
		}
		last = off + len(text)
	}
	if last < len(src) {
		b.WriteString(html.EscapeString(src[last:]))
	}
	return template.HTML(b.String())
}

func isBuiltinType(s string) bool {
	switch s {
	case "bool", "byte", "rune", "string", "error", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "any", "nil", "true", "false":
		return true
	}
	return false
}

// loadGuides renders every Markdown file in dir; a leading front matter
// block gives title, order and summary.
func (s *Site) loadGuides(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("guides: %w", err)
	}
	md := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(mdparser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(ghtml.WithUnsafe()))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		g := &Guide{Slug: strings.TrimSuffix(e.Name(), ".md"), Title: strings.TrimSuffix(e.Name(), ".md")}
		body := string(raw)
		if strings.HasPrefix(body, "---\n") {
			end := strings.Index(body[4:], "\n---")
			if end >= 0 {
				for _, line := range strings.Split(body[4:4+end], "\n") {
					k, v, _ := strings.Cut(line, ":")
					v = strings.TrimSpace(v)
					switch strings.TrimSpace(k) {
					case "title":
						g.Title = v
					case "order":
						g.Order, _ = strconv.Atoi(v)
					case "summary":
						g.Summary = v
					case "group":
						g.Group = v
					}
				}
				body = body[4+end+4:]
			}
		}
		// The Markdown copy links to the Markdown pages.
		g.Markdown = "# " + g.Title + "\n\n" + strings.TrimLeft(strings.ReplaceAll(body, ".html)", ".md)"), "\n")
		var buf bytes.Buffer
		if err := md.Convert([]byte(body), &buf); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		rendered := highlightFences(buf.String())
		g.Body = template.HTML(rendered)
		g.Headings = collectHeadings(rendered)
		s.Guides = append(s.Guides, g)
	}
	s.groupGuides()
	return nil
}

// groupGuides sorts the guides into their sections: the sections in the
// order guideGroups lists them with "Other" last, and inside a section
// by order then title. Site.Guides ends up in the same order, so the
// pages that read it flat follow the grouping too.
func (s *Site) groupGuides() {
	index := func(g *Guide) int {
		for i, title := range guideGroups {
			if g.Group == title {
				return i
			}
		}
		return len(guideGroups)
	}
	sort.Slice(s.Guides, func(i, j int) bool {
		a, b := s.Guides[i], s.Guides[j]
		if ai, bi := index(a), index(b); ai != bi {
			return ai < bi
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Title < b.Title
	})
	for i, title := range append(append([]string{}, guideGroups...), "Other") {
		grp := GuideGroup{Title: title}
		for _, g := range s.Guides {
			if index(g) == i {
				grp.Guides = append(grp.Guides, g)
			}
		}
		if len(grp.Guides) > 0 {
			s.GuideGroups = append(s.GuideGroups, grp)
		}
	}
}

var (
	fenceRE   = regexp.MustCompile(`(?s)<pre><code class="language-go">(.*?)</code></pre>`)
	headingRE = regexp.MustCompile(`<h2 id="([^"]+)">(.*?)</h2>`)
	tagRE     = regexp.MustCompile(`<[^>]+>`)
)

func highlightFences(s string) string {
	return fenceRE.ReplaceAllStringFunc(s, func(m string) string {
		inner := fenceRE.FindStringSubmatch(m)[1]
		return `<pre class="code"><code>` + string(highlight(html.UnescapeString(inner))) + `</code></pre>`
	})
}

func collectHeadings(s string) []heading {
	var out []heading
	for _, m := range headingRE.FindAllStringSubmatch(s, -1) {
		out = append(out, heading{ID: m[1], Text: tagRE.ReplaceAllString(m[2], "")})
	}
	return out
}

// group sorts packages into sidebar sections.
func (s *Site) group() {
	used := map[*Package]bool{}
	for _, g := range groups {
		grp := Group{Title: g.Title}
		for _, want := range g.Paths {
			for _, p := range s.Packages {
				rel := shortName(p.Rel)
				if used[p] {
					continue
				}
				if rel == want || (strings.HasSuffix(want, "/") && strings.HasPrefix(rel, want)) {
					grp.Packages = append(grp.Packages, p)
					used[p] = true
				}
			}
		}
		if len(grp.Packages) > 0 {
			s.Groups = append(s.Groups, grp)
		}
	}
	rest := Group{Title: "Other"}
	for _, p := range s.Packages {
		if !used[p] {
			rest.Packages = append(rest.Packages, p)
		}
	}
	if len(rest.Packages) > 0 {
		s.Groups = append(s.Groups, rest)
	}
}

// write renders every page into dir.
func (s *Site) write(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		return err
	}
	for _, name := range []string{"site.css", "site.js"} {
		data, _ := assets.ReadFile(name)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	syms, _ := json.Marshal(s.symbols)
	if err := os.WriteFile(filepath.Join(dir, "symbols.json"), syms, 0o644); err != nil {
		return err
	}
	// Images the guides refer to sit beside them in docs/guides and are
	// copied next to the rendered pages, so a guide's relative link works
	// both on the site and when the Markdown is read in the repository.
	if entries, err := os.ReadDir("docs/guides"); err == nil {
		if err := os.MkdirAll(filepath.Join(dir, "guides"), 0o755); err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() || strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join("docs/guides", e.Name()))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, "guides", e.Name()), data, 0o644); err != nil {
				return err
			}
		}
	}
	tmpl := template.Must(template.New("site").Funcs(template.FuncMap{
		"depth": func(url string) string { return strings.Repeat("../", strings.Count(url, "/")) },
		"short": shortName,
	}).Parse(layoutTmpl + indexTmpl + guideTmpl + packageTmpl))
	render := func(url, name string, data any) error {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
			return fmt.Errorf("%s: %w", url, err)
		}
		full := filepath.Join(dir, filepath.FromSlash(url))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		s.pages[url] = buf.Bytes()
		return os.WriteFile(full, buf.Bytes(), 0o644)
	}
	if err := render("index.html", "index", page{Site: s, URL: "index.html", Title: "Bunyip", Markdown: "llms.txt"}); err != nil {
		return err
	}
	// Every page is also written as Markdown, and llms.txt indexes them,
	// so a language model reads the documentation without parsing HTML.
	writeText := func(url, text string) error {
		full := filepath.Join(dir, filepath.FromSlash(url))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		s.pages[url] = []byte(text)
		return os.WriteFile(full, []byte(text), 0o644)
	}
	var full strings.Builder
	fmt.Fprintf(&full, "# Bunyip\n\n%s\n", llmsIntro)
	for _, g := range s.Guides {
		url := "guides/" + g.Slug + ".html"
		if err := render(url, "guide", page{Site: s, URL: url, Title: g.Title, Guide: g, Markdown: "guides/" + g.Slug + ".md"}); err != nil {
			return err
		}
		if err := writeText("guides/"+g.Slug+".md", g.Markdown); err != nil {
			return err
		}
		fmt.Fprintf(&full, "\n\n---\n\n%s", g.Markdown)
	}
	for _, p := range s.Packages {
		if err := render(p.URL, "package", page{Site: s, URL: p.URL, Title: shortName(p.Rel), Package: p, Markdown: p.MarkdownURL()}); err != nil {
			return err
		}
		md := packageMarkdown(p)
		if err := writeText(p.MarkdownURL(), md); err != nil {
			return err
		}
		fmt.Fprintf(&full, "\n\n---\n\n%s", md)
	}
	if err := writeText("llms.txt", s.llmsIndex()); err != nil {
		return err
	}
	return writeText("llms-full.txt", full.String())
}

// llmsIntro opens llms.txt and llms-full.txt.
const llmsIntro = "> A complete game engine in Go: a Vulkan renderer without cgo that draws 2D sprites and physically based 3D models in the same frame, an entity component system, rigid-body physics, skeletal animation, celestial mechanics, an immediate-mode interface, an audio mixer with a tracker player, and asset, save, translation and networking services, for real-time and turn-based games.\n\nThe guides explain each area of the engine; the package pages are the full API reference with doc comments, declarations and examples. Import path: github.com/matjam/bunyip."

// llmsIndex writes llms.txt: the site's pages as Markdown links with a
// line of description each.
func (s *Site) llmsIndex() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Bunyip\n\n%s\n\n", llmsIntro)
	fmt.Fprintf(&b, "The whole documentation in one file: %sllms-full.txt\n\n", s.Base)
	for i, grp := range s.GuideGroups {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "## %s guides\n\n", grp.Title)
		for _, g := range grp.Guides {
			fmt.Fprintf(&b, "- [%s](%sguides/%s.md): %s\n", g.Title, s.Base, g.Slug, g.Summary)
		}
	}
	for _, grp := range s.Groups {
		fmt.Fprintf(&b, "\n## %s\n\n", grp.Title)
		for _, p := range grp.Packages {
			fmt.Fprintf(&b, "- [%s](%s%s): %s\n", shortName(p.Rel), s.Base, p.MarkdownURL(), strings.TrimSpace(p.Synopsis))
		}
	}
	return b.String()
}

// packageMarkdown renders a package's reference as Markdown.
func packageMarkdown(p *Package) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n`import \"%s\"`\n\n", shortName(p.Rel), p.ImportPath)
	if p.DocMD != "" {
		b.WriteString(p.DocMD + "\n\n")
	}
	values := func(title string, vals []*Value) {
		if len(vals) == 0 {
			return
		}
		fmt.Fprintf(&b, "## %s\n\n", title)
		for _, v := range vals {
			fmt.Fprintf(&b, "```go\n%s\n```\n\n", strings.TrimSpace(v.DeclText))
			if v.DocMD != "" {
				b.WriteString(v.DocMD + "\n\n")
			}
		}
	}
	examples := func(list []*Example) {
		for _, ex := range list {
			fmt.Fprintf(&b, "Example%s:\n\n", map[bool]string{true: " (" + ex.Suffix + ")", false: ""}[ex.Suffix != ""])
			if ex.DocMD != "" {
				b.WriteString(ex.DocMD + "\n\n")
			}
			fmt.Fprintf(&b, "```go\n%s\n```\n\n", strings.TrimSpace(ex.CodeText))
			if ex.Output != "" {
				fmt.Fprintf(&b, "Output:\n\n```\n%s\n```\n\n", ex.Output)
			}
		}
	}
	fn := func(level string, f *Func) {
		fmt.Fprintf(&b, "%s %s\n\n```go\n%s\n```\n\n", level, f.ID, strings.TrimSpace(f.DeclText))
		if f.DocMD != "" {
			b.WriteString(f.DocMD + "\n\n")
		}
		examples(f.Examples)
	}
	values("Constants", p.Consts)
	values("Variables", p.Vars)
	if len(p.Funcs) > 0 {
		b.WriteString("## Functions\n\n")
		for _, f := range p.Funcs {
			fn("###", f)
		}
	}
	if len(p.Types) > 0 {
		b.WriteString("## Types\n\n")
		for _, t := range p.Types {
			fmt.Fprintf(&b, "### %s\n\n```go\n%s\n```\n\n", t.Name, strings.TrimSpace(t.DeclText))
			if t.DocMD != "" {
				b.WriteString(t.DocMD + "\n\n")
			}
			for _, v := range append(append([]*Value{}, t.Consts...), t.Vars...) {
				fmt.Fprintf(&b, "```go\n%s\n```\n\n", strings.TrimSpace(v.DeclText))
				if v.DocMD != "" {
					b.WriteString(v.DocMD + "\n\n")
				}
			}
			examples(t.Examples)
			for _, f := range t.Funcs {
				fn("####", f)
			}
			for _, m := range t.Methods {
				fn("####", m)
			}
		}
	}
	if len(p.Examples) > 0 {
		b.WriteString("## Examples\n\n")
		examples(p.Examples)
	}
	return strings.TrimSpace(b.String()) + "\n"
}

// page is the data every template sees.
type page struct {
	Site     *Site
	URL      string
	Title    string
	Markdown string // the same page as Markdown, relative to the site root
	Guide    *Guide
	Package  *Package
}

// Root is the relative path back to the site root from this page.
func (p page) Root() string { return strings.Repeat("../", strings.Count(p.URL, "/")) }

// Active reports whether a sidebar link points at this page.
func (p page) Active(url string) bool { return p.URL == url }

const layoutTmpl = `{{define "layout"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · Bunyip</title>
{{if .Markdown}}<link rel="alternate" type="text/markdown" href="{{.Root}}{{.Markdown}}" title="{{.Title}} as Markdown">
{{end}}<link rel="stylesheet" href="{{.Root}}site.css">
<script defer src="{{.Root}}site.js" data-root="{{.Root}}"></script>
</head>
<body>
<header class="top">
<button class="menu" aria-label="Menu">☰</button>
<a class="brand" href="{{.Root}}index.html">Bunyip</a>
<span class="tagline">a game engine in Go</span>
<div class="search"><input id="search" type="search" placeholder="Search symbols…" autocomplete="off"><div id="results" class="results" hidden></div></div>
<a class="gh" href="` + repo + `">GitHub</a>
</header>
<div class="shell">
<nav class="side">
{{range .Site.GuideGroups}}<section><h4>{{.Title}} guides</h4><ul>
{{range .Guides}}<li><a href="{{$.Root}}guides/{{.Slug}}.html"{{if $.Active (printf "guides/%s.html" .Slug)}} class="active"{{end}}>{{.Title}}</a></li>
{{end}}</ul></section>
{{end}}{{range .Site.Groups}}<section><h4>{{.Title}}</h4><ul>
{{range .Packages}}<li><a href="{{$.Root}}{{.URL}}"{{if $.Active .URL}} class="active"{{end}}>{{short .Rel}}</a></li>
{{end}}</ul></section>
{{end}}</nav>
<main class="content">
{{template "body" .}}
</main>
</div>
</body>
</html>{{end}}`

const indexTmpl = `{{define "index"}}{{template "layout" .}}{{end}}
{{define "body"}}{{if .Package}}{{template "packageBody" .}}{{else if .Guide}}{{template "guideBody" .}}{{else}}{{template "indexBody" .}}{{end}}{{end}}
{{define "indexBody"}}
<div class="hero">
<h1>Bunyip</h1>
<p class="lead">A game engine in Go for real-time and turn-based games: roguelikes, 4X, arcade, and anything that wants 2D sprites and 3D models on the same screen. Vulkan underneath, no cgo, native window and audio layers, and every subsystem a game needs from the first frame to the shipped build.</p>
<p class="actions"><a class="button" href="guides/getting-started.html">Get started</a> <a class="button secondary" href="guides/tetris.html">Build Tetris</a> <a class="button secondary" href="pkg/bunyip.html">API reference</a></p>
</div>
<div class="cards">
<a class="card" href="pkg/gfx.html"><h3>Rendering</h3><p>Sprites, tilemaps and scalable text on top of a physically based 3D renderer with cascaded shadows, ambient occlusion, bloom and skeletal animation. No Vulkan knowledge required.</p></a>
<a class="card" href="pkg/ui.html"><h3>Interface</h3><p>An immediate-mode toolkit rebuilt every frame: panels, buttons, sliders, drop-downs, scroll areas, text fields with IME support, eight colour themes and texture skins.</p></a>
<a class="card" href="pkg/audio.html"><h3>Audio</h3><p>A mixer with streamed music, positional voices, reverb and filters, priorities and fades, plus a tracker player for MOD, S3M, XM and IT.</p></a>
<a class="card" href="pkg/ecs.html"><h3>Game services</h3><p>An archetype-based entity component system with systems, resources and events, assets with packs and hot reload, saves and settings, seeded random numbers, timers and tweens, grids with pathfinding, and TCP and UDP messaging.</p></a>
</div>
<h2>Guides</h2>
{{range .Site.GuideGroups}}<h3>{{.Title}}</h3><ul class="guide-list">
{{range .Guides}}<li><a href="guides/{{.Slug}}.html">{{.Title}}</a>{{if .Summary}} <span class="dim">— {{.Summary}}</span>{{end}}</li>
{{end}}</ul>
{{end}}<h2>Packages</h2>
{{range .Site.Groups}}<h3>{{.Title}}</h3><table class="pkgs">
{{range .Packages}}<tr><td><a href="{{.URL}}">{{short .Rel}}</a></td><td>{{.Synopsis}}</td></tr>
{{end}}</table>
{{end}}
{{end}}`

const guideTmpl = `{{define "guide"}}{{template "layout" .}}{{end}}
{{define "guideBody"}}
<article class="guide">
{{if .Guide.Headings}}<aside class="toc"><h4>On this page</h4><ul>{{range .Guide.Headings}}<li><a href="#{{.ID}}">{{.Text}}</a></li>{{end}}</ul></aside>{{end}}
<h1>{{.Guide.Title}}</h1>
{{.Guide.Body}}
</article>
{{end}}`

const packageTmpl = `{{define "package"}}{{template "layout" .}}{{end}}
{{define "example"}}<details class="example" open><summary>Example{{if .Suffix}} ({{.Suffix}}){{end}}</summary>
{{.Doc}}<pre class="code"><code>{{.Code}}</code></pre>
{{if .Output}}<div class="output"><span>Output</span><pre>{{.Output}}</pre></div>{{end}}</details>{{end}}
{{define "func"}}<div class="decl" id="{{.ID}}">
<h4><a class="anchor" href="#{{.ID}}">{{.Name}}</a> <a class="src" href="{{.Src}}">source</a></h4>
<pre class="code sig"><code>{{.Decl}}</code></pre>
{{.Doc}}
{{range .Examples}}{{template "example" .}}{{end}}
</div>{{end}}
{{define "value"}}<div class="decl">
<pre class="code"><code>{{.Decl}}</code></pre>
{{.Doc}}
</div>{{end}}
{{define "packageBody"}}
{{$p := .Package}}
<article class="package">
<p class="crumbs">{{if $p.IsCommand}}Command{{else}}Package{{end}} <code>{{$p.ImportPath}}</code></p>
<h1>{{short $p.Rel}}</h1>
{{$p.Doc}}
{{if or $p.Consts $p.Vars $p.Funcs $p.Types}}
<h2 id="index">Index</h2>
<ul class="index">
{{if $p.Consts}}<li><a href="#constants">Constants</a></li>{{end}}
{{if $p.Vars}}<li><a href="#variables">Variables</a></li>{{end}}
{{range $p.Funcs}}<li><a href="#{{.ID}}"><code>{{.Decl}}</code></a></li>{{end}}
{{range $p.Types}}<li><a href="#{{.Name}}">type {{.Name}}</a>
{{if or .Funcs .Methods}}<ul>{{range .Funcs}}<li><a href="#{{.ID}}"><code>{{.Decl}}</code></a></li>{{end}}
{{range .Methods}}<li><a href="#{{.ID}}"><code>{{.Decl}}</code></a></li>{{end}}</ul>{{end}}</li>{{end}}
</ul>
{{end}}
{{if $p.Examples}}<h2 id="examples">Examples</h2>{{range $p.Examples}}{{template "example" .}}{{end}}{{end}}
{{if $p.Consts}}<h2 id="constants">Constants</h2>{{range $p.Consts}}{{template "value" .}}{{end}}{{end}}
{{if $p.Vars}}<h2 id="variables">Variables</h2>{{range $p.Vars}}{{template "value" .}}{{end}}{{end}}
{{if $p.Funcs}}<h2 id="functions">Functions</h2>{{range $p.Funcs}}{{template "func" .}}{{end}}{{end}}
{{if $p.Types}}<h2 id="types">Types</h2>
{{range $p.Types}}<div class="type" id="{{.Name}}">
<h3><a class="anchor" href="#{{.Name}}">type {{.Name}}</a> <a class="src" href="{{.Src}}">source</a></h3>
<pre class="code"><code>{{.Decl}}</code></pre>
{{.Doc}}
{{range .Examples}}{{template "example" .}}{{end}}
{{range .Consts}}{{template "value" .}}{{end}}
{{range .Vars}}{{template "value" .}}{{end}}
{{range .Funcs}}{{template "func" .}}{{end}}
{{range .Methods}}{{template "func" .}}{{end}}
</div>{{end}}{{end}}
<h2 id="files">Source files</h2>
<p class="files">{{range $p.Files}}<a href="` + repo + `/blob/main/{{$p.Rel}}{{if $p.Rel}}/{{end}}{{.}}">{{.}}</a> {{end}}</p>
</article>
{{end}}`

// unused helper kept for path joining in templates if needed.
var _ = path.Join
