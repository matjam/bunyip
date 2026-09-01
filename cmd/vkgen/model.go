package main

import (
	"fmt"
	"regexp"
	"strings"
)

type kind int

const (
	kindBase kind = iota
	kindHandle
	kindEnum
	kindBitmask
	kindStruct
	kindUnion
	kindFuncPtr
	kindExternal
	kindAlias
	kindOther // define, include: never emitted
)

// typ is one named type from the registry after the XML has been read.
type typ struct {
	Name  string
	Kind  kind
	Alias string // target, when Kind is kindAlias

	Base   string // C type a basetype is a typedef of
	Ptr    int    // pointer depth of that typedef
	Opaque bool   // forward-declared; only pointers to it exist

	Bits  string // for a bitmask: the FlagBits enum that holds its values
	Width int    // 32 or 64
	Flags string // for a FlagBits enum: the Flags type it belongs to

	Members []decl
}

type enumVal struct {
	Name  string
	Expr  string // Go expression, empty when Alias is set
	Alias string
}

type enumType struct {
	Name      string
	IsBitmask bool
	Width     int
	Values    []enumVal
	seen      map[string]bool
}

func (e *enumType) add(v enumVal) {
	if e.seen[v.Name] {
		return
	}
	e.seen[v.Name] = true
	e.Values = append(e.Values, v)
}

type cmd struct {
	Name   string
	Alias  string
	Ret    string
	Params []decl
}

type model struct {
	HeaderVersion string
	Types         map[string]*typ
	Enums         map[string]*enumType
	Consts        []enumVal
	ConstValues   map[string]int64
	Cmds          map[string]*cmd
	constSeen     map[string]bool
	layouts       map[string]layout
}

var reHeaderVersion = regexp.MustCompile(`<name>VK_HEADER_VERSION</name>\s*(\d+)`)

func buildModel(reg *registry) (*model, error) {
	m := &model{
		Types:       map[string]*typ{},
		Enums:       map[string]*enumType{},
		ConstValues: map[string]int64{},
		Cmds:        map[string]*cmd{},
		constSeen:   map[string]bool{},
		layouts:     map[string]layout{},
	}
	for _, t := range reg.Types {
		if !forVulkan(t.API) {
			continue
		}
		if err := m.addType(t); err != nil {
			return nil, err
		}
	}
	m.linkFlagBits()
	for _, b := range reg.Enums {
		if err := m.addEnumBlock(b); err != nil {
			return nil, err
		}
	}
	for _, c := range reg.Commands {
		if !forVulkan(c.API) {
			continue
		}
		if err := m.addCommand(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *model) addType(t typeDef) error {
	name := t.name()
	if t.Category == "define" && name == "VK_HEADER_VERSION" {
		if h := reHeaderVersion.FindStringSubmatch(t.Inner); h != nil {
			m.HeaderVersion = h[1]
		}
	}
	if t.Alias != "" {
		m.Types[name] = &typ{Name: name, Kind: kindAlias, Alias: t.Alias}
		return nil
	}
	ty := &typ{Name: name}
	switch t.Category {
	case "basetype":
		ty.Kind = kindBase
		ty.Base, ty.Ptr, ty.Opaque = parseBaseType(t.Inner)
	case "handle":
		ty.Kind = kindHandle
	case "enum":
		ty.Kind = kindEnum
	case "bitmask":
		ty.Kind = kindBitmask
		ty.Bits = t.Requires
		ty.Width = 32
		if t.BitValues != "" {
			ty.Bits = t.BitValues
		}
		if strings.Contains(t.Inner, "VkFlags64") {
			ty.Width = 64
		}
	case "struct", "union":
		ty.Kind = kindStruct
		if t.Category == "union" {
			ty.Kind = kindUnion
		}
		for _, mem := range t.Members {
			if !forVulkan(mem.API) {
				continue
			}
			d, err := parseDecl(mem.Inner)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			ty.Members = append(ty.Members, d)
		}
	case "funcpointer":
		ty.Kind = kindFuncPtr
	case "":
		ty.Kind = kindExternal
	default:
		ty.Kind = kindOther
	}
	m.Types[name] = ty
	return nil
}

// linkFlagBits records, on each FlagBits enum, the Flags type whose values it
// defines, so the enum can be emitted as an alias of that type.
func (m *model) linkFlagBits() {
	for _, t := range m.Types {
		if t.Kind != kindBitmask || t.Bits == "" {
			continue
		}
		if bits, ok := m.Types[t.Bits]; ok && bits.Kind == kindEnum {
			bits.Flags = t.Name
			bits.Width = t.Width
		}
	}
}

func (m *model) enum(name string) *enumType {
	e, ok := m.Enums[name]
	if !ok {
		e = &enumType{Name: name, Width: 32, seen: map[string]bool{}}
		m.Enums[name] = e
	}
	return e
}

func (m *model) addEnumBlock(b enumBlock) error {
	if b.Type == "constants" {
		for _, en := range b.Entries {
			if err := m.addConst(en); err != nil {
				return err
			}
		}
		return nil
	}
	e := m.enum(b.Name)
	e.IsBitmask = b.Type == "bitmask"
	if b.BitWidth == "64" {
		e.Width = 64
	}
	for _, en := range b.Entries {
		if !forVulkan(en.API) || en.Protect != "" {
			continue
		}
		v, err := enumValue(en, "")
		if err != nil {
			return fmt.Errorf("%s: %w", b.Name, err)
		}
		e.add(v)
	}
	return nil
}

func (m *model) addConst(en enumEntry) error {
	if !forVulkan(en.API) || m.constSeen[en.Name] {
		return nil
	}
	m.constSeen[en.Name] = true
	if en.Alias != "" {
		m.Consts = append(m.Consts, enumVal{Name: en.Name, Alias: en.Alias})
		if v, ok := m.ConstValues[en.Alias]; ok {
			m.ConstValues[en.Name] = v
		}
		return nil
	}
	expr, n, err := constExpr(en.Value, en.Type)
	if err != nil {
		return fmt.Errorf("constant %s: %w", en.Name, err)
	}
	m.Consts = append(m.Consts, enumVal{Name: en.Name, Expr: expr})
	m.ConstValues[en.Name] = n
	return nil
}

func (m *model) addCommand(c command) error {
	name := c.name()
	if c.Alias != "" {
		m.Cmds[name] = &cmd{Name: name, Alias: c.Alias}
		return nil
	}
	cm := &cmd{Name: name, Ret: c.Proto.Type}
	for _, p := range c.Params {
		if !forVulkan(p.API) {
			continue
		}
		d, err := parseDecl(p.Inner)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		cm.Params = append(cm.Params, d)
	}
	m.Cmds[name] = cm
	return nil
}
