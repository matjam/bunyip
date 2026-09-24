package phys

import (
	"slices"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// index2 is index3 for the 2D colliders: every collider placed in the
// world between steps and queries, walked once per update by the step
// and once per query, with a sorted list of the still colliders for the
// step and a tree of bounds, flat at z zero, for the queries.
type index2 struct {
	rows       []entry2
	meta       []rowMeta2
	rowOf      []int32
	keyX, endX []float32
	tree       aabbTree
	dirty      []int32
	changed    bool
	statics    axisList
	triggers   []int32
	moving     axisList
	unsorted   []int32
	mark       []uint32
	stamp      uint32
	cands      []int32
	// walkPosition is the row the walk in progress reaches next.
	walkPosition int
}

type rowMeta2 struct {
	key    placeKey2
	leaf   int32
	queued bool
}

// placeKey2 is the part of a 2D collider's transform and component that
// its placement depends on, with an owned copy of a polygon's or a
// chain's points.
type placeKey2 struct {
	pos     lin.Vec2
	rot     float32
	off     lin.Vec2
	shape   Shape2
	simple  bool // shape is nil or a comparable value, compared with ==
	trigger bool
}

// placement2 places a 2D collider: the position of the shape's origin
// and its bounds. Like placement3 it is the one place that does this and
// is kept out of line so every caller runs the same instructions.
//
//go:noinline
func placement2(t *gfx.Transform2, c *Collider2) (pos, lo, hi lin.Vec2) {
	cs, sn := cosSin(t.Rotation)
	pos = t.Position.Add(rotate2(c.Offset, cs, sn))
	lo, hi = c.Shape.bounds(pos, t.Rotation)
	return pos, lo, hi
}

func simpleShape2(s Shape2) bool {
	switch s.(type) {
	case nil, Circle, Box2, Capsule2, Edge2:
		return true
	}
	return false
}

func keyOf2(t *gfx.Transform2, c *Collider2) placeKey2 {
	k := placeKey2{pos: t.Position, rot: t.Rotation, off: c.Offset, trigger: c.Trigger, simple: simpleShape2(c.Shape)}
	switch s := c.Shape.(type) {
	case Polygon2:
		k.shape = Polygon2{Points: slices.Clone(s.Points)}
	case Chain2:
		k.shape = Chain2{Points: slices.Clone(s.Points), Loop: s.Loop}
	default:
		k.shape = c.Shape
	}
	return k
}

func (k *placeKey2) same(t *gfx.Transform2, c *Collider2) bool {
	if !sameBits2(t.Position, k.pos) || !sameBits(t.Rotation, k.rot) || !sameBits2(c.Offset, k.off) || c.Trigger != k.trigger {
		return false
	}
	if k.simple {
		return c.Shape == k.shape
	}
	switch a := c.Shape.(type) {
	case Polygon2:
		b, ok := k.shape.(Polygon2)
		return ok && slices.Equal(a.Points, b.Points)
	case Chain2:
		b, ok := k.shape.(Chain2)
		return ok && a.Loop == b.Loop && slices.Equal(a.Points, b.Points)
	}
	return false
}

func (x *index2) row(e ecs.Entity) (int, bool) {
	i := slot(e)
	if i < len(x.rowOf) {
		if k := int(x.rowOf[i]); k >= 0 && k < len(x.rows) && x.rows[k].e == e {
			return k, true
		}
	}
	return 0, false
}

func (x *index2) refresh(q *ecs.Query2[gfx.Transform2, Collider2]) {
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

func (x *index2) visit(e ecs.Entity, t *gfx.Transform2, c *Collider2) {
	k := x.walkPosition
	x.walkPosition++
	if k < len(x.rows) {
		r := &x.rows[k]
		if r.e == e && r.t == t && r.c == c && x.meta[k].key.same(t, c) {
			return
		}
	} else {
		x.rows = append(x.rows, entry2{})
		x.meta = append(x.meta, rowMeta2{leaf: nullNode})
		x.keyX = append(x.keyX, 0)
		x.endX = append(x.endX, 0)
	}
	x.set(k, e, t, c)
}

func (x *index2) set(k int, e ecs.Entity, t *gfx.Transform2, c *Collider2) {
	r := &x.rows[k]
	*r = entry2{e: e, t: t, c: c, bi: -1}
	i := slot(e)
	for len(x.rowOf) <= i {
		x.rowOf = append(x.rowOf, -1)
	}
	x.rowOf[i] = int32(k)
	x.meta[k].key = keyOf2(t, c)
	x.place(k)
	x.changed = true
}

func (x *index2) place(k int) {
	r := &x.rows[k]
	r.shaped = r.c.Shape != nil
	if r.shaped {
		r.pos, r.lo, r.hi = placement2(r.t, r.c)
		x.keyX[k], x.endX[k] = r.lo.X, r.hi.X
	}
	if m := &x.meta[k]; !m.queued {
		m.queued = true
		x.dirty = append(x.dirty, int32(k))
	}
}

func (x *index2) replace(k int) {
	r, m := &x.rows[k], &x.meta[k]
	if sameBits2(r.t.Position, m.key.pos) && sameBits(r.t.Rotation, m.key.rot) {
		return
	}
	m.key.pos, m.key.rot = r.t.Position, r.t.Rotation
	x.place(k)
}

func flat(v lin.Vec2) lin.Vec3 { return lin.V3(v.X, v.Y, 0) }

func (x *index2) sync() {
	for _, k := range x.dirty {
		if int(k) >= len(x.rows) {
			continue
		}
		m, r := &x.meta[k], &x.rows[k]
		m.queued = false
		switch {
		case r.shaped && m.leaf == nullNode:
			m.leaf = x.tree.insert(k, flat(r.lo), flat(r.hi))
		case r.shaped:
			x.tree.move(m.leaf, flat(r.lo), flat(r.hi))
		case m.leaf != nullNode:
			x.tree.remove(m.leaf)
			m.leaf = nullNode
		}
	}
	x.dirty = x.dirty[:0]
}

func (x *index2) update(q *ecs.Query2[gfx.Transform2, Collider2]) {
	x.refresh(q)
	x.sync()
}

func (x *index2) candidates(lo, hi lin.Vec2) []int32 {
	x.cands = x.tree.overlap(x.cands[:0], flat(lo), flat(hi))
	slices.Sort(x.cands)
	return x.cands
}

func (x *index2) rayCandidates(r Ray2) []int32 {
	x.cands = x.tree.ray(x.cands[:0], flat(r.Origin), flat(r.Dir))
	slices.Sort(x.cands)
	return x.cands
}

func (x *index2) gather() {
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

func (x *index2) still(k int) bool {
	r := &x.rows[k]
	return r.shaped && r.b == nil && !r.c.Trigger
}

func (x *index2) nextStamp() {
	for len(x.mark) < len(x.rows) {
		x.mark = append(x.mark, 0)
	}
	x.stamp++
	if x.stamp == 0 {
		clear(x.mark)
		x.stamp = 1
	}
}

func (x *index2) sortMoving(set []int32) {
	if !slices.Equal(set, x.unsorted) {
		x.unsorted = append(x.unsorted[:0], set...)
		x.moving.order = append(x.moving.order[:0], set...)
	}
	x.moving.sort(x.keyX)
	x.moving.spanned = false
}
