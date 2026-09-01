package main

import "fmt"

func (m *model) emitStructs(sel *selection, pkg string) (*file, error) {
	f := newFile("zz_structs.go", pkg, m.HeaderVersion, "structs", "unsafe")
	f.p("// Structures passed to and returned from Vulkan. Field order and types\n")
	f.p("// reproduce the C layout; zz_layout_test.go asserts every size and offset.\n\n")
	for _, name := range m.sortedTypes(sel, kindStruct) {
		t := m.Types[name]
		f.p("type %s struct {\n\t_ structs.HostLayout\n", name)
		for _, d := range t.Members {
			gt, err := m.goType(d)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", name, d.Name, err)
			}
			f.p("\t%s %s\n", goFieldName(d.Name), gt)
		}
		f.p("}\n\n")
	}
	return f, nil
}

// emitUnions renders each union as raw storage of the right size and
// alignment, with one typed accessor per member.
func (m *model) emitUnions(sel *selection, pkg string) (*file, error) {
	f := newFile("zz_unions.go", pkg, m.HeaderVersion, "structs", "unsafe")
	f.p("// Unions are stored as raw words; each member is reached through an accessor\n")
	f.p("// that reinterprets the storage, as C would.\n\n")
	for _, name := range m.sortedTypes(sel, kindUnion) {
		t := m.Types[name]
		l, err := m.layoutOf(name)
		if err != nil {
			return nil, err
		}
		word := "uint32"
		if l.Align == 8 {
			word = "uint64"
		}
		f.p("type %s struct {\n\t_ structs.HostLayout\n\traw [%d]%s\n}\n\n", name, l.Size/l.Align, word)
		for _, d := range t.Members {
			gt, err := m.goType(d)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", name, d.Name, err)
			}
			f.p("// %s views the union as its %s member.\n", goFieldName(d.Name), d.Name)
			f.p("func (u *%s) %s() *%s { return (*%s)(unsafe.Pointer(u)) }\n\n", name, goFieldName(d.Name), gt, gt)
		}
	}
	return f, nil
}
