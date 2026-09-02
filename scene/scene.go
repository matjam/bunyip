// Package scene is a small entity store: entities are cheap handles,
// components are plain Go values attached by type, and a parent-child
// hierarchy composes gfx.Transform components into world matrices.
// Iteration is by component type, so a game loop can visit every
// entity that has, say, both a Position and a Health.
package scene

import (
	"reflect"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// Entity identifies a thing in a World. The zero Entity is "none". A
// handle from a despawned entity is never confused with a new one that
// reused its slot.
type Entity struct {
	id  uint32 // slot index plus one
	gen uint32
}

// Valid reports whether the handle refers to some entity (not None).
func (e Entity) Valid() bool { return e.id != 0 }

// None is the absent entity.
var None = Entity{}

// World holds entities and their components.
type World struct {
	gens     []uint32
	alive    []bool
	free     []uint32
	count    int
	stores   map[reflect.Type]store
	parent   []Entity
	children [][]Entity
}

// store is the type-erased view of a component store.
type store interface {
	remove(e Entity)
}

// NewWorld makes an empty world.
func NewWorld() *World {
	return &World{stores: map[reflect.Type]store{}}
}

// Spawn creates an entity with no components.
func (w *World) Spawn() Entity {
	var slot uint32
	if n := len(w.free); n > 0 {
		slot = w.free[n-1]
		w.free = w.free[:n-1]
	} else {
		slot = uint32(len(w.gens))
		w.gens = append(w.gens, 0)
		w.alive = append(w.alive, false)
		w.parent = append(w.parent, None)
		w.children = append(w.children, nil)
	}
	w.alive[slot] = true
	w.count++
	return Entity{id: slot + 1, gen: w.gens[slot]}
}

// Alive reports whether the entity still exists.
func (w *World) Alive(e Entity) bool {
	i := int(e.id) - 1
	return i >= 0 && i < len(w.gens) && w.alive[i] && w.gens[i] == e.gen
}

// Count is the number of live entities.
func (w *World) Count() int { return w.count }

// Despawn removes an entity, its components and its descendants.
func (w *World) Despawn(e Entity) {
	if !w.Alive(e) {
		return
	}
	i := e.id - 1
	for _, c := range w.children[i] {
		w.Despawn(c)
	}
	w.children[i] = nil
	if p := w.parent[i]; p.Valid() && w.Alive(p) {
		w.detach(p, e)
	}
	w.parent[i] = None
	for _, s := range w.stores {
		s.remove(e)
	}
	w.alive[i] = false
	w.gens[i]++
	w.free = append(w.free, i)
	w.count--
}

func (w *World) detach(parent, child Entity) {
	list := w.children[parent.id-1]
	for k, c := range list {
		if c == child {
			w.children[parent.id-1] = append(list[:k], list[k+1:]...)
			return
		}
	}
}

// SetParent attaches child under parent, or detaches it with None. A
// child follows its parent's transform and is despawned with it.
func (w *World) SetParent(child, parent Entity) {
	if !w.Alive(child) || child == parent {
		return
	}
	i := child.id - 1
	if old := w.parent[i]; old.Valid() && w.Alive(old) {
		w.detach(old, child)
	}
	if !w.Alive(parent) {
		w.parent[i] = None
		return
	}
	// Refuse cycles: an ancestor cannot become a descendant.
	for a := parent; a.Valid(); a, _ = w.Parent(a) {
		if a == child {
			w.parent[i] = None
			return
		}
	}
	w.parent[i] = parent
	w.children[parent.id-1] = append(w.children[parent.id-1], child)
}

// Parent returns the entity's parent, if it has one.
func (w *World) Parent(e Entity) (Entity, bool) {
	if !w.Alive(e) {
		return None, false
	}
	p := w.parent[e.id-1]
	return p, p.Valid()
}

// Children returns the entity's direct children; do not modify the slice.
func (w *World) Children(e Entity) []Entity {
	if !w.Alive(e) {
		return nil
	}
	return w.children[e.id-1]
}

// WorldMatrix composes the gfx.Transform components from the root down
// to e; entities without one contribute identity.
func (w *World) WorldMatrix(e Entity) lin.Mat4 {
	if !w.Alive(e) {
		return lin.Identity()
	}
	local := lin.Identity()
	if t, ok := Get[gfx.Transform](w, e); ok {
		local = t.Matrix()
	}
	if p, ok := w.Parent(e); ok {
		return w.WorldMatrix(p).Mul(local)
	}
	return local
}

// typedStore keeps components of one type densely packed.
type typedStore[T any] struct {
	dense    []T
	entities []Entity
	sparse   []int32 // slot index to dense index, -1 when absent
}

func (s *typedStore[T]) index(e Entity) int {
	i := int(e.id) - 1
	if i < 0 || i >= len(s.sparse) || s.sparse[i] < 0 {
		return -1
	}
	d := int(s.sparse[i])
	if s.entities[d] != e {
		return -1 // a stale handle from a reused slot
	}
	return d
}

func (s *typedStore[T]) remove(e Entity) {
	d := s.index(e)
	if d < 0 {
		return
	}
	last := len(s.dense) - 1
	moved := s.entities[last]
	s.dense[d], s.entities[d] = s.dense[last], moved
	s.sparse[moved.id-1] = int32(d)
	s.sparse[e.id-1] = -1
	var zero T
	s.dense[last] = zero
	s.dense, s.entities = s.dense[:last], s.entities[:last]
}

func getStore[T any](w *World, create bool) *typedStore[T] {
	var zero T
	key := reflect.TypeOf(zero)
	if s, ok := w.stores[key]; ok {
		return s.(*typedStore[T])
	}
	if !create {
		return nil
	}
	s := &typedStore[T]{}
	w.stores[key] = s
	return s
}

// Set attaches or replaces a component on e.
func Set[T any](w *World, e Entity, c T) {
	if !w.Alive(e) {
		return
	}
	s := getStore[T](w, true)
	if d := s.index(e); d >= 0 {
		s.dense[d] = c
		return
	}
	i := int(e.id) - 1
	for len(s.sparse) <= i {
		s.sparse = append(s.sparse, -1)
	}
	s.sparse[i] = int32(len(s.dense))
	s.dense = append(s.dense, c)
	s.entities = append(s.entities, e)
}

// Get returns a pointer to e's component, valid until the next Set or
// Remove of that type, so callers can modify it in place.
func Get[T any](w *World, e Entity) (*T, bool) {
	s := getStore[T](w, false)
	if s == nil {
		return nil, false
	}
	d := s.index(e)
	if d < 0 {
		return nil, false
	}
	return &s.dense[d], true
}

// Has reports whether e carries a T.
func Has[T any](w *World, e Entity) bool {
	_, ok := Get[T](w, e)
	return ok
}

// Remove detaches a component; nothing happens if e has none.
func Remove[T any](w *World, e Entity) {
	if s := getStore[T](w, false); s != nil {
		s.remove(e)
	}
}

// Each visits every entity with a T. The callback may modify the
// component through the pointer, but must not Set or Remove components
// of that type while iterating.
func Each[T any](w *World, fn func(e Entity, c *T)) {
	s := getStore[T](w, false)
	if s == nil {
		return
	}
	for i := range s.dense {
		fn(s.entities[i], &s.dense[i])
	}
}

// Each2 visits every entity carrying both an A and a B.
func Each2[A, B any](w *World, fn func(e Entity, a *A, b *B)) {
	Each(w, func(e Entity, a *A) {
		if b, ok := Get[B](w, e); ok {
			fn(e, a, b)
		}
	})
}

// Count returns how many entities carry a T.
func Count[T any](w *World) int {
	if s := getStore[T](w, false); s != nil {
		return len(s.dense)
	}
	return 0
}
