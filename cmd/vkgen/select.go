package main

import (
	"fmt"
	"slices"
	"strings"
)

// selection is the set of types and commands the generated binding covers:
// everything required by the chosen core versions and extensions, closed over
// the types those definitions mention.
type selection struct {
	Types map[string]bool
	Cmds  map[string]bool
}

func (m *model) selectAPI(reg *registry, maxVersion string, exts []string) (*selection, error) {
	sel := &selection{Types: map[string]bool{}, Cmds: map[string]bool{}}
	var roots, cmdRoots []string
	for _, f := range reg.Features {
		if !forVulkan(f.API) || f.Number > maxVersion {
			continue
		}
		for _, r := range f.Requires {
			t, c, err := m.applyRequire(r, "")
			if err != nil {
				return nil, fmt.Errorf("feature %s: %w", f.Name, err)
			}
			roots, cmdRoots = append(roots, t...), append(cmdRoots, c...)
		}
	}
	byName := map[string]extension{}
	for _, e := range reg.Extensions {
		byName[e.Name] = e
	}
	for _, name := range exts {
		e, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("extension %s is not in the registry", name)
		}
		if !strings.Contains(e.Supported, "vulkan") {
			return nil, fmt.Errorf("extension %s is not supported by the Vulkan API (%q)", name, e.Supported)
		}
		for _, r := range e.Requires {
			t, c, err := m.applyRequire(r, e.Number)
			if err != nil {
				return nil, fmt.Errorf("extension %s: %w", name, err)
			}
			roots, cmdRoots = append(roots, t...), append(cmdRoots, c...)
		}
	}
	for _, c := range cmdRoots {
		if err := m.closeCommand(sel, c); err != nil {
			return nil, err
		}
	}
	for _, t := range roots {
		if err := m.closeType(sel, t); err != nil {
			return nil, err
		}
	}
	return sel, nil
}

// applyRequire records the enum values a <require> block adds and returns the
// type and command names it lists.
func (m *model) applyRequire(r require, extNumber string) (types, cmds []string, err error) {
	if !forVulkan(r.API) {
		return nil, nil, nil
	}
	for _, t := range r.Types {
		types = append(types, t.Name)
	}
	for _, c := range r.Commands {
		cmds = append(cmds, c.Name)
	}
	for _, en := range r.Enums {
		if !forVulkan(en.API) || en.Protect != "" {
			continue
		}
		if en.Extends == "" {
			if err := m.addConst(en); err != nil {
				return nil, nil, err
			}
			continue
		}
		v, err := enumValue(en, extNumber)
		if err != nil {
			return nil, nil, err
		}
		m.enum(en.Extends).add(v)
	}
	return types, cmds, nil
}

func (m *model) closeCommand(sel *selection, name string) error {
	if sel.Cmds[name] {
		return nil
	}
	c, ok := m.Cmds[name]
	if !ok {
		return fmt.Errorf("command %s is not in the registry", name)
	}
	sel.Cmds[name] = true
	if c.Alias != "" {
		return m.closeCommand(sel, c.Alias)
	}
	if err := m.closeType(sel, c.Ret); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	for _, p := range c.Params {
		if err := m.closeType(sel, p.Type); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func (m *model) closeType(sel *selection, name string) error {
	if name == "void" || sel.Types[name] {
		return nil
	}
	if _, ok := cPrimitives[name]; ok {
		return nil
	}
	t, ok := m.Types[name]
	if !ok {
		return fmt.Errorf("type %s is not in the registry", name)
	}
	if t.Kind == kindOther {
		return nil // defines and includes have no Go form
	}
	sel.Types[name] = true
	var deps []string
	switch t.Kind {
	case kindAlias:
		deps = append(deps, t.Alias)
	case kindStruct, kindUnion:
		for _, d := range t.Members {
			deps = append(deps, d.Type)
		}
	case kindBitmask:
		if t.Bits != "" {
			deps = append(deps, t.Bits)
		}
	case kindEnum:
		if t.Flags != "" {
			deps = append(deps, t.Flags)
		}
	case kindBase:
		if t.Base != "" {
			deps = append(deps, t.Base)
		}
	}
	for _, d := range deps {
		if err := m.closeType(sel, d); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// sortedTypes returns the selected type names of one kind, sorted.
func (m *model) sortedTypes(sel *selection, k kind) []string {
	var names []string
	for name := range sel.Types {
		if m.Types[name].Kind == k {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func (m *model) sortedCmds(sel *selection) []string {
	names := make([]string, 0, len(sel.Cmds))
	for name := range sel.Cmds {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
