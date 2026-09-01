package main

import "fmt"

func (m *model) emitBaseTypes(sel *selection, pkg string) (*file, error) {
	f := newFile("zz_types.go", pkg, m.HeaderVersion)
	f.p("// Vulkan base typedefs, handles and function pointer types.\n")
	f.p("// Handles are pointers or 64-bit integers in C; both are 8 bytes on the\n")
	f.p("// platforms this binding supports, so every handle is a uintptr.\n\n")
	for _, name := range m.sortedTypes(sel, kindBase) {
		t := m.Types[name]
		if t.Opaque {
			continue
		}
		under, err := m.underlyingGo(t)
		if err != nil {
			return nil, err
		}
		f.p("type %s %s\n", name, under)
	}
	f.p("\n")
	for _, name := range m.sortedTypes(sel, kindHandle) {
		f.p("type %s uintptr\n", name)
	}
	f.p("\n")
	for _, name := range m.sortedTypes(sel, kindFuncPtr) {
		f.p("type %s uintptr\n", name)
	}
	f.p("\n")
	for _, name := range m.sortedTypes(sel, kindAlias) {
		target := m.resolveAlias(name)
		switch m.Types[target].Kind {
		case kindBase, kindHandle, kindFuncPtr, kindStruct, kindUnion, kindEnum, kindBitmask:
			f.p("type %s = %s\n", name, m.Types[name].Alias)
		}
	}
	return f, nil
}

// resolveAlias follows alias chains to the defining type.
func (m *model) resolveAlias(name string) string {
	for {
		t := m.Types[name]
		if t.Kind != kindAlias {
			return name
		}
		name = t.Alias
	}
}

func (m *model) emitConstants(pkg string) *file {
	f := newFile("zz_constants.go", pkg, m.HeaderVersion)
	f.p("// HeaderVersion is the VK_HEADER_VERSION of the registry this binding was generated from.\n")
	f.p("const HeaderVersion = %s\n\n", m.HeaderVersion)
	f.p("const (\n")
	for _, c := range m.Consts {
		if c.Alias != "" {
			f.p("\t%s = %s\n", c.Name, c.Alias)
		} else {
			f.p("\t%s = %s\n", c.Name, c.Expr)
		}
	}
	f.p(")\n")
	return f
}

func (m *model) emitEnums(sel *selection, pkg string) *file {
	f := newFile("zz_enums.go", pkg, m.HeaderVersion)
	f.p("// Enumerations and flag bits. A FlagBits type is an alias of its Flags type\n")
	f.p("// so that values combine with | without conversion.\n\n")
	for _, name := range m.sortedTypes(sel, kindBitmask) {
		t := m.Types[name]
		f.p("type %s %s\n", name, intType(t.Width))
	}
	f.p("\n")
	for _, name := range m.sortedTypes(sel, kindEnum) {
		t := m.Types[name]
		constType := name
		switch {
		case t.Flags != "" && sel.Types[t.Flags]:
			constType = t.Flags
			f.p("type %s = %s\n", name, t.Flags)
		case t.Width == 64:
			f.p("type %s uint64\n", name)
		case t.Flags != "":
			f.p("type %s uint32\n", name)
		default:
			f.p("type %s int32\n", name)
		}
		e, ok := m.Enums[name]
		if !ok || len(e.Values) == 0 {
			continue
		}
		f.p("\nconst (\n")
		for _, v := range e.Values {
			if v.Alias != "" {
				f.p("\t%s = %s\n", v.Name, v.Alias)
			} else {
				f.p("\t%s %s = %s\n", v.Name, constType, v.Expr)
			}
		}
		f.p(")\n\n")
	}
	return f
}

func intType(width int) string {
	if width == 64 {
		return "uint64"
	}
	return "uint32"
}

func (m *model) checkConsts() error {
	for _, c := range m.Consts {
		if c.Alias == "" && c.Expr == "" {
			return fmt.Errorf("constant %s has no value", c.Name)
		}
	}
	return nil
}
