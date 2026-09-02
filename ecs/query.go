package ecs

// Filter narrows a query beyond the components it reads.
type Filter func(w *World, incl, excl *mask)

// With requires entities to carry T without reading it.
func With[T any]() Filter {
	return func(w *World, incl, _ *mask) { incl.set(componentID[T](w)) }
}

// Without excludes entities carrying T.
func Without[T any]() Filter {
	return func(w *World, _, excl *mask) { excl.set(componentID[T](w)) }
}

// matcher caches the archetypes a query touches until the world gains
// a new one.
type matcher struct {
	w       *World
	incl    mask
	excl    mask
	version uint64
	archs   []*archetype
}

func (m *matcher) refresh() {
	if m.version == m.w.version && m.archs != nil {
		return
	}
	m.archs = m.archs[:0]
	for _, a := range m.w.archs {
		if a.mask.contains(m.incl) && !a.mask.intersects(m.excl) {
			m.archs = append(m.archs, a)
		}
	}
	m.version = m.w.version
}

func (m *matcher) count() int {
	m.refresh()
	n := 0
	for _, a := range m.archs {
		n += len(a.entities)
	}
	return n
}

func newMatcher(w *World, ids []ComponentID, filters []Filter) matcher {
	m := matcher{w: w}
	for _, id := range ids {
		m.incl.set(id)
	}
	for _, f := range filters {
		f(w, &m.incl, &m.excl)
	}
	return m
}

// Rows are visited from the last to the first, so despawning or
// restructuring the entity being visited is safe: the row swapped into
// its place has already been seen. Entities that move into a table
// during the walk land past the row count captured at its start and are
// not visited. Changes to other entities should go through Commands.

// rows returns the starting row index for a table walk.
func rows(a *archetype) int { return len(a.entities) - 1 }

// Query1 iterates entities with an A.
type Query1[A any] struct {
	m matcher
	a ComponentID
}

// NewQuery1 makes a query; keep it and reuse it across frames.
func NewQuery1[A any](w *World, filters ...Filter) *Query1[A] {
	a := componentID[A](w)
	upgrade[A](w, a)
	return &Query1[A]{m: newMatcher(w, []ComponentID{a}, filters), a: a}
}

// Each calls fn for every matching entity.
func (q *Query1[A]) Each(fn func(e Entity, a *A)) {
	q.m.refresh()
	for _, arch := range q.m.archs {
		ca := arch.columns[arch.column(q.a)].(*typedColumn[A])
		for i := rows(arch); i >= 0 && i < len(arch.entities); i-- {
			fn(arch.entities[i], &ca.data[i])
		}
	}
}

// Count is the number of matching entities.
func (q *Query1[A]) Count() int { return q.m.count() }

// First returns the first matching entity, if any.
func (q *Query1[A]) First() (Entity, *A, bool) {
	q.m.refresh()
	for _, arch := range q.m.archs {
		if len(arch.entities) > 0 {
			ca := arch.columns[arch.column(q.a)].(*typedColumn[A])
			return arch.entities[0], &ca.data[0], true
		}
	}
	return None, nil, false
}

// Query2 iterates entities with an A and a B.
type Query2[A, B any] struct {
	m    matcher
	a, b ComponentID
}

// NewQuery2 makes a query; keep it and reuse it across frames.
func NewQuery2[A, B any](w *World, filters ...Filter) *Query2[A, B] {
	a, b := componentID[A](w), componentID[B](w)
	upgrade[A](w, a)
	upgrade[B](w, b)
	return &Query2[A, B]{m: newMatcher(w, []ComponentID{a, b}, filters), a: a, b: b}
}

// Each calls fn for every matching entity.
func (q *Query2[A, B]) Each(fn func(e Entity, a *A, b *B)) {
	q.m.refresh()
	for _, arch := range q.m.archs {
		ca := arch.columns[arch.column(q.a)].(*typedColumn[A])
		cb := arch.columns[arch.column(q.b)].(*typedColumn[B])
		for i := rows(arch); i >= 0 && i < len(arch.entities); i-- {
			fn(arch.entities[i], &ca.data[i], &cb.data[i])
		}
	}
}

// Count is the number of matching entities.
func (q *Query2[A, B]) Count() int { return q.m.count() }

// Query3 iterates entities with an A, a B and a C.
type Query3[A, B, C any] struct {
	m       matcher
	a, b, c ComponentID
}

// NewQuery3 makes a query; keep it and reuse it across frames.
func NewQuery3[A, B, C any](w *World, filters ...Filter) *Query3[A, B, C] {
	a, b, c := componentID[A](w), componentID[B](w), componentID[C](w)
	upgrade[A](w, a)
	upgrade[B](w, b)
	upgrade[C](w, c)
	return &Query3[A, B, C]{m: newMatcher(w, []ComponentID{a, b, c}, filters), a: a, b: b, c: c}
}

// Each calls fn for every matching entity.
func (q *Query3[A, B, C]) Each(fn func(e Entity, a *A, b *B, c *C)) {
	q.m.refresh()
	for _, arch := range q.m.archs {
		ca := arch.columns[arch.column(q.a)].(*typedColumn[A])
		cb := arch.columns[arch.column(q.b)].(*typedColumn[B])
		cc := arch.columns[arch.column(q.c)].(*typedColumn[C])
		for i := rows(arch); i >= 0 && i < len(arch.entities); i-- {
			fn(arch.entities[i], &ca.data[i], &cb.data[i], &cc.data[i])
		}
	}
}

// Count is the number of matching entities.
func (q *Query3[A, B, C]) Count() int { return q.m.count() }

// Query4 iterates entities with four components.
type Query4[A, B, C, D any] struct {
	m          matcher
	a, b, c, d ComponentID
}

// NewQuery4 makes a query; keep it and reuse it across frames.
func NewQuery4[A, B, C, D any](w *World, filters ...Filter) *Query4[A, B, C, D] {
	a, b, c, d := componentID[A](w), componentID[B](w), componentID[C](w), componentID[D](w)
	upgrade[A](w, a)
	upgrade[B](w, b)
	upgrade[C](w, c)
	upgrade[D](w, d)
	return &Query4[A, B, C, D]{m: newMatcher(w, []ComponentID{a, b, c, d}, filters), a: a, b: b, c: c, d: d}
}

// Each calls fn for every matching entity.
func (q *Query4[A, B, C, D]) Each(fn func(e Entity, a *A, b *B, c *C, d *D)) {
	q.m.refresh()
	for _, arch := range q.m.archs {
		ca := arch.columns[arch.column(q.a)].(*typedColumn[A])
		cb := arch.columns[arch.column(q.b)].(*typedColumn[B])
		cc := arch.columns[arch.column(q.c)].(*typedColumn[C])
		cd := arch.columns[arch.column(q.d)].(*typedColumn[D])
		for i := rows(arch); i >= 0 && i < len(arch.entities); i-- {
			fn(arch.entities[i], &ca.data[i], &cb.data[i], &cc.data[i], &cd.data[i])
		}
	}
}

// Count is the number of matching entities.
func (q *Query4[A, B, C, D]) Count() int { return q.m.count() }

// Each is a one-off iteration over entities with a T, for code that
// does not keep a query around.
func Each[T any](w *World, fn func(e Entity, t *T)) { NewQuery1[T](w).Each(fn) }

// Each2 is a one-off iteration over entities with an A and a B.
func Each2[A, B any](w *World, fn func(e Entity, a *A, b *B)) { NewQuery2[A, B](w).Each(fn) }

// Count returns how many entities carry a T.
func Count[T any](w *World) int { return NewQuery1[T](w).Count() }
