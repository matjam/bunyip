package main

import (
	"fmt"
	"regexp"
	"strings"
)

// decl is one C declaration from a <member> or <param> element: a base type
// name, a pointer depth, array dimensions and an optional bitfield width.
type decl struct {
	Type     string
	Name     string
	Ptr      int
	Arrays   []string // each element is a literal or a constant name
	Bitfield string
}

var (
	reComment = regexp.MustCompile(`(?s)<comment>.*?</comment>`)
	reTag     = regexp.MustCompile(`<[^>]+>`)
	reType    = regexp.MustCompile(`<type>([^<]+)</type>`)
	reName    = regexp.MustCompile(`<name>([^<]+)</name>`)
	reArray   = regexp.MustCompile(`\[([^\]]+)\]`)
	reBits    = regexp.MustCompile(`:\s*(\d+)`)
)

// parseDecl reads the mixed content of a <member> or <param> element.
func parseDecl(inner string) (decl, error) {
	var d decl
	inner = reComment.ReplaceAllString(inner, "")
	if m := reType.FindStringSubmatch(inner); m != nil {
		d.Type = strings.TrimSpace(m[1])
	} else {
		return d, fmt.Errorf("declaration without <type>: %q", inner)
	}
	if m := reName.FindStringSubmatch(inner); m != nil {
		d.Name = strings.TrimSpace(m[1])
	} else {
		return d, fmt.Errorf("declaration without <name>: %q", inner)
	}
	loc := reName.FindStringIndex(inner)
	before := reTag.ReplaceAllString(inner[:loc[0]], "")
	after := reTag.ReplaceAllString(inner[loc[1]:], "")
	d.Ptr = strings.Count(before, "*")
	for _, m := range reArray.FindAllStringSubmatch(after, -1) {
		d.Arrays = append(d.Arrays, strings.TrimSpace(m[1]))
	}
	if m := reBits.FindStringSubmatch(after); m != nil {
		d.Bitfield = m[1]
	}
	return d, nil
}

// parseBaseType reads a category="basetype" element. It returns the underlying
// C type and pointer depth, or opaque=true for a forward-declared struct or
// Objective-C class that is only ever used through a pointer.
func parseBaseType(inner string) (base string, ptr int, opaque bool) {
	inner = reComment.ReplaceAllString(inner, "")
	m := reType.FindStringSubmatch(inner)
	text := reTag.ReplaceAllString(inner, "")
	if m == nil {
		if strings.Contains(text, "typedef") && strings.Contains(text, "*") {
			return "void", 1, false
		}
		return "", 0, true
	}
	loc := reName.FindStringIndex(inner)
	before := reTag.ReplaceAllString(inner[:loc[0]], "")
	return strings.TrimSpace(m[1]), strings.Count(before, "*"), false
}
