package phys

import (
	"math"

	"github.com/matjam/bunyip/ecs"
	"github.com/matjam/bunyip/gfx"
	"github.com/matjam/bunyip/lin"
)

// Settings2 is the world resource that tunes 2D simulation. Zero
// substeps or iterations mean 4 and 8.
type Settings2 struct {
	Gravity    lin.Vec2
	Substeps   int // integration steps per update; more is stabler
	Iterations int // solver passes per step; more is stiffer
}

// Body2 makes a 2D entity move. Mass zero is static; Kinematic bodies
// move by their velocity and push others without being pushed.
type Body2 struct {
	Vel    lin.Vec2
	AngVel float32 // radians per second, anticlockwise
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
	Sleeping     bool // set by the game to freeze a body

	force  lin.Vec2
	torque float32
	// Derived each step from the mass and collider.
	invMass, invInertia float32
}

// Dynamic2 returns a moving body with sensible defaults.
func Dynamic2(mass float32) Body2 {
	return Body2{Mass: mass, Restitution: 0.1, Friction: 0.5, GravityScale: 1}
}

// Kinematic2 returns a body moved by its velocity that others cannot push.
func Kinematic2() Body2 { return Body2{Kinematic: true, Friction: 0.5, GravityScale: 1} }

// AddForce accumulates a force (units of mass·distance/s²) until the
// next update.
func (b *Body2) AddForce(f lin.Vec2) { b.force = b.force.Add(f) }

// AddTorque accumulates a torque until the next update.
func (b *Body2) AddTorque(t float32) { b.torque += t }

// AddImpulse changes velocity at once by impulse/mass.
func (b *Body2) AddImpulse(i lin.Vec2) {
	if b.Mass > 0 && !b.Kinematic {
		b.Vel = b.Vel.Add(i.Mul(1 / b.Mass))
	}
}

// Collider2 gives an entity a shape. An entity with a Collider2 and no
// Body2 is a static obstacle.
type Collider2 struct {
	Shape   Shape2
	Offset  lin.Vec2 // shape centre relative to the transform
	Trigger bool     // overlaps are reported but not resolved
	Layers
}

// Collision2 is emitted for each pair of colliders that touched this
// update; A and B are ordered by entity id.
type Collision2 struct {
	A, B   ecs.Entity
	Point  lin.Vec2
	Normal lin.Vec2 // from A to B
	Depth  float32
}

// Trigger2 is emitted while a trigger collider overlaps another collider.
type Trigger2 struct {
	Trigger, Other ecs.Entity
}

// entry2 is one collider prepared for a step.
type entry2 struct {
	e      ecs.Entity
	t      *gfx.Transform2
	b      *Body2 // nil for static
	c      *Collider2
	pos    lin.Vec2
	lo, hi lin.Vec2
}

type state2 struct {
	bodies    *ecs.Query2[gfx.Transform2, Body2]
	colliders *ecs.Query2[gfx.Transform2, Collider2]
	entries   []entry2
}

func stateOf2(w *ecs.World) *state2 {
	s := ecs.Resource[state2](w)
	if s == nil {
		ecs.SetResource(w, state2{bodies: ecs.NewQuery2[gfx.Transform2, Body2](w), colliders: ecs.NewQuery2[gfx.Transform2, Collider2](w)})
		s = ecs.Resource[state2](w)
	}
	return s
}

// System2 advances every 2D body by dt seconds.
func System2(w *ecs.World, dt float64) {
	if dt <= 0 {
		return
	}
	settings := ecs.Resource[Settings2](w)
	if settings == nil {
		ecs.SetResource(w, Settings2{})
		settings = ecs.Resource[Settings2](w)
	}
	substeps, iterations := settings.Substeps, settings.Iterations
	if substeps <= 0 {
		substeps = 4
	}
	if iterations <= 0 {
		iterations = 8
	}
	s := stateOf2(w)
	h := float32(dt) / float32(substeps)
	reported := map[pairKey]bool{}
	for range substeps {
		s.step(w, settings, h, iterations, reported)
	}
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform2, b *Body2) {
		b.force, b.torque = lin.Vec2{}, 0
	})
}

func (s *state2) step(w *ecs.World, settings *Settings2, h float32, iterations int, reported map[pairKey]bool) {
	// Integrate velocities.
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform2, b *Body2) {
		if b.Sleeping {
			return
		}
		if b.Kinematic || b.Mass <= 0 {
			b.invMass, b.invInertia = 0, 0
			return
		}
		b.invMass = 1 / b.Mass
		b.invInertia = 0
		if !b.LockRotation {
			if c, ok := ecs.Get[Collider2](w, e); ok && c.Shape != nil {
				if i := c.Shape.inertia(b.Mass); i > 0 {
					b.invInertia = 1 / i
				}
			}
		}
		gs := b.GravityScale
		if gs == 0 {
			gs = 1
		}
		acc := settings.Gravity.Mul(gs).Add(b.force.Mul(b.invMass))
		b.Vel = b.Vel.Add(acc.Mul(h))
		b.AngVel += b.torque * b.invInertia * h
		if b.LinearDamping > 0 {
			b.Vel = b.Vel.Mul(max(0, 1-b.LinearDamping*h))
		}
		if b.AngularDamping > 0 {
			b.AngVel *= max(0, 1-b.AngularDamping*h)
		}
	})
	// Gather colliders with bounds.
	s.entries = s.entries[:0]
	s.colliders.Each(func(e ecs.Entity, t *gfx.Transform2, c *Collider2) {
		if c.Shape == nil {
			return
		}
		b, _ := ecs.Get[Body2](w, e)
		cs, sn := cosSin(t.Rotation)
		pos := t.Position.Add(rotate2(c.Offset, cs, sn))
		lo, hi := c.Shape.bounds(pos, t.Rotation)
		s.entries = append(s.entries, entry2{e: e, t: t, b: b, c: c, pos: pos, lo: lo, hi: hi})
	})
	// Broadphase and contact generation.
	var arbiters []arbiter2
	en := s.entries
	sweepPairs(len(en), func(i int) float32 { return en[i].lo.X }, func(i int) float32 { return en[i].hi.X }, func(i, j int) {
		a, b := &en[i], &en[j]
		if a.lo.Y > b.hi.Y || b.lo.Y > a.hi.Y {
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
		contacts := collide2(a.c.Shape, a.pos, a.t.Rotation, b.c.Shape, b.pos, b.t.Rotation)
		if len(contacts) == 0 {
			return
		}
		if a.c.Trigger || b.c.Trigger {
			key := keyOf(a.e, b.e)
			if !reported[key] {
				reported[key] = true
				if a.c.Trigger {
					ecs.Emit(w, Trigger2{Trigger: a.e, Other: b.e})
				}
				if b.c.Trigger {
					ecs.Emit(w, Trigger2{Trigger: b.e, Other: a.e})
				}
			}
			return
		}
		key := keyOf(a.e, b.e)
		if !reported[key] {
			reported[key] = true
			c := contacts[0]
			ev := Collision2{A: a.e, B: b.e, Point: c.point, Normal: c.normal, Depth: c.depth}
			if key.a != a.e {
				ev.A, ev.B, ev.Normal = b.e, a.e, c.normal.Mul(-1)
			}
			ecs.Emit(w, ev)
		}
		arbiters = append(arbiters, newArbiter2(a, b, contacts, h))
	})
	// Solve.
	for range iterations {
		for i := range arbiters {
			arbiters[i].solve()
		}
	}
	// Integrate positions.
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform2, b *Body2) {
		if b.Sleeping || (b.Mass <= 0 && !b.Kinematic) {
			return
		}
		t.Position = t.Position.Add(b.Vel.Mul(h))
		if !b.LockRotation || b.Kinematic {
			t.Rotation += b.AngVel * h
		}
	})
}

// arbiter2 holds a pair's contacts through the solver iterations.
type arbiter2 struct {
	a, b     *entry2
	contacts []solverContact2
	friction float32
}

type solverContact2 struct {
	rA, rB      lin.Vec2
	normal      lin.Vec2
	tangent     lin.Vec2
	massNormal  float32
	massTangent float32
	bias        float32
	pn, pt      float32
}

func bodyVel2(e *entry2) (lin.Vec2, float32, float32, float32) {
	if e.b == nil {
		return lin.Vec2{}, 0, 0, 0
	}
	return e.b.Vel, e.b.AngVel, e.b.invMass, e.b.invInertia
}

func newArbiter2(a, b *entry2, contacts []contact2, h float32) arbiter2 {
	arb := arbiter2{a: a, b: b}
	var fa, fb, ra, rb float32 = 0.5, 0.5, 0, 0
	if a.b != nil {
		fa, ra = a.b.Friction, a.b.Restitution
	}
	if b.b != nil {
		fb, rb = b.b.Friction, b.b.Restitution
	}
	arb.friction = float32(math.Sqrt(float64(fa * fb)))
	restitution := max(ra, rb)
	va, wa, ima, iia := bodyVel2(a)
	vb, wb, imb, iib := bodyVel2(b)
	for _, c := range contacts {
		sc := solverContact2{normal: c.normal}
		sc.rA = c.point.Sub(a.t.Position)
		sc.rB = c.point.Sub(b.t.Position)
		rnA, rnB := cross2(sc.rA, c.normal), cross2(sc.rB, c.normal)
		kn := ima + imb + iia*rnA*rnA + iib*rnB*rnB
		if kn > 0 {
			sc.massNormal = 1 / kn
		}
		sc.tangent = lin.V2(c.normal.Y, -c.normal.X)
		rtA, rtB := cross2(sc.rA, sc.tangent), cross2(sc.rB, sc.tangent)
		kt := ima + imb + iia*rtA*rtA + iib*rtB*rtB
		if kt > 0 {
			sc.massTangent = 1 / kt
		}
		sc.bias = baumgarte / h * max(c.depth-slop, 0)
		// Restitution from the approach speed before the solve.
		dv := vb.Add(crossSV(wb, sc.rB)).Sub(va).Sub(crossSV(wa, sc.rA))
		if vn := dv.Dot(c.normal); vn < -restitutionThreshold {
			sc.bias += -restitution * vn
		}
		arb.contacts = append(arb.contacts, sc)
	}
	return arb
}

func (arb *arbiter2) solve() {
	a, b := arb.a, arb.b
	for i := range arb.contacts {
		c := &arb.contacts[i]
		va, wa, ima, iia := bodyVel2(a)
		vb, wb, imb, iib := bodyVel2(b)
		dv := vb.Add(crossSV(wb, c.rB)).Sub(va).Sub(crossSV(wa, c.rA))
		vn := dv.Dot(c.normal)
		dpn := c.massNormal * (-vn + c.bias)
		pn0 := c.pn
		c.pn = max(pn0+dpn, 0)
		dpn = c.pn - pn0
		p := c.normal.Mul(dpn)
		if a.b != nil {
			a.b.Vel = a.b.Vel.Sub(p.Mul(ima))
			a.b.AngVel -= iia * cross2(c.rA, p)
		}
		if b.b != nil {
			b.b.Vel = b.b.Vel.Add(p.Mul(imb))
			b.b.AngVel += iib * cross2(c.rB, p)
		}
		// Friction.
		va, wa, _, _ = bodyVel2(a)
		vb, wb, _, _ = bodyVel2(b)
		dv = vb.Add(crossSV(wb, c.rB)).Sub(va).Sub(crossSV(wa, c.rA))
		vt := dv.Dot(c.tangent)
		dpt := c.massTangent * -vt
		maxPt := arb.friction * c.pn
		pt0 := c.pt
		c.pt = lin.Clamp(pt0+dpt, -maxPt, maxPt)
		dpt = c.pt - pt0
		p = c.tangent.Mul(dpt)
		if a.b != nil {
			a.b.Vel = a.b.Vel.Sub(p.Mul(ima))
			a.b.AngVel -= iia * cross2(c.rA, p)
		}
		if b.b != nil {
			b.b.Vel = b.b.Vel.Add(p.Mul(imb))
			b.b.AngVel += iib * cross2(c.rB, p)
		}
	}
}

// Hit2 is what a raycast found.
type Hit2 struct {
	Entity   ecs.Entity
	Point    lin.Vec2
	Normal   lin.Vec2
	Distance float32 // along the ray, as a fraction of Dir's length
}

// Raycast2 finds the nearest collider along the ray, ignoring triggers
// and colliders the mask excludes.
func Raycast2(w *ecs.World, r Ray2, mask uint32) (Hit2, bool) {
	best := Hit2{Distance: float32(math.Inf(1))}
	found := false
	stateOf2(w).colliders.Each(func(e ecs.Entity, t *gfx.Transform2, c *Collider2) {
		if c.Shape == nil || c.Trigger || !(Layers{Mask: mask}).collides(c.Layers) {
			return
		}
		cs, sn := cosSin(t.Rotation)
		pos := t.Position.Add(rotate2(c.Offset, cs, sn))
		if tt, n, ok := rayShape2(r, c.Shape, pos, t.Rotation); ok && tt < best.Distance {
			best = Hit2{Entity: e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt}
			found = true
		}
	})
	return best, found
}
