package phys

import (
	"math"
	"slices"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// index3 keeps every 3D collider placed in the world between steps and
// queries: its rotation, the position of its shape and its bounds, in
// the order the collider query walks them. A walk compares each
// collider's transform and shape with what its placement was computed
// from and places again only what changed, so a level of still colliders
// is placed once rather than once per substep and once per query.
//
// The step sweeps the moving colliders against each other and against
// the still ones, which it keeps in a list of their own sorted along the
// sweep axis and rebuilds only when a still collider changes. The
// queries search a tree of the colliders' bounds and test what it finds
// in walk order, which is the order the collider query visits, so every
// query finds the same hits in the same order as a walk over every
// collider would.
type index3 struct {
	rows []entry3
	meta []rowMeta3
	// rowOf maps an entity's slot to its row; a row whose entity does not
	// match is stale.
	rowOf []int32
	// keyX and endX are each row's bounds on the sweep axis, by row.
	keyX, endX []float32
	tree       aabbTree
	// dirty lists the rows whose tree leaf is behind their placement.
	dirty []int32
	// changed is set when any row was placed again, added or removed, so
	// the step rebuilds its list of still colliders.
	changed bool
	// The step's lists: the still colliders sorted along the sweep axis,
	// the triggers without a body, and the moving colliders, sorted each
	// substep. unsorted is the moving set in the order it was gathered,
	// to tell whether it changed since the last sort.
	statics  axisList
	triggers []int32
	moving   axisList
	unsorted []int32
	mark     []uint32
	stamp    uint32
	cands    []int32
	// walkPosition is the row the walk in progress reaches next.
	walkPosition int
}

// rowMeta3 is what a row's placement was computed from and where it
// sits in the query tree. The walk reads only this, which is kept small.
type rowMeta3 struct {
	key    placeKey3
	leaf   int32
	queued bool
}

// placeKey3 is the collider a row was placed from: the entity and the
// components it had, and the parts of them the placement depends on.
// A sphere, box or capsule is held as its kind and sizes; any other
// shape as an owned copy of the parts that can change in place.
type placeKey3 struct {
	e       ecs.Entity
	t       *gfx.Transform
	c       *Collider3
	pos     lin.Vec3
	rot     lin.Quat
	off     lin.Vec3
	size    lin.Vec3
	kind    uint8
	trigger bool
	shape   Shape3
}

// The kinds of shape a placement key holds by its sizes.
const (
	keyNone = iota
	keySphere
	keyBox
	keyCapsule
	keyOther
)

// placement3 places a collider in the world: the rotation, the position
// of the shape's origin and the bounds. Everything that places a
// collider calls it, so the step, the queries and the shape cache agree
// to the bit. It is kept out of line so that every caller runs the same
// instructions.
//
//go:noinline
func placement3(t *gfx.Transform, c *Collider3) (pos lin.Vec3, rot mat3, lo, hi lin.Vec3) {
	rot = mat3FromQuat(t.Rotation)
	pos = t.Position.Add(rot.mulVec(c.Offset))
	lo, hi = c.Shape.bounds(pos, rot)
	return pos, rot, lo, hi
}

func sameBits(a, b float32) bool { return math.Float32bits(a) == math.Float32bits(b) }

func sameBits2(a, b lin.Vec2) bool { return sameBits(a.X, b.X) && sameBits(a.Y, b.Y) }

func sameBits3(a, b lin.Vec3) bool {
	return sameBits(a.X, b.X) && sameBits(a.Y, b.Y) && sameBits(a.Z, b.Z)
}

func sameBitsQ(a, b lin.Quat) bool {
	return sameBits(a.X, b.X) && sameBits(a.Y, b.Y) && sameBits(a.Z, b.Z) && sameBits(a.W, b.W)
}

func keyOf3(e ecs.Entity, t *gfx.Transform, c *Collider3) placeKey3 {
	k := placeKey3{e: e, t: t, c: c, pos: t.Position, rot: t.Rotation, off: c.Offset, trigger: c.Trigger}
	switch s := c.Shape.(type) {
	case nil:
		k.kind = keyNone
	case Sphere:
		k.kind, k.size = keySphere, lin.V3(s.Radius, 0, 0)
	case Box3:
		k.kind, k.size = keyBox, s.Half
	case Capsule:
		k.kind, k.size = keyCapsule, lin.V3(s.Radius, s.HalfHeight, 0)
	default:
		k.kind, k.shape = keyOther, snapshotPlaced3(s)
	}
	return k
}

// same reports whether a collider is still the one the key was made
// from, with the same placement. The numbers are compared bit for bit,
// so a sign of zero or a NaN is noticed the way arithmetic would notice
// it.
func (k *placeKey3) same(e ecs.Entity, t *gfx.Transform, c *Collider3) bool {
	if k.e != e || k.t != t || k.c != c || c.Trigger != k.trigger ||
		!sameBits3(t.Position, k.pos) || !sameBitsQ(t.Rotation, k.rot) || !sameBits3(c.Offset, k.off) {
		return false
	}
	switch k.kind {
	case keyNone:
		return c.Shape == nil
	case keySphere:
		s, ok := c.Shape.(Sphere)
		return ok && sameBits(s.Radius, k.size.X)
	case keyBox:
		s, ok := c.Shape.(Box3)
		return ok && sameBits3(s.Half, k.size)
	case keyCapsule:
		s, ok := c.Shape.(Capsule)
		return ok && sameBits(s.Radius, k.size.X) && sameBits(s.HalfHeight, k.size.Y)
	}
	return samePlaced3(c.Shape, k.shape)
}

// samePlaced3 compares a shape with a snapshot for placement: hulls and
// compounds by value, meshes by the identity of their triangles, since
// a mesh's bounds come from its triangle tree.
func samePlaced3(a, b Shape3) bool {
	switch a := a.(type) {
	case MeshShape:
		b, ok := b.(MeshShape)
		return ok && sameMesh(a, b)
	case Compound3:
		b, ok := b.(Compound3)
		if !ok || len(a.Parts) != len(b.Parts) {
			return false
		}
		for i, p := range a.Parts {
			q := b.Parts[i]
			if p.Offset != q.Offset || p.Rotation != q.Rotation || !samePlaced3(p.Shape, q.Shape) {
				return false
			}
		}
		return true
	}
	return sameGeometry3(a, b)
}

// sameMesh reports whether two meshes share their triangles.
func sameMesh(a, b MeshShape) bool {
	if a.tree != b.tree || len(a.Vertices) != len(b.Vertices) || len(a.Indices) != len(b.Indices) {
		return false
	}
	if len(a.Vertices) > 0 && &a.Vertices[0] != &b.Vertices[0] {
		return false
	}
	return len(a.Indices) == 0 || &a.Indices[0] == &b.Indices[0]
}

// snapshotPlaced3 owns the parts of a shape that can change in place;
// a mesh is kept by identity.
func snapshotPlaced3(s Shape3) Shape3 {
	if c, ok := s.(Compound3); ok {
		parts := slices.Clone(c.Parts)
		for i := range parts {
			parts[i].Shape = snapshotPlaced3(parts[i].Shape)
		}
		return Compound3{Parts: parts}
	}
	if m, ok := s.(MeshShape); ok {
		return m
	}
	return snapshotGeometry3(s)
}

// row returns the row of an entity's collider.
func (x *index3) row(e ecs.Entity) (int, bool) {
	i := slot(e)
	if i < len(x.rowOf) {
		if k := int(x.rowOf[i]); k >= 0 && k < len(x.rows) && x.rows[k].e == e {
			return k, true
		}
	}
	return 0, false
}

// refresh walks the colliders and brings every row up to date, placing
// again the colliders that moved or changed and the rows whose entity
// changed.
func (x *index3) refresh(q *ecs.Query2[gfx.Transform, Collider3]) {
	x.walkPosition = 0
	q.Each(x.visit)
	if k := x.walkPosition; k < len(x.rows) {
		for i := k; i < len(x.rows); i++ {
			if leaf := x.meta[i].leaf; leaf != nullNode {
				x.tree.remove(leaf)
			}
		}
		x.rows, x.meta = x.rows[:k], x.meta[:k]
		x.keyX, x.endX = x.keyX[:k], x.endX[:k]
		x.changed = true
	}
}

// visit is one step of the walk refresh makes. It is a method value
// rather than a closure so the walk allocates nothing.
func (x *index3) visit(e ecs.Entity, t *gfx.Transform, c *Collider3) {
	k := x.walkPosition
	x.walkPosition++
	if k < len(x.meta) {
		if x.meta[k].key.same(e, t, c) {
			return
		}
	} else {
		x.rows = append(x.rows, entry3{})
		x.meta = append(x.meta, rowMeta3{leaf: nullNode})
		x.keyX = append(x.keyX, 0)
		x.endX = append(x.endX, 0)
	}
	x.set(k, e, t, c)
}

// set fills row k from a collider.
func (x *index3) set(k int, e ecs.Entity, t *gfx.Transform, c *Collider3) {
	r := &x.rows[k]
	*r = entry3{e: e, t: t, c: c, bi: -1}
	i := slot(e)
	for len(x.rowOf) <= i {
		x.rowOf = append(x.rowOf, -1)
	}
	x.rowOf[i] = int32(k)
	x.meta[k].key = keyOf3(e, t, c)
	x.place(k)
	x.changed = true
}

// place computes row k's placement from its transform and collider.
func (x *index3) place(k int) {
	r := &x.rows[k]
	r.shaped = r.c.Shape != nil
	if r.shaped {
		r.pos, r.rot, r.lo, r.hi = placement3(r.t, r.c)
		x.keyX[k], x.endX[k] = r.lo.X, r.hi.X
	}
	if m := &x.meta[k]; !m.queued {
		m.queued = true
		x.dirty = append(x.dirty, int32(k))
	}
}

// replace places a row again when its transform has moved since it was
// last placed; the step calls it for the moving bodies every substep.
func (x *index3) replace(k int) {
	r, m := &x.rows[k], &x.meta[k]
	if sameBits3(r.t.Position, m.key.pos) && sameBitsQ(r.t.Rotation, m.key.rot) {
		return
	}
	m.key.pos, m.key.rot = r.t.Position, r.t.Rotation
	x.place(k)
}

// sync brings the query tree up to date with the rows' placements.
func (x *index3) sync() {
	for _, k := range x.dirty {
		if int(k) >= len(x.rows) {
			continue
		}
		m, r := &x.meta[k], &x.rows[k]
		m.queued = false
		switch {
		case r.shaped && m.leaf == nullNode:
			m.leaf = x.tree.insert(k, r.lo, r.hi)
		case r.shaped:
			x.tree.move(m.leaf, r.lo, r.hi)
		case m.leaf != nullNode:
			x.tree.remove(m.leaf)
			m.leaf = nullNode
		}
	}
	x.dirty = x.dirty[:0]
}

// update prepares the index for queries: every row current and the tree
// in step with them.
func (x *index3) update(q *ecs.Query2[gfx.Transform, Collider3]) {
	x.refresh(q)
	x.sync()
}

// candidates returns the rows whose tree leaves overlap a box, in walk
// order.
func (x *index3) candidates(lo, hi lin.Vec3) []int32 {
	x.cands = x.tree.overlap(x.cands[:0], lo, hi)
	slices.Sort(x.cands)
	return x.cands
}

// rayCandidates returns the rows whose tree leaves the ray passes
// through, in walk order.
func (x *index3) rayCandidates(r Ray3) []int32 {
	x.cands = x.tree.ray(x.cands[:0], r.Origin, r.Dir)
	slices.Sort(x.cands)
	return x.cands
}

// gather sorts out the still colliders and the triggers without a body
// after rows have changed. A row is still when it has a shape, no body
// and is not a trigger; the body links must be set before it runs.
func (x *index3) gather() {
	if !x.changed {
		return
	}
	x.changed = false
	x.nextStamp()
	kept := x.statics.order[:0]
	for _, k := range x.statics.order {
		if int(k) < len(x.rows) && x.still(int(k)) && x.mark[k] != x.stamp {
			x.mark[k] = x.stamp
			kept = append(kept, k)
		}
	}
	x.triggers = x.triggers[:0]
	for k := range x.rows {
		r := &x.rows[k]
		if !r.shaped || r.b != nil {
			continue
		}
		if r.c.Trigger {
			x.triggers = append(x.triggers, int32(k))
		} else if x.mark[k] != x.stamp {
			kept = append(kept, int32(k))
		}
	}
	x.statics.order = kept
	x.statics.sort(x.keyX)
	x.statics.measure(x.keyX, x.endX)
}

func (x *index3) still(k int) bool {
	r := &x.rows[k]
	return r.shaped && r.b == nil && !r.c.Trigger
}

// nextStamp starts a fresh mark over the rows.
func (x *index3) nextStamp() {
	for len(x.mark) < len(x.rows) {
		x.mark = append(x.mark, 0)
	}
	x.stamp++
	if x.stamp == 0 {
		clear(x.mark)
		x.stamp = 1
	}
}

// sortMoving puts the moving set in sweep order. The set is usually the
// one sorted last substep, whose order is kept and only touched up.
func (x *index3) sortMoving(set []int32) {
	if !slices.Equal(set, x.unsorted) {
		x.unsorted = append(x.unsorted[:0], set...)
		x.moving.order = append(x.moving.order[:0], set...)
	}
	x.moving.sort(x.keyX)
	x.moving.spanned = false
}
