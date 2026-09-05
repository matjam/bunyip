package ecs

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// Prefab is a template of components, with child prefabs for a
// hierarchy, that Spawn stamps into a world any number of times. Build
// one in code with NewPrefab and Child, read one from JSON with
// ParsePrefab, or take one from a live entity with PrefabOf.
type Prefab struct {
	comps    []any
	children []*Prefab
}

// NewPrefab makes a prefab from component values. Pointers are refused;
// pass the struct.
func NewPrefab(comps ...any) *Prefab {
	for _, c := range comps {
		if t := reflect.TypeOf(c); t == nil || t.Kind() == reflect.Pointer {
			panic(fmt.Sprintf("ecs: prefab component must be a value, got %T", c))
		}
	}
	return &Prefab{comps: comps}
}

// Child adds child prefabs, spawned under the instance with SetParent,
// and returns the prefab for chaining.
func (p *Prefab) Child(children ...*Prefab) *Prefab {
	p.children = append(p.children, children...)
	return p
}

// Components returns copies of the prefab's component values.
func (p *Prefab) Components() []any {
	out := make([]any, len(p.comps))
	for i, c := range p.comps {
		out[i] = deepCopy(c)
	}
	return out
}

// Children returns the prefab's child prefabs; do not modify the slice.
func (p *Prefab) Children() []*Prefab { return p.children }

// Spawn creates an entity from the prefab, and its children under it.
// Every instance gets its own copy of the components (see Clone for
// how deep the copy goes), so changing one does not change the others
// or the prefab.
func (p *Prefab) Spawn(w *World) Entity {
	e := w.SpawnWith(p.Components()...)
	for _, c := range p.children {
		SetParent(w, c.Spawn(w), e)
	}
	return e
}

// PrefabOf snapshots an entity and its descendants as a prefab, leaving
// out Parent and Children. Spawning it gives a copy of the tree.
func PrefabOf(w *World, e Entity) *Prefab {
	if !w.Alive(e) {
		return nil
	}
	p := &Prefab{comps: ownComponents(w, e)}
	for _, c := range ChildrenOf(w, e) {
		if cp := PrefabOf(w, c); cp != nil {
			p.children = append(p.children, cp)
		}
	}
	return p
}

// ownComponents returns deep copies of an entity's components other
// than Parent and Children.
func ownComponents(w *World, e Entity) []any {
	var out []any
	for _, v := range w.ComponentValues(e) {
		if t := reflect.TypeOf(v); t == parentType || t == childrenType {
			continue
		}
		out = append(out, deepCopy(v))
	}
	return out
}

// ParsePrefab reads a prefab from JSON. The form is an object with a
// "components" object keyed by registered name, each value encoded as
// in a save file, and an optional "children" array of the same form.
// An unregistered name is an UnregisteredError.
func ParsePrefab(data []byte) (*Prefab, error) {
	var p Prefab
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

type prefabDoc struct {
	Components map[string]json.RawMessage `json:"components,omitempty"`
	Children   []*Prefab                  `json:"children,omitempty"`
}

// MarshalJSON writes the prefab in the form ParsePrefab reads. A
// component type without a registered name is an UnregisteredError.
func (p Prefab) MarshalJSON() ([]byte, error) {
	doc := prefabDoc{Children: p.children}
	if len(p.comps) > 0 {
		doc.Components = map[string]json.RawMessage{}
	}
	var missing []string
	for _, c := range p.comps {
		name, ok := nameOf(reflect.TypeOf(c))
		if !ok {
			missing = append(missing, reflect.TypeOf(c).String())
			continue
		}
		b, err := json.Marshal(c)
		if err != nil {
			return nil, fmt.Errorf("ecs: prefab %s: %w", name, err)
		}
		doc.Components[name] = b
	}
	if len(missing) > 0 {
		return nil, &UnregisteredError{Names: missing}
	}
	return json.Marshal(doc)
}

// UnmarshalJSON reads the form MarshalJSON writes.
func (p *Prefab) UnmarshalJSON(data []byte) error {
	var doc prefabDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	var unknown []string
	comps := make([]any, 0, len(doc.Components))
	for _, name := range sortedKeys(doc.Components) {
		t, ok := typeNamed(name)
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		v, err := decodeValue(t, doc.Components[name])
		if err != nil {
			return fmt.Errorf("ecs: prefab %s: %w", name, err)
		}
		comps = append(comps, v.Elem().Interface())
	}
	if len(unknown) > 0 {
		return &UnregisteredError{Names: unknown}
	}
	p.comps, p.children = comps, doc.Children
	return nil
}

// Clone makes a new entity carrying copies of e's components, attached
// to e's parent. Children are not copied; CloneTree does that. Entity
// fields that referred to e refer to the copy.
//
// The copy is deep through exported fields: slices, maps, pointers and
// interface values get their own storage. Unexported fields are copied
// as values, so a slice or pointer in one is shared with the original.
// Custom JSON methods are not used for cloning. Remapper handles entity
// references, not general deep copying; initialize private mutable state
// separately when it must be independent in the clone.
func Clone(w *World, e Entity) Entity {
	if !w.Alive(e) {
		return None
	}
	c := w.Spawn()
	cloneInto(w, e, c, map[Entity]Entity{e: c})
	if p, ok := ParentOf(w, e); ok {
		SetParent(w, c, p)
	}
	return c
}

// CloneTree clones e and all its descendants, keeping the hierarchy
// between the copies and attaching the root copy to e's parent. Entity
// fields that referred to something in the tree refer to its copy;
// references outside the tree are kept as they are.
func CloneTree(w *World, e Entity) Entity {
	if !w.Alive(e) {
		return None
	}
	m := map[Entity]Entity{}
	var alloc func(Entity)
	alloc = func(src Entity) {
		m[src] = w.Spawn()
		for _, c := range ChildrenOf(w, src) {
			if w.Alive(c) {
				alloc(c)
			}
		}
	}
	alloc(e)
	for src, dst := range m {
		cloneInto(w, src, dst, m)
	}
	for src, dst := range m {
		kids := append([]Entity(nil), ChildrenOf(w, src)...)
		for _, c := range kids {
			if n, ok := m[c]; ok {
				SetParent(w, n, dst)
			}
		}
	}
	root := m[e]
	if p, ok := ParentOf(w, e); ok {
		SetParent(w, root, p)
	}
	return root
}

// cloneInto gives dst copies of src's components, with entity
// references rewritten through m.
func cloneInto(w *World, src, dst Entity, m map[Entity]Entity) {
	remap := func(e Entity) Entity {
		if n, ok := m[e]; ok {
			return n
		}
		return e
	}
	comps := ownComponents(w, src)
	for i, c := range comps {
		comps[i] = remapAny(c, remap)
	}
	w.setComponents(dst, comps)
}
