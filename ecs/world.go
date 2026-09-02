package ecs

import (
	"time"

	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// SetResource stores a singleton value of type T on the world: the
// rules, the score, the input state, anything there is one of.
func SetResource[T any](w *World, v T) { w.resources[typeOf[T]()] = &v }

// Resource returns a pointer to the world's T, or nil when unset.
func Resource[T any](w *World) *T {
	if p, ok := w.resources[typeOf[T]()]; ok {
		return p.(*T)
	}
	return nil
}

// MustResource returns the world's T and panics when it is missing.
func MustResource[T any](w *World) *T {
	p := Resource[T](w)
	if p == nil {
		panic("ecs: missing resource " + typeOf[T]().String())
	}
	return p
}

// System is a step of the simulation run by World.Update.
type System func(w *World, dt float64)

// AddSystem registers a system; systems run in registration order.
func (w *World) AddSystem(name string, fn System) {
	w.systems = append(w.systems, system{name: name, fn: fn})
	w.stats = append(w.stats, SystemStat{Name: name})
}

// SystemStat is the last Update's timing for one system.
type SystemStat struct {
	Name string
	MS   float64
}

// Update runs every system in order with dt. Events emitted during the
// previous Update (or between updates, in Draw) are cleared first, so
// order systems with producers before consumers.
func (w *World) Update(dt float64) {
	for _, q := range w.events {
		q.clear()
	}
	for i, s := range w.systems {
		start := time.Now()
		s.fn(w, dt)
		w.stats[i].MS = float64(time.Since(start).Microseconds()) / 1000
	}
}

// Stats reports each system's time in the last Update.
func (w *World) Stats() []SystemStat { return w.stats }

// eventQueue holds one type's events for the current Update.
type eventQueue interface{ clear() }

type typedEvents[T any] struct {
	current []T
}

func (q *typedEvents[T]) clear() { q.current = q.current[:0] }

func eventsOf[T any](w *World) *typedEvents[T] {
	t := typeOf[T]()
	q, ok := w.events[t]
	if !ok {
		q = &typedEvents[T]{}
		w.events[t] = q
	}
	return q.(*typedEvents[T])
}

// Emit queues an event for systems to read with Events.
func Emit[T any](w *World, ev T) {
	q := eventsOf[T](w)
	q.current = append(q.current, ev)
}

// Events returns the events of type T emitted since the start of this
// Update. Draw sees what the last Update emitted.
func Events[T any](w *World) []T { return eventsOf[T](w).current }

// Parent links an entity under another; SetParent maintains it.
type Parent struct{ Entity Entity }

// Children lists an entity's direct children; SetParent maintains it.
type Children struct{ List []Entity }

// SetParent attaches child under parent, or detaches it with None. A
// child follows its parent's transform (see WorldMatrix) and is
// despawned with it. Cycles are refused.
func SetParent(w *World, child, parent Entity) {
	if !w.Alive(child) || child == parent {
		return
	}
	if p, ok := Get[Parent](w, child); ok && w.Alive(p.Entity) {
		detach(w, p.Entity, child)
	}
	if !w.Alive(parent) {
		Remove[Parent](w, child)
		return
	}
	for a := parent; a.Valid(); {
		if a == child {
			Remove[Parent](w, child)
			return
		}
		p, ok := Get[Parent](w, a)
		if !ok {
			break
		}
		a = p.Entity
	}
	Add(w, child, Parent{Entity: parent})
	ch, ok := Get[Children](w, parent)
	if !ok {
		Add(w, parent, Children{List: []Entity{child}})
		return
	}
	ch.List = append(ch.List, child)
}

func detach(w *World, parent, child Entity) {
	ch, ok := Get[Children](w, parent)
	if !ok {
		return
	}
	for i, c := range ch.List {
		if c == child {
			ch.List = append(ch.List[:i], ch.List[i+1:]...)
			return
		}
	}
}

// ParentOf returns the entity's parent, if it has one.
func ParentOf(w *World, e Entity) (Entity, bool) {
	if p, ok := Get[Parent](w, e); ok && w.Alive(p.Entity) {
		return p.Entity, true
	}
	return None, false
}

// ChildrenOf returns the entity's direct children; do not modify the slice.
func ChildrenOf(w *World, e Entity) []Entity {
	if ch, ok := Get[Children](w, e); ok {
		return ch.List
	}
	return nil
}

// WorldMatrix composes the gfx.Transform components from the root down
// to e; entities without one contribute identity.
func WorldMatrix(w *World, e Entity) lin.Mat4 {
	if !w.Alive(e) {
		return lin.Identity()
	}
	local := lin.Identity()
	if t, ok := Get[gfx.Transform](w, e); ok {
		local = t.Matrix()
	}
	if p, ok := ParentOf(w, e); ok {
		return WorldMatrix(w, p).Mul(local)
	}
	return local
}

// Commands record structural changes to apply later with Apply, for
// code running inside a query that must not change other entities'
// tables mid-iteration.
type Commands struct {
	ops []func(w *World)
}

// Spawn records creating an entity with components.
func (c *Commands) Spawn(comps ...any) {
	c.ops = append(c.ops, func(w *World) { w.SpawnWith(comps...) })
}

// Despawn records removing an entity.
func (c *Commands) Despawn(e Entity) {
	c.ops = append(c.ops, func(w *World) { w.Despawn(e) })
}

// Add records attaching component values to an entity.
func (c *Commands) Add(e Entity, comps ...any) {
	c.ops = append(c.ops, func(w *World) {
		if !w.Alive(e) {
			return
		}
		for _, comp := range comps {
			addAny(w, e, comp)
		}
	})
}

// RemoveCmd records detaching a T from an entity.
func RemoveCmd[T any](c *Commands, e Entity) {
	c.ops = append(c.ops, func(w *World) { Remove[T](w, e) })
}

// Apply runs the recorded changes in order and clears the buffer.
func (c *Commands) Apply(w *World) {
	for _, op := range c.ops {
		op(w)
	}
	c.ops = c.ops[:0]
}

// Len is the number of pending commands.
func (c *Commands) Len() int { return len(c.ops) }

// addAny attaches a component given as an any value.
func addAny(w *World, e Entity, v any) {
	id := w.idOfValue(v)
	m := &w.meta[e.id-1]
	a := m.arch
	if col := a.column(id); col >= 0 {
		a.columns[col].setAny(int(m.row), v)
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
	dst.columns[dst.column(id)].setAny(int(m.row), v)
}
