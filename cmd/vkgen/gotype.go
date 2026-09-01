package main

import (
	"fmt"
	"strings"
)

// cPrimitives maps the C scalar types vk.xml uses to Go types and sizes.
var cPrimitives = map[string]struct {
	Go   string
	Size uintptr
}{
	"char":     {"byte", 1},
	"float":    {"float32", 4},
	"double":   {"float64", 8},
	"int8_t":   {"int8", 1},
	"uint8_t":  {"uint8", 1},
	"int16_t":  {"int16", 2},
	"uint16_t": {"uint16", 2},
	"int32_t":  {"int32", 4},
	"uint32_t": {"uint32", 4},
	"int64_t":  {"int64", 8},
	"uint64_t": {"uint64", 8},
	"size_t":   {"uintptr", 8},
	"int":      {"int32", 4},
}

// externals covers platform types the registry declares but does not define.
// Size zero marks an opaque type that only appears behind a pointer.
var externals = map[string]struct {
	Go   string
	Size uintptr
}{
	"HINSTANCE":           {"uintptr", 8},
	"HWND":                {"uintptr", 8},
	"HMONITOR":            {"uintptr", 8},
	"HANDLE":              {"uintptr", 8},
	"DWORD":               {"uint32", 4},
	"LPCWSTR":             {"*uint16", 8},
	"SECURITY_ATTRIBUTES": {"", 0},
	"xcb_connection_t":    {"", 0},
	"xcb_visualid_t":      {"uint32", 4},
	"xcb_window_t":        {"uint32", 4},
	"Display":             {"", 0},
	"Window":              {"uint64", 8},
	"VisualID":            {"uint64", 8},
	"wl_display":          {"", 0},
	"wl_surface":          {"", 0},
}

var goKeywords = map[string]bool{
	"type": true, "range": true, "func": true, "map": true, "len": true, "cap": true,
	"var": true, "const": true, "chan": true, "go": true, "select": true, "defer": true,
	"interface": true, "struct": true, "package": true, "import": true, "return": true,
	"break": true, "case": true, "continue": true, "default": true, "else": true,
	"fallthrough": true, "for": true, "goto": true, "if": true, "switch": true,
}

func goParamName(name string) string {
	if goKeywords[name] {
		return name + "_"
	}
	return name
}

func goFieldName(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

// goType renders a declaration as a Go type. Pointers to void, char and opaque
// types become unsafe.Pointer, *byte and unsafe.Pointer respectively; every
// other pointer keeps its element type.
func (m *model) goType(d decl) (string, error) {
	base, opaque, err := m.goBase(d.Type)
	if err != nil {
		return "", err
	}
	var s string
	switch {
	case d.Ptr == 0:
		if opaque {
			return "", fmt.Errorf("opaque type %s used by value", d.Type)
		}
		s = base
	case opaque || d.Type == "void":
		s = strings.Repeat("*", d.Ptr-1) + "unsafe.Pointer"
	default:
		s = strings.Repeat("*", d.Ptr) + base
	}
	for i := len(d.Arrays) - 1; i >= 0; i-- {
		s = "[" + d.Arrays[i] + "]" + s
	}
	return s, nil
}

// goBase returns the Go spelling of a named C type, and whether it is opaque.
func (m *model) goBase(name string) (string, bool, error) {
	if p, ok := cPrimitives[name]; ok {
		return p.Go, false, nil
	}
	if name == "void" {
		return "", true, nil
	}
	t, ok := m.Types[name]
	if !ok {
		return "", false, fmt.Errorf("unknown type %s", name)
	}
	switch t.Kind {
	case kindExternal:
		e, ok := externals[name]
		if !ok {
			return "", false, fmt.Errorf("external type %s has no mapping", name)
		}
		return e.Go, e.Size == 0, nil
	case kindBase:
		if t.Opaque {
			return "", true, nil
		}
		return name, false, nil
	default:
		return name, false, nil
	}
}

// underlyingGo returns the Go type a basetype typedef is declared as.
func (m *model) underlyingGo(t *typ) (string, error) {
	if t.Ptr > 0 {
		return "uintptr", nil
	}
	s, _, err := m.goBase(t.Base)
	return s, err
}
