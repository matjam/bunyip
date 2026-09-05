package ecs

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/matjam/bunyip/gfx"
)

// The registry maps component and resource types to the names save
// files and prefab files use, so a file written by one build of the
// game loads in the next.
var registry = struct {
	sync.RWMutex
	byName map[string]reflect.Type
	byType map[reflect.Type]string
}{byName: map[string]reflect.Type{}, byType: map[reflect.Type]string{}}

func init() {
	Register[gfx.Transform]("gfx.Transform")
	Register[gfx.Transform2]("gfx.Transform2")
}

// Register names a component or resource type for Save, Load and
// prefab files. The name is what the file holds, so choose one that
// stays stable when the type moves or is renamed. Registering the same
// type under the same name again does nothing; binding a name or a type
// that is already bound to something else panics. gfx.Transform,
// gfx.Transform2 and ecs.Name are registered under those names by
// default.
func Register[T any](name string) {
	t := typeOf[T]()
	if t == nil || t.Kind() == reflect.Pointer {
		panic("ecs: Register needs a value type, not an interface or pointer")
	}
	if name == "" {
		panic("ecs: Register needs a name")
	}
	registry.Lock()
	defer registry.Unlock()
	if old, ok := registry.byName[name]; ok && old != t {
		panic(fmt.Sprintf("ecs: name %q is registered for %v, cannot register %v", name, old, t))
	}
	if old, ok := registry.byType[t]; ok && old != name {
		panic(fmt.Sprintf("ecs: %v is registered as %q, cannot register as %q", t, old, name))
	}
	registry.byName[name] = t
	registry.byType[t] = name
}

func nameOf(t reflect.Type) (string, bool) {
	registry.RLock()
	defer registry.RUnlock()
	name, ok := registry.byType[t]
	return name, ok
}

func typeNamed(name string) (reflect.Type, bool) {
	registry.RLock()
	defer registry.RUnlock()
	t, ok := registry.byName[name]
	return t, ok
}

// UnregisteredError reports types Save met that have no registered
// name, or names Load or ParsePrefab met that no type is registered
// under. Names holds the Go type names or the file's names respectively.
type UnregisteredError struct {
	Names []string
}

func (e *UnregisteredError) Error() string {
	return "ecs: unregistered: " + strings.Join(e.Names, ", ")
}

// MarshalJSON encodes the entity as its ID, so components and resources
// holding entities save and load with encoding/json. Load rewrites the
// IDs to the handles it creates.
func (e Entity) MarshalJSON() ([]byte, error) { return strconv.AppendUint(nil, e.ID(), 10), nil }

// UnmarshalJSON decodes an entity written by MarshalJSON.
func (e *Entity) UnmarshalJSON(b []byte) error {
	return e.UnmarshalText([]byte(strings.Trim(string(b), `"`)))
}

// MarshalText encodes the entity as its ID, for use as a map key.
func (e Entity) MarshalText() ([]byte, error) { return e.MarshalJSON() }

// UnmarshalText decodes an entity written by MarshalText.
func (e *Entity) UnmarshalText(b []byte) error {
	if string(b) == "null" {
		*e = None
		return nil
	}
	n, err := strconv.ParseUint(string(b), 10, 64)
	if err != nil {
		return fmt.Errorf("ecs: entity %q: %w", b, err)
	}
	*e = entityFromID(n)
	return nil
}

func entityFromID(n uint64) Entity { return Entity{id: uint32(n), gen: uint32(n >> 32)} }

const saveVersion = 1

type saveDoc struct {
	Version   int                        `json:"version"`
	Entities  []saveEntity               `json:"entities"`
	Resources map[string]json.RawMessage `json:"resources,omitempty"`
}

type saveEntity struct {
	ID         uint64                     `json:"id"`
	Parent     uint64                     `json:"parent,omitempty"`
	Children   []uint64                   `json:"children,omitempty"`
	Components map[string]json.RawMessage `json:"components"`
}

// SaveOptions adjust World.Save.
type SaveOptions struct {
	// SkipUnregistered leaves out components and resources whose type
	// has no registered name instead of failing with UnregisteredError.
	SkipUnregistered bool
	// Indent writes the file with line breaks and indentation.
	Indent bool
}

// Save writes every live entity as JSON: its ID, its parent and
// children, and each component encoded with encoding/json under its
// registered name. Registered resources follow. Parent and Children
// components are written as the links, not as components. A component
// or resource type without a registered name fails the save with an
// UnregisteredError naming every such type, unless
// SaveOptions.SkipUnregistered is set.
func (w *World) Save(out io.Writer, opts ...SaveOptions) error {
	var o SaveOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	doc := saveDoc{Version: saveVersion, Entities: []saveEntity{}}
	missing := map[string]bool{}
	for _, e := range w.Entities() {
		m := &w.meta[e.id-1]
		a := m.arch
		rec := saveEntity{ID: e.ID(), Components: map[string]json.RawMessage{}}
		if p, ok := ParentOf(w, e); ok {
			rec.Parent = p.ID()
		}
		for _, c := range ChildrenOf(w, e) {
			if w.Alive(c) {
				rec.Children = append(rec.Children, c.ID())
			}
		}
		for i, id := range a.ids {
			t := w.comps[id].typ
			if t == parentType || t == childrenType {
				continue
			}
			name, ok := nameOf(t)
			if !ok {
				missing[t.String()] = true
				continue
			}
			b, err := json.Marshal(a.columns[i].getAny(int(m.row)))
			if err != nil {
				return fmt.Errorf("ecs: save %s of %v: %w", name, e, err)
			}
			rec.Components[name] = b
		}
		doc.Entities = append(doc.Entities, rec)
	}
	for t, p := range w.resources {
		name, ok := nameOf(t)
		if !ok {
			missing[t.String()] = true
			continue
		}
		b, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("ecs: save resource %s: %w", name, err)
		}
		if doc.Resources == nil {
			doc.Resources = map[string]json.RawMessage{}
		}
		doc.Resources[name] = b
	}
	if len(missing) > 0 && !o.SkipUnregistered {
		return &UnregisteredError{Names: sortedKeys(missing)}
	}
	var b []byte
	var err error
	if o.Indent {
		b, err = json.MarshalIndent(doc, "", "  ")
	} else {
		b, err = json.Marshal(doc)
	}
	if err != nil {
		return fmt.Errorf("ecs: save: %w", err)
	}
	_, err = out.Write(append(b, '\n'))
	return err
}

// LoadOptions adjust World.Load.
type LoadOptions struct {
	// SkipUnknown leaves out components and resources saved under a
	// name this build has not registered instead of failing with
	// UnregisteredError.
	SkipUnknown bool
}

// Load adds the entities and resources of a file written by Save to
// the world. Every entity gets a new handle; parent links, Children
// lists and the Entity fields inside components and resources are
// rewritten to the new handles (see Remapper), and a reference to an
// entity the file does not contain becomes None. Loaded resources
// replace the world's. Entities already in the world are untouched.
//
// Names with no registered type fail the load before anything is added,
// with an UnregisteredError listing them, unless LoadOptions.SkipUnknown
// is set. A value that fails to decode stops the load part way.
func (w *World) Load(in io.Reader, opts ...LoadOptions) error {
	var o LoadOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	var doc saveDoc
	if err := json.NewDecoder(in).Decode(&doc); err != nil {
		return fmt.Errorf("ecs: load: %w", err)
	}
	if doc.Version != saveVersion {
		return fmt.Errorf("ecs: load: unsupported save version %d", doc.Version)
	}
	unknown := map[string]bool{}
	for _, rec := range doc.Entities {
		for name := range rec.Components {
			if _, ok := typeNamed(name); !ok {
				unknown[name] = true
			}
		}
	}
	for name := range doc.Resources {
		if _, ok := typeNamed(name); !ok {
			unknown[name] = true
		}
	}
	if len(unknown) > 0 && !o.SkipUnknown {
		return &UnregisteredError{Names: sortedKeys(unknown)}
	}

	// Allocate every entity first so references can be rewritten.
	ids := make(map[uint64]Entity, len(doc.Entities))
	for _, rec := range doc.Entities {
		if _, dup := ids[rec.ID]; dup {
			return fmt.Errorf("ecs: load: entity %d appears twice", rec.ID)
		}
		ids[rec.ID] = w.Spawn()
	}
	remap := func(e Entity) Entity { return ids[e.ID()] }
	for _, rec := range doc.Entities {
		comps := make([]any, 0, len(rec.Components)+2)
		for _, name := range sortedKeys(rec.Components) {
			t, ok := typeNamed(name)
			if !ok {
				continue
			}
			p, err := decodeValue(t, rec.Components[name])
			if err != nil {
				return fmt.Errorf("ecs: load %s of entity %d: %w", name, rec.ID, err)
			}
			remapPtr(p, remap)
			comps = append(comps, p.Elem().Interface())
		}
		if p := ids[rec.Parent]; p.Valid() {
			comps = append(comps, Parent{Entity: p})
		}
		var kids []Entity
		for _, id := range rec.Children {
			if c := ids[id]; c.Valid() {
				kids = append(kids, c)
			}
		}
		if len(kids) > 0 {
			comps = append(comps, Children{List: kids})
		}
		w.setComponents(ids[rec.ID], comps)
	}
	// Parent links come from the file; a cycle among them would spin
	// every ancestor walk forever.
	for _, e := range ids {
		for a, n := e, 0; ; n++ {
			p, ok := Get[Parent](w, a)
			if !ok || !w.Alive(p.Entity) {
				break
			}
			if n >= len(ids) {
				return fmt.Errorf("ecs: load: entity %d is in a parent cycle", e.ID())
			}
			a = p.Entity
		}
	}
	for _, name := range sortedKeys(doc.Resources) {
		t, ok := typeNamed(name)
		if !ok {
			continue
		}
		p, err := decodeValue(t, doc.Resources[name])
		if err != nil {
			return fmt.Errorf("ecs: load resource %s: %w", name, err)
		}
		remapPtr(p, remap)
		w.resources[t] = p.Interface()
	}
	return nil
}

// decodeValue unmarshals raw into a new value of type t and returns a
// pointer to it.
func decodeValue(t reflect.Type, raw json.RawMessage) (reflect.Value, error) {
	p := reflect.New(t)
	if err := json.Unmarshal(raw, p.Interface()); err != nil {
		return p, err
	}
	return p, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
