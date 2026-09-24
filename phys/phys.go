// Package phys simulates rigid bodies in 2D and 3D on the entity
// component system. Shapes attached to entities collide, bounce, slide
// and stack under gravity, and the game reads what touched what.
//
// A 2D entity carries a gfx.Transform2, a Body2 and a Collider2; a 3D
// entity a gfx.Transform, a Body3 and a Collider3. Register System2 or
// System3 (or both) on the world and set a Settings2 or Settings3
// resource for gravity and solver quality. Each update the system
// integrates velocities, finds overlapping shapes with a sweep over
// bounding boxes, generates contact points, resolves them with
// sequential impulses (restitution, friction, positional correction)
// and emits Collision and Trigger events.
// Times are seconds, distances use the game's world units, and angles
// are radians unless a field says otherwise. Transform scale does not
// resize colliders; size the shapes themselves. A zero gravity vector
// means no gravity. World systems and queries require serialized access.
//
//	w.SetResource(phys.Settings3{Gravity: lin.V3(0, -9.8, 0)})
//	w.SpawnWith(gfx.At(0, 5, 0), phys.Dynamic3(1), phys.Collider3{Shape: phys.Sphere{Radius: 0.5}})
//	w.SpawnWith(gfx.Transform{}, phys.Collider3{Shape: phys.Box3{Half: lin.V3(10, 0.5, 10)}}) // static floor
//	w.AddSystem("physics", phys.System3)
//
// Names carry their dimension as a suffix where a type exists in both
// (Body2 and Body3, Box2 and Box3, Collider2 and Collider3, the events,
// the systems and the settings); shapes that exist in one dimension only
// are plain words (Circle, Polygon2 for the 2D polygon, Sphere).
//
// Shapes in 3D are Sphere, Box3, Capsule, ConvexHull, Compound3 (parts
// placed on one body) and MeshShape (a static triangle mesh for terrain
// and levels, with a triangle tree built by NewMeshShape). Sphere and
// box pairs have exact tests; every other pair collides through support
// functions (GJK for distance, EPA for penetration) with face manifolds
// clipped the same way as the box test's. Shapes in 2D are Circle, Box2,
// Polygon2, Capsule2, and for terrain Edge2 and Chain2.
//
// Queries inspect the world between updates. Raycast2
// and Raycast3 return the nearest collider along a ray and RaycastAll2
// and RaycastAll3 every one in order; OverlapShape2 and OverlapShape3
// (with OverlapCircle2, OverlapBox2, OverlapSphere3 and OverlapBox3)
// return everything a placed shape touches; ShapeCast2 and ShapeCast3
// sweep a shape and return the first thing in its way; Nearest2 and
// Nearest3 find the closest collider to a point. SignedDistance2 and
// SignedDistance3 measure a point against one placed shape without
// touching the world, for code that pushes points out of solids, such
// as the soft bodies in phys/soft. The queries that return a slice have
// an Into form (RaycastAll2Into, RaycastAll3Into, OverlapShape2Into,
// OverlapShape3Into) that appends to a slice the caller reuses, so a
// game can reuse result and scratch storage after their buffers grow.
// Each collider's placement (its rotation, position and bounds) is kept
// between steps and queries, and queries search a tree of those bounds,
// so a ray or a sweep tests only the colliders near it, in the order a
// walk over every collider would, with the same results. A query still
// walks the collider components once to notice transforms and shapes the
// game changed since the last step and places again only those; a
// character controller's move walks once for all its sweeps. In-place
// hull and compound edits are noticed even when bounds stay unchanged.
// MeshShape geometry must remain immutable.
//
// Joints are components on their own entities that name the bodies they
// connect: DistanceJoint2 and DistanceJoint3 (rods and ropes),
// RevoluteJoint2 and HingeJoint3 (pins, with angle limits and a motor),
// BallJoint3 (a shoulder or hip, with cone and twist limits),
// PrismaticJoint2 and PrismaticJoint3 (sliders, with travel limits, a
// motor and a spring), WheelJoint2 (a wheel on a sprung suspension with
// a motor on its spin), SpringJoint2 and SpringJoint3 (damped springs)
// and FixedJoint2 and FixedJoint3 (welds). They are solved with the
// contacts, in entity order.
// NewRagdoll3 spawns a humanoid of capsules on limited joints
// from a RagdollSpec, and Ragdoll3.Pose places it from an animated
// character's bones. A body with CCD set is swept against static
// geometry every substep so it cannot tunnel, and its bounding sphere is
// swept against the other moving bodies. With Settings.SleepTime set,
// bodies that rest for that long sleep until touched or pushed
// (Body.Asleep, Body.Wake).
//
// CharacterController2 and CharacterController3 move an upright capsule
// by sweeps rather than dynamics. The capsule slides along walls, climbs
// steps up to StepHeight, walks slopes up to MaxSlope and reports
// Grounded.
//
// For orbits and spaceflight, see the orbit package. It works with the
// same transforms at astronomical scale.
package phys

import (
	"github.com/matjam/bunyip/ecs"
)

// Solver tuning shared by both dimensions.
const (
	slop      = 0.005 // penetration allowed before correction, in world units
	baumgarte = 0.2   // fraction of the remaining penetration corrected per step
	// Bounces slower than this are absorbed, so resting contacts settle.
	restitutionThreshold = 1.0
	// Passes over the contacts after the positions have moved that take
	// the speed the position correction added back out.
	relaxIterations = 2
)

// Layers restrict which colliders meet: two colliders collide when each
// one's Layer bits appear in the other's Mask. Zero means "all".
type Layers struct {
	Layer uint32
	Mask  uint32
}

func (l Layers) collides(o Layers) bool {
	la, ma := l.Layer, l.Mask
	lb, mb := o.Layer, o.Mask
	if la == 0 {
		la = ^uint32(0)
	}
	if lb == 0 {
		lb = ^uint32(0)
	}
	if ma == 0 {
		ma = ^uint32(0)
	}
	if mb == 0 {
		mb = ^uint32(0)
	}
	return la&mb != 0 && lb&ma != 0
}

// pairKey orders two entities so a pair is reported once.
type pairKey struct{ a, b ecs.Entity }

func keyOf(a, b ecs.Entity) pairKey {
	if a.ID() < b.ID() {
		return pairKey{a, b}
	}
	return pairKey{b, a}
}

// slotMap maps an entity to an int by its slot in the world, with a
// stamp per slot so a whole map is emptied by bumping the generation.
type slotMap struct {
	stamp []uint32
	value []int32
	gen   uint32
}

func (m *slotMap) reset() {
	m.gen++
	if m.gen == 0 {
		clear(m.stamp)
		m.gen = 1
	}
}

// slot is the entity's index in the world; the generation is the high
// half of the id and does not pick the row.
func slot(e ecs.Entity) int { return int(uint32(e.ID())) }

func (m *slotMap) set(e ecs.Entity, v int) {
	i := slot(e)
	for len(m.stamp) <= i {
		m.stamp = append(m.stamp, 0)
		m.value = append(m.value, 0)
	}
	m.stamp[i], m.value[i] = m.gen, int32(v)
}

func (m *slotMap) get(e ecs.Entity) (int, bool) {
	i := slot(e)
	if i < len(m.stamp) && m.stamp[i] == m.gen {
		return int(m.value[i]), true
	}
	return 0, false
}

// pairSet records which pairs an update has already reported. It is an
// open-addressed table with a stamp per slot, so emptying it costs one
// increment and reporting a pair costs no allocation.
type pairSet struct {
	keys  []pairKey
	stamp []uint32
	gen   uint32
	count int
}

func (p *pairSet) reset() {
	p.gen++
	p.count = 0
	if p.gen == 0 {
		clear(p.stamp)
		p.gen = 1
	}
	if len(p.stamp) == 0 {
		p.keys = make([]pairKey, 64)
		p.stamp = make([]uint32, 64)
	}
}

func pairHash(k pairKey) uint32 {
	h := k.a.ID()*0x9e3779b97f4a7c15 ^ k.b.ID()*0xc2b2ae3d27d4eb4f
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	return uint32(h >> 32)
}

// add records the pair and reports whether it was already there.
func (p *pairSet) add(k pairKey) bool {
	if p.count*4 >= len(p.stamp)*3 {
		p.grow()
	}
	mask := uint32(len(p.stamp) - 1)
	for i := pairHash(k) & mask; ; i = (i + 1) & mask {
		if p.stamp[i] != p.gen {
			p.stamp[i], p.keys[i] = p.gen, k
			p.count++
			return false
		}
		if p.keys[i] == k {
			return true
		}
	}
}

func (p *pairSet) grow() {
	old, stamps := p.keys, p.stamp
	n := len(stamps) * 2
	p.keys, p.stamp = make([]pairKey, n), make([]uint32, n)
	mask := uint32(n - 1)
	for i, s := range stamps {
		if s != p.gen {
			continue
		}
		k := old[i]
		j := pairHash(k) & mask
		for p.stamp[j] == p.gen {
			j = (j + 1) & mask
		}
		p.stamp[j], p.keys[j] = p.gen, k
	}
}
