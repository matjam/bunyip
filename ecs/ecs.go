// Package ecs is the engine's entity component system. Entities are
// cheap handles, components are plain Go structs stored in dense
// per-type columns, and queries iterate every entity carrying a set of
// components with no lookups in the loop.
//
// Storage is by archetype. Every distinct set of component types gets a
// table with one column per type, and an entity lives in exactly one
// table. Adding or removing a component moves the entity to another
// table, so structural changes cost a copy, while iterating a hundred
// thousand entities reads a few slices in order.
//
//	w := ecs.NewWorld()
//	e := w.SpawnWith(Position{1, 2}, Velocity{0.5, 0})
//	q := ecs.NewQuery2[Position, Velocity](w)
//	q.Each(func(e ecs.Entity, p *Position, v *Velocity) {
//		p.X += v.X
//	})
//
// Systems are functions registered on the world and run in order by
// Update. Resources are singletons such as the score or the rules.
// Events are per-frame queues that carry values between systems.
//
// Worlds save and load as JSON. Register names each component and
// resource type so files stay valid across builds. Save writes every
// live entity, its parent links and the registered resources, and Load
// recreates them with fresh handles, rewriting the Entity fields inside
// components. A Prefab is a template of components (and child prefabs)
// that spawns independent copies. Clone and CloneTree copy an entity
// that already exists.
package ecs

import (
	"fmt"
	"reflect"
	"sort"
)

// Entity identifies a thing in a World. The zero Entity is None. A
// handle from a despawned entity is never confused with a new one that
// reused its slot.
type Entity struct {
	id  uint32 // slot index plus one
	gen uint32
}

// None is the absent entity.
var None = Entity{}

// Valid reports whether the handle refers to some entity (not None).
func (e Entity) Valid() bool { return e.id != 0 }

// ID is a number unique among live entities, for debugging and maps.
func (e Entity) ID() uint64 { return uint64(e.gen)<<32 | uint64(e.id) }

// String formats the entity as index and generation.
func (e Entity) String() string { return fmt.Sprintf("e%d.%d", e.id, e.gen) }

// ComponentID numbers a component type within a world.
type ComponentID uint16

const maxComponents = 256

// mask is a set of component ids.
type mask [maxComponents / 64]uint64

func (m *mask) set(id ComponentID)   { m[id/64] |= 1 << (id % 64) }
func (m *mask) clear(id ComponentID) { m[id/64] &^= 1 << (id % 64) }
func (m mask) has(id ComponentID) bool {
	return m[id/64]&(1<<(id%64)) != 0
}

func (m mask) contains(o mask) bool {
	for i := range m {
		if m[i]&o[i] != o[i] {
			return false
		}
	}
	return true
}

func (m mask) intersects(o mask) bool {
	for i := range m {
		if m[i]&o[i] != 0 {
			return true
		}
	}
	return false
}

// column is one component type's storage inside an archetype.
type column interface {
	appendZero()
	setAny(row int, v any)
	getAny(row int) any
	// moveTo appends row's value to dst (a column of the same type).
	moveTo(dst column, row int)
	swapRemove(row int)
	len() int
}

type typedColumn[T any] struct{ data []T }

func (c *typedColumn[T]) appendZero()           { var z T; c.data = append(c.data, z) }
func (c *typedColumn[T]) setAny(row int, v any) { c.data[row] = v.(T) }
func (c *typedColumn[T]) getAny(row int) any    { return c.data[row] }
func (c *typedColumn[T]) moveTo(dst column, row int) {
	dst.(*typedColumn[T]).data = append(dst.(*typedColumn[T]).data, c.data[row])
}
func (c *typedColumn[T]) swapRemove(row int) {
	last := len(c.data) - 1
	c.data[row] = c.data[last]
	var z T
	c.data[last] = z
	c.data = c.data[:last]
}
func (c *typedColumn[T]) len() int { return len(c.data) }

// archetype is a table of entities sharing one component set.
type archetype struct {
	mask     mask
	ids      []ComponentID
	columns  []column
	index    []int16 // component id to column, -1 when absent; len maxComponents
	entities []Entity
	addEdge  map[ComponentID]*archetype
	remEdge  map[ComponentID]*archetype
}

func (a *archetype) column(id ComponentID) int { return int(a.index[id]) }

type entityMeta struct {
	gen   uint32
	alive bool
	arch  *archetype
	row   int32
}

type compInfo struct {
	typ       reflect.Type
	newColumn func() column
	typed     bool // columns are typedColumn[T] rather than reflect-backed
}

// World holds entities, their components, systems, resources and events.
type World struct {
	meta  []entityMeta
	free  []uint32
	count int

	comps   []compInfo
	compIDs map[reflect.Type]ComponentID

	archs     []*archetype
	archByKey map[mask]*archetype
	empty     *archetype
	version   uint64 // bumps when an archetype is created; queries re-match

	resources map[reflect.Type]any
	events    map[reflect.Type]eventQueue
	systems   []system
	stats     []SystemStat

	// oneOff memoises the queries Each, Each2, Each3, Each4 and Count
	// build, one per component set in the order the call named it.
	oneOff map[queryKey]any

	updates uint64 // bumps at the start of every Update
	wmat    worldMatrices
}

type system struct {
	name string
	fn   func(w *World, dt float64)
}

// NewWorld makes an empty world.
func NewWorld() *World {
	w := &World{compIDs: map[reflect.Type]ComponentID{}, archByKey: map[mask]*archetype{},
		resources: map[reflect.Type]any{}, events: map[reflect.Type]eventQueue{},
		oneOff: map[queryKey]any{}}
	w.empty = w.archetypeFor(mask{})
	return w
}

// Count is the number of live entities.
func (w *World) Count() int { return w.count }

// Alive reports whether the entity still exists.
func (w *World) Alive(e Entity) bool {
	i := int(e.id) - 1
	return i >= 0 && i < len(w.meta) && w.meta[i].alive && w.meta[i].gen == e.gen
}

func (w *World) archetypeFor(m mask) *archetype {
	if a, ok := w.archByKey[m]; ok {
		return a
	}
	a := &archetype{mask: m, index: make([]int16, maxComponents), addEdge: map[ComponentID]*archetype{}, remEdge: map[ComponentID]*archetype{}}
	for i := range a.index {
		a.index[i] = -1
	}
	for id := range w.comps {
		if m.has(ComponentID(id)) {
			a.index[id] = int16(len(a.columns))
			a.ids = append(a.ids, ComponentID(id))
			a.columns = append(a.columns, w.comps[id].newColumn())
		}
	}
	w.archs = append(w.archs, a)
	w.archByKey[m] = a
	w.version++
	return a
}

// componentID registers T on first use, and upgrades a type first seen
// through SpawnWith (reflect-backed columns) to typed columns.
func componentID[T any](w *World) ComponentID {
	var z T
	t := reflect.TypeOf(z)
	if t == nil {
		panic("ecs: interface types cannot be components")
	}
	if id, ok := w.compIDs[t]; ok {
		if !w.comps[id].typed {
			upgrade[T](w, id)
		}
		return id
	}
	return w.register(t, func() column { return &typedColumn[T]{} }, true)
}

func (w *World) register(t reflect.Type, newColumn func() column, typed bool) ComponentID {
	if len(w.comps) >= maxComponents {
		panic("ecs: too many component types")
	}
	id := ComponentID(len(w.comps))
	w.comps = append(w.comps, compInfo{typ: t, newColumn: newColumn, typed: typed})
	w.compIDs[t] = id
	return id
}

// idOfValue finds or registers the component type of a value, which
// must not be a pointer.
func (w *World) idOfValue(v any) ComponentID {
	t := reflect.TypeOf(v)
	if t == nil || t.Kind() == reflect.Pointer {
		panic(fmt.Sprintf("ecs: component must be a value, got %T", v))
	}
	if id, ok := w.compIDs[t]; ok {
		return id
	}
	return w.register(t, func() column { return newReflectColumn(t) }, false)
}

// newReflectColumn builds a typed column for a type first seen through
// an any value, without generics available.
func newReflectColumn(t reflect.Type) column {
	return &anyColumn{typ: t, data: reflect.MakeSlice(reflect.SliceOf(t), 0, 0)}
}

// anyColumn stores values of a type known only at run time. Get[T]
// reads it through reflection-free unsafe indexing when T matches.
type anyColumn struct {
	typ  reflect.Type
	data reflect.Value // []T
}

func (c *anyColumn) appendZero()           { c.data = reflect.Append(c.data, reflect.Zero(c.typ)) }
func (c *anyColumn) setAny(row int, v any) { c.data.Index(row).Set(reflect.ValueOf(v)) }
func (c *anyColumn) getAny(row int) any    { return c.data.Index(row).Interface() }
func (c *anyColumn) moveTo(dst column, row int) {
	d := dst.(*anyColumn)
	d.data = reflect.Append(d.data, c.data.Index(row))
}
func (c *anyColumn) swapRemove(row int) {
	last := c.data.Len() - 1
	c.data.Index(row).Set(c.data.Index(last))
	c.data.Index(last).Set(reflect.Zero(c.typ))
	c.data = c.data.Slice(0, last)
}
func (c *anyColumn) len() int { return c.data.Len() }

// Spawn creates an entity with no components.
func (w *World) Spawn() Entity {
	e := w.allocate()
	w.place(e, w.empty)
	return e
}

// SpawnWith creates an entity carrying the given component values.
func (w *World) SpawnWith(comps ...any) Entity {
	var m mask
	for _, c := range comps {
		m.set(w.idOfValue(c))
	}
	e := w.allocate()
	a := w.archetypeFor(m)
	w.meta[e.id-1].arch, w.meta[e.id-1].row = a, int32(len(a.entities))
	a.entities = append(a.entities, e)
	for i := range a.columns {
		a.columns[i].appendZero()
	}
	row := len(a.entities) - 1
	for _, c := range comps {
		a.columns[a.column(w.idOfValue(c))].setAny(row, c)
	}
	return e
}

func (w *World) allocate() Entity {
	var slot uint32
	if n := len(w.free); n > 0 {
		slot = w.free[n-1]
		w.free = w.free[:n-1]
	} else {
		slot = uint32(len(w.meta))
		w.meta = append(w.meta, entityMeta{})
	}
	m := &w.meta[slot]
	m.alive = true
	w.count++
	w.wmat.valid = false // a new entity has no cached matrix
	return Entity{id: slot + 1, gen: m.gen}
}

func (w *World) place(e Entity, a *archetype) {
	m := &w.meta[e.id-1]
	m.arch, m.row = a, int32(len(a.entities))
	a.entities = append(a.entities, e)
	for _, c := range a.columns {
		c.appendZero()
	}
}

// remove takes the entity out of its archetype, swapping the last row in.
func (w *World) remove(e Entity) {
	m := &w.meta[e.id-1]
	a, row := m.arch, int(m.row)
	last := len(a.entities) - 1
	moved := a.entities[last]
	a.entities[row] = moved
	a.entities = a.entities[:last]
	for _, c := range a.columns {
		c.swapRemove(row)
	}
	if moved != e {
		w.meta[moved.id-1].row = int32(row)
	}
}

// move transfers the entity to archetype dst, keeping shared components.
func (w *World) move(e Entity, dst *archetype) {
	m := &w.meta[e.id-1]
	src, row := m.arch, int(m.row)
	for i, id := range src.ids {
		if j := dst.column(id); j >= 0 {
			src.columns[i].moveTo(dst.columns[j], row)
		}
	}
	for i, id := range dst.ids {
		if src.column(id) < 0 {
			dst.columns[i].appendZero()
		}
	}
	dst.entities = append(dst.entities, e)
	w.remove(e)
	m.arch, m.row = dst, int32(len(dst.entities)-1)
}

// Despawn removes an entity, its components and its children.
func (w *World) Despawn(e Entity) {
	if !w.Alive(e) {
		return
	}
	if ch, ok := Get[Children](w, e); ok {
		kids := append([]Entity(nil), ch.List...)
		for _, c := range kids {
			w.Despawn(c)
		}
	}
	if p, ok := Get[Parent](w, e); ok && w.Alive(p.Entity) {
		detach(w, p.Entity, e)
	}
	w.remove(e)
	m := &w.meta[e.id-1]
	m.alive = false
	m.gen++
	m.arch = nil
	w.free = append(w.free, e.id-1)
	w.count--
	w.wmat.valid = false
}

// Add attaches a component, or replaces it when already present.
func Add[T any](w *World, e Entity, v T) {
	if !w.Alive(e) {
		return
	}
	id := componentID[T](w)
	m := &w.meta[e.id-1]
	a := m.arch
	if col := a.column(id); col >= 0 {
		a.columns[col].(*typedColumn[T]).data[m.row] = v
		return
	}
	dst, ok := a.addEdge[id]
	if !ok {
		nm := a.mask
		nm.set(id)
		dst = w.archetypeFor(nm)
		a.addEdge[id] = dst
	}
	w.move(e, dst)
	dst.columns[dst.column(id)].(*typedColumn[T]).data[m.row] = v
}

// Remove detaches a component; nothing happens if the entity has none.
func Remove[T any](w *World, e Entity) {
	if !w.Alive(e) {
		return
	}
	id := componentID[T](w)
	a := w.meta[e.id-1].arch
	if a.column(id) < 0 {
		return
	}
	dst, ok := a.remEdge[id]
	if !ok {
		nm := a.mask
		nm.clear(id)
		dst = w.archetypeFor(nm)
		a.remEdge[id] = dst
	}
	w.move(e, dst)
}

// Get returns a pointer to the entity's component. The pointer is valid
// until the next structural change (Add, Remove, Despawn, Spawn) that
// touches its table, so read or write it right away.
func Get[T any](w *World, e Entity) (*T, bool) {
	if !w.Alive(e) {
		return nil, false
	}
	id, ok := w.compIDs[typeOf[T]()]
	if !ok {
		return nil, false
	}
	if !w.comps[id].typed {
		upgrade[T](w, id)
	}
	m := &w.meta[e.id-1]
	col := m.arch.column(id)
	if col < 0 {
		return nil, false
	}
	return &m.arch.columns[col].(*typedColumn[T]).data[m.row], true
}

// Has reports whether the entity carries a T.
func Has[T any](w *World, e Entity) bool {
	_, ok := Get[T](w, e)
	return ok
}

// typeOf is T's reflect.Type. It goes through a nil pointer rather than
// a value of T, because putting a value in an interface copies it onto
// the heap, and a large resource looked up every frame would allocate
// its own size each time.
func typeOf[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

// upgrade replaces reflect-backed columns for T with typed ones so
// queries and Get run without reflection. It runs once per type, the
// first time a generic function meets a type first seen via SpawnWith.
func upgrade[T any](w *World, id ComponentID) {
	w.comps[id].newColumn = func() column { return &typedColumn[T]{} }
	w.comps[id].typed = true
	for _, a := range w.archs {
		col := a.column(id)
		if col < 0 {
			continue
		}
		if ac, ok := a.columns[col].(*anyColumn); ok {
			tc := &typedColumn[T]{data: make([]T, ac.len())}
			for i := range tc.data {
				tc.data[i] = ac.data.Index(i).Interface().(T)
			}
			a.columns[col] = tc
		}
	}
}

// Components lists the component types an entity carries.
func (w *World) Components(e Entity) []reflect.Type {
	if !w.Alive(e) {
		return nil
	}
	a := w.meta[e.id-1].arch
	out := make([]reflect.Type, 0, len(a.ids))
	for _, id := range a.ids {
		out = append(out, w.comps[id].typ)
	}
	return out
}

// ComponentValues returns a copy of each of the entity's components as
// an any value, in the same order as Components. Changing a returned
// value does not change the entity; Add writes it back.
func (w *World) ComponentValues(e Entity) []any {
	if !w.Alive(e) {
		return nil
	}
	m := &w.meta[e.id-1]
	out := make([]any, len(m.arch.columns))
	for i, c := range m.arch.columns {
		out[i] = c.getAny(int(m.row))
	}
	return out
}

// setComponents attaches several components given as any values with
// one table move, replacing any the entity already carries.
func (w *World) setComponents(e Entity, comps []any) {
	m := &w.meta[e.id-1]
	nm := m.arch.mask
	ids := make([]ComponentID, len(comps))
	for i, c := range comps {
		ids[i] = w.idOfValue(c)
		nm.set(ids[i])
	}
	if nm != m.arch.mask {
		w.move(e, w.archetypeFor(nm))
	}
	a := m.arch
	for i, c := range comps {
		a.columns[a.column(ids[i])].setAny(int(m.row), c)
	}
}

// Entities lists every live entity, in no particular order.
func (w *World) Entities() []Entity {
	out := make([]Entity, 0, w.count)
	for _, a := range w.archs {
		out = append(out, a.entities...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}
