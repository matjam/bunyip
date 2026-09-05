package ecs

import (
	"sort"
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

// Resources lists the type names of the resources set on the world,
// sorted, for a debug view that shows what a world holds.
func (w *World) Resources() []string {
	out := make([]string, 0, len(w.resources))
	for t := range w.resources {
		out = append(out, t.String())
	}
	sort.Strings(out)
	return out
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
// order systems with producers before consumers. A system turned off
// with SetSystemEnabled is skipped and keeps its last timing.
func (w *World) Update(dt float64) {
	w.updates++
	for _, q := range w.events {
		q.clear()
	}
	for i, s := range w.systems {
		if s.off {
			continue
		}
		start := time.Now()
		s.fn(w, dt)
		w.stats[i].MS = float64(time.Since(start).Microseconds()) / 1000
	}
}

// Stats reports each system's most recent timing in milliseconds.
// The returned slice belongs to the world; do not modify it, and copy it
// to retain a snapshot across updates. Disabled systems keep their last timing.
func (w *World) Stats() []SystemStat { return w.stats }

// Updates counts the Update calls the world has run, so a debug view can
// tell whether the simulation advanced between two frames.
func (w *World) Updates() uint64 { return w.updates }

// SetSystemEnabled turns a system on or off by the name it was
// registered under, for a debugger that pauses part of the simulation. A
// system that is off is skipped by Update and keeps its place in the
// order; a name no system has is ignored. Systems start on.
func (w *World) SetSystemEnabled(name string, on bool) {
	for i := range w.systems {
		if w.systems[i].name == name {
			w.systems[i].off = !on
		}
	}
}

// SystemEnabled reports whether a system runs. A name no system has
// reads as off.
func (w *World) SystemEnabled(name string) bool {
	for i := range w.systems {
		if w.systems[i].name == name {
			return !w.systems[i].off
		}
	}
	return false
}

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
// Update. Draw sees what the last Update emitted. The slice belongs to
// the event queue; copy it to retain events across Emit or Update calls.
func Events[T any](w *World) []T { return eventsOf[T](w).current }

// Parent links an entity under another; SetParent maintains it.
type Parent struct{ Entity Entity }

// Children lists an entity's direct children; SetParent maintains it.
type Children struct{ List []Entity }

// SetParent attaches child under parent, or detaches it with None. A
// child follows its parent's transform (see WorldMatrix) and is
// despawned with it. A missing parent detaches the child. Self-parenting
// is ignored; an attempted longer cycle detaches the child from its old parent.
func SetParent(w *World, child, parent Entity) {
	if !w.Alive(child) || child == parent {
		return
	}
	w.wmat.valid = false
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

// worldMatrices caches one matrix per entity slot, filled by
// UpdateWorldMatrices and read by WorldMatrix while it is fresh.
type worldMatrices struct {
	valid bool
	stamp uint64 // the World.updates the pass ran under
	mats  []lin.Mat4
	gens  []uint32
	has   []bool
}

// UpdateWorldMatrices composes every entity's world matrix in one walk
// from the roots down and caches the results, so WorldMatrix costs a
// slice index instead of a climb up the parent chain. Call it from a
// system after the ones that move transforms and before the ones that
// read world positions. The cache lasts until the next World.Update, so
// Draw reads what the last Update left; spawning, changing a parent link or
// despawning drops it, and a transform written after the pass is not
// seen until the pass runs again.
func UpdateWorldMatrices(w *World) {
	c := &w.wmat
	n := len(w.meta)
	if len(c.mats) < n {
		c.mats = make([]lin.Mat4, n)
		c.gens = make([]uint32, n)
		c.has = make([]bool, n)
	} else {
		clear(c.has)
	}
	// The ids are resolved once so the walk indexes columns directly
	// instead of looking a type up per entity.
	tid, cid, pid := componentID[gfx.Transform](w), componentID[Children](w), componentID[Parent](w)
	for _, a := range w.archs {
		if a.mask.has(pid) {
			continue // reached from its parent instead
		}
		for _, e := range a.entities {
			walkMatrices(w, c, e, lin.Identity(), tid, cid)
		}
	}
	c.valid, c.stamp = true, w.updates
}

// walkMatrices writes e's world matrix and its subtree's.
func walkMatrices(w *World, c *worldMatrices, e Entity, parent lin.Mat4, tid, cid ComponentID) {
	meta := &w.meta[e.id-1]
	a := meta.arch
	local := lin.Identity()
	if col := a.column(tid); col >= 0 {
		local = a.columns[col].(*typedColumn[gfx.Transform]).data[meta.row].Matrix()
	}
	m := parent.Mul(local)
	i := int(e.id) - 1
	c.mats[i], c.gens[i], c.has[i] = m, e.gen, true
	col := a.column(cid)
	if col < 0 {
		return
	}
	list := a.columns[col].(*typedColumn[Children]).data[meta.row].List
	for _, k := range list {
		if w.Alive(k) {
			walkMatrices(w, c, k, m, tid, cid)
		}
	}
}

// WorldMatrix composes the gfx.Transform components from the root down
// to e; entities without one contribute identity. When
// UpdateWorldMatrices has run in this update the cached matrix is
// returned instead of walking the chain.
func WorldMatrix(w *World, e Entity) lin.Mat4 {
	if !w.Alive(e) {
		return lin.Identity()
	}
	if c := &w.wmat; c.valid && c.stamp == w.updates {
		if i := int(e.id) - 1; i < len(c.has) && c.has[i] && c.gens[i] == e.gen {
			return c.mats[i]
		}
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
// tables mid-iteration. The zero value is ready to use. Component values
// passed to Spawn and Add are retained until Apply, not deep-copied;
// do not mutate their referenced storage or argument slices before then.
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

// RemoveLater records detaching a T from an entity, the deferred form
// of Remove; methods cannot be generic, so it is a function on Commands.
func RemoveLater[T any](c *Commands, e Entity) {
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
