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
	Gravity    lin.Vec2 // world units per second squared; zero disables gravity
	Substeps   int      // integration steps per update; more is stabler
	Iterations int      // solver passes per step; more is stiffer
	// SleepTime is how long a body and everything touching it must rest
	// before they sleep and drop out of the simulation until touched;
	// zero means bodies never sleep. Half a second suits most games.
	SleepTime float32
	// SleepThreshold is the speed (units and radians per second) below
	// which a body counts as resting; zero means 0.05.
	SleepThreshold float32
}

// Body2 makes a 2D entity move. Mass zero is static; Kinematic bodies
// move by their velocity and push others without being pushed.
type Body2 struct {
	Vel    lin.Vec2
	AngVel float32 // radians per second; positive rotates +X toward +Y (clockwise on screen)
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
	// CCD sweeps the body against static colliders every substep and
	// stops it at the first one, so a fast small body cannot pass
	// through a thin wall.
	CCD bool

	force  lin.Vec2
	torque float32
	// Derived each step from the mass and collider.
	invMass, invInertia float32
	// Sleeping bookkeeping and the fraction of this substep's motion
	// CCD allows.
	asleep    bool
	sleepTime float32
	fraction  float32
}

// Dynamic2 returns a body with the given mass, restitution 0.1, friction
// 0.5 and gravity scale 1. A positive mass makes it dynamic.
func Dynamic2(mass float32) Body2 {
	return Body2{Mass: mass, Restitution: 0.1, Friction: 0.5, GravityScale: 1}
}

// Kinematic2 returns a body moved by its velocity that others cannot push.
func Kinematic2() Body2 { return Body2{Kinematic: true, Friction: 0.5, GravityScale: 1} }

// AddForce accumulates a force (units of mass·distance/s²) until the
// next update.
func (b *Body2) AddForce(f lin.Vec2) { b.force = b.force.Add(f); b.Wake() }

// AddTorque accumulates a torque until the next update.
func (b *Body2) AddTorque(t float32) { b.torque += t; b.Wake() }

// AddImpulse changes velocity at once by impulse/mass.
func (b *Body2) AddImpulse(i lin.Vec2) {
	if b.Mass > 0 && !b.Kinematic {
		b.Vel = b.Vel.Add(i.Mul(1 / b.Mass))
		b.Wake()
	}
}

// Asleep reports that the body has come to rest and is skipped by the
// simulation until something touches or pushes it.
func (b *Body2) Asleep() bool { return b.asleep }

// Wake clears automatic sleep and its timer. It does not clear Sleeping,
// which remains under the game's control.
func (b *Body2) Wake() { b.asleep, b.sleepTime = false, 0 }

// Collider2 gives an entity a shape. An entity with a Collider2 and no
// Body2 is a static obstacle. It also needs gfx.Transform2. A nil Shape
// is ignored; transform scale does not resize the shape.
type Collider2 struct {
	Shape   Shape2
	Offset  lin.Vec2 // shape centre relative to the transform
	Trigger bool     // overlaps are reported but not resolved
	Layers
}

// Collision2 is emitted for each pair of colliders that touched this
// update; A and B are ordered by entity id. Impulse is the total normal
// impulse the contact applied in the first substep it was seen, a
// measure of how hard the hit was.
type Collision2 struct {
	A, B    ecs.Entity
	Point   lin.Vec2
	Normal  lin.Vec2 // from A to B
	Depth   float32
	Impulse float32
}

// Trigger2 is emitted while a trigger collider overlaps another collider.
// A pair is reported in its first overlapping substep of the update;
// when both colliders are triggers, each receives its own event.
type Trigger2 struct {
	Trigger, Other ecs.Entity
}

// entry2 is one collider placed in the world: a row of the collider
// index, which the step and the queries share.
type entry2 struct {
	e      ecs.Entity
	t      *gfx.Transform2
	b      *Body2 // the body during a step; nil for a collider without one
	c      *Collider2
	pos    lin.Vec2
	lo, hi lin.Vec2
	bi     int  // index into the step's dynamic bodies, -1 otherwise
	shaped bool // the collider has a shape; rows without one are skipped
}

type bodyRec2 struct {
	e ecs.Entity
	t *gfx.Transform2
	b *Body2
}

type state2 struct {
	bodies    *ecs.Query2[gfx.Transform2, Body2]
	colliders *ecs.Query2[gfx.Transform2, Collider2]
	distance  *ecs.Query1[DistanceJoint2]
	revolute  *ecs.Query1[RevoluteJoint2]
	prismatic *ecs.Query1[PrismaticJoint2]
	wheel     *ecs.Query1[WheelJoint2]
	spring    *ecs.Query1[SpringJoint2]
	fixed     *ecs.Query1[FixedJoint2]
	// cols is every collider placed in the world, kept between steps.
	cols index2
	// dynamic is the step's dynamic bodies and index maps each to its
	// place there; all is every body and bodyAt maps each to its place.
	dynamic []bodyRec2
	index   slotMap
	all     []bodyRec2
	bodyAt  slotMap
	// bodyRows are the collider rows of this step's bodies and moveSet
	// those with the triggers that have no body.
	bodyRows []int32
	moveSet  []int32
	parent   []int
	// Scratch kept between steps so a step allocates nothing: ss serves
	// the step and qs the queries, which the game may call while the
	// step's buffers still hold contacts.
	ss, qs scratch2
	// The buffers the sweeps and the queries gather their candidates
	// into, and the hits a character controller reads back.
	cands  []int32
	qcands []candidate2
	hits   []Hit2
	// The furthest any moving body travels along the sweep axis this
	// substep, which widens the band a moving pair is searched for in,
	// and whether any body sweeps at all.
	motion           float32
	anyCCD           bool
	contacts         []contact2
	arbiters         []arbiter2
	events           []pending2
	reported         pairSet
	rest             []float32
	joints           []jointSolver2
	items            []jointItem2
	distanceSolvers  []distanceSolver2
	revoluteSolvers  []revoluteSolver2
	prismaticSolvers []prismaticSolver2
	wheelSolvers     []wheelSolver2
	springSolvers    []springSolver2
	fixedSolvers     []fixedSolver2
}

// pending2 is a collision event waiting for the solver to say how hard
// the hit was.
type pending2 struct {
	ev  Collision2
	arb int
}

func stateOf2(w *ecs.World) *state2 {
	s := w.Resource[state2]()
	if s == nil {
		w.SetResource(state2{
			bodies:    w.Query2[gfx.Transform2, Body2](),
			colliders: w.Query2[gfx.Transform2, Collider2](),
			distance:  w.Query1[DistanceJoint2](),
			revolute:  w.Query1[RevoluteJoint2](),
			prismatic: w.Query1[PrismaticJoint2](),
			wheel:     w.Query1[WheelJoint2](),
			spring:    w.Query1[SpringJoint2](),
			fixed:     w.Query1[FixedJoint2](),
		})
		s = w.Resource[state2]()
	}
	return s
}

// System2 advances every 2D body by dt seconds.
func System2(w *ecs.World, dt float64) {
	if dt <= 0 {
		return
	}
	settings := w.Resource[Settings2]()
	if settings == nil {
		w.SetResource(Settings2{})
		settings = w.Resource[Settings2]()
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
	s.reported.reset()
	// As in 3D, only the step moves colliders between its substeps, so
	// the walk over every collider happens once an update.
	s.cols.refresh(s.colliders)
	for i := range substeps {
		s.step(w, settings, h, iterations, i == substeps-1, float32(dt))
	}
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform2, b *Body2) {
		b.force, b.torque = lin.Vec2{}, 0
	})
}

// active2 reports a body that can move others this step: an awake
// dynamic body or a kinematic one that is moving.
func active2(b *Body2) bool {
	if b == nil {
		return false
	}
	if b.Kinematic {
		return !b.Sleeping && (b.Vel != (lin.Vec2{}) || b.AngVel != 0)
	}
	return b.invMass > 0
}

func (s *state2) step(w *ecs.World, settings *Settings2, h float32, iterations int, last bool, dt float32) {
	// Integrate velocities, and link each body's collider row to it.
	x := &s.cols
	s.dynamic = s.dynamic[:0]
	s.all = s.all[:0]
	s.bodyRows = s.bodyRows[:0]
	s.index.reset()
	s.bodyAt.reset()
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform2, b *Body2) {
		b.fraction = 1
		s.bodyAt.set(e, len(s.all))
		s.all = append(s.all, bodyRec2{e: e, t: t, b: b})
		row, hasRow := x.row(e)
		var r *entry2
		if hasRow {
			r = &x.rows[row]
			r.b, r.bi = b, -1
			if r.shaped {
				s.bodyRows = append(s.bodyRows, int32(row))
			}
		}
		if b.Sleeping || b.Kinematic || b.Mass <= 0 {
			b.invMass, b.invInertia = 0, 0
			return
		}
		if r != nil {
			r.bi = len(s.dynamic)
		}
		s.index.set(e, len(s.dynamic))
		s.dynamic = append(s.dynamic, bodyRec2{e: e, t: t, b: b})
		if b.asleep {
			if b.Vel != (lin.Vec2{}) || b.AngVel != 0 || b.force != (lin.Vec2{}) || b.torque != 0 {
				b.Wake()
			} else {
				b.invMass, b.invInertia = 0, 0
				return
			}
		}
		b.invMass = 1 / b.Mass
		b.invInertia = 0
		if !b.LockRotation {
			if r != nil && r.c.Shape != nil {
				if i := r.c.Shape.inertia(b.Mass); i > 0 {
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
	// Place the bodies' colliders where the bodies now are; the still
	// colliders keep the placement the update's walk gave them.
	s.motion, s.anyCCD = 0, false
	for _, k := range s.bodyRows {
		x.replace(int(k))
		if b := x.rows[k].b; b.invMass > 0 {
			s.motion = max(s.motion, abs32(b.Vel.X)*h)
			s.anyCCD = s.anyCCD || b.CCD
		}
	}
	x.gather()
	s.moveSet = append(append(s.moveSet[:0], s.bodyRows...), x.triggers...)
	x.sortMoving(s.moveSet)
	s.parent = s.parent[:0]
	for i := range s.dynamic {
		s.parent = append(s.parent, i)
	}
	// Broadphase and contact generation.
	s.arbiters = s.arbiters[:0]
	s.events = s.events[:0]
	en := x.rows
	sweepPairs(x.keyX, x.endX, x.moving.order, x.statics.order, func(i, j int) {
		a, b := &en[i], &en[j]
		if a.lo.Y > b.hi.Y || b.lo.Y > a.hi.Y {
			return
		}
		aActive, bActive := active2(a.b), active2(b.b)
		if !aActive && !bActive && !a.c.Trigger && !b.c.Trigger {
			return
		}
		if !a.c.Layers.collides(b.c.Layers) {
			return
		}
		s.contacts = collide2(&s.ss, s.contacts[:0], a.c.Shape, a.pos, a.t.Rotation, b.c.Shape, b.pos, b.t.Rotation)
		contacts := s.contacts
		if len(contacts) == 0 {
			return
		}
		key := keyOf(a.e, b.e)
		if a.c.Trigger || b.c.Trigger {
			if !s.reported.add(key) {
				if a.c.Trigger {
					w.Emit(Trigger2{Trigger: a.e, Other: b.e})
				}
				if b.c.Trigger {
					w.Emit(Trigger2{Trigger: b.e, Other: a.e})
				}
			}
			return
		}
		if a.b != nil && a.b.asleep && bActive {
			a.b.Wake()
		}
		if b.b != nil && b.b.asleep && aActive {
			b.b.Wake()
		}
		if a.bi >= 0 && b.bi >= 0 {
			s.union(a.bi, b.bi)
		}
		if !s.reported.add(key) {
			c := contacts[0]
			ev := Collision2{A: a.e, B: b.e, Point: c.point, Normal: c.normal, Depth: c.depth}
			if key.a != a.e {
				ev.A, ev.B, ev.Normal = b.e, a.e, c.normal.Mul(-1)
			}
			s.events = append(s.events, pending2{ev, len(s.arbiters)})
		}
		initArbiter2(s.nextArbiter(), a, b, contacts, h)
	})
	// Joints.
	joints := gatherJoints2(w, s)
	for _, j := range joints {
		j.prepare(h)
		ja, jb := j.sides()
		if ia, ok := s.index.get(ja); ok {
			if ib, ok := s.index.get(jb); ok {
				s.union(ia, ib)
			}
		}
	}
	// Solve.
	for range iterations {
		for _, j := range joints {
			j.solve()
		}
		for i := range s.arbiters {
			s.arbiters[i].solve(true)
		}
	}
	for _, p := range s.events {
		for _, c := range s.arbiters[p.arb].contacts {
			p.ev.Impulse += c.pn
		}
		w.Emit(p.ev)
	}
	// Continuous collision: clamp fast bodies to their first static hit.
	// Each clamp only lowers a fraction, so the order the bodies are
	// swept in does not change the result.
	for _, k := range s.bodyRows {
		if !s.anyCCD {
			break
		}
		e := &en[k]
		if !e.b.CCD || e.b.invMass == 0 {
			continue
		}
		s.sweepDynamic(e, h)
		delta := e.b.Vel.Mul(h)
		length := delta.Len()
		if length < 1e-6 {
			continue
		}
		if f, ok := s.sweepStatic(e, delta); ok {
			e.b.fraction = min(e.b.fraction, min(1, f+0.5*slop/length))
		}
	}
	// Integrate positions.
	for _, r := range s.dynamic {
		b, t := r.b, r.t
		if b.invMass == 0 {
			continue
		}
		t.Position = t.Position.Add(b.Vel.Mul(h * b.fraction))
		if !b.LockRotation {
			t.Rotation += b.AngVel * h * b.fraction
		}
	}
	s.bodies.Each(func(e ecs.Entity, t *gfx.Transform2, b *Body2) {
		if b.Kinematic && !b.Sleeping {
			t.Position = t.Position.Add(b.Vel.Mul(h))
			t.Rotation += b.AngVel * h
		}
	})
	// Relax: the positions have taken the correction the bias asked for,
	// so take the speed it added back out. Without this a resting stack
	// keeps the separating speed the bias gave it and never rests below
	// the sleep threshold. Only the speed the update ends with is read,
	// by the sleep test and by the game, so the pass runs once, after the
	// last substep, and the sleep test with it.
	if !last {
		return
	}
	for range relaxIterations {
		for i := range s.arbiters {
			s.arbiters[i].solve(false)
		}
	}
	s.sleep(settings, dt)
}

func (s *state2) find(i int) int {
	for s.parent[i] != i {
		s.parent[i] = s.parent[s.parent[i]]
		i = s.parent[i]
	}
	return i
}

func (s *state2) union(a, b int) {
	ra, rb := s.find(a), s.find(b)
	if ra != rb {
		s.parent[rb] = ra
	}
}

// sleep puts islands that have rested long enough to sleep.
func (s *state2) sleep(settings *Settings2, h float32) {
	thr, after := settings.SleepThreshold, settings.SleepTime
	if after <= 0 {
		return
	}
	if thr <= 0 {
		thr = 0.05
	}
	rest := s.rest[:0]
	for i, r := range s.dynamic {
		b := r.b
		if !b.asleep {
			if b.Vel.Len() < thr && abs32(b.AngVel) < thr {
				b.sleepTime += h
			} else {
				b.sleepTime = 0
			}
		}
		root := s.find(i)
		for len(rest) <= root {
			rest = append(rest, float32(math.Inf(1)))
		}
		rest[root] = min(rest[root], b.sleepTime)
	}
	s.rest = rest
	for i, r := range s.dynamic {
		b := r.b
		if !b.asleep && rest[s.find(i)] >= after {
			b.asleep = true
			b.Vel, b.AngVel = lin.Vec2{}, 0
		}
	}
}

// sweepStatic sweeps a body's collider along delta against colliders
// that cannot move this step and returns the fraction at which it first
// touches one. Candidates come from the broadphase's sorted axis, so the
// cost is the colliders near the sweep rather than every collider.
func (s *state2) sweepStatic(e *entry2, delta lin.Vec2) (float32, bool) {
	slo, shi := e.lo.Min(e.lo.Add(delta)), e.hi.Max(e.hi.Add(delta))
	ext := e.hi.Sub(e.lo)
	minHalf := min(ext.X, ext.Y) / 2
	best, found := float32(1), false
	x := &s.cols
	s.cands = x.statics.overlapping(s.cands[:0], x.keyX, x.endX, slo.X, shi.X)
	s.cands = x.moving.overlapping(s.cands, x.keyX, x.endX, slo.X, shi.X)
	for _, ci := range s.cands {
		o := &x.rows[ci]
		if o == e || o.c.Trigger || active2(o.b) || !e.c.Layers.collides(o.c.Layers) {
			continue
		}
		if o.lo.Y > shi.Y || slo.Y > o.hi.Y {
			continue
		}
		oext := o.hi.Sub(o.lo)
		if t, _, ok := marchSweep2(&s.ss, e.c.Shape, e.pos, e.t.Rotation, delta, minHalf, o.c.Shape, o.pos, o.t.Rotation, min(oext.X, oext.Y)/2, best); ok && t < best {
			best, found = t, true
		}
	}
	return best, found
}

// sweepDynamic is the speculative contact between a CCD body and the
// other moving bodies: for each pair whose bounding circles meet along
// their relative motion this substep, the body's shape is swept against
// the other's and both are held at the moment they would touch, so the
// next substep's contact catches them. The search band is widened by the
// furthest anything travels this substep, so a partner moving toward the
// body is still found.
func (s *state2) sweepDynamic(e *entry2, h float32) {
	ca, ra := circleOfBounds2(e.lo, e.hi)
	ext := e.hi.Sub(e.lo)
	minHalf := min(ext.X, ext.Y) / 2
	slo := min(e.lo.X, e.lo.X+e.b.Vel.X*h) - s.motion
	shi := max(e.hi.X, e.hi.X+e.b.Vel.X*h) + s.motion
	x := &s.cols
	s.cands = x.moving.overlapping(s.cands[:0], x.keyX, x.endX, slo, shi)
	for _, ci := range s.cands {
		o := &x.rows[ci]
		if o == e || o.b == nil || o.b.invMass == 0 || o.c.Trigger || !e.c.Layers.collides(o.c.Layers) {
			continue
		}
		if o.b.CCD && o.e.ID() < e.e.ID() {
			continue // the pair was swept from the other side
		}
		cb, rb := circleOfBounds2(o.lo, o.hi)
		d := cb.Sub(ca)
		delta := e.b.Vel.Sub(o.b.Vel).Mul(h) // e's motion as o sees it
		length := delta.Len()
		if length < 1e-6 || !spheresMeet(d.Dot(d), -d.Dot(delta), length*length, ra+rb) {
			continue
		}
		oext := o.hi.Sub(o.lo)
		if t, _, ok := marchSweep2(&s.ss, e.c.Shape, e.pos, e.t.Rotation, delta, minHalf, o.c.Shape, o.pos, o.t.Rotation, min(oext.X, oext.Y)/2, 1); ok && t < 1 {
			f := min(1, t+0.5*slop/length)
			e.b.fraction = min(e.b.fraction, f)
			o.b.fraction = min(o.b.fraction, f)
		}
	}
}

// circleOfBounds2 is the circle around a bounding box.
func circleOfBounds2(lo, hi lin.Vec2) (lin.Vec2, float32) {
	return lo.Add(hi).Mul(0.5), hi.Sub(lo).Len() / 2
}

// arbiter2 holds a pair's contacts through the solver iterations. It
// keeps the two bodies directly, because the solver reads their velocity
// and inverse inertia twice per contact per iteration.
type arbiter2 struct {
	ba, bb   *Body2 // nil for a static collider
	contacts []solverContact2
	friction float32
}

// nextArbiter extends the arbiter list by one, keeping the contact
// buffer the reused element already holds.
func (s *state2) nextArbiter() *arbiter2 {
	n := len(s.arbiters)
	if n < cap(s.arbiters) {
		s.arbiters = s.arbiters[:n+1]
	} else {
		s.arbiters = append(s.arbiters, arbiter2{})
	}
	return &s.arbiters[n]
}

type solverContact2 struct {
	rA, rB      lin.Vec2
	normal      lin.Vec2
	tangent     lin.Vec2
	massNormal  float32
	massTangent float32
	// bias is the separating speed the position correction asks for and
	// restBias the speed restitution asks for. They are kept apart
	// because the relax pass drops the first and keeps the second.
	bias     float32
	restBias float32
	pn, pt   float32
}

// bodyVel2 reads a body's velocity and inverse mass, or zeros for a
// static collider.
func bodyVel2(b *Body2) (lin.Vec2, float32, float32, float32) {
	if b == nil {
		return lin.Vec2{}, 0, 0, 0
	}
	return b.Vel, b.AngVel, b.invMass, b.invInertia
}

// initArbiter2 prepares one pair's contacts in place, reusing whatever
// contact storage arb already holds.
func initArbiter2(arb *arbiter2, a, b *entry2, contacts []contact2, h float32) {
	arb.ba, arb.bb = a.b, b.b
	arb.contacts = arb.contacts[:0]
	var fa, fb, ra, rb float32 = 0.5, 0.5, 0, 0
	if a.b != nil {
		fa, ra = a.b.Friction, a.b.Restitution
	}
	if b.b != nil {
		fb, rb = b.b.Friction, b.b.Restitution
	}
	arb.friction = float32(math.Sqrt(float64(fa * fb)))
	restitution := max(ra, rb)
	va, wa, ima, iia := bodyVel2(a.b)
	vb, wb, imb, iib := bodyVel2(b.b)
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
			sc.restBias = -restitution * vn
		}
		arb.contacts = append(arb.contacts, sc)
	}
}

// solve applies one pass of normal and friction impulses. With useBias
// the normal impulse also drives the position correction; the relax pass
// after the positions have moved calls it without, which takes the speed
// that correction added back out.
func (arb *arbiter2) solve(useBias bool) {
	a, b := arb.ba, arb.bb
	_, _, ima, iia := bodyVel2(a)
	_, _, imb, iib := bodyVel2(b)
	for i := range arb.contacts {
		c := &arb.contacts[i]
		bias := c.restBias
		if useBias {
			bias += c.bias
		}
		va, wa, _, _ := bodyVel2(a)
		vb, wb, _, _ := bodyVel2(b)
		dv := vb.Add(crossSV(wb, c.rB)).Sub(va).Sub(crossSV(wa, c.rA))
		vn := dv.Dot(c.normal)
		dpn := c.massNormal * (-vn + bias)
		pn0 := c.pn
		c.pn = max(pn0+dpn, 0)
		dpn = c.pn - pn0
		p := c.normal.Mul(dpn)
		if a != nil {
			a.Vel = a.Vel.Sub(p.Mul(ima))
			a.AngVel -= iia * cross2(c.rA, p)
		}
		if b != nil {
			b.Vel = b.Vel.Add(p.Mul(imb))
			b.AngVel += iib * cross2(c.rB, p)
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
		if a != nil {
			a.Vel = a.Vel.Sub(p.Mul(ima))
			a.AngVel -= iia * cross2(c.rA, p)
		}
		if b != nil {
			b.Vel = b.Vel.Add(p.Mul(imb))
			b.AngVel += iib * cross2(c.rB, p)
		}
	}
}

// Hit2 is what a query found. Distance is the fraction along the ray or
// sweep for casts, the penetration depth for overlaps and the gap for
// Nearest2.
type Hit2 struct {
	Entity   ecs.Entity
	Point    lin.Vec2
	Normal   lin.Vec2
	Distance float32
}

// Raycast2 finds the nearest collider along the ray, ignoring triggers
// and colliders the mask excludes.
func Raycast2(w *ecs.World, r Ray2, mask uint32) (Hit2, bool) {
	return raycast2(w, r, mask, ecs.None)
}

func raycast2(w *ecs.World, r Ray2, mask uint32, exclude ecs.Entity) (Hit2, bool) {
	return queryState2(w).raycast(r, mask, exclude)
}

// raycast is raycast2 on an index already brought up to date, trying
// the colliders the tree finds in walk order.
func (st *state2) raycast(r Ray2, mask uint32, exclude ecs.Entity) (Hit2, bool) {
	best := Hit2{Distance: float32(math.Inf(1))}
	found := false
	x := &st.cols
	for _, k := range x.rayCandidates(r) {
		p := &x.rows[k]
		if p.c.Trigger || p.e == exclude || !(Layers{Mask: mask}).collides(p.c.Layers) {
			continue
		}
		if !raySlab2(r, p.lo, p.hi, min(best.Distance, 1)) {
			continue
		}
		if tt, n, ok := rayShape2(&st.qs, r, p.c.Shape, p.pos, p.t.Rotation); ok && tt < best.Distance {
			best = Hit2{Entity: p.e, Point: r.Origin.Add(r.Dir.Mul(tt)), Normal: n, Distance: tt}
			found = true
		}
	}
	return best, found
}
