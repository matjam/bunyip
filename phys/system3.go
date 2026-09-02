package phys

import (
	"math"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// Settings3 is the world resource that tunes 3D simulation. Zero
// substeps or iterations mean 4 and 8.
type Settings3 struct {
	Gravity    lin.Vec3
	Substeps   int
	Iterations int
}

// Body3 makes a 3D entity move. Mass zero is static; Kinematic bodies
// move by their velocity and push others without being pushed.
type Body3 struct {
	Vel    lin.Vec3
	AngVel lin.Vec3 // radians per second about each world axis
	Mass   float32
	// Restitution is bounciness, 0 to 1; Friction is the Coulomb
	// coefficient, 0 slides freely.
	Restitution, Friction float32
	// Damping removes a fraction of velocity per second.
	LinearDamping, AngularDamping float32
	// GravityScale multiplies the world gravity; zero means 1.
	GravityScale float32
	Kinematic    bool
	LockRotation bool
	Sleeping     bool

	force  lin.Vec3
	torque lin.Vec3
	// Derived each step.
	invMass    float32
	invInertia mat3 // world space
}

// Dynamic3 returns a moving body with sensible defaults.
func Dynamic3(mass float32) Body3 {
	return Body3{Mass: mass, Restitution: 0.1, Friction: 0.5, GravityScale: 1}
}

// Kinematic3 returns a body moved by its velocity that others cannot push.
func Kinematic3() Body3 { return Body3{Kinematic: true, Friction: 0.5, GravityScale: 1} }

// AddForce accumulates a force until the next update.
func (b *Body3) AddForce(f lin.Vec3) { b.force = b.force.Add(f) }

// AddTorque accumulates a torque until the next update.
func (b *Body3) AddTorque(t lin.Vec3) { b.torque = b.torque.Add(t) }

// AddImpulse changes velocity at once by impulse/mass.
func (b *Body3) AddImpulse(i lin.Vec3) {
	if b.Mass > 0 && !b.Kinematic {
		b.Vel = b.Vel.Add(i.Mul(1 / b.Mass))
	}
}

// Collider3 gives an entity a shape. An entity with a Collider3 and no
// Body3 is a static obstacle.
type Collider3 struct {
	Shape   Shape3
	Offset  lin.Vec3
	Trigger bool
	Layers
}

// Collision3 is emitted for each pair of colliders that touched this
// update; A and B are ordered by entity id.
type Collision3 struct {
	A, B   ecs.Entity
	Point  lin.Vec3
	Normal lin.Vec3 // from A to B
	Depth  float32
}

// Trigger3 is emitted while a trigger collider overlaps another collider.
type Trigger3 struct {
	Trigger, Other ecs.Entity
}

type entry3 struct {
	e      ecs.Entity
	t      *gfx.Transform
	b      *Body3
	c      *Collider3
	pos    lin.Vec3
	rot    mat3
	lo, hi lin.Vec3
}

type state3 struct {
	bodies    *ecs.Query2[gfx.Transform, Body3]
	colliders *ecs.Query2[gfx.Transform, Collider3]
	entries   []entry3
}

func stateOf3(w *ecs.World) *state3 {
	s := ecs.Resource[state3](w)
	if s == nil {
		ecs.SetResource(w, state3{bodies: ecs.NewQuery2[gfx.Transform, Body3](w), colliders: ecs.NewQuery2[gfx.Transform, Collider3](w)})
		s = ecs.Resource[state3](w)
	}
	return s
}

// System3 advances every 3D body by dt seconds.
func System3(w *ecs.World, dt float64) {
	if dt <= 0 {
		return
	}
	settings := ecs.Resource[Settings3](w)
	if settings == nil {
		ecs.SetResource(w, Settings3{})
		settings = ecs.Resource[Settings3](w)
	}
	substeps, iterations := settings.Substeps, settings.Iterations
	if substeps <= 0 {
		substeps = 4
	}
	if iterations <= 0 {
		iterations = 8
	}
	s := stateOf3(w)
	h := float32(dt) / float32(substeps)
	reported := map[pairKey]bool{}
	for range substeps {
		s.step(w, settings, h, iterations, reported)
	}
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform, b *Body3) {
		b.force, b.torque = lin.Vec3{}, lin.Vec3{}
	})
}

func (s *state3) step(w *ecs.World, settings *Settings3, h float32, iterations int, reported map[pairKey]bool) {
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform, b *Body3) {
		if b.Sleeping {
			return
		}
		if b.Kinematic || b.Mass <= 0 {
			b.invMass, b.invInertia = 0, mat3{}
			return
		}
		b.invMass = 1 / b.Mass
		b.invInertia = mat3{}
		if !b.LockRotation {
			if c, ok := ecs.Get[Collider3](w, e); ok && c.Shape != nil {
				i := c.Shape.inertia(b.Mass)
				r := mat3FromQuat(t.Rotation)
				local := diag3(inv(i.X), inv(i.Y), inv(i.Z))
				b.invInertia = r.mul(local).mul(r.transpose())
			}
		}
		gs := b.GravityScale
		if gs == 0 {
			gs = 1
		}
		acc := settings.Gravity.Mul(gs).Add(b.force.Mul(b.invMass))
		b.Vel = b.Vel.Add(acc.Mul(h))
		b.AngVel = b.AngVel.Add(b.invInertia.mulVec(b.torque).Mul(h))
		if b.LinearDamping > 0 {
			b.Vel = b.Vel.Mul(max(0, 1-b.LinearDamping*h))
		}
		if b.AngularDamping > 0 {
			b.AngVel = b.AngVel.Mul(max(0, 1-b.AngularDamping*h))
		}
	})
	s.entries = s.entries[:0]
	s.colliders.Each(func(e ecs.Entity, t *gfx.Transform, c *Collider3) {
		if c.Shape == nil {
			return
		}
		b, _ := ecs.Get[Body3](w, e)
		rot := mat3FromQuat(t.Rotation)
		pos := t.Position.Add(rot.mulVec(c.Offset))
		lo, hi := c.Shape.bounds(pos, rot)
		s.entries = append(s.entries, entry3{e: e, t: t, b: b, c: c, pos: pos, rot: rot, lo: lo, hi: hi})
	})
	var arbiters []arbiter3
	en := s.entries
	sweepPairs(len(en), func(i int) float32 { return en[i].lo.X }, func(i int) float32 { return en[i].hi.X }, func(i, j int) {
		a, b := &en[i], &en[j]
		if a.lo.Y > b.hi.Y || b.lo.Y > a.hi.Y || a.lo.Z > b.hi.Z || b.lo.Z > a.hi.Z {
			return
		}
		aStatic := a.b == nil || a.b.invMass == 0
		bStatic := b.b == nil || b.b.invMass == 0
		if aStatic && bStatic && !a.c.Trigger && !b.c.Trigger {
			return
		}
		if !a.c.Layers.collides(b.c.Layers) {
			return
		}
		contacts := collide3(a.c.Shape, a.pos, a.rot, b.c.Shape, b.pos, b.rot)
		if len(contacts) == 0 {
			return
		}
		key := keyOf(a.e, b.e)
		if a.c.Trigger || b.c.Trigger {
			if !reported[key] {
				reported[key] = true
				if a.c.Trigger {
					ecs.Emit(w, Trigger3{Trigger: a.e, Other: b.e})
				}
				if b.c.Trigger {
					ecs.Emit(w, Trigger3{Trigger: b.e, Other: a.e})
				}
			}
			return
		}
		if !reported[key] {
			reported[key] = true
			c := contacts[0]
			ev := Collision3{A: a.e, B: b.e, Point: c.point, Normal: c.normal, Depth: c.depth}
			if key.a != a.e {
				ev.A, ev.B, ev.Normal = b.e, a.e, c.normal.Mul(-1)
			}
			ecs.Emit(w, ev)
		}
		arbiters = append(arbiters, newArbiter3(a, b, contacts, h))
	})
	for range iterations {
		for i := range arbiters {
			arbiters[i].solve()
		}
	}
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform, b *Body3) {
		if b.Sleeping || (b.Mass <= 0 && !b.Kinematic) {
			return
		}
		t.Position = t.Position.Add(b.Vel.Mul(h))
		if (!b.LockRotation || b.Kinematic) && b.AngVel != (lin.Vec3{}) {
			q := t.Rotation
			if q == (lin.Quat{}) {
				q = lin.QuatIdentity()
			}
			wq := lin.Quat{X: b.AngVel.X, Y: b.AngVel.Y, Z: b.AngVel.Z, W: 0}
			dq := wq.Mul(q)
			t.Rotation = lin.Quat{X: q.X + 0.5*h*dq.X, Y: q.Y + 0.5*h*dq.Y, Z: q.Z + 0.5*h*dq.Z, W: q.W + 0.5*h*dq.W}.Norm()
		}
	})
}

func inv(v float32) float32 {
	if v <= 0 {
		return 0
	}
	return 1 / v
}

type arbiter3 struct {
	a, b     *entry3
	contacts []solverContact3
	friction float32
}

type solverContact3 struct {
	rA, rB       lin.Vec3
	normal       lin.Vec3
	t1, t2       lin.Vec3
	massNormal   float32
	massT1       float32
	massT2       float32
	bias         float32
	pn, pt1, pt2 float32
}

func bodyVel3(e *entry3) (lin.Vec3, lin.Vec3, float32, mat3) {
	if e.b == nil {
		return lin.Vec3{}, lin.Vec3{}, 0, mat3{}
	}
	return e.b.Vel, e.b.AngVel, e.b.invMass, e.b.invInertia
}

// effectiveMass is 1 / (imA + imB + n·((IinvA (rA×n))×rA + (IinvB (rB×n))×rB)).
func effectiveMass(n, rA, rB lin.Vec3, ima float32, iia mat3, imb float32, iib mat3) float32 {
	ra := iia.mulVec(rA.Cross(n)).Cross(rA)
	rb := iib.mulVec(rB.Cross(n)).Cross(rB)
	k := ima + imb + n.Dot(ra.Add(rb))
	if k <= 0 {
		return 0
	}
	return 1 / k
}

func newArbiter3(a, b *entry3, contacts []contact3, h float32) arbiter3 {
	arb := arbiter3{a: a, b: b}
	var fa, fb, ra, rb float32 = 0.5, 0.5, 0, 0
	if a.b != nil {
		fa, ra = a.b.Friction, a.b.Restitution
	}
	if b.b != nil {
		fb, rb = b.b.Friction, b.b.Restitution
	}
	arb.friction = float32(math.Sqrt(float64(fa * fb)))
	restitution := max(ra, rb)
	va, wa, ima, iia := bodyVel3(a)
	vb, wb, imb, iib := bodyVel3(b)
	for _, c := range contacts {
		sc := solverContact3{normal: c.normal}
		sc.rA = c.point.Sub(a.t.Position)
		sc.rB = c.point.Sub(b.t.Position)
		sc.massNormal = effectiveMass(c.normal, sc.rA, sc.rB, ima, iia, imb, iib)
		// Two tangents spanning the contact plane.
		ref := lin.V3(1, 0, 0)
		if math.Abs(float64(c.normal.X)) > 0.9 {
			ref = lin.V3(0, 1, 0)
		}
		sc.t1 = c.normal.Cross(ref).Norm()
		sc.t2 = c.normal.Cross(sc.t1)
		sc.massT1 = effectiveMass(sc.t1, sc.rA, sc.rB, ima, iia, imb, iib)
		sc.massT2 = effectiveMass(sc.t2, sc.rA, sc.rB, ima, iia, imb, iib)
		sc.bias = baumgarte / h * max(c.depth-slop, 0)
		dv := vb.Add(wb.Cross(sc.rB)).Sub(va).Sub(wa.Cross(sc.rA))
		if vn := dv.Dot(c.normal); vn < -restitutionThreshold {
			sc.bias += -restitution * vn
		}
		arb.contacts = append(arb.contacts, sc)
	}
	return arb
}

func (arb *arbiter3) applyImpulse(c *solverContact3, p lin.Vec3) {
	if arb.a.b != nil && arb.a.b.invMass > 0 {
		arb.a.b.Vel = arb.a.b.Vel.Sub(p.Mul(arb.a.b.invMass))
		arb.a.b.AngVel = arb.a.b.AngVel.Sub(arb.a.b.invInertia.mulVec(c.rA.Cross(p)))
	}
	if arb.b.b != nil && arb.b.b.invMass > 0 {
		arb.b.b.Vel = arb.b.b.Vel.Add(p.Mul(arb.b.b.invMass))
		arb.b.b.AngVel = arb.b.b.AngVel.Add(arb.b.b.invInertia.mulVec(c.rB.Cross(p)))
	}
}

func (arb *arbiter3) relativeVelocity(c *solverContact3) lin.Vec3 {
	va, wa, _, _ := bodyVel3(arb.a)
	vb, wb, _, _ := bodyVel3(arb.b)
	return vb.Add(wb.Cross(c.rB)).Sub(va).Sub(wa.Cross(c.rA))
}

func (arb *arbiter3) solve() {
	for i := range arb.contacts {
		c := &arb.contacts[i]
		dv := arb.relativeVelocity(c)
		vn := dv.Dot(c.normal)
		dpn := c.massNormal * (-vn + c.bias)
		pn0 := c.pn
		c.pn = max(pn0+dpn, 0)
		arb.applyImpulse(c, c.normal.Mul(c.pn-pn0))
		maxPt := arb.friction * c.pn
		dv = arb.relativeVelocity(c)
		dpt1 := c.massT1 * -dv.Dot(c.t1)
		pt10 := c.pt1
		c.pt1 = lin.Clamp(pt10+dpt1, -maxPt, maxPt)
		arb.applyImpulse(c, c.t1.Mul(c.pt1-pt10))
		dv = arb.relativeVelocity(c)
		dpt2 := c.massT2 * -dv.Dot(c.t2)
		pt20 := c.pt2
		c.pt2 = lin.Clamp(pt20+dpt2, -maxPt, maxPt)
		arb.applyImpulse(c, c.t2.Mul(c.pt2-pt20))
	}
}

// Hit3 is what a raycast found.
type Hit3 struct {
	Entity   ecs.Entity
	Point    lin.Vec3
	Normal   lin.Vec3
	Distance float32 // along the ray, as a fraction of Dir's length
}

// Raycast3 finds the nearest collider along the ray, ignoring triggers
// and colliders the mask excludes.
func Raycast3(w *ecs.World, r Ray3, mask uint32) (Hit3, bool) {
	best := Hit3{Distance: float32(math.Inf(1))}
	found := false
	stateOf3(w).colliders.Each(func(e ecs.Entity, t *gfx.Transform, c *Collider3) {
		if c.Shape == nil || c.Trigger || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		rot := mat3FromQuat(t.Rotation)
		pos := t.Position.Add(rot.mulVec(c.Offset))
		if tt, n, ok := rayShape3(r, c.Shape, pos, rot); ok && tt < best.Distance {
			best = Hit3{Entity: e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt}
			found = true
		}
	})
	return best, found
}
