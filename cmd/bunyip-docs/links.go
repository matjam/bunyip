package main

import (
	"go/doc/comment"
	"net/url"
	"path"
	"regexp"
	"strings"

	mdast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// docLinkURL is shared by the HTML and Markdown comment printers. Links
// are relative to the package page, including packages nested several
// directories deep; external symbols retain their GoDoc fragments.
func (r *renderer) docLinkURL(link *comment.DocLink, extension string) string {
	fragment := link.Name
	if link.Recv != "" {
		fragment = link.Recv
		if link.Name != "" {
			fragment += "." + link.Name
		}
	}
	if fragment != "" {
		fragment = "#" + fragment
	}
	if link.ImportPath != "" && link.ImportPath != module && !strings.HasPrefix(link.ImportPath, module+"/") {
		return "https://pkg.go.dev/" + link.ImportPath + fragment
	}
	rel := r.rel
	if link.ImportPath != "" {
		rel = strings.TrimPrefix(strings.TrimPrefix(link.ImportPath, module), "/")
	}
	target := strings.TrimSuffix(pkgURL(rel), ".html") + extension
	from := strings.Split(path.Dir(pkgURL(r.rel)), "/")
	to := strings.Split(target, "/")
	for len(from) > 0 && len(to) > 0 && from[0] == to[0] {
		from, to = from[1:], to[1:]
	}
	return strings.Repeat("../", len(from)) + strings.Join(to, "/") + fragment
}

// markdownLinks points local page links at the Markdown copies without
// changing external destinations or examples of links inside code.
func markdownLinks(body string) string {
	return rewriteMarkdownLinks(body, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.IsAbs() || u.Host != "" || !strings.HasSuffix(u.Path, ".html") {
			return raw
		}
		end := strings.IndexAny(raw, "?#")
		if end < 0 {
			end = len(raw)
		}
		if !strings.HasSuffix(raw[:end], ".html") {
			return raw
		}
		return strings.TrimSuffix(raw[:end], ".html") + ".md" + raw[end:]
	})
}

// aggregateLinks preserves the original page as the context for links
// when many Markdown documents are concatenated into llms-full.txt.
func aggregateLinks(body, page, base string) string {
	pageURL, err := url.Parse(base + page)
	if err != nil {
		return body
	}
	return rewriteMarkdownLinks(body, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.IsAbs() || u.Host != "" {
			return raw
		}
		return pageURL.ResolveReference(u).String()
	})
}

// Destinations of inline links/images and reference definitions. Code is
// masked using the Markdown parser's source ranges before matching, so
// examples and inline code remain byte-for-byte unchanged.
var markdownDestinationRE = regexp.MustCompile(`(?m)(?:\]\([ \t]*|^[ ]{0,3}\[[^]\n]+\]:[ \t]*)(<?)([^\s<>]+)`)

func rewriteMarkdownLinks(body string, rewrite func(string) string) string {
	source := []byte(body)
	masked := []byte(body)
	doc := newMarkdown().Parser().Parse(text.NewReader(source))
	mask := func(start, stop int) {
		for i := start; i < stop; i++ {
			if masked[i] != '\n' {
				masked[i] = ' '
			}
		}
	}
	_ = mdast.Walk(doc, func(n mdast.Node, entering bool) (mdast.WalkStatus, error) {
		if !entering {
			return mdast.WalkContinue, nil
		}
		switch n.(type) {
		case *mdast.CodeBlock, *mdast.FencedCodeBlock:
			for i := 0; i < n.Lines().Len(); i++ {
				line := n.Lines().At(i)
				mask(line.Start, line.Stop)
			}
		case *mdast.CodeSpan:
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				if span, ok := child.(*mdast.Text); ok {
					mask(span.Segment.Start, span.Segment.Stop)
				}
			}
		}
		return mdast.WalkContinue, nil
	})
	var out strings.Builder
	last := 0
	for offset := 0; offset < len(masked); {
		match := markdownDestinationRE.FindSubmatchIndex(masked[offset:])
		if match == nil {
			break
		}
		for i := range match {
			if match[i] >= 0 {
				match[i] += offset
			}
		}
		start, stop := match[4], match[5]
		// An unbracketed destination can contain balanced parentheses;
		// the first unmatched closing parenthesis ends the link.
		if match[2] == match[3] {
			depth := 0
			for i := start; i < stop; i++ {
				if source[i] == '\\' {
					i++
					continue
				}
				if source[i] == '(' {
					depth++
				}
				if source[i] == ')' {
					if depth == 0 {
						stop = i
						break
					}
					depth--
				}
			}
		}
		out.WriteString(body[last:start])
		out.WriteString(rewrite(body[start:stop]))
		last = stop
		offset = stop
	}
	out.WriteString(body[last:])
	return out.String()
}
