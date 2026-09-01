package main

import "fmt"

// emitLayoutTest writes a test asserting that every generated struct and union
// has the size, alignment and member offsets the C compiler would give it.
func (m *model) emitLayoutTest(sel *selection, pkg string) (*file, error) {
	f := newFile("zz_layout_test.go", pkg, m.HeaderVersion, "testing", "unsafe")
	f.p("type layoutCase struct {\n\tname string\n\tsize, wantSize, align, wantAlign uintptr\n\toffsets, wantOffsets []uintptr\n}\n\n")
	f.p("func TestGeneratedLayout(t *testing.T) {\n")
	f.p("\tfor _, c := range generatedLayouts() {\n")
	f.p("\t\tt.Run(c.name, func(t *testing.T) {\n")
	f.p("\t\t\tif c.size != c.wantSize {\n\t\t\t\tt.Errorf(\"size %%d, C says %%d\", c.size, c.wantSize)\n\t\t\t}\n")
	f.p("\t\t\tif c.align != c.wantAlign {\n\t\t\t\tt.Errorf(\"alignment %%d, C says %%d\", c.align, c.wantAlign)\n\t\t\t}\n")
	f.p("\t\t\tfor i := range c.offsets {\n\t\t\t\tif c.offsets[i] != c.wantOffsets[i] {\n")
	f.p("\t\t\t\t\tt.Errorf(\"member %%d at offset %%d, C says %%d\", i, c.offsets[i], c.wantOffsets[i])\n\t\t\t\t}\n\t\t\t}\n")
	f.p("\t\t})\n\t}\n}\n\n")
	f.p("func generatedLayouts() []layoutCase {\n\treturn []layoutCase{\n")
	for _, k := range []kind{kindStruct, kindUnion} {
		for _, name := range m.sortedTypes(sel, k) {
			if err := m.emitLayoutCase(f, name, k == kindStruct); err != nil {
				return nil, err
			}
		}
	}
	f.p("\t}\n}\n")
	return f, nil
}

func (m *model) emitLayoutCase(f *file, name string, withOffsets bool) error {
	t := m.Types[name]
	l, err := m.layoutOf(name)
	if err != nil {
		return fmt.Errorf("layout test: %w", err)
	}
	f.p("\t\t{name: %q, size: unsafe.Sizeof(%s{}), wantSize: %d, align: unsafe.Alignof(%s{}), wantAlign: %d,\n",
		name, name, l.Size, name, l.Align)
	if !withOffsets {
		f.p("\t\t},\n")
		return nil
	}
	f.p("\t\t\toffsets: []uintptr{")
	for _, d := range t.Members {
		f.p("unsafe.Offsetof(%s{}.%s), ", name, goFieldName(d.Name))
	}
	f.p("},\n\t\t\twantOffsets: []uintptr{")
	for _, off := range l.Offsets {
		f.p("%d, ", off)
	}
	f.p("}},\n")
	return nil
}
