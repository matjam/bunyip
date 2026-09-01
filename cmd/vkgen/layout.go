package main

import (
	"fmt"
	"strconv"
)

const ptrSize = 8

type layout struct {
	Size, Align uintptr
	Offsets     []uintptr // per member, for structs and unions
}

// layoutOf computes the C layout of a named type on a 64-bit platform.
// Vulkan only uses fixed-width scalars and pointers, so LP64 and LLP64 agree.
func (m *model) layoutOf(name string) (layout, error) {
	if l, ok := m.layouts[name]; ok {
		return l, nil
	}
	l, err := m.computeLayout(name)
	if err != nil {
		return l, err
	}
	m.layouts[name] = l
	return l, nil
}

func (m *model) computeLayout(name string) (layout, error) {
	if p, ok := cPrimitives[name]; ok {
		return layout{p.Size, p.Size, nil}, nil
	}
	t, ok := m.Types[name]
	if !ok {
		return layout{}, fmt.Errorf("layout: unknown type %s", name)
	}
	switch t.Kind {
	case kindAlias:
		return m.layoutOf(t.Alias)
	case kindHandle, kindFuncPtr:
		return layout{ptrSize, ptrSize, nil}, nil
	case kindEnum:
		if t.Width == 64 {
			return layout{8, 8, nil}, nil
		}
		return layout{4, 4, nil}, nil
	case kindBitmask:
		if t.Width == 64 {
			return layout{8, 8, nil}, nil
		}
		return layout{4, 4, nil}, nil
	case kindBase:
		if t.Ptr > 0 {
			return layout{ptrSize, ptrSize, nil}, nil
		}
		if t.Opaque {
			return layout{}, fmt.Errorf("layout: opaque type %s", name)
		}
		return m.layoutOf(t.Base)
	case kindExternal:
		e, ok := externals[name]
		if !ok || e.Size == 0 {
			return layout{}, fmt.Errorf("layout: external type %s", name)
		}
		return layout{e.Size, e.Size, nil}, nil
	case kindStruct:
		return m.structLayout(t)
	case kindUnion:
		return m.unionLayout(t)
	}
	return layout{}, fmt.Errorf("layout: type %s cannot be laid out", name)
}

func (m *model) declLayout(d decl) (size, align uintptr, err error) {
	if d.Bitfield != "" {
		return 0, 0, fmt.Errorf("bitfield member %s is not supported", d.Name)
	}
	if d.Ptr > 0 {
		size, align = ptrSize, ptrSize
	} else {
		l, err := m.layoutOf(d.Type)
		if err != nil {
			return 0, 0, err
		}
		size, align = l.Size, l.Align
	}
	for _, a := range d.Arrays {
		n, err := m.arrayLen(a)
		if err != nil {
			return 0, 0, fmt.Errorf("member %s: %w", d.Name, err)
		}
		size *= n
	}
	return size, align, nil
}

func (m *model) arrayLen(s string) (uintptr, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return uintptr(n), nil
	}
	if v, ok := m.ConstValues[s]; ok {
		return uintptr(v), nil
	}
	return 0, fmt.Errorf("array length %q is not a known constant", s)
}

func (m *model) structLayout(t *typ) (layout, error) {
	var l layout
	for _, d := range t.Members {
		size, align, err := m.declLayout(d)
		if err != nil {
			return l, fmt.Errorf("%s: %w", t.Name, err)
		}
		off := roundUp(l.Size, align)
		l.Offsets = append(l.Offsets, off)
		l.Size = off + size
		l.Align = max(l.Align, align)
	}
	l.Size = roundUp(l.Size, l.Align)
	return l, nil
}

func (m *model) unionLayout(t *typ) (layout, error) {
	var l layout
	for _, d := range t.Members {
		size, align, err := m.declLayout(d)
		if err != nil {
			return l, fmt.Errorf("%s: %w", t.Name, err)
		}
		l.Offsets = append(l.Offsets, 0)
		l.Size = max(l.Size, size)
		l.Align = max(l.Align, align)
	}
	l.Size = roundUp(l.Size, l.Align)
	return l, nil
}

func roundUp(n, align uintptr) uintptr {
	if align == 0 {
		return n
	}
	return (n + align - 1) / align * align
}
