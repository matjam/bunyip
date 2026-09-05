package main

import (
	"fmt"
	"strings"
)

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
	if len(s.Programs) > 0 {
		b.WriteString("\n## Example programs\n\n")
		fmt.Fprintf(&b, "Screenshot-capable examples run headless; the window smoke test requires a desktop. Each walkthrough explains the whole program section by section. The list: %sexamples/index.md\n\n", s.Base)
		for _, p := range s.Programs {
			fmt.Fprintf(&b, "- [%s](%s%s): %s\n", p.Title, s.Base, p.MarkdownURL(), p.Summary)
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

// programIndexMarkdown lists every example with its summary.
func (s *Site) programIndexMarkdown() string {
	var b strings.Builder
	b.WriteString("# Example programs\n\n")
	b.WriteString("One directory per example under `examples/`. Screenshot-capable examples take `-seconds N` and `-shot file.png` for headless verification. The `window` smoke test requires a desktop. Every example has a walkthrough that explains its source section by section.\n\n")
	for _, p := range s.Programs {
		fmt.Fprintf(&b, "- [%s](%s.md): %s\n", p.Title, p.Name, p.Summary)
	}
	return b.String()
}

// programMarkdown renders one walkthrough as the Markdown page, with the
// screenshot and the link to the source ahead of the body.
func programMarkdown(p *Program) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", p.Title)
	if p.Shot {
		fmt.Fprintf(&b, "![%s](%s.png)\n\n", p.Title, p.Name)
	}
	fmt.Fprintf(&b, "Source: [`examples/%s`](%s) (%s)\n\n", p.Name, p.SourceURL(), strings.Join(p.Files, ", "))
	body := strings.TrimPrefix(p.Markdown, "# "+p.Title+"\n\n")
	b.WriteString(body)
	return strings.TrimSpace(b.String()) + "\n"
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
			writeAnchors(&b, v.Names)
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
		writeAnchors(&b, []string{f.ID})
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
			writeAnchors(&b, []string{t.Name})
			for _, member := range t.Members {
				writeAnchors(&b, []string{member.Name})
			}
			fmt.Fprintf(&b, "### %s\n\n```go\n%s\n```\n\n", t.Name, strings.TrimSpace(t.DeclText))
			if t.DocMD != "" {
				b.WriteString(t.DocMD + "\n\n")
			}
			for _, v := range append(append([]*Value{}, t.Consts...), t.Vars...) {
				writeAnchors(&b, v.Names)
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
