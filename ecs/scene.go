package ecs

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

const sceneVersion = 1

func init() { Register[Name]("ecs.Name") }

// Name labels an entity so a scene can find it again. Instantiate adds
// one to every entity its scene names, and ExportScene writes it back as
// the scene entity's name instead of as a component. The zero value is
// the empty name, which no lookup matches.
type Name struct{ Text string }

// NameOf returns the entity's name, and false when it has none or the
// name is empty.
func NameOf(w *World, e Entity) (string, bool) {
	if n, ok := Get[Name](w, e); ok && n.Text != "" {
		return n.Text, true
	}
	return "", false
}

var nameType = reflect.TypeOf(Name{})

// Scene is a document of entities a world spawns as a unit: a name,
// free-form properties for the game's own use, and a list of entities
// with their components, parent links and prefab references. Read one
// with ParseScene, build one with NewScene, take one from a live world
// with World.ExportScene, and spawn it with World.Instantiate.
//
// Entities are numbered from one in Entities order. SceneEntity.Parent
// holds that number, and so does an Entity field inside a component, so
// zero means no parent and the None entity in both places.
type Scene struct {
	// Name labels the scene. It is written to the document and read
	// back; nothing else uses it. The zero value is no name.
	Name string
	// Properties are the game's own values, such as the music track or
	// the wave number. They are encoded with the scene and come back
	// decoded by encoding/json, so a number arrives as a float64. The
	// zero value is no properties.
	Properties map[string]any
	// Entities are the entities the scene spawns, in the order
	// Instantiate creates them. The zero value is an empty scene.
	Entities []SceneEntity
}

// SceneEntity is one entity of a Scene.
type SceneEntity struct {
	// Name labels the entity for SceneInstance.Entity and becomes a
	// Name component on the spawned entity. Names are unique within a
	// scene. The zero value leaves the entity unnamed.
	Name string `json:"name,omitempty"`
	// Parent is the number of the entity this one hangs under, counted
	// from one in Scene.Entities order. The zero value makes it a root
	// of the scene.
	Parent int `json:"parent,omitempty"`
	// Prefab names a prefab in the library Instantiate uses. The zero
	// value builds the entity from Components alone; otherwise the
	// prefab is spawned and each entry of Components replaces the
	// prefab's whole component of that type.
	Prefab string `json:"prefab,omitempty"`
	// Components holds each component encoded the way World.Save
	// encodes it, keyed by the name Register gave its type. The zero
	// value is no components.
	Components map[string]json.RawMessage `json:"components,omitempty"`
}

// sceneDoc is the wire form; Scene itself carries no version field.
type sceneDoc struct {
	Version    int            `json:"version"`
	Name       string         `json:"name,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Entities   []SceneEntity  `json:"entities"`
}

// SceneRef returns the value to put in a component's Entity field to
// refer to the entity numbered n in a scene, counting from one.
// Instantiate rewrites it to the handle that entity was given. A number
// of zero, or one no entity has, is None.
func SceneRef(n int) Entity {
	if n <= 0 {
		return None
	}
	return entityFromID(uint64(n))
}

// NewScene makes an empty scene with a name.
func NewScene(name string) *Scene { return &Scene{Name: name} }

// AddEntity appends an entity carrying the component values and returns
// its number, its place in the scene counted from one. Pass an empty
// name for an entity nothing needs to find again. Each component is
// encoded the way World.Save encodes it, so its type needs a name from
// Register; a type without one is an UnregisteredError and nothing is
// added. Use SceneRef for an Entity field that points at another entity
// of the scene.
func (s *Scene) AddEntity(name string, comps ...any) (int, error) {
	return s.add(SceneEntity{Name: name}, comps)
}

// AddPrefab appends an entity built from the library prefab called
// prefab and returns its number. Each override replaces the prefab's
// whole component of that type rather than merging field by field, and
// the prefab's children spawn as they are. Instantiate fails with a
// MissingPrefabError when its library has no prefab of that name.
func (s *Scene) AddPrefab(name, prefab string, overrides ...any) (int, error) {
	return s.add(SceneEntity{Name: name, Prefab: prefab}, overrides)
}

func (s *Scene) add(rec SceneEntity, comps []any) (int, error) {
	if rec.Name != "" {
		for _, e := range s.Entities {
			if e.Name == rec.Name {
				return 0, fmt.Errorf("ecs: scene: two entities are named %q", rec.Name)
			}
		}
	}
	var missing []string
	for _, c := range comps {
		t := reflect.TypeOf(c)
		if t == nil || t.Kind() == reflect.Pointer {
			return 0, fmt.Errorf("ecs: scene: component must be a value, got %T", c)
		}
		cname, ok := nameOf(t)
		if !ok {
			missing = append(missing, t.String())
			continue
		}
		b, err := json.Marshal(c)
		if err != nil {
			return 0, fmt.Errorf("ecs: scene %s: %w", cname, err)
		}
		if rec.Components == nil {
			rec.Components = map[string]json.RawMessage{}
		}
		rec.Components[cname] = b
	}
	if len(missing) > 0 {
		return 0, &UnregisteredError{Names: missing}
	}
	s.Entities = append(s.Entities, rec)
	return len(s.Entities), nil
}

// SetParent hangs the entity numbered child under the entity numbered
// parent, both counted from one. A parent of zero makes the child a
// root. A number outside the scene, or a child equal to its parent, is
// ignored.
func (s *Scene) SetParent(child, parent int) {
	if child < 1 || child > len(s.Entities) || parent < 0 || parent > len(s.Entities) || child == parent {
		return
	}
	s.Entities[child-1].Parent = parent
}

// SetProperty stores a free-form value under key for the game's own
// use. It is encoded with the scene and comes back decoded by
// encoding/json, so a number arrives as a float64.
func (s *Scene) SetProperty(key string, v any) {
	if s.Properties == nil {
		s.Properties = map[string]any{}
	}
	s.Properties[key] = v
}

// ParseScene reads a scene document: a JSON object with a "version" of
// 1, an optional "name" and "properties", and an "entities" array. Each
// entity has an optional "name", an optional "parent" holding another
// entity's number counted from one, an optional "prefab" naming a
// prefab in the library Instantiate uses, and a "components" object
// keyed by registered name with each value encoded as a save file
// encodes it.
//
// A version other than 1, a parent outside the scene or in a cycle, and
// two entities with the same name are all errors. Component names are
// checked by Instantiate, not here, so a scene parses in a build that
// has not registered every type in it.
func ParseScene(data []byte) (*Scene, error) {
	var doc sceneDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("ecs: scene: %w", err)
	}
	if doc.Version != sceneVersion {
		return nil, fmt.Errorf("ecs: scene: unsupported version %d", doc.Version)
	}
	s := &Scene{Name: doc.Name, Properties: doc.Properties, Entities: doc.Entities}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Encode writes the scene as the document ParseScene reads, indented so
// it stays readable in a text editor and useful in version control. A
// scene whose parent links are out of range or circular, or that names
// two entities the same, is an error.
func (s *Scene) Encode() ([]byte, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	doc := sceneDoc{Version: sceneVersion, Name: s.Name, Properties: s.Properties, Entities: s.Entities}
	if doc.Entities == nil {
		doc.Entities = []SceneEntity{}
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("ecs: scene: %w", err)
	}
	return append(b, '\n'), nil
}

// validate checks the links and names a document must get right before
// anything reads it.
func (s *Scene) validate() error {
	named := make(map[string]bool, len(s.Entities))
	for i, e := range s.Entities {
		if e.Parent < 0 || e.Parent > len(s.Entities) {
			return fmt.Errorf("ecs: scene: entity %d has parent %d, which is outside the scene", i+1, e.Parent)
		}
		if e.Parent == i+1 {
			return fmt.Errorf("ecs: scene: entity %d is its own parent", i+1)
		}
		if e.Name == "" {
			continue
		}
		if named[e.Name] {
			return fmt.Errorf("ecs: scene: two entities are named %q", e.Name)
		}
		named[e.Name] = true
	}
	for i := range s.Entities {
		n, steps := s.Entities[i].Parent, 0
		for n != 0 {
			if steps > len(s.Entities) {
				return fmt.Errorf("ecs: scene: entity %d is in a parent cycle", i+1)
			}
			n, steps = s.Entities[n-1].Parent, steps+1
		}
	}
	return nil
}

// PrefabLibrary holds prefabs under the names scene documents reference.
// Build one at start-up and either pass it in InstantiateOptions or
// store it on the world with SetResource, which is where Instantiate
// looks when the options carry none. The zero value is an empty library,
// and every reference into it is a MissingPrefabError.
type PrefabLibrary map[string]*Prefab

// MissingPrefabError reports a scene reference to a prefab the library
// does not hold. Name is the name the scene asked for.
type MissingPrefabError struct{ Name string }

func (e *MissingPrefabError) Error() string {
	return fmt.Sprintf("ecs: scene: no prefab named %q", e.Name)
}

// InstantiateOptions adjust World.Instantiate.
type InstantiateOptions struct {
	// Prefabs resolves the scene's prefab references. The zero value
	// falls back to the world's PrefabLibrary resource.
	Prefabs PrefabLibrary
	// Parent hangs every root of the instance under this entity, which
	// is how a level goes under a container or a room under a floor.
	// The zero value, None, leaves the roots unparented.
	Parent Entity
	// Offset moves each root of the instance by this much, added to the
	// position in its gfx.Transform or gfx.Transform2. A root with
	// neither component is not moved. The zero value moves nothing.
	Offset lin.Vec3
	// SkipUnknown leaves out components stored under a name this build
	// has not registered instead of failing with UnregisteredError.
	SkipUnknown bool
}

// SceneInstance is one spawned copy of a scene. Keep it to find the
// copy's entities by name and to remove the copy again.
type SceneInstance struct {
	// Roots are the entities the scene left unparented, in scene order.
	// With InstantiateOptions.Parent they hang under that entity.
	Roots []Entity
	// Spawned lists every entity the copy created, in scene order, with
	// a prefab's children after the entity that referenced it.
	Spawned []Entity

	named map[string]Entity
}

// Entity returns the entity this copy of the scene gave the name, and
// false when the scene names nothing that.
func (si *SceneInstance) Entity(name string) (Entity, bool) {
	e, ok := si.named[name]
	return e, ok
}

// Despawn removes every entity the copy spawned, along with any
// children they have gained since, and leaves other copies of the same
// scene alone. Entities already gone are skipped. The instance is empty
// afterwards.
func (si *SceneInstance) Despawn(w *World) {
	for _, e := range si.Spawned {
		w.Despawn(e)
	}
	si.Roots, si.Spawned, si.named = nil, nil, nil
}

// Instantiate spawns a copy of the scene into the world and returns
// what it made. Every call makes fresh entities, so several copies of
// one scene live side by side. Components are decoded and attached, the
// scene's parent links are rebuilt, every named entity gets a Name
// component, and an Entity field holding an entity number is rewritten
// to the handle that entity was given; a number the scene does not use
// becomes None. A prefab reference spawns the library's prefab with its
// children and writes the entity's own components over the prefab's.
//
// A component name this build has not registered fails the call with an
// UnregisteredError listing the names, unless
// InstantiateOptions.SkipUnknown is set, and a prefab reference the
// library cannot resolve is a MissingPrefabError. Nothing is left in the
// world when the call fails.
func (w *World) Instantiate(scene *Scene, opts ...InstantiateOptions) (*SceneInstance, error) {
	var o InstantiateOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	if scene == nil {
		return nil, errors.New("ecs: instantiate: no scene")
	}
	if err := scene.validate(); err != nil {
		return nil, err
	}
	lib := o.Prefabs
	if lib == nil {
		if p := Resource[PrefabLibrary](w); p != nil {
			lib = *p
		}
	}
	unknown := map[string]bool{}
	for _, rec := range scene.Entities {
		for name := range rec.Components {
			if _, ok := typeNamed(name); !ok {
				unknown[name] = true
			}
		}
		if rec.Prefab != "" {
			if _, ok := lib[rec.Prefab]; !ok {
				return nil, &MissingPrefabError{Name: rec.Prefab}
			}
		}
	}
	if len(unknown) > 0 && !o.SkipUnknown {
		return nil, &UnregisteredError{Names: sortedKeys(unknown)}
	}

	si := &SceneInstance{named: map[string]Entity{}}
	refs := make([]Entity, len(scene.Entities))
	for i, rec := range scene.Entities {
		if rec.Prefab == "" {
			refs[i] = w.Spawn()
			si.Spawned = append(si.Spawned, refs[i])
			continue
		}
		refs[i] = lib[rec.Prefab].Spawn(w)
		si.Spawned = append(si.Spawned, refs[i])
		si.Spawned = append(si.Spawned, descendants(w, refs[i])...)
	}
	remap := func(e Entity) Entity {
		if n := e.ID(); n >= 1 && n <= uint64(len(refs)) {
			return refs[n-1]
		}
		return None
	}
	for i, rec := range scene.Entities {
		comps := make([]any, 0, len(rec.Components)+1)
		for _, name := range sortedKeys(rec.Components) {
			t, ok := typeNamed(name)
			if !ok {
				continue // SkipUnknown; the check above rejected it otherwise
			}
			p, err := decodeValue(t, rec.Components[name])
			if err != nil {
				si.Despawn(w)
				return nil, fmt.Errorf("ecs: scene %s of entity %d: %w", name, i+1, err)
			}
			remapPtr(p, remap)
			comps = append(comps, p.Elem().Interface())
		}
		if rec.Name != "" {
			comps = append(comps, Name{Text: rec.Name})
			si.named[rec.Name] = refs[i]
		}
		if len(comps) > 0 {
			w.setComponents(refs[i], comps)
		}
	}
	for i, rec := range scene.Entities {
		if rec.Parent != 0 {
			SetParent(w, refs[i], refs[rec.Parent-1])
			continue
		}
		si.Roots = append(si.Roots, refs[i])
		if o.Parent.Valid() {
			SetParent(w, refs[i], o.Parent)
		}
	}
	if o.Offset != (lin.Vec3{}) {
		for _, e := range si.Roots {
			offsetEntity(w, e, o.Offset)
		}
	}
	return si, nil
}

// offsetEntity moves an entity's transform, whichever dimension it has.
func offsetEntity(w *World, e Entity, d lin.Vec3) {
	if t, ok := Get[gfx.Transform](w, e); ok {
		t.Position = t.Position.Add(d)
		return
	}
	if t, ok := Get[gfx.Transform2](w, e); ok {
		t.Position = t.Position.Add(lin.V2(d.X, d.Y))
	}
}

// descendants lists an entity's children and theirs, depth first.
func descendants(w *World, e Entity) []Entity {
	var out []Entity
	for _, c := range ChildrenOf(w, e) {
		out = append(out, c)
		out = append(out, descendants(w, c)...)
	}
	return out
}

// ExportScene captures entities and everything under them as a scene
// document Instantiate spawns again, which is how a game writes out
// what it built or what the player changed. Pass the roots to capture,
// or none to capture every entity in the world that has no parent.
//
// Each entity's components are encoded the way World.Save encodes them,
// its Name component becomes the scene entity's name, and an Entity
// field pointing at another captured entity becomes that entity's
// number; a reference to anything else becomes None. A component type
// with no name from Register fails the call with an UnregisteredError
// naming every such type.
//
// The document holds components, never prefab references, because a
// live entity does not remember which prefab made it.
func (w *World) ExportScene(roots ...Entity) (*Scene, error) {
	if len(roots) == 0 {
		for _, e := range w.Entities() {
			if _, ok := ParentOf(w, e); !ok {
				roots = append(roots, e)
			}
		}
	}
	var list []Entity
	var parents []int
	num := map[Entity]int{}
	var walk func(e Entity, parent int)
	walk = func(e Entity, parent int) {
		if !w.Alive(e) || num[e] != 0 {
			return
		}
		list = append(list, e)
		parents = append(parents, parent)
		n := len(list)
		num[e] = n
		for _, c := range ChildrenOf(w, e) {
			walk(c, n)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	ref := func(e Entity) Entity { return SceneRef(num[e]) }

	s := &Scene{Entities: make([]SceneEntity, 0, len(list))}
	missing := map[string]bool{}
	for i, e := range list {
		rec := SceneEntity{Parent: parents[i]}
		if n, ok := NameOf(w, e); ok {
			rec.Name = n
		}
		m := &w.meta[e.id-1]
		a := m.arch
		for col, id := range a.ids {
			t := w.comps[id].typ
			if t == parentType || t == childrenType || t == nameType {
				continue
			}
			cname, ok := nameOf(t)
			if !ok {
				missing[t.String()] = true
				continue
			}
			b, err := json.Marshal(remapAny(a.columns[col].getAny(int(m.row)), ref))
			if err != nil {
				return nil, fmt.Errorf("ecs: scene %s of %v: %w", cname, e, err)
			}
			if rec.Components == nil {
				rec.Components = map[string]json.RawMessage{}
			}
			rec.Components[cname] = b
		}
		s.Entities = append(s.Entities, rec)
	}
	if len(missing) > 0 {
		return nil, &UnregisteredError{Names: sortedKeys(missing)}
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}
