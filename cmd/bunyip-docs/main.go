// Command bunyip-docs renders the module's documentation as a static
// website: guides written in Markdown, a walkthrough of every example
// program, every package's godoc with its examples, a symbol search, and
// links back to the source on GitHub.
//
//	CGO_ENABLED=0 go run ./cmd/bunyip-docs -out site
//
// Run from the module root. The -out flag defaults to site; -guides and
// -examples default to docs/guides and docs/examples. The -base flag
// defaults to https://matjam.github.io/bunyip/ and controls published
// links in llms.txt and llms-full.txt. Generation writes local files;
// publishing is a separate step handled by the repository's Docs workflow.
//
// The example walkthroughs are the Markdown files in docs/examples, one
// per directory under examples, with the same front matter as a guide
// plus an example key naming the directory. A screenshot beside a
// walkthrough, docs/examples/<name>.png, is shown at the top of its
// page. The pages of examples/ are not rendered as packages; the
// walkthroughs document them instead.
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
	{"Engine", []string{"bunyip", "input", "console"}},
	{"Graphics", []string{"gfx", "gfx/", "anim", "ui", "particle", "tiled", "gltf", "lin"}},
	{"Simulation", []string{"ecs", "phys", "phys/soft", "orbit", "orbit/sol"}},
	{"Audio", []string{"audio", "audio/tracker"}},
	{"Services", []string{"asset", "save", "locale", "rng", "timer", "tween", "grid", "grid/", "network"}},
	{"Tools", []string{"cmd/"}},
}

// guideGroups orders the guide sections, from a guide's `group` front
// matter key. A guide with no group, or with a group not listed here,
// falls into a trailing "Other" section.
var guideGroups = []string{"Start", "Engine", "Graphics", "Simulation", "Audio"}

func main() {
	out := flag.String("out", "site", "output directory")
	guides := flag.String("guides", "docs/guides", "directory of Markdown guides")
	walkthroughs := flag.String("examples", "docs/examples", "directory of Markdown example walkthroughs")
	base := flag.String("base", siteURL, "the published site URL, for llms.txt and links in llms-full.txt")
	flag.Parse()
	site, err := build(".", *guides, *walkthroughs)
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
	Programs    []*Program
	Packages    []*Package
	Groups      []Group
	Base        string // the published URL, with a trailing slash
	guideDir    string
	exampleDir  string // the directory of Markdown walkthroughs
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

// Program is one example program: the walkthrough in docs/examples, the
// screenshot beside it, and the links to the source. An example with no
// walkthrough yet is still listed, with Missing set.
type Program struct {
	Title, Name, Summary string
	Body                 template.HTML
	Markdown             string // the source, with the front matter replaced by a heading
	Headings             []heading
	Files                []string // the .go files of examples/<name>
	Shot                 bool     // docs/examples/<name>.png exists
	Missing              bool     // no walkthrough is written yet
}

// URL is the walkthrough's page, relative to the site root.
func (p *Program) URL() string { return "examples/" + p.Name + ".html" }

// MarkdownURL is the walkthrough's Markdown page, beside the HTML one.
func (p *Program) MarkdownURL() string { return "examples/" + p.Name + ".md" }

// SourceURL is the example's directory on GitHub.
func (p *Program) SourceURL() string { return repo + "/tree/main/examples/" + p.Name }

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
	Members      []symbol // exported fields and explicitly declared interface methods
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

func build(root, guideDir, exampleDir string) (*Site, error) {
	site := &Site{pages: map[string][]byte{}, guideDir: guideDir, exampleDir: exampleDir}
	if err := site.loadPackages(root); err != nil {
		return nil, err
	}
	if err := site.loadGuides(guideDir); err != nil {
		return nil, err
	}
	if err := site.loadPrograms(filepath.Join(root, "examples"), exampleDir); err != nil {
		return nil, err
	}
	site.group()
	return site, nil
}

// loadPackages walks the module for Go packages outside internal and
// testdata directories. The examples are skipped: they are documented by
// the walkthroughs in docs/examples rather than as packages, and their
// unexported helpers do not belong in the symbol index.
func (s *Site) loadPackages(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || name == "internal" || name == "testdata" || name == "third_party" || name == "site" || name == "bin" || name == "docs" || name == "examples") {
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
	if err != nil {
		return fmt.Errorf("walk packages in %s: %w", root, err)
	}
	sort.Strings(dirs)
	seen := map[string]bool{}
	for _, dir := range dirs {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return fmt.Errorf("package path %s relative to %s: %w", dir, root, err)
		}
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
		tt.Members = typeMembers(t)
		p.Types = append(p.Types, tt)
	}
	s.symbols = append(s.symbols, packageSymbols(p)...)
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
		return r.docLinkURL(link, ".html")
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
		return r.docLinkURL(link, ".md")
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
	rel := filepath.Join(r.rel, filepath.Base(pos.Filename))
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

// newMarkdown builds the Markdown renderer the guides and the example
// walkthroughs share.
func newMarkdown() goldmark.Markdown {
	return goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(mdparser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(ghtml.WithUnsafe()))
}

// frontMatter splits a leading block fenced by "---" lines off a
// Markdown source and returns its keys with the body that follows. A
// source with no such block comes back unchanged with no keys.
func frontMatter(src string) (map[string]string, string) {
	meta := map[string]string{}
	if !strings.HasPrefix(src, "---\n") {
		return meta, src
	}
	end := strings.Index(src[4:], "\n---")
	if end < 0 {
		return meta, src
	}
	for _, line := range strings.Split(src[4:4+end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return meta, src[4+end+4:]
}

// loadGuides renders every Markdown file in dir; a leading front matter
// block gives title, order and summary.
func (s *Site) loadGuides(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("guides: %w", err)
	}
	md := newMarkdown()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		g := &Guide{Slug: strings.TrimSuffix(e.Name(), ".md"), Title: strings.TrimSuffix(e.Name(), ".md")}
		meta, body := frontMatter(string(raw))
		if v, ok := meta["title"]; ok {
			g.Title = v
		}
		g.Order, _ = strconv.Atoi(meta["order"])
		g.Summary = meta["summary"]
		g.Group = meta["group"]
		// The Markdown copy links to the Markdown pages.
		g.Markdown = "# " + g.Title + "\n\n" + strings.TrimLeft(markdownLinks(body), "\n")
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

// loadPrograms lists every example program, in alphabetical order, and
// renders the walkthrough written for it. srcDir is the examples
// directory of the module; docDir holds one Markdown walkthrough per
// example, named for its directory. An example with no walkthrough is
// still listed, so the site holds together while one is being written.
func (s *Site) loadPrograms(srcDir, docDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("examples: %w", err)
	}
	md := newMarkdown()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		files, err := goFiles(filepath.Join(srcDir, name))
		if err != nil {
			return err
		}
		if len(files) == 0 {
			continue
		}
		p := &Program{Title: name, Name: name, Files: files}
		if _, err := os.Stat(filepath.Join(docDir, name+".png")); err == nil {
			p.Shot = true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("example screenshot %s: %w", name, err)
		}
		raw, err := os.ReadFile(filepath.Join(docDir, name+".md"))
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			p.Missing = true
			p.Summary = "the walkthrough for this example is not written yet"
			p.Markdown = "# " + p.Title + "\n\n" + p.Summary + "\n"
			s.Programs = append(s.Programs, p)
			continue
		}
		meta, body := frontMatter(string(raw))
		if v, ok := meta["title"]; ok {
			p.Title = v
		}
		p.Summary = meta["summary"]
		p.Markdown = "# " + p.Title + "\n\n" + strings.TrimLeft(markdownLinks(body), "\n")
		var buf bytes.Buffer
		if err := md.Convert([]byte(body), &buf); err != nil {
			return fmt.Errorf("%s: %w", name+".md", err)
		}
		rendered := highlightFences(buf.String())
		p.Body = template.HTML(rendered)
		p.Headings = collectHeadings(rendered)
		s.Programs = append(s.Programs, p)
	}
	sort.Slice(s.Programs, func(i, j int) bool { return s.Programs[i].Name < s.Programs[j].Name })
	return nil
}

// goFiles lists the Go files of one directory, sorted, without
// descending into subdirectories.
func goFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
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

// copyImages copies every file that is not Markdown from src into dst,
// creating dst when src holds any. A missing src is not an error.
func copyImages(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read images in %s: %w", src, err)
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
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
	// Images the pages refer to sit beside their Markdown, in docs/guides
	// and docs/examples, and are copied next to the rendered pages, so a
	// relative link works both on the site and when the Markdown is read
	// in the repository.
	if err := copyImages(s.guideDir, filepath.Join(dir, "guides")); err != nil {
		return err
	}
	if err := copyImages(s.exampleDir, filepath.Join(dir, "examples")); err != nil {
		return err
	}
	tmpl := template.Must(template.New("site").Funcs(template.FuncMap{
		"depth": func(url string) string { return strings.Repeat("../", strings.Count(url, "/")) },
		"short": shortName,
	}).Parse(layoutTmpl + indexTmpl + guideTmpl + programTmpl + packageTmpl))
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
		fmt.Fprintf(&full, "\n\n---\n\n%s", aggregateLinks(g.Markdown, "guides/"+g.Slug+".md", s.Base))
	}
	if len(s.Programs) > 0 {
		if err := render("examples/index.html", "programIndex", page{Site: s, URL: "examples/index.html", Title: "Example programs", Programs: true, Markdown: "examples/index.md"}); err != nil {
			return err
		}
		if err := writeText("examples/index.md", s.programIndexMarkdown()); err != nil {
			return err
		}
	}
	for _, p := range s.Programs {
		if err := render(p.URL(), "program", page{Site: s, URL: p.URL(), Title: p.Title, Program: p, Markdown: p.MarkdownURL()}); err != nil {
			return err
		}
		md := programMarkdown(p)
		if err := writeText(p.MarkdownURL(), md); err != nil {
			return err
		}
		fmt.Fprintf(&full, "\n\n---\n\n%s", aggregateLinks(md, p.MarkdownURL(), s.Base))
	}
	for _, p := range s.Packages {
		if err := render(p.URL, "package", page{Site: s, URL: p.URL, Title: shortName(p.Rel), Package: p, Markdown: p.MarkdownURL()}); err != nil {
			return err
		}
		md := packageMarkdown(p)
		if err := writeText(p.MarkdownURL(), md); err != nil {
			return err
		}
		fmt.Fprintf(&full, "\n\n---\n\n%s", aggregateLinks(md, p.MarkdownURL(), s.Base))
	}
	if err := writeText("llms.txt", s.llmsIndex()); err != nil {
		return err
	}
	return writeText("llms-full.txt", full.String())
}
